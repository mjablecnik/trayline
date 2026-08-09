package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// errorResponse is the JSON body returned for every API error case
// (Requirement: consistent error schema across VALIDATION_ERROR, NOT_FOUND,
// CONFLICT).
type errorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// taskResponse is the JSON representation of a Task returned by the
// delete, update, retry, and stop endpoints.
type taskResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Command   string    `json:"command"`
	Status    string    `json:"status"`
	ExitCode  *int      `json:"exit_code,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func toTaskResponse(t *Task) taskResponse {
	return taskResponse{
		ID:        t.ID,
		Name:      t.Name,
		Command:   t.Command,
		Status:    string(t.Status),
		ExitCode:  t.ExitCode,
		CreatedAt: t.CreatedAt,
	}
}

// createTaskRequest is the JSON body accepted by POST /tasks.
type createTaskRequest struct {
	Command  string `json:"command"`
	Name     string `json:"name,omitempty"`
	Cwd      string `json:"cwd,omitempty"`
	Position *int   `json:"position,omitempty"`
}

// createTaskResponse is the JSON body returned by POST /tasks (Requirement 2.9).
type createTaskResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Command   string    `json:"command"`
	Status    string    `json:"status"`
	Position  int       `json:"position"`
	CreatedAt time.Time `json:"created_at"`
}

// taskListItem is one entry of the GET /tasks response array (Requirement 7.1).
type taskListItem struct {
	Position  int       `json:"position"`
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Command   string    `json:"command"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// updateTaskRequest is the JSON body accepted by PATCH /tasks/{identifier}.
type updateTaskRequest struct {
	Command string `json:"command,omitempty"`
	Name    string `json:"name,omitempty"`
}

// idNameResponse is the JSON body returned by POST /tasks/skip (Requirement 6.3).
type idNameResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// queueActionResponse is the JSON body returned by POST /queue/resume
// (Requirements 6.5, 6.7).
type queueActionResponse struct {
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
}

// taskBrief identifies the currently running task in a queue status
// response (Requirement 10.4).
type taskBrief struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Command string `json:"command"`
}

// failedInfo identifies the currently failed task in a queue status
// response (Requirement 10.3).
type failedInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Command  string `json:"command"`
	ExitCode int    `json:"exit_code"`
}

// queueStatusResponse is the JSON body returned by GET
// /projects/{project}/queue/status (Requirement 10.1).
type queueStatusResponse struct {
	State        string      `json:"state"`
	PendingCount int         `json:"pendingCount"`
	CurrentTask  *taskBrief  `json:"currentTask,omitempty"`
	FailedTask   *failedInfo `json:"failedTask,omitempty"`
}

// projectListItem is one entry of the GET /projects response array (FR-4.3).
type projectListItem struct {
	Name         string `json:"name"`
	State        string `json:"state"`
	PendingCount int    `json:"pendingCount"`
}

// Handler registers and serves the Taskline HTTP API on top of a Registry of
// per-project Queues and Workers (FR-4.1, FR-4.2).
type Handler struct {
	registry *Registry
}

// NewHandler returns a Handler ready to be registered on a ServeMux.
func NewHandler(registry *Registry) *Handler {
	return &Handler{registry: registry}
}

// Register adds every Taskline route to mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", h.handleHealth)
	mux.HandleFunc("GET /projects", h.handleListProjects)

	mux.HandleFunc("POST /projects/{project}/tasks", h.handleCreateTask)
	mux.HandleFunc("GET /projects/{project}/tasks", h.handleListTasks)
	mux.HandleFunc("DELETE /projects/{project}/tasks/{identifier}", h.handleDeleteTask)
	mux.HandleFunc("PATCH /projects/{project}/tasks/{identifier}", h.handleUpdateTask)
	mux.HandleFunc("POST /projects/{project}/tasks/retry", h.handleRetry)
	mux.HandleFunc("POST /projects/{project}/tasks/skip", h.handleSkip)
	mux.HandleFunc("POST /projects/{project}/tasks/stop", h.handleStop)
	mux.HandleFunc("POST /projects/{project}/queue/resume", h.handleResume)
	mux.HandleFunc("GET /projects/{project}/queue/status", h.handleQueueStatus)
	mux.HandleFunc("GET /projects/{project}/logs", h.handleGetLogs)
	mux.HandleFunc("GET /projects/{project}/logs/stream", h.handleStreamLogs)
}

// handleHealth responds to GET /health without touching the Registry so it
// always answers quickly regardless of any project's Queue state
// (Requirements 17.1, 17.2, FR-4.7).
func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleListProjects implements GET /projects (FR-4.3).
func (h *Handler) handleListProjects(w http.ResponseWriter, r *http.Request) {
	summaries := h.registry.List()
	items := make([]projectListItem, len(summaries))
	for i, s := range summaries {
		items[i] = projectListItem{Name: s.Name, State: string(s.State), PendingCount: s.PendingCount}
	}
	writeJSON(w, http.StatusOK, items)
}

// instance resolves the {project} path value to a ProjectInstance, creating
// it on demand (FR-1.4). On an invalid project name it writes a 400
// VALIDATION_ERROR response and returns ok=false.
func (h *Handler) instance(w http.ResponseWriter, r *http.Request) (*ProjectInstance, bool) {
	inst, err := h.registry.GetOrCreate(r.PathValue("project"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return nil, false
	}
	return inst, true
}

// handleCreateTask implements POST /projects/{project}/tasks (Requirement 2).
func (h *Handler) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	inst, ok := h.instance(w, r)
	if !ok {
		return
	}

	var req createTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "request body is not valid JSON")
		return
	}
	if req.Position != nil && *req.Position < 0 {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", ErrInvalidPosition.Error())
		return
	}

	task, err := inst.Queue.AddTask(req.Command, req.Name, req.Cwd, req.Position)
	if err != nil {
		writeQueueError(w, err)
		return
	}

	h.persist(inst)
	inst.Worker.Notify()

	writeJSON(w, http.StatusCreated, createTaskResponse{
		ID:        task.ID,
		Name:      task.Name,
		Command:   task.Command,
		Status:    string(task.Status),
		Position:  h.taskPosition(inst, task.ID),
		CreatedAt: task.CreatedAt,
	})
}

// handleListTasks implements GET /projects/{project}/tasks (Requirement 7).
func (h *Handler) handleListTasks(w http.ResponseWriter, r *http.Request) {
	inst, ok := h.instance(w, r)
	if !ok {
		return
	}

	tasks := inst.Queue.List()
	items := make([]taskListItem, len(tasks))
	for i, t := range tasks {
		items[i] = taskListItem{
			Position:  i,
			ID:        t.ID,
			Name:      t.Name,
			Command:   t.Command,
			Status:    string(t.Status),
			CreatedAt: t.CreatedAt,
		}
	}
	writeJSON(w, http.StatusOK, items)
}

// handleDeleteTask implements DELETE /projects/{project}/tasks/{identifier}
// (Requirement 8).
func (h *Handler) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	inst, ok := h.instance(w, r)
	if !ok {
		return
	}

	task, err := inst.Queue.DeleteTask(r.PathValue("identifier"))
	if err != nil {
		writeQueueError(w, err)
		return
	}
	h.persist(inst)
	writeJSON(w, http.StatusOK, toTaskResponse(task))
}

// handleUpdateTask implements PATCH /projects/{project}/tasks/{identifier}
// (Requirement 9).
func (h *Handler) handleUpdateTask(w http.ResponseWriter, r *http.Request) {
	inst, ok := h.instance(w, r)
	if !ok {
		return
	}

	var req updateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "request body is not valid JSON")
		return
	}

	task, err := inst.Queue.UpdateTask(r.PathValue("identifier"), req.Command, req.Name)
	if err != nil {
		writeQueueError(w, err)
		return
	}
	h.persist(inst)
	writeJSON(w, http.StatusOK, toTaskResponse(task))
}

// handleRetry implements POST /projects/{project}/tasks/retry (Requirement
// 6.1, 6.2, FR-4.9).
func (h *Handler) handleRetry(w http.ResponseWriter, r *http.Request) {
	inst, ok := h.instance(w, r)
	if !ok {
		return
	}

	task, err := inst.Queue.Retry()
	if err != nil {
		writeQueueError(w, err)
		return
	}
	h.persist(inst)
	inst.Worker.Notify()
	writeJSON(w, http.StatusOK, toTaskResponse(task))
}

// handleSkip implements POST /projects/{project}/tasks/skip (Requirement
// 6.3, 6.4, FR-4.10).
func (h *Handler) handleSkip(w http.ResponseWriter, r *http.Request) {
	inst, ok := h.instance(w, r)
	if !ok {
		return
	}

	task, err := inst.Queue.Skip()
	if err != nil {
		writeQueueError(w, err)
		return
	}
	h.persist(inst)
	inst.Worker.Notify()
	writeJSON(w, http.StatusOK, idNameResponse{ID: task.ID, Name: task.Name})
}

// handleStop implements POST /projects/{project}/tasks/stop (Requirement 5,
// FR-4.8). Worker.Stop blocks until the running command process has fully
// terminated and the Task has transitioned to "failed" (Requirement 5.1),
// including state persistence and failure notification handled inside
// Worker.finishTask.
func (h *Handler) handleStop(w http.ResponseWriter, r *http.Request) {
	inst, ok := h.instance(w, r)
	if !ok {
		return
	}

	task, err := inst.Worker.Stop()
	if err != nil {
		writeQueueError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toTaskResponse(task))
}

// handleResume implements POST /projects/{project}/queue/resume
// (Requirement 6.5-6.8).
func (h *Handler) handleResume(w http.ResponseWriter, r *http.Request) {
	inst, ok := h.instance(w, r)
	if !ok {
		return
	}

	empty, err := inst.Queue.Resume()
	if err != nil {
		writeQueueError(w, err)
		return
	}
	h.persist(inst)

	if empty {
		writeJSON(w, http.StatusOK, queueActionResponse{State: string(QueueIdle), Message: "queue is empty"})
		return
	}
	inst.Worker.Notify()
	writeJSON(w, http.StatusOK, queueActionResponse{State: string(QueueRunning)})
}

// handleQueueStatus implements GET /projects/{project}/queue/status
// (Requirement 10).
func (h *Handler) handleQueueStatus(w http.ResponseWriter, r *http.Request) {
	inst, ok := h.instance(w, r)
	if !ok {
		return
	}

	state := inst.Queue.CurrentState()
	resp := queueStatusResponse{State: string(state), PendingCount: inst.Queue.PendingCount()}

	switch state {
	case QueueRunning:
		if t := inst.Queue.CurrentTask(); t != nil {
			resp.CurrentTask = &taskBrief{ID: t.ID, Name: t.Name, Command: t.Command}
		}
	case QueueHalted:
		if t := inst.Queue.FailedTaskInfo(); t != nil {
			exitCode := 0
			if t.ExitCode != nil {
				exitCode = *t.ExitCode
			}
			resp.FailedTask = &failedInfo{ID: t.ID, Name: t.Name, Command: t.Command, ExitCode: exitCode}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleGetLogs implements GET /projects/{project}/logs?tail=N (FR-4.4). It
// reads the project's log file directly from disk, since inst.LogWriter's
// file handle is write-only (append mode).
func (h *Handler) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	inst, ok := h.instance(w, r)
	if !ok {
		return
	}

	data, err := os.ReadFile(h.registry.LogPath(inst.Name))
	if err != nil && !os.IsNotExist(err) {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to read log file")
		return
	}
	content := string(data)

	if tailParam := r.URL.Query().Get("tail"); tailParam != "" {
		n, err := strconv.Atoi(tailParam)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "tail must be a non-negative integer")
			return
		}
		content = tailLines(content, n)
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(content))
}

// tailLines returns the last n lines of content, preserving a trailing
// newline when any lines are returned. n <= 0 returns an empty string.
func tailLines(content string, n int) string {
	if n <= 0 {
		return ""
	}
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// handleStreamLogs implements GET /projects/{project}/logs/stream (FR-4.5),
// streaming newly written log lines to the client via Server-Sent Events
// until the request's context is cancelled (NFR-4.2).
func (h *Handler) handleStreamLogs(w http.ResponseWriter, r *http.Request) {
	inst, ok := h.instance(w, r)
	if !ok {
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "streaming not supported")
		return
	}

	ch := inst.LogWriter.Subscribe()
	defer inst.LogWriter.Unsubscribe(ch)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for {
		select {
		case line, open := <-ch:
			if !open {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", strings.TrimRight(string(line), "\n"))
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// taskPosition returns id's index in inst.Queue's display order (as
// returned by List), or -1 if it is no longer present.
func (h *Handler) taskPosition(inst *ProjectInstance, id string) int {
	for i, t := range inst.Queue.List() {
		if t.ID == id {
			return i
		}
	}
	return -1
}

// persist saves inst.Queue to inst.StateFile, logging (but never returning)
// any error so a write failure never blocks an API response (Requirement
// 11.5).
func (h *Handler) persist(inst *ProjectInstance) {
	if inst.StateFile == "" {
		return
	}
	if err := SaveState(inst.Queue, inst.StateFile); err != nil {
		logError("project %s: failed to persist state: %v", inst.Name, err)
	}
}

// writeQueueError maps a Queue/Worker error to the appropriate HTTP status
// and error code (Requirement: Error Handling strategy in design.md).
func writeQueueError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrTaskNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
	case errors.Is(err, ErrNameTaken),
		errors.Is(err, ErrTaskRunning),
		errors.Is(err, ErrTaskFailedImmutable),
		errors.Is(err, ErrNoFailedTask),
		errors.Is(err, ErrQueueAlreadyRunning),
		errors.Is(err, ErrQueueHalted),
		errors.Is(err, ErrNoRunningTask):
		writeError(w, http.StatusConflict, "CONFLICT", err.Error())
	default:
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Error: code, Message: message})
}

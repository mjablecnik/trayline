package main

import (
	"encoding/json"
	"errors"
	"net/http"
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

// queueStatusResponse is the JSON body returned by GET /queue/status
// (Requirement 10.1).
type queueStatusResponse struct {
	State        string      `json:"state"`
	PendingCount int         `json:"pendingCount"`
	CurrentTask  *taskBrief  `json:"currentTask,omitempty"`
	FailedTask   *failedInfo `json:"failedTask,omitempty"`
}

// Handler registers and serves the Taskline HTTP API on top of a Queue and
// its Worker (Requirement: server/handler.go route registration).
type Handler struct {
	queue     *Queue
	worker    *Worker
	stateFile string
}

// NewHandler returns a Handler ready to be registered on a ServeMux.
// stateFile is where the Queue is persisted after every handler-initiated
// mutation (Requirement 11.2); persistence is skipped if stateFile is empty.
func NewHandler(queue *Queue, worker *Worker, stateFile string) *Handler {
	return &Handler{queue: queue, worker: worker, stateFile: stateFile}
}

// Register adds every Taskline route to mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", h.handleHealth)
	mux.HandleFunc("POST /tasks", h.handleCreateTask)
	mux.HandleFunc("GET /tasks", h.handleListTasks)
	mux.HandleFunc("DELETE /tasks/{identifier}", h.handleDeleteTask)
	mux.HandleFunc("PATCH /tasks/{identifier}", h.handleUpdateTask)
	mux.HandleFunc("POST /tasks/retry", h.handleRetry)
	mux.HandleFunc("POST /tasks/skip", h.handleSkip)
	mux.HandleFunc("POST /tasks/stop", h.handleStop)
	mux.HandleFunc("POST /queue/resume", h.handleResume)
	mux.HandleFunc("GET /queue/status", h.handleQueueStatus)
}

// handleHealth responds to GET /health without touching the Queue so it
// always answers quickly regardless of Queue state (Requirements 17.1, 17.2).
func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleCreateTask implements POST /tasks (Requirement 2).
func (h *Handler) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var req createTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "request body is not valid JSON")
		return
	}
	if req.Position != nil && *req.Position < 0 {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", ErrInvalidPosition.Error())
		return
	}

	task, err := h.queue.AddTask(req.Command, req.Name, req.Cwd, req.Position)
	if err != nil {
		writeQueueError(w, err)
		return
	}

	h.persist()
	h.worker.Notify()

	writeJSON(w, http.StatusCreated, createTaskResponse{
		ID:        task.ID,
		Name:      task.Name,
		Command:   task.Command,
		Status:    string(task.Status),
		Position:  h.taskPosition(task.ID),
		CreatedAt: task.CreatedAt,
	})
}

// handleListTasks implements GET /tasks (Requirement 7).
func (h *Handler) handleListTasks(w http.ResponseWriter, r *http.Request) {
	tasks := h.queue.List()
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

// handleDeleteTask implements DELETE /tasks/{identifier} (Requirement 8).
func (h *Handler) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	task, err := h.queue.DeleteTask(r.PathValue("identifier"))
	if err != nil {
		writeQueueError(w, err)
		return
	}
	h.persist()
	writeJSON(w, http.StatusOK, toTaskResponse(task))
}

// handleUpdateTask implements PATCH /tasks/{identifier} (Requirement 9).
func (h *Handler) handleUpdateTask(w http.ResponseWriter, r *http.Request) {
	var req updateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "request body is not valid JSON")
		return
	}

	task, err := h.queue.UpdateTask(r.PathValue("identifier"), req.Command, req.Name)
	if err != nil {
		writeQueueError(w, err)
		return
	}
	h.persist()
	writeJSON(w, http.StatusOK, toTaskResponse(task))
}

// handleRetry implements POST /tasks/retry (Requirement 6.1, 6.2).
func (h *Handler) handleRetry(w http.ResponseWriter, r *http.Request) {
	task, err := h.queue.Retry()
	if err != nil {
		writeQueueError(w, err)
		return
	}
	h.persist()
	h.worker.Notify()
	writeJSON(w, http.StatusOK, toTaskResponse(task))
}

// handleSkip implements POST /tasks/skip (Requirement 6.3, 6.4).
func (h *Handler) handleSkip(w http.ResponseWriter, r *http.Request) {
	task, err := h.queue.Skip()
	if err != nil {
		writeQueueError(w, err)
		return
	}
	h.persist()
	h.worker.Notify()
	writeJSON(w, http.StatusOK, idNameResponse{ID: task.ID, Name: task.Name})
}

// handleStop implements POST /tasks/stop (Requirement 5). Worker.Stop blocks
// until the running command process has fully terminated and the Task has
// transitioned to "failed" (Requirement 5.1), including state persistence
// and failure notification handled inside Worker.finishTask.
func (h *Handler) handleStop(w http.ResponseWriter, r *http.Request) {
	task, err := h.worker.Stop()
	if err != nil {
		writeQueueError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toTaskResponse(task))
}

// handleResume implements POST /queue/resume (Requirement 6.5–6.8).
func (h *Handler) handleResume(w http.ResponseWriter, r *http.Request) {
	empty, err := h.queue.Resume()
	if err != nil {
		writeQueueError(w, err)
		return
	}
	h.persist()

	if empty {
		writeJSON(w, http.StatusOK, queueActionResponse{State: string(QueueIdle), Message: "queue is empty"})
		return
	}
	h.worker.Notify()
	writeJSON(w, http.StatusOK, queueActionResponse{State: string(QueueRunning)})
}

// handleQueueStatus implements GET /queue/status (Requirement 10).
func (h *Handler) handleQueueStatus(w http.ResponseWriter, r *http.Request) {
	state := h.queue.CurrentState()
	resp := queueStatusResponse{State: string(state), PendingCount: h.queue.PendingCount()}

	switch state {
	case QueueRunning:
		if t := h.queue.CurrentTask(); t != nil {
			resp.CurrentTask = &taskBrief{ID: t.ID, Name: t.Name, Command: t.Command}
		}
	case QueueHalted:
		if t := h.queue.FailedTaskInfo(); t != nil {
			exitCode := 0
			if t.ExitCode != nil {
				exitCode = *t.ExitCode
			}
			resp.FailedTask = &failedInfo{ID: t.ID, Name: t.Name, Command: t.Command, ExitCode: exitCode}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// taskPosition returns id's index in the Queue's display order (as returned
// by List), or -1 if it is no longer present.
func (h *Handler) taskPosition(id string) int {
	for i, t := range h.queue.List() {
		if t.ID == id {
			return i
		}
	}
	return -1
}

// persist saves the Queue to h.stateFile, logging (but never returning) any
// error so a write failure never blocks an API response (Requirement 11.5).
func (h *Handler) persist() {
	if h.stateFile == "" {
		return
	}
	if err := SaveState(h.queue, h.stateFile); err != nil {
		logError("failed to persist state: %v", err)
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

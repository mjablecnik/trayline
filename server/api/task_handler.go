package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"server/core"
	"server/docker"
	"server/store"
)

const (
	maxPromptLen    = 32000
	longPollTimeout = 30 * time.Second
)

// StateSaver is the minimal interface TaskHandler needs for state persistence.
type StateSaver interface {
	Save() error
}

// TaskHandler handles one-shot task REST endpoints.
type TaskHandler struct {
	store    *store.TaskStore
	cm       *docker.ContainerManager
	logger   *core.Logger
	stateMgr StateSaver
}

// NewTaskHandler creates a TaskHandler.
func NewTaskHandler(store *store.TaskStore, cm *docker.ContainerManager, logger *core.Logger, stateMgr StateSaver) *TaskHandler {
	return &TaskHandler{store: store, cm: cm, logger: logger, stateMgr: stateMgr}
}

// saveState persists server state to disk, logging any error.
func (h *TaskHandler) saveState(ctx context.Context) {
	if h.stateMgr == nil {
		return
	}
	if err := h.stateMgr.Save(); err != nil && h.logger != nil {
		h.logger.Error(ctx, "state save error: "+err.Error())
	}
}

// HandlePostRun handles POST /run.
func (h *TaskHandler) HandlePostRun(w http.ResponseWriter, r *http.Request) {
	var req RunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, core.ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: "request body is not valid JSON: " + err.Error(),
		})
		return
	}

	if req.Prompt == "" {
		writeJSON(w, http.StatusBadRequest, core.ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: "prompt is required and must not be empty",
		})
		return
	}
	if len(req.Prompt) > maxPromptLen {
		writeJSON(w, http.StatusBadRequest, core.ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: fmt.Sprintf("prompt exceeds maximum length of %d characters", maxPromptLen),
		})
		return
	}
	if req.Agent != "kiro" && req.Agent != "claude" {
		writeJSON(w, http.StatusBadRequest, core.ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: `agent must be "kiro" or "claude"`,
		})
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	task := &store.Task{
		ID:           uuid.NewString(),
		Status:       store.TaskQueued,
		Agent:        req.Agent,
		Prompt:       req.Prompt,
		Model:        req.Model,
		System:       req.System,
		OutputFormat: req.OutputFormat,
		CreatedAt:    time.Now(),
		CancelFunc:   cancel,
		Done:         make(chan struct{}),
	}
	h.store.Add(task)
	h.saveState(r.Context())

	go h.executeTask(ctx, task)

	select {
	case <-task.Done:
		t := h.store.Get(task.ID)
		writeJSON(w, http.StatusOK, taskToRunResponse(t))
	case <-time.After(longPollTimeout):
		t := h.store.Get(task.ID)
		writeJSON(w, http.StatusAccepted, RunAcceptedResponse{ID: t.ID, Status: t.Status})
	case <-r.Context().Done():
	}
}

// executeTask runs a one-shot task in a goroutine.
func (h *TaskHandler) executeTask(ctx context.Context, task *store.Task) {
	defer close(task.Done)

	var proceed bool
	h.store.Update(task.ID, func(t *store.Task) {
		if t.Status == store.TaskCancelled {
			proceed = false
			return
		}
		t.Status = store.TaskRunning
		proceed = true
	})
	if !proceed {
		return
	}
	h.logger.Info(context.Background(), fmt.Sprintf("task %s status: running", task.ID))
	h.saveState(context.Background())

	effectivePrompt := task.Prompt
	if task.OutputFormat != "" {
		switch task.OutputFormat {
		case "json":
			effectivePrompt += "\n\nRespond with valid JSON only."
		case "text":
			effectivePrompt += "\n\nRespond with plain text."
		case "markdown":
			effectivePrompt += "\n\nRespond with markdown."
		}
	}

	result, err := h.cm.RunOneShot(ctx, task.Agent, effectivePrompt, task.Model, task.System, task.CreatedAt)

	now := time.Now()
	var finalStatus store.TaskStatus
	h.store.Update(task.ID, func(t *store.Task) {
		if store.IsTerminal(t.Status) {
			finalStatus = t.Status
			return
		}
		t.CompletedAt = &now
		switch {
		case err != nil && ctx.Err() == context.Canceled:
			t.Status = store.TaskCancelled
		case err != nil:
			t.Status = store.TaskFailed
			t.Error = err.Error()
		case result.ExitCode != 0:
			t.Status = store.TaskFailed
			t.Error = result.Stderr
			if t.Error == "" {
				t.Error = result.Stdout
			}
			if t.Error == "" {
				t.Error = fmt.Sprintf("container exited with non-zero code %d", result.ExitCode)
			}
		default:
			t.Status = store.TaskCompleted
			t.Result = result.Stdout
			if task.Agent == "kiro" {
				t.Result = docker.StripANSI(result.Stdout)
			}
			if t.OutputFormat != "" {
				valid := validateOutputFormat(t.OutputFormat, result.Stdout)
				t.Valid = &valid
			}
		}
		finalStatus = t.Status
	})
	h.logger.Info(context.Background(), fmt.Sprintf("task %s status: %s", task.ID, finalStatus))
	h.saveState(context.Background())
}

// validateOutputFormat returns true if output satisfies the requested format.
func validateOutputFormat(format, output string) bool {
	switch format {
	case "json":
		var v interface{}
		return json.Unmarshal([]byte(output), &v) == nil
	case "text", "markdown":
		return true
	default:
		return false
	}
}

// taskToRunResponse builds a RunResponse from a Task.
func taskToRunResponse(t *store.Task) RunResponse {
	resp := RunResponse{
		ID:          t.ID,
		Status:      t.Status,
		Agent:       t.Agent,
		CreatedAt:   t.CreatedAt,
		CompletedAt: t.CompletedAt,
	}
	if t.Status == store.TaskCompleted {
		resp.Result = t.Result
		resp.Valid = t.Valid
	}
	if t.Status == store.TaskFailed {
		resp.Error = t.Error
	}
	return resp
}

// HandleGetRun handles GET /run/{id}.
func (h *TaskHandler) HandleGetRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t := h.store.Get(id)
	if t == nil {
		writeJSON(w, http.StatusNotFound, core.ErrorResponse{
			Error:   "NOT_FOUND",
			Message: fmt.Sprintf("task %q not found", id),
		})
		return
	}
	writeJSON(w, http.StatusOK, taskToRunResponse(t))
}

// HandleGetRuns handles GET /runs.
func (h *TaskHandler) HandleGetRuns(w http.ResponseWriter, r *http.Request) {
	tasks := h.store.List()
	summaries := make([]TaskSummary, len(tasks))
	for i, t := range tasks {
		summaries[i] = TaskSummary{
			ID:        t.ID,
			Status:    t.Status,
			Agent:     t.Agent,
			CreatedAt: t.CreatedAt,
		}
	}
	writeJSON(w, http.StatusOK, summaries)
}

// HandleCancelRun handles POST /run/{id}/cancel.
func (h *TaskHandler) HandleCancelRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	t := h.store.Get(id)
	if t == nil {
		writeJSON(w, http.StatusNotFound, core.ErrorResponse{
			Error:   "NOT_FOUND",
			Message: fmt.Sprintf("task %q not found", id),
		})
		return
	}

	var cancelFn context.CancelFunc
	var conflict bool
	h.store.Update(id, func(t *store.Task) {
		if store.IsTerminal(t.Status) {
			conflict = true
			return
		}
		now := time.Now()
		t.Status = store.TaskCancelled
		t.CompletedAt = &now
		cancelFn = t.CancelFunc
	})

	if conflict {
		writeJSON(w, http.StatusConflict, core.ErrorResponse{
			Error:   "CONFLICT",
			Message: "task is already in a terminal status and cannot be cancelled",
		})
		return
	}

	if cancelFn != nil {
		cancelFn()
	}

	h.saveState(r.Context())

	writeJSON(w, http.StatusOK, map[string]string{
		"id":     id,
		"status": string(store.TaskCancelled),
	})
}

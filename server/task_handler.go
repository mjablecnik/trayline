package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
)

const (
	maxPromptLen    = 32000
	longPollTimeout = 30 * time.Second
)

// TaskHandler handles one-shot task REST endpoints.
type TaskHandler struct {
	store  *TaskStore
	cm     *ContainerManager
	logger *Logger
}

// NewTaskHandler creates a TaskHandler.
func NewTaskHandler(store *TaskStore, cm *ContainerManager, logger *Logger) *TaskHandler {
	return &TaskHandler{store: store, cm: cm, logger: logger}
}

// HandlePostRun handles POST /run: validates the request, creates a queued task,
// launches it in a goroutine, and long-polls for up to 30 seconds.
func (h *TaskHandler) HandlePostRun(w http.ResponseWriter, r *http.Request) {
	var req RunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: "request body is not valid JSON: " + err.Error(),
		})
		return
	}

	if req.Prompt == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: "prompt is required and must not be empty",
		})
		return
	}
	if len(req.Prompt) > maxPromptLen {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: fmt.Sprintf("prompt exceeds maximum length of %d characters", maxPromptLen),
		})
		return
	}
	if req.Agent != "kiro" && req.Agent != "claude" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: `agent must be "kiro" or "claude"`,
		})
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	task := &Task{
		ID:           uuid.NewString(),
		Status:       TaskQueued,
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

	go h.executeTask(ctx, task)

	select {
	case <-task.Done:
		t := h.store.Get(task.ID)
		writeJSON(w, http.StatusOK, taskToRunResponse(t))
	case <-time.After(longPollTimeout):
		t := h.store.Get(task.ID)
		writeJSON(w, http.StatusAccepted, RunAcceptedResponse{ID: t.ID, Status: t.Status})
	case <-r.Context().Done():
		// Client disconnected; task continues in the background.
	}
}

// executeTask runs a one-shot task in a goroutine, updating task status as it progresses.
func (h *TaskHandler) executeTask(ctx context.Context, task *Task) {
	defer close(task.Done)

	// Transition to "running" unless the cancel handler already set "cancelled".
	var proceed bool
	h.store.Update(task.ID, func(t *Task) {
		if t.Status == TaskCancelled {
			proceed = false
			return
		}
		t.Status = TaskRunning
		proceed = true
	})
	if !proceed {
		return
	}

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
	h.store.Update(task.ID, func(t *Task) {
		if isTerminalStatus(t.Status) {
			return // Already set by cancel handler.
		}
		t.CompletedAt = &now
		switch {
		case err != nil && ctx.Err() == context.Canceled:
			t.Status = TaskCancelled
		case err != nil:
			t.Status = TaskFailed
			t.Error = err.Error()
		case result.ExitCode != 0:
			t.Status = TaskFailed
			t.Error = result.Stderr
			if t.Error == "" {
				t.Error = fmt.Sprintf("container exited with non-zero code %d", result.ExitCode)
			}
		default:
			t.Status = TaskCompleted
			t.Result = result.Stdout
			if t.OutputFormat != "" {
				valid := validateOutputFormat(t.OutputFormat, result.Stdout)
				t.Valid = &valid
			}
		}
	})
}

// isTerminalStatus reports whether a task status is in a terminal (non-progressing) state.
func isTerminalStatus(s TaskStatus) bool {
	return s == TaskCompleted || s == TaskFailed || s == TaskCancelled
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

// taskToRunResponse builds a RunResponse from a Task, including only the fields
// that should be present for that task's current status.
func taskToRunResponse(t *Task) RunResponse {
	resp := RunResponse{
		ID:          t.ID,
		Status:      t.Status,
		Agent:       t.Agent,
		CreatedAt:   t.CreatedAt,
		CompletedAt: t.CompletedAt,
	}
	if t.Status == TaskCompleted {
		resp.Result = t.Result
		resp.Valid = t.Valid
	}
	if t.Status == TaskFailed {
		resp.Error = t.Error
	}
	return resp
}

// HandleGetRun handles GET /run/{id}.
func (h *TaskHandler) HandleGetRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t := h.store.Get(id)
	if t == nil {
		writeJSON(w, http.StatusNotFound, ErrorResponse{
			Error:   "NOT_FOUND",
			Message: fmt.Sprintf("task %q not found", id),
		})
		return
	}
	writeJSON(w, http.StatusOK, taskToRunResponse(t))
}

// HandleGetRuns handles GET /runs: returns all tasks ordered by created_at descending.
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
		writeJSON(w, http.StatusNotFound, ErrorResponse{
			Error:   "NOT_FOUND",
			Message: fmt.Sprintf("task %q not found", id),
		})
		return
	}

	var cancelFn context.CancelFunc
	var conflict bool
	h.store.Update(id, func(t *Task) {
		if isTerminalStatus(t.Status) {
			conflict = true
			return
		}
		now := time.Now()
		t.Status = TaskCancelled
		t.CompletedAt = &now
		cancelFn = t.CancelFunc
	})

	if conflict {
		writeJSON(w, http.StatusConflict, ErrorResponse{
			Error:   "CONFLICT",
			Message: "task is already in a terminal status and cannot be cancelled",
		})
		return
	}

	if cancelFn != nil {
		cancelFn()
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"id":     id,
		"status": string(TaskCancelled),
	})
}

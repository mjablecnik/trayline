package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"remote/core"
	"remote/docker"
	"remote/store"
)

const (
	longPollTimeout = 30 * time.Second
)

// StateSaver is the minimal interface TaskHandler needs for state persistence.
type StateSaver interface {
	Save() error
}

// ContainerRunner is the minimal interface TaskHandler needs to execute agent containers.
type ContainerRunner interface {
	RunOneShot(ctx context.Context, agent, prompt, model, system string, createdAt time.Time, onStart func(containerID string)) (*docker.ContainerResult, error)
	RunOneShotStreaming(ctx context.Context, agent, prompt, model, system string, createdAt time.Time) (*docker.OneShotStream, error)
	StopAndRemoveContainer(ctx context.Context, containerID string) error
}

// TaskHandler handles one-shot task REST endpoints.
type TaskHandler struct {
	store          *store.TaskStore
	cm             ContainerRunner
	logger         *core.Logger
	stateMgr       StateSaver
	workspaceDir   string
	maxUploadSize  int64
	maxUploadFiles int
	maxPromptLen   int
}

// NewTaskHandler creates a TaskHandler.
func NewTaskHandler(store *store.TaskStore, cm ContainerRunner, logger *core.Logger, stateMgr StateSaver, workspaceDir string, maxUploadSize int64, maxUploadFiles int, maxPromptLen int) *TaskHandler {
	return &TaskHandler{store: store, cm: cm, logger: logger, stateMgr: stateMgr, workspaceDir: workspaceDir, maxUploadSize: maxUploadSize, maxUploadFiles: maxUploadFiles, maxPromptLen: maxPromptLen}
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
	var fileHeaders []*multipart.FileHeader

	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "multipart/form-data") {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			writeJSON(w, http.StatusBadRequest, core.ErrorResponse{
				Error:   "VALIDATION_ERROR",
				Message: "failed to parse multipart form: " + err.Error(),
			})
			return
		}
		req.Prompt = r.FormValue("prompt")
		req.Agent = r.FormValue("agent")
		req.Model = r.FormValue("model")
		req.System = r.FormValue("system")
		req.OutputFormat = r.FormValue("output_format")
		if r.MultipartForm != nil {
			fileHeaders = r.MultipartForm.File["files"]
		}
	} else {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, core.ErrorResponse{
				Error:   "VALIDATION_ERROR",
				Message: "request body is not valid JSON: " + err.Error(),
			})
			return
		}
	}

	if req.Prompt == "" {
		writeJSON(w, http.StatusBadRequest, core.ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: "prompt is required and must not be empty",
		})
		return
	}
	if len(req.Prompt) > h.maxPromptLen {
		writeJSON(w, http.StatusBadRequest, core.ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: fmt.Sprintf("prompt exceeds maximum length of %d characters", h.maxPromptLen),
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

	taskID := uuid.NewString()
	basePrompt := req.Prompt

	if len(fileHeaders) > 0 && h.workspaceDir != "" {
		uploaded, err := SaveUploadedFiles(fileHeaders, h.workspaceDir, taskID, h.maxUploadSize, h.maxUploadFiles)
		if err != nil {
			var valErr *UploadValidationError
			if errors.As(err, &valErr) {
				writeJSON(w, http.StatusBadRequest, core.ErrorResponse{
					Error:   "VALIDATION_ERROR",
					Message: err.Error(),
				})
			} else {
				h.logger.Error(r.Context(), "upload error: "+err.Error())
				writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
					Error:   "INTERNAL_ERROR",
					Message: "failed to store uploaded files",
				})
			}
			return
		}
		if len(uploaded) > 0 {
			basePrompt = BuildUploadMetadata(uploaded) + req.Prompt
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	task := &store.Task{
		ID:           taskID,
		Status:       store.TaskQueued,
		Agent:        req.Agent,
		Prompt:       basePrompt,
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
	if h.workspaceDir != "" {
		defer func() {
			if err := CleanupUploadDir(h.workspaceDir, task.ID); err != nil {
				h.logger.Warn(context.Background(), fmt.Sprintf("failed to clean up upload dir for task %s: %v", task.ID, err))
			}
		}()
	}
	// Recover from a panic anywhere in this goroutine (including inside container
	// execution) so a single bad task can't crash the whole server and strand its
	// container: this runs before the deferred close(task.Done) above, so waiters
	// see the failed status instead of a stale "running" one.
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		h.logger.Error(context.Background(), fmt.Sprintf("task %s panicked: %v", task.ID, r))
		now := time.Now()
		var containerID string
		h.store.Update(task.ID, func(t *store.Task) {
			containerID = t.ContainerID
			if !store.IsTerminal(t.Status) {
				t.Status = store.TaskFailed
				t.Error = fmt.Sprintf("internal error: %v", r)
				t.CompletedAt = &now
			}
		})
		if containerID != "" {
			if err := h.cm.StopAndRemoveContainer(context.Background(), containerID); err != nil {
				h.logger.Warn(context.Background(), fmt.Sprintf("task %s: failed to clean up container %s after panic: %v", task.ID, containerID, err))
			}
		}
		h.saveState(context.Background())
	}()

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

	result, err := h.cm.RunOneShot(ctx, task.Agent, effectivePrompt, task.Model, task.System, task.CreatedAt, func(containerID string) {
		h.store.Update(task.ID, func(t *store.Task) {
			t.ContainerID = containerID
		})
		h.saveState(context.Background())
	})

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

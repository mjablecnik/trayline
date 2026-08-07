package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"remote/core"
	"remote/store"
)

// WorkflowHandler handles workflow scheduling, listing, detail, edit, and
// cancel REST endpoints.
type WorkflowHandler struct {
	store    *store.WorkflowStore
	config   *core.Config
	logger   *core.Logger
	stateMgr StateSaver
	queues   *WorkflowQueueManager
}

// NewWorkflowHandler creates a WorkflowHandler.
func NewWorkflowHandler(workflowStore *store.WorkflowStore, config *core.Config, logger *core.Logger, stateMgr StateSaver, queues *WorkflowQueueManager) *WorkflowHandler {
	return &WorkflowHandler{
		store:    workflowStore,
		config:   config,
		logger:   logger,
		stateMgr: stateMgr,
		queues:   queues,
	}
}

// projectExists reports whether name is a safe project name that resolves to
// an existing directory under config.ProjectsDir. Workflow scheduling does
// not require the project to be a git repository, only for the directory to
// exist — same rule as PipelineHandler.projectExists/SpecHandler.projectExists.
func (h *WorkflowHandler) projectExists(name string) bool {
	if name == "" || name == "." || name == ".." || !projectNameRe.MatchString(name) {
		return false
	}
	info, err := os.Stat(filepath.Join(h.config.ProjectsDir, name))
	return err == nil && info.IsDir()
}

// pipelineFileExists reports whether pipelineType/name refers to an existing
// pipeline YAML file under config.PipelinesDir.
func (h *WorkflowHandler) pipelineFileExists(pipelineType, name string) bool {
	if name == "" || name == "." || name == ".." || !projectNameRe.MatchString(name) {
		return false
	}
	info, err := os.Stat(filepath.Join(h.config.PipelinesDir, pipelineType, name+".yaml"))
	return err == nil && !info.IsDir()
}

// writeWorkflowNotFound writes the standard 404 body for an unresolvable workflow ID.
func writeWorkflowNotFound(w http.ResponseWriter, id string) {
	writeJSON(w, http.StatusNotFound, core.ErrorResponse{
		Error:   "NOT_FOUND",
		Message: fmt.Sprintf("workflow %q not found", id),
	})
}

// persist saves workflow state to disk, logging any error.
func (h *WorkflowHandler) persist(ctx context.Context) {
	if h.stateMgr == nil {
		return
	}
	if err := h.stateMgr.Save(); err != nil && h.logger != nil {
		h.logger.Error(ctx, "workflow state save error: "+err.Error())
	}
}

// validatePipelineAndVariables validates a pipeline reference (format
// "type/name", referencing an existing pipeline file) and a variables map,
// returning an HTTP error message if either is invalid, or "" if both are valid.
func (h *WorkflowHandler) validatePipelineAndVariables(pipelineRef string, variables map[string]string) string {
	pipelineType, pipelineName, ok := parsePipelineRef(pipelineRef)
	if !ok {
		return `pipeline must be in the form "type/name" where type is one of tasks, processes, workflows`
	}
	if !h.pipelineFileExists(pipelineType, pipelineName) {
		return fmt.Sprintf("pipeline %q not found", pipelineRef)
	}
	if !isValidVariablesMap(variables) {
		return "variables must contain at most 50 entries, with keys matching ^[a-zA-Z0-9_-]{1,100}$ and values up to 1000 characters"
	}
	return ""
}

// HandleSchedule handles POST /projects/{name}/workflows.
func (h *WorkflowHandler) HandleSchedule(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !h.projectExists(name) {
		writeProjectNotFound(w, name)
		return
	}

	var req ScheduleWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, core.ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: "invalid JSON body",
		})
		return
	}
	if req.Variables == nil {
		req.Variables = map[string]string{}
	}

	if req.Pipeline == "" {
		writeJSON(w, http.StatusBadRequest, core.ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: "pipeline is required",
		})
		return
	}
	if msg := h.validatePipelineAndVariables(req.Pipeline, req.Variables); msg != "" {
		writeJSON(w, http.StatusBadRequest, core.ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: msg,
		})
		return
	}

	wf := &store.Workflow{
		ID:        uuid.NewString(),
		Project:   name,
		Pipeline:  req.Pipeline,
		Variables: req.Variables,
		Status:    store.WorkflowQueued,
		CreatedAt: time.Now(),
	}
	h.store.Add(wf)
	h.persist(r.Context())

	// Snapshot before Enqueue: once enqueued, the project's processor
	// goroutine may immediately pick this workflow up and start mutating its
	// fields concurrently, so this workflow's *store.Workflow pointer must
	// not be dereferenced directly after this point (see .agents/MEMORY.md).
	snapshot, _ := h.store.Snapshot(wf.ID)

	h.queues.Enqueue(name)

	writeJSON(w, http.StatusCreated, workflowToResponse(snapshot, true))
}

// HandleList handles GET /projects/{name}/workflows.
func (h *WorkflowHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !h.projectExists(name) {
		writeProjectNotFound(w, name)
		return
	}

	workflows := h.store.ListByProjectSnapshot(name)
	resp := make([]WorkflowResponse, len(workflows))
	for i, wf := range workflows {
		resp[i] = workflowToResponse(wf, false)
	}
	writeJSON(w, http.StatusOK, resp)
}

// HandleListAll handles GET /workflows — returns all active (queued/running)
// workflows across all projects.
func (h *WorkflowHandler) HandleListAll(w http.ResponseWriter, r *http.Request) {
	workflows := h.store.ListActiveSnapshot()
	resp := make([]GlobalWorkflowResponse, len(workflows))
	for i, wf := range workflows {
		resp[i] = GlobalWorkflowResponse{
			WorkflowResponse: workflowToResponse(wf, false),
			Project:          wf.Project,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// HandleDetail handles GET /projects/{name}/workflows/{id}.
func (h *WorkflowHandler) HandleDetail(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !h.projectExists(name) {
		writeProjectNotFound(w, name)
		return
	}

	id := r.PathValue("id")
	wf, ok := h.store.Snapshot(id)
	if !ok || wf.Project != name {
		writeWorkflowNotFound(w, id)
		return
	}

	writeJSON(w, http.StatusOK, workflowToResponse(wf, true))
}

// HandleEdit handles PUT /projects/{name}/workflows/{id}.
func (h *WorkflowHandler) HandleEdit(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !h.projectExists(name) {
		writeProjectNotFound(w, name)
		return
	}

	id := r.PathValue("id")
	existing, ok := h.store.Snapshot(id)
	if !ok || existing.Project != name {
		writeWorkflowNotFound(w, id)
		return
	}

	var req EditWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, core.ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: "invalid JSON body",
		})
		return
	}
	if req.Variables == nil {
		req.Variables = map[string]string{}
	}

	pipeline := existing.Pipeline
	if req.Pipeline != "" {
		pipeline = req.Pipeline
	}
	if msg := h.validatePipelineAndVariables(pipeline, req.Variables); msg != "" {
		writeJSON(w, http.StatusBadRequest, core.ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: msg,
		})
		return
	}

	var conflict bool
	h.store.Update(id, func(wf *store.Workflow) {
		if wf.Status != store.WorkflowQueued {
			conflict = true
			return
		}
		wf.Pipeline = pipeline
		wf.Variables = req.Variables
	})
	if conflict {
		writeJSON(w, http.StatusConflict, core.ErrorResponse{
			Error:   "CONFLICT",
			Message: "only queued workflows can be edited",
		})
		return
	}

	h.persist(r.Context())

	updated, _ := h.store.Snapshot(id)
	writeJSON(w, http.StatusOK, workflowToResponse(updated, true))
}

// HandleCancel handles DELETE /projects/{name}/workflows/{id}.
func (h *WorkflowHandler) HandleCancel(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !h.projectExists(name) {
		writeProjectNotFound(w, name)
		return
	}

	id := r.PathValue("id")
	existing, ok := h.store.Snapshot(id)
	if !ok || existing.Project != name {
		writeWorkflowNotFound(w, id)
		return
	}

	var (
		conflict   bool
		wasQueued  bool
		wasRunning bool
	)
	now := time.Now()
	h.store.Update(id, func(wf *store.Workflow) {
		switch wf.Status {
		case store.WorkflowQueued:
			wf.Status = store.WorkflowCancelled
			wf.CompletedAt = &now
			wasQueued = true
		case store.WorkflowRunning:
			wasRunning = true
		default:
			conflict = true
		}
	})

	if conflict {
		writeJSON(w, http.StatusConflict, core.ErrorResponse{
			Error:   "CONFLICT",
			Message: "workflow is already in a terminal status",
		})
		return
	}

	if wasQueued {
		h.store.Evict(name)
		h.persist(r.Context())
	}

	if wasRunning {
		// CancelRunning sends SIGTERM and returns without waiting for the
		// container to actually stop; the queue processor finalizes the
		// workflow's status to "cancelled" once the container's output
		// stream closes (Requirement 6.6). A transient race is possible: the
		// workflow could complete naturally between the Update above and this
		// call, in which case CancelRunning reports it's no longer running.
		if err := h.queues.CancelRunning(id); err != nil {
			writeJSON(w, http.StatusConflict, core.ErrorResponse{
				Error:   "CONFLICT",
				Message: "workflow is no longer running",
			})
			return
		}
	}

	updated, _ := h.store.Snapshot(id)
	writeJSON(w, http.StatusOK, workflowToResponse(updated, true))
}

// wsLogPollInterval bounds how long the log-streaming loop waits between
// checks of a workflow's status while waiting for it to start running or to
// reach a terminal status; output itself is delivered immediately via the
// subscriber channel, not on this poll.
const wsLogPollInterval = 100 * time.Millisecond

// wsLogSubBuffer is the channel capacity for a live log-stream subscriber.
// workflowOutputWriter.Write sends to it non-blocking (see workflow_queue.go),
// so a full channel means this subscriber silently drops chunks rather than
// stalling the workflow's output pipe.
const wsLogSubBuffer = 64

// workflowLogMessage is the JSON shape of every message sent on the
// GET /projects/{name}/workflows/{id}/logs WebSocket (Requirement 7.2-7.5).
type workflowLogMessage struct {
	Type      string `json:"type"`
	Data      string `json:"data,omitempty"`
	Status    string `json:"status,omitempty"`
	ExitCode  *int   `json:"exit_code,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

// HandleLogs handles GET /projects/{name}/workflows/{id}/logs, a WebSocket
// endpoint that streams a workflow's stdout/stderr output live while it runs,
// or replays the buffered log immediately if it has already finished.
func (h *WorkflowHandler) HandleLogs(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !h.projectExists(name) {
		writeProjectNotFound(w, name)
		return
	}

	id := r.PathValue("id")
	wf, ok := h.store.Snapshot(id)
	if !ok || wf.Project != name {
		writeWorkflowNotFound(w, id)
		return
	}

	// CLI clients set a real Authorization header on the upgrade request, so
	// it can be validated (and rejected with a true HTTP 401) before
	// upgrading (Requirement 7.8). Browser clients cannot set custom
	// WebSocket headers and instead authenticate via the post-upgrade
	// handshake in wsAuth below — same convention as HandleChat.
	authHeader := r.Header.Get("Authorization")
	if bearerTokenInvalid(authHeader, h.config.APIToken) {
		writeJSON(w, http.StatusUnauthorized, core.ErrorResponse{
			Error:   "UNAUTHORIZED",
			Message: "invalid token",
		})
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		if h.logger != nil {
			h.logger.Error(r.Context(), "websocket upgrade failed: "+err.Error())
		}
		return
	}

	if authHeader == "" && !h.wsAuth(conn) {
		return
	}

	h.streamLogs(conn, id)
}

// bearerTokenInvalid reports whether header is a present-but-invalid "Bearer
// <token>" Authorization header. An absent header is not invalid here — it
// defers to the post-upgrade auth handshake instead.
func bearerTokenInvalid(header, expected string) bool {
	if header == "" {
		return false
	}
	token, ok := strings.CutPrefix(header, "Bearer ")
	return !ok || !ValidateWSToken(token, expected)
}

// wsAuth reads the first WebSocket message and validates it as an auth
// message ({"type": "auth", "token": "..."}), for clients that didn't send an
// Authorization header on the upgrade request. Closes the connection and
// returns false on any failure.
func (h *WorkflowHandler) wsAuth(conn *websocket.Conn) bool {
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, data, err := conn.ReadMessage()
	conn.SetReadDeadline(time.Time{})
	if err != nil {
		conn.Close()
		return false
	}

	var msg struct {
		Type  string `json:"type"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &msg); err != nil || msg.Type != "auth" || msg.Token == "" || !ValidateWSToken(msg.Token, h.config.APIToken) {
		conn.Close()
		return false
	}
	return true
}

// streamLogs drives a workflow's log WebSocket to completion: it waits out a
// queued workflow, streams a running one, or immediately replays a terminal
// one, then closes the connection.
func (h *WorkflowHandler) streamLogs(conn *websocket.Conn, id string) {
	defer conn.Close()

	disconnected := make(chan struct{})
	go h.drainClient(conn, disconnected)

	wf, ok := h.store.Snapshot(id)
	if !ok {
		return
	}

	if wf.Status == store.WorkflowQueued {
		if !h.writeLogMsg(conn, workflowLogMessage{Type: "waiting"}) {
			return
		}
		wf, ok = h.waitForStart(id, disconnected)
		if !ok {
			return
		}
	}

	if wf.Status == store.WorkflowRunning {
		h.streamRunning(conn, id, disconnected)
		return
	}

	h.sendStoredLogAndFinish(conn, wf)
}

// drainClient reads (and discards) client frames until the connection errors
// or closes, then closes disconnected exactly once. The log stream is
// server-to-client only, but a read pump is required to detect the client
// going away and to service control frames (ping/pong, close).
func (h *WorkflowHandler) drainClient(conn *websocket.Conn, disconnected chan struct{}) {
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			close(disconnected)
			return
		}
	}
}

// waitForStart polls until id leaves "queued" status (starts running, or is
// cancelled before it starts) or the client disconnects.
func (h *WorkflowHandler) waitForStart(id string, disconnected <-chan struct{}) (store.Workflow, bool) {
	ticker := time.NewTicker(wsLogPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-disconnected:
			return store.Workflow{}, false
		case <-ticker.C:
			wf, ok := h.store.Snapshot(id)
			if !ok {
				return store.Workflow{}, false
			}
			if wf.Status != store.WorkflowQueued {
				return wf, true
			}
		}
	}
}

// streamRunning subscribes to id's live output and forwards it to conn until
// the workflow reaches a terminal status (then sends a "finished" message) or
// the client disconnects.
func (h *WorkflowHandler) streamRunning(conn *websocket.Conn, id string, disconnected <-chan struct{}) {
	sub := make(chan string, wsLogSubBuffer)
	var buffered string
	h.store.Update(id, func(wf *store.Workflow) {
		if wf.LogBuffer != nil {
			buffered = wf.LogBuffer.String()
		}
		wf.LogSubs = append(wf.LogSubs, sub)
	})
	defer h.unsubscribe(id, sub)

	if buffered != "" && !h.writeLogMsg(conn, workflowLogMessage{Type: "output", Data: buffered}) {
		return
	}

	ticker := time.NewTicker(wsLogPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-disconnected:
			return
		case chunk := <-sub:
			if !h.writeLogMsg(conn, workflowLogMessage{Type: "output", Data: chunk}) {
				return
			}
		case <-ticker.C:
			wf, ok := h.store.Snapshot(id)
			if !ok {
				return
			}
			if store.IsWorkflowTerminal(wf.Status) {
				h.drainRemaining(conn, sub)
				h.sendFinished(conn, wf)
				return
			}
		}
	}
}

// drainRemaining flushes any output chunks already queued in sub, without
// blocking, so the final "finished" message isn't sent ahead of output that
// arrived just before the workflow's terminal status was observed.
func (h *WorkflowHandler) drainRemaining(conn *websocket.Conn, sub chan string) {
	for {
		select {
		case chunk := <-sub:
			if !h.writeLogMsg(conn, workflowLogMessage{Type: "output", Data: chunk}) {
				return
			}
		default:
			return
		}
	}
}

// unsubscribe removes sub from id's live subscriber list.
func (h *WorkflowHandler) unsubscribe(id string, sub chan string) {
	h.store.Update(id, func(wf *store.Workflow) {
		for i, s := range wf.LogSubs {
			if s == sub {
				wf.LogSubs = append(wf.LogSubs[:i], wf.LogSubs[i+1:]...)
				return
			}
		}
	})
}

// sendStoredLogAndFinish replays a terminal workflow's captured log as a
// single output message, then sends "finished" (Requirement 7.4).
func (h *WorkflowHandler) sendStoredLogAndFinish(conn *websocket.Conn, wf store.Workflow) {
	if wf.LogBuffer != nil {
		if log := wf.LogBuffer.String(); log != "" {
			if !h.writeLogMsg(conn, workflowLogMessage{Type: "output", Data: log}) {
				return
			}
		}
	}
	h.sendFinished(conn, wf)
}

// sendFinished sends the terminal "finished" message for wf.
func (h *WorkflowHandler) sendFinished(conn *websocket.Conn, wf store.Workflow) {
	var truncated bool
	if wf.LogBuffer != nil {
		truncated = wf.LogBuffer.Wrapped()
	}
	h.writeLogMsg(conn, workflowLogMessage{
		Type:      "finished",
		Status:    string(wf.Status),
		ExitCode:  wf.ExitCode,
		Truncated: truncated,
	})
}

// writeLogMsg marshals and writes msg to conn, returning false if either step fails.
func (h *WorkflowHandler) writeLogMsg(conn *websocket.Conn, msg workflowLogMessage) bool {
	data, err := json.Marshal(msg)
	if err != nil {
		return false
	}
	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return conn.WriteMessage(websocket.TextMessage, data) == nil
}

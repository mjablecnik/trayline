package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"remote/core"
	"remote/docker"
	"remote/store"
)

// validProjectName matches only safe project directory names.
var validProjectName = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// ProjectAgentHandler handles project-scoped AI agent endpoints.
type ProjectAgentHandler struct {
	store    *store.SessionStore
	cm       *docker.ContainerManager
	logger   *core.Logger
	config   *core.Config
	stateMgr StateSaver
}

// NewProjectAgentHandler creates a ProjectAgentHandler.
func NewProjectAgentHandler(
	store *store.SessionStore,
	cm *docker.ContainerManager,
	logger *core.Logger,
	config *core.Config,
	stateMgr StateSaver,
) *ProjectAgentHandler {
	return &ProjectAgentHandler{
		store:    store,
		cm:       cm,
		logger:   logger,
		config:   config,
		stateMgr: stateMgr,
	}
}

// validateProjectName checks the project name is safe. Returns an error
// response if invalid, or nil if valid.
func (h *ProjectAgentHandler) validateProjectName(name string) *core.ErrorResponse {
	if name == "" || !validProjectName.MatchString(name) || strings.Contains(name, "..") {
		return &core.ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: "project name contains invalid characters",
		}
	}
	return nil
}

// projectExists checks that PROJECTS_DIR/{name} exists and contains .git/.
func (h *ProjectAgentHandler) projectExists(name string) bool {
	projectPath := filepath.Join(h.config.ProjectsDir, name)
	info, err := os.Stat(filepath.Join(projectPath, ".git"))
	return err == nil && info.IsDir()
}

// validateAgent checks that the agent query parameter is one of the
// supported agent types. Returns an error response if invalid, or nil if valid.
func (h *ProjectAgentHandler) validateAgent(agent string) *core.ErrorResponse {
	if agent != "kiro" && agent != "claude" {
		return &core.ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: `agent query parameter must be "kiro" or "claude"`,
		}
	}
	return nil
}

// isKnownMessageType reports whether t is a recognized WSClientMessage type.
func isKnownMessageType(t string) bool {
	return t == "message" || t == "interrupt" || t == "terminate"
}

// saveState persists server state to disk, logging any error.
func (h *ProjectAgentHandler) saveState(ctx context.Context) {
	if h.stateMgr == nil {
		return
	}
	if err := h.stateMgr.Save(); err != nil && h.logger != nil {
		h.logger.Error(ctx, "state save error: "+err.Error())
	}
}

// writeWS sends a WSServerMessage to a connection.
func (h *ProjectAgentHandler) writeWS(conn *websocket.Conn, msg WSServerMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	conn.WriteMessage(websocket.TextMessage, data)
}

// writeWSToSession safely sends a message to the session's current connection, if any.
func (h *ProjectAgentHandler) writeWSToSession(sessionID string, msg WSServerMessage) {
	sess := h.store.Get(sessionID)
	if sess == nil {
		return
	}
	sess.ConnMu.Lock()
	defer sess.ConnMu.Unlock()
	if sess.Conn != nil {
		h.writeWS(sess.Conn, msg)
	}
}

// HandleProjectChat handles WS /projects/{name}/chat.
func (h *ProjectAgentHandler) HandleProjectChat(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := h.validateProjectName(name); err != nil {
		writeJSON(w, http.StatusBadRequest, err)
		return
	}
	if !h.projectExists(name) {
		writeJSON(w, http.StatusNotFound, core.ErrorResponse{
			Error:   "NOT_FOUND",
			Message: fmt.Sprintf("project %q not found", name),
		})
		return
	}

	agent := r.URL.Query().Get("agent")
	if err := h.validateAgent(agent); err != nil {
		writeJSON(w, http.StatusBadRequest, err)
		return
	}
	model := r.URL.Query().Get("model")
	system := r.URL.Query().Get("system")

	if !h.cm.TryAcquireSlot() {
		writeJSON(w, http.StatusServiceUnavailable, core.ErrorResponse{
			Error:   "SERVICE_UNAVAILABLE",
			Message: "server is at capacity, try again later",
		})
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.cm.ReleaseChatSlot()
		h.logger.Error(r.Context(), "websocket upgrade failed: "+err.Error())
		return
	}

	now := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	sessionID := uuid.NewString()

	sess := &store.Session{
		ID:            sessionID,
		Agent:         agent,
		Model:         model,
		System:        system,
		Project:       name,
		CreatedAt:     now,
		LastMessageAt: now,
		Conn:          conn,
		Active:        true,
		Ctx:           ctx,
		CancelFunc:    cancel,
	}
	h.store.Add(sess)
	h.logger.Info(r.Context(), fmt.Sprintf("project session %s created (project: %s, agent: %s)", sessionID, name, agent))
	h.saveState(r.Context())

	containerID, err := h.cm.StartProjectChatContainer(ctx, agent, model, system, name)
	if err != nil {
		h.writeWS(conn, WSServerMessage{Type: "error", Message: "failed to create agent container: " + err.Error()})
		conn.Close()
		cancel()
		h.cm.ReleaseChatSlot()
		h.store.Remove(sessionID)
		return
	}

	attached, err := h.cm.AttachChatContainer(ctx, containerID)
	if err != nil {
		h.writeWS(conn, WSServerMessage{Type: "error", Message: "failed to attach to container: " + err.Error()})
		conn.Close()
		cancel()
		h.cm.StopAndRemoveContainer(context.Background(), containerID)
		h.cm.ReleaseChatSlot()
		h.store.Remove(sessionID)
		return
	}

	if err := h.cm.StartContainer(ctx, containerID); err != nil {
		h.writeWS(conn, WSServerMessage{Type: "error", Message: "failed to start container: " + err.Error()})
		attached.Close()
		conn.Close()
		cancel()
		h.cm.StopAndRemoveContainer(context.Background(), containerID)
		h.cm.ReleaseChatSlot()
		h.store.Remove(sessionID)
		return
	}

	h.store.Update(sessionID, func(s *store.Session) {
		s.ContainerID = containerID
		s.Stdin = attached.Conn
	})

	h.writeWS(conn, WSServerMessage{Type: "session_started", SessionID: sessionID})

	go h.streamOutput(ctx, sessionID, attached)
	go h.readClient(ctx, sessionID, conn)

	go func() {
		<-ctx.Done()
		attached.Close()
		h.cm.StopAndRemoveContainer(context.Background(), containerID)
		h.cm.ReleaseChatSlot()
		h.store.Remove(sessionID)
		h.saveState(context.Background())
	}()
}

// HandleProjectChatReconnect handles WS /projects/{name}/chat/{id}.
func (h *ProjectAgentHandler) HandleProjectChatReconnect(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := h.validateProjectName(name); err != nil {
		writeJSON(w, http.StatusBadRequest, err)
		return
	}

	id := r.PathValue("id")
	sess := h.store.Get(id)
	if sess == nil || !sess.Active || sess.Project != name {
		writeJSON(w, http.StatusNotFound, core.ErrorResponse{
			Error:   "NOT_FOUND",
			Message: fmt.Sprintf("session %q not found or is no longer active", id),
		})
		return
	}

	sess.ConnMu.Lock()
	if sess.Conn != nil {
		sess.ConnMu.Unlock()
		writeJSON(w, http.StatusConflict, core.ErrorResponse{
			Error:   "CONFLICT",
			Message: "session already has an active connection",
		})
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		sess.ConnMu.Unlock()
		h.logger.Error(r.Context(), "websocket upgrade failed: "+err.Error())
		return
	}
	sess.Conn = conn
	sess.ConnMu.Unlock()

	ctx := sess.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	h.store.Update(id, func(s *store.Session) {
		s.LastMessageAt = time.Now()
	})

	h.writeWS(conn, WSServerMessage{Type: "session_resumed", SessionID: id, Agent: sess.Agent, Model: sess.Model})
	go h.readClient(ctx, id, conn)
}

// HandleProjectSessions handles GET /projects/{name}/sessions.
func (h *ProjectAgentHandler) HandleProjectSessions(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := h.validateProjectName(name); err != nil {
		writeJSON(w, http.StatusBadRequest, err)
		return
	}
	if !h.projectExists(name) {
		writeJSON(w, http.StatusNotFound, core.ErrorResponse{
			Error:   "NOT_FOUND",
			Message: fmt.Sprintf("project %q not found", name),
		})
		return
	}

	sessions := h.store.ListByProject(name)
	result := make([]projectSessionSummary, len(sessions))
	for i, s := range sessions {
		result[i] = projectSessionSummary{
			SessionID:     s.ID,
			Agent:         s.Agent,
			Model:         s.Model,
			CreatedAt:     s.CreatedAt,
			LastMessageAt: s.LastMessageAt,
		}
	}
	writeJSON(w, http.StatusOK, result)
}

// HandleTerminateProjectSession handles POST /projects/{name}/sessions/{id}/terminate.
func (h *ProjectAgentHandler) HandleTerminateProjectSession(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := h.validateProjectName(name); err != nil {
		writeJSON(w, http.StatusBadRequest, err)
		return
	}

	id := r.PathValue("id")
	sess := h.store.Get(id)
	if sess == nil || sess.Project != name {
		writeJSON(w, http.StatusNotFound, core.ErrorResponse{
			Error:   "NOT_FOUND",
			Message: fmt.Sprintf("session %q not found", id),
		})
		return
	}

	h.logger.Info(r.Context(), fmt.Sprintf("project session %s terminated: user-initiated", id))

	sess.ConnMu.Lock()
	if sess.Conn != nil {
		h.writeWS(sess.Conn, WSServerMessage{Type: "terminated"})
		sess.Conn.Close()
		sess.Conn = nil
	}
	sess.ConnMu.Unlock()

	if sess.CancelFunc != nil {
		sess.CancelFunc()
	}

	h.saveState(r.Context())

	writeJSON(w, http.StatusOK, map[string]string{
		"session_id": id,
		"status":     "terminated",
	})
}

// streamOutput reads from the attached container and sends output/done to the WebSocket.
// Unlike SessionHandler.streamOutput, it also refreshes sess.LastMessageAt on each chunk
// of agent output so the idle timeout resets on agent activity as well as client activity.
func (h *ProjectAgentHandler) streamOutput(ctx context.Context, sessionID string, attached dockertypes.HijackedResponse) {
	sess := h.store.Get(sessionID)
	if sess == nil {
		return
	}

	if sess.Agent == "claude" {
		pr, pw := io.Pipe()
		go func() {
			defer pw.Close()
			stdcopy.StdCopy(pw, pw, attached.Reader)
		}()
		h.streamOutputClaude(ctx, sessionID, pr)
	} else {
		h.streamOutputPlainText(ctx, sessionID, attached.Reader)
	}
}

// touchLastMessageAt refreshes the session's idle timer.
func (h *ProjectAgentHandler) touchLastMessageAt(sessionID string) {
	h.store.Update(sessionID, func(s *store.Session) {
		s.LastMessageAt = time.Now()
	})
}

// streamOutputClaude handles NDJSON protocol output from claude CLI (stream-json mode).
func (h *ProjectAgentHandler) streamOutputClaude(ctx context.Context, sessionID string, reader interface{ Read([]byte) (int, error) }) {
	lineCh := make(chan string, 32)
	go func() {
		defer close(lineCh)
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		for scanner.Scan() {
			lineCh <- scanner.Text()
		}
	}()

	sess := h.store.Get(sessionID)
	if sess != nil && sess.Stdin != nil {
		initMsg := map[string]interface{}{
			"type":       "control_request",
			"request_id": "init_" + sessionID[:8],
			"request": map[string]interface{}{
				"subtype": "initialize",
				"hooks":   nil,
				"agents":  nil,
			},
		}
		data, _ := json.Marshal(initMsg)
		fmt.Fprintf(sess.Stdin, "%s\n", data)
	}

	initialized := false
	for {
		select {
		case line, ok := <-lineCh:
			if !ok {
				h.writeWSToSession(sessionID, WSServerMessage{Type: "done"})
				return
			}
			h.touchLastMessageAt(sessionID)

			var msg map[string]interface{}
			if err := json.Unmarshal([]byte(line), &msg); err != nil {
				h.writeWSToSession(sessionID, WSServerMessage{Type: "output", Data: line + "\n"})
				continue
			}

			msgType, _ := msg["type"].(string)

			switch msgType {
			case "control_response":
				if !initialized {
					initialized = true
					h.logger.Info(ctx, fmt.Sprintf("project session %s: claude CLI initialized", sessionID))
				}

			case "system":
				continue

			case "assistant":
				message, _ := msg["message"].(map[string]interface{})
				if message == nil {
					continue
				}
				content, _ := message["content"].([]interface{})
				for _, block := range content {
					b, _ := block.(map[string]interface{})
					if b == nil {
						continue
					}
					blockType, _ := b["type"].(string)
					switch blockType {
					case "text":
						text, _ := b["text"].(string)
						if text != "" {
							h.writeWSToSession(sessionID, WSServerMessage{Type: "output", Data: text})
						}
					}
				}

			case "result":
				subtype, _ := msg["subtype"].(string)
				if subtype == "compact_boundary" {
					h.writeWSToSession(sessionID, WSServerMessage{Type: "context_compacted"})
				}
				h.writeWSToSession(sessionID, WSServerMessage{Type: "done"})

			case "stream_event":
				event, _ := msg["event"].(map[string]interface{})
				if event == nil {
					continue
				}
				eventType, _ := event["type"].(string)
				if eventType == "content_block_delta" {
					delta, _ := event["delta"].(map[string]interface{})
					if delta != nil {
						deltaType, _ := delta["type"].(string)
						if deltaType == "text_delta" {
							text, _ := delta["text"].(string)
							if text != "" {
								h.writeWSToSession(sessionID, WSServerMessage{Type: "output", Data: text})
							}
						}
					}
				}
			}

		case <-ctx.Done():
			return
		}
	}
}

// streamOutputPlainText handles plain text output from kiro CLI.
func (h *ProjectAgentHandler) streamOutputPlainText(ctx context.Context, sessionID string, reader interface{ Read([]byte) (int, error) }) {
	lineCh := make(chan string, 32)
	go func() {
		defer close(lineCh)
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			lineCh <- scanner.Text()
		}
	}()

	pendingDone := false
	for {
		if pendingDone {
			select {
			case line, ok := <-lineCh:
				if !ok {
					h.writeWSToSession(sessionID, WSServerMessage{Type: "done"})
					return
				}
				h.touchLastMessageAt(sessionID)
				if isContextCompaction(line) {
					h.writeWSToSession(sessionID, WSServerMessage{Type: "context_compacted"})
				}
				h.writeWSToSession(sessionID, WSServerMessage{Type: "output", Data: line + "\n"})
			case <-time.After(idleTurnTimeout):
				h.writeWSToSession(sessionID, WSServerMessage{Type: "done"})
				pendingDone = false
			case <-ctx.Done():
				return
			}
		} else {
			select {
			case line, ok := <-lineCh:
				if !ok {
					h.writeWSToSession(sessionID, WSServerMessage{Type: "done"})
					return
				}
				h.touchLastMessageAt(sessionID)
				if isContextCompaction(line) {
					h.writeWSToSession(sessionID, WSServerMessage{Type: "context_compacted"})
				}
				h.writeWSToSession(sessionID, WSServerMessage{Type: "output", Data: line + "\n"})
				pendingDone = true
			case <-ctx.Done():
				return
			}
		}
	}
}

// readClient reads WebSocket messages from the client and processes them.
func (h *ProjectAgentHandler) readClient(ctx context.Context, sessionID string, conn *websocket.Conn) {
	defer func() {
		h.store.Update(sessionID, func(s *store.Session) {
			s.ConnMu.Lock()
			if s.Conn == conn {
				s.Conn = nil
			}
			s.ConnMu.Unlock()
		})
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var msg WSClientMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			h.writeWS(conn, WSServerMessage{Type: "error", Message: "invalid message format"})
			continue
		}

		h.touchLastMessageAt(sessionID)

		sess := h.store.Get(sessionID)
		if sess == nil {
			return
		}

		if !isKnownMessageType(msg.Type) {
			h.writeWS(conn, WSServerMessage{Type: "error", Message: "unknown message type"})
			continue
		}

		switch msg.Type {
		case "message":
			if sess.Stdin != nil && msg.Prompt != "" {
				prompt := msg.Prompt
				if sess.Agent == "claude" {
					userMsg := map[string]interface{}{
						"type":               "user",
						"session_id":         nil,
						"message":            map[string]interface{}{"role": "user", "content": prompt},
						"parent_tool_use_id": nil,
					}
					data, _ := json.Marshal(userMsg)
					fmt.Fprintf(sess.Stdin, "%s\n", data)
				} else {
					fmt.Fprintf(sess.Stdin, "%s\n", prompt)
				}
			}
		case "interrupt":
			if sess.ContainerID != "" {
				if err := h.cm.KillContainer(ctx, sess.ContainerID, "SIGINT"); err != nil {
					h.writeWS(conn, WSServerMessage{Type: "error", Message: "failed to send interrupt: " + err.Error()})
				}
			}
		case "terminate":
			h.logger.Info(ctx, fmt.Sprintf("project session %s terminated: user-initiated", sessionID))
			h.writeWS(conn, WSServerMessage{Type: "terminated"})
			conn.Close()
			if sess.CancelFunc != nil {
				sess.CancelFunc()
			}
			return
		}
	}
}

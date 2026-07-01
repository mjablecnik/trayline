package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// SessionHandler handles WebSocket chat session endpoints.
type SessionHandler struct {
	store    *SessionStore
	cm       *ContainerManager
	logger   *Logger
	config   *Config
	stateMgr *StateManager // set after construction via field assignment
}

// NewSessionHandler creates a SessionHandler.
func NewSessionHandler(store *SessionStore, cm *ContainerManager, logger *Logger, config *Config) *SessionHandler {
	return &SessionHandler{store: store, cm: cm, logger: logger, config: config}
}

// saveState persists server state to disk, logging any error.
func (h *SessionHandler) saveState(ctx context.Context) {
	if h.stateMgr == nil {
		return
	}
	if err := h.stateMgr.Save(); err != nil && h.logger != nil {
		h.logger.Error(ctx, "state save error: "+err.Error())
	}
}

// HandleChat handles WS /chat: validates params, upgrades connection, starts container, begins I/O.
func (h *SessionHandler) HandleChat(w http.ResponseWriter, r *http.Request) {
	agent := r.URL.Query().Get("agent")
	if agent != "kiro" && agent != "claude" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: `agent query parameter must be "kiro" or "claude"`,
		})
		return
	}
	model := r.URL.Query().Get("model")
	system := r.URL.Query().Get("system")

	// Check capacity BEFORE upgrading WebSocket so we can still return HTTP 503 (Req 14.5).
	if !h.cm.TryAcquireSlot() {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{
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

	sess := &Session{
		ID:            sessionID,
		Agent:         agent,
		Model:         model,
		System:        system,
		CreatedAt:     now,
		LastMessageAt: now,
		Conn:          conn,
		Active:        true,
		Ctx:           ctx,
		CancelFunc:    cancel,
	}
	h.store.Add(sess)
	h.logger.Info(r.Context(), fmt.Sprintf("session %s created (agent: %s)", sessionID, agent))
	h.saveState(r.Context())

	// Slot was pre-acquired above; StartChatContainer does not acquire another.
	containerID, err := h.cm.StartChatContainer(ctx, agent, model, system)
	if err != nil {
		h.writeWS(conn, WSServerMessage{Type: "error", Message: "failed to start agent container: " + err.Error()})
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

	h.store.Update(sessionID, func(s *Session) {
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

// HandleChatReconnect handles WS /chat/{id}: reconnects a client to an existing session.
func (h *SessionHandler) HandleChatReconnect(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess := h.store.Get(id)
	if sess == nil || !sess.Active {
		writeJSON(w, http.StatusNotFound, ErrorResponse{
			Error:   "NOT_FOUND",
			Message: fmt.Sprintf("session %q not found or is no longer active", id),
		})
		return
	}

	sess.ConnMu.Lock()
	if sess.Conn != nil {
		sess.ConnMu.Unlock()
		writeJSON(w, http.StatusConflict, ErrorResponse{
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

	h.writeWS(conn, WSServerMessage{Type: "session_resumed", SessionID: id})
	go h.readClient(ctx, id, conn)
}

// HandleGetSessions handles GET /sessions.
func (h *SessionHandler) HandleGetSessions(w http.ResponseWriter, r *http.Request) {
	sessions := h.store.List()
	type sessionSummary struct {
		SessionID     string    `json:"session_id"`
		Agent         string    `json:"agent"`
		Model         string    `json:"model,omitempty"`
		CreatedAt     time.Time `json:"created_at"`
		LastMessageAt time.Time `json:"last_message_at"`
	}
	result := make([]sessionSummary, len(sessions))
	for i, s := range sessions {
		result[i] = sessionSummary{
			SessionID:     s.ID,
			Agent:         s.Agent,
			Model:         s.Model,
			CreatedAt:     s.CreatedAt,
			LastMessageAt: s.LastMessageAt,
		}
	}
	writeJSON(w, http.StatusOK, result)
}

// HandleTerminateSession handles POST /sessions/{id}/terminate.
func (h *SessionHandler) HandleTerminateSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess := h.store.Get(id)
	if sess == nil {
		writeJSON(w, http.StatusNotFound, ErrorResponse{
			Error:   "NOT_FOUND",
			Message: fmt.Sprintf("session %q not found", id),
		})
		return
	}

	h.logger.Info(r.Context(), fmt.Sprintf("session %s terminated: user-initiated", id))

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

// StartIdleTimeoutChecker starts a background goroutine that terminates idle sessions.
func (h *SessionHandler) StartIdleTimeoutChecker(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				h.checkIdleSessions()
			}
		}
	}()
}

func (h *SessionHandler) checkIdleSessions() {
	timeout := h.config.SessionTimeout
	now := time.Now()
	terminated := false
	for _, sess := range h.store.All() {
		if now.Sub(sess.LastMessageAt) > timeout {
			h.logger.Info(context.Background(), fmt.Sprintf("session %s terminated: timeout", sess.ID))
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
			terminated = true
		}
	}
	if terminated {
		h.saveState(context.Background())
	}
}

// writeWS sends a WSServerMessage to a connection.
func (h *SessionHandler) writeWS(conn *websocket.Conn, msg WSServerMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	conn.WriteMessage(websocket.TextMessage, data)
}

// writeWSToSession safely sends a message to the session's current connection, if any.
func (h *SessionHandler) writeWSToSession(sessionID string, msg WSServerMessage) {
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

// idleTurnTimeout is the quiet period after the last output line before emitting "done".
// Clients rely on "done" to know when the agent has finished a turn (Req 8.9).
const idleTurnTimeout = 500 * time.Millisecond

// streamOutput reads from the attached container and sends output/done to the WebSocket.
// It emits {"type":"done"} after each agent turn is detected via an idle-output timeout.
func (h *SessionHandler) streamOutput(ctx context.Context, sessionID string, attached dockertypes.HijackedResponse) {
	pr, pw := io.Pipe()

	go func() {
		defer pw.Close()
		// stdcopy demultiplexes Docker's multiplexed stream into stdout and stderr.
		stdcopy.StdCopy(pw, pw, attached.Reader)
	}()

	// Close the pipe reader when the context is cancelled so the scanner goroutine exits.
	go func() {
		<-ctx.Done()
		pr.Close()
	}()

	lineCh := make(chan string, 32)
	go func() {
		defer close(lineCh)
		scanner := bufio.NewScanner(pr)
		for scanner.Scan() {
			lineCh <- scanner.Text()
		}
	}()

	// pendingDone tracks whether the agent has produced output since the last "done".
	// When true, a "done" should be sent after idleTurnTimeout of no new output.
	pendingDone := false
	for {
		if pendingDone {
			select {
			case line, ok := <-lineCh:
				if !ok {
					// Container exited — flush pending done and exit.
					h.writeWSToSession(sessionID, WSServerMessage{Type: "done"})
					return
				}
				if isContextCompaction(line) {
					h.writeWSToSession(sessionID, WSServerMessage{Type: "context_compacted"})
				}
				h.writeWSToSession(sessionID, WSServerMessage{Type: "output", Data: line + "\n"})
				// Reset: new output arrived, keep pendingDone = true (idle timer restarts on next loop).
			case <-time.After(idleTurnTimeout):
				// No output for idleTurnTimeout — agent finished this turn.
				h.writeWSToSession(sessionID, WSServerMessage{Type: "done"})
				pendingDone = false
			}
		} else {
			select {
			case line, ok := <-lineCh:
				if !ok {
					// Container exited with no pending output.
					h.writeWSToSession(sessionID, WSServerMessage{Type: "done"})
					return
				}
				if isContextCompaction(line) {
					h.writeWSToSession(sessionID, WSServerMessage{Type: "context_compacted"})
				}
				h.writeWSToSession(sessionID, WSServerMessage{Type: "output", Data: line + "\n"})
				pendingDone = true
			}
		}
	}
}

// readClient reads WebSocket messages from the client and processes them.
func (h *SessionHandler) readClient(ctx context.Context, sessionID string, conn *websocket.Conn) {
	defer func() {
		// On disconnect: clear conn but keep session alive for reconnection.
		h.store.Update(sessionID, func(s *Session) {
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

		h.store.Update(sessionID, func(s *Session) {
			s.LastMessageAt = time.Now()
		})

		sess := h.store.Get(sessionID)
		if sess == nil {
			return
		}

		switch msg.Type {
		case "message":
			if sess.Stdin != nil && msg.Prompt != "" {
				fmt.Fprintf(sess.Stdin, "%s\n", msg.Prompt)
			}
		case "interrupt":
			if sess.ContainerID != "" {
				if err := h.cm.KillContainer(ctx, sess.ContainerID, "SIGINT"); err != nil {
					h.writeWS(conn, WSServerMessage{Type: "error", Message: "failed to send interrupt: " + err.Error()})
				}
			}
		case "terminate":
			h.logger.Info(ctx, fmt.Sprintf("session %s terminated: user-initiated", sessionID))
			h.writeWS(conn, WSServerMessage{Type: "terminated"})
			conn.Close()
			if sess.CancelFunc != nil {
				sess.CancelFunc()
			}
			return
		default:
			h.writeWS(conn, WSServerMessage{Type: "error", Message: "unknown message type"})
		}
	}
}

// isContextCompaction detects context compaction indicators in agent output.
func isContextCompaction(line string) bool {
	lower := strings.ToLower(line)
	return strings.Contains(lower, "context compacted") ||
		strings.Contains(lower, "compacting context")
}

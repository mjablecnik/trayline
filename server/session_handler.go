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
	store  *SessionStore
	cm     *ContainerManager
	logger *Logger
	config *Config
}

// NewSessionHandler creates a SessionHandler.
func NewSessionHandler(store *SessionStore, cm *ContainerManager, logger *Logger, config *Config) *SessionHandler {
	return &SessionHandler{store: store, cm: cm, logger: logger, config: config}
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

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
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

	containerID, err := h.cm.StartChatContainer(ctx, agent, model, system, now)
	if err != nil {
		h.writeWS(conn, WSServerMessage{Type: "error", Message: "failed to start agent container: " + err.Error()})
		conn.Close()
		cancel()
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
	for _, sess := range h.store.All() {
		if now.Sub(sess.LastMessageAt) > timeout {
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
		}
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

// streamOutput reads from the attached container and sends output/done to the WebSocket.
func (h *SessionHandler) streamOutput(ctx context.Context, sessionID string, attached dockertypes.HijackedResponse) {
	pr, pw := io.Pipe()

	go func() {
		defer pw.Close()
		// stdcopy demultiplexes Docker's multiplexed stream into stdout and stderr.
		stdcopy.StdCopy(pw, pw, attached.Reader)
	}()

	// Stop reading if the context is cancelled.
	go func() {
		<-ctx.Done()
		pr.Close()
	}()

	scanner := bufio.NewScanner(pr)
	for scanner.Scan() {
		line := scanner.Text()
		if isContextCompaction(line) {
			h.writeWSToSession(sessionID, WSServerMessage{Type: "context_compacted"})
		}
		h.writeWSToSession(sessionID, WSServerMessage{Type: "output", Data: line + "\n"})
	}

	h.writeWSToSession(sessionID, WSServerMessage{Type: "done"})
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
			if sess.Stdin != nil {
				fmt.Fprintf(sess.Stdin, "\x03")
			}
		case "terminate":
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

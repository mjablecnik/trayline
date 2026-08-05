package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"remote/core"
	"remote/docker"
	"remote/store"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// SessionHandler handles WebSocket chat session endpoints.
type SessionHandler struct {
	store        *store.SessionStore
	cm           *docker.ContainerManager
	logger       *core.Logger
	config       *core.Config
	stateMgr     StateSaver
	filesMu      sync.RWMutex
	sessionFiles map[string][]UploadedFile
}

// NewSessionHandler creates a SessionHandler.
func NewSessionHandler(store *store.SessionStore, cm *docker.ContainerManager, logger *core.Logger, config *core.Config, stateMgr StateSaver) *SessionHandler {
	return &SessionHandler{
		store:        store,
		cm:           cm,
		logger:       logger,
		config:       config,
		stateMgr:     stateMgr,
		sessionFiles: make(map[string][]UploadedFile),
	}
}

func (h *SessionHandler) addSessionFile(sessionID string, f UploadedFile) {
	h.filesMu.Lock()
	defer h.filesMu.Unlock()
	h.sessionFiles[sessionID] = append(h.sessionFiles[sessionID], f)
}

func (h *SessionHandler) getSessionFiles(sessionID string) []UploadedFile {
	h.filesMu.RLock()
	defer h.filesMu.RUnlock()
	files := h.sessionFiles[sessionID]
	if len(files) == 0 {
		return nil
	}
	cp := make([]UploadedFile, len(files))
	copy(cp, files)
	return cp
}

func (h *SessionHandler) removeSessionFiles(sessionID string) {
	h.filesMu.Lock()
	defer h.filesMu.Unlock()
	delete(h.sessionFiles, sessionID)
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

// StreamOutputForRecovery implements store.SessionOutputStreamer.
// Called by StateManager after restart to re-attach output streaming for recovered sessions.
func (h *SessionHandler) StreamOutputForRecovery(ctx context.Context, sessionID string, attached interface{}) {
	if a, ok := attached.(dockertypes.HijackedResponse); ok {
		go h.streamOutput(ctx, sessionID, a)
	}
}

// wsAuth reads the first WebSocket message and validates it as an auth message.
// Returns true if auth succeeded. On failure, sends an error and closes the connection.
func (h *SessionHandler) wsAuth(conn *websocket.Conn) bool {
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
	if err := json.Unmarshal(data, &msg); err != nil || msg.Type != "auth" || msg.Token == "" {
		h.writeWS(conn, WSServerMessage{Type: "error", Message: "first message must be {\"type\": \"auth\", \"token\": \"...\"}"})
		conn.Close()
		return false
	}

	if !ValidateWSToken(msg.Token, h.config.APIToken) {
		h.writeWS(conn, WSServerMessage{Type: "error", Message: "invalid token"})
		conn.Close()
		return false
	}

	return true
}

// HandleChat handles WS /chat.
func (h *SessionHandler) HandleChat(w http.ResponseWriter, r *http.Request) {
	agent := r.URL.Query().Get("agent")
	if agent != "kiro" && agent != "claude" {
		writeJSON(w, http.StatusBadRequest, core.ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: `agent query parameter must be "kiro" or "claude"`,
		})
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

	// Authenticate: first message must be {"type": "auth", "token": "..."}.
	// CLI clients that send Bearer header in the upgrade request skip this step.
	if r.Header.Get("Authorization") == "" {
		if !h.wsAuth(conn) {
			h.cm.ReleaseChatSlot()
			return
		}
	}

	now := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	sessionID := uuid.NewString()

	sess := &store.Session{
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
		SlotHeld:      true,
	}
	h.store.Add(sess)
	h.logger.Info(r.Context(), fmt.Sprintf("session %s created (agent: %s)", sessionID, agent))
	h.saveState(r.Context())

	containerID, err := h.cm.StartChatContainer(ctx, agent, model, system)
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
		h.releaseSlotIfHeld(sessionID)
		h.store.Remove(sessionID)
		h.saveState(context.Background())
		if err := CleanupUploadDir(h.config.WorkspaceDir, sessionID); err != nil {
			h.logger.Warn(context.Background(), fmt.Sprintf("session %s: failed to clean upload dir: %s", sessionID, err.Error()))
		}
		h.removeSessionFiles(sessionID)
	}()
}

// releaseSlotIfHeld releases the session's chat slot exactly once, if it
// currently holds one. Safe to call from multiple code paths (client
// disconnect, session termination/timeout) without double-releasing.
func (h *SessionHandler) releaseSlotIfHeld(sessionID string) {
	held := false
	h.store.Update(sessionID, func(s *store.Session) {
		if s.SlotHeld {
			s.SlotHeld = false
			held = true
		}
	})
	if held {
		h.cm.ReleaseChatSlot()
	}
}

// HandleChatReconnect handles WS /chat/{id}.
func (h *SessionHandler) HandleChatReconnect(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess := h.store.Get(id)
	if sess == nil || !sess.Active {
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
	sess.ConnMu.Unlock()

	// Reconnecting re-acquires a chat slot: disconnecting released it, so a
	// client coming back must compete for capacity like a new session would.
	if !h.cm.TryAcquireSlot() {
		writeJSON(w, http.StatusServiceUnavailable, core.ErrorResponse{
			Error:   "SERVICE_UNAVAILABLE",
			Message: "server is at capacity, try again later",
		})
		return
	}

	sess.ConnMu.Lock()
	if sess.Conn != nil {
		// Another reconnect raced us while we were acquiring the slot.
		sess.ConnMu.Unlock()
		h.cm.ReleaseChatSlot()
		writeJSON(w, http.StatusConflict, core.ErrorResponse{
			Error:   "CONFLICT",
			Message: "session already has an active connection",
		})
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		sess.ConnMu.Unlock()
		h.cm.ReleaseChatSlot()
		h.logger.Error(r.Context(), "websocket upgrade failed: "+err.Error())
		return
	}

	// Authenticate: first message must be {"type": "auth", "token": "..."}.
	// CLI clients that send Bearer header in the upgrade request skip this step.
	if r.Header.Get("Authorization") == "" {
		if !h.wsAuth(conn) {
			sess.ConnMu.Unlock()
			h.cm.ReleaseChatSlot()
			return
		}
	}

	sess.Conn = conn
	sess.ConnMu.Unlock()

	h.store.Update(id, func(s *store.Session) {
		s.SlotHeld = true
	})

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
		Project       string    `json:"project,omitempty"`
		CreatedAt     time.Time `json:"created_at"`
		LastMessageAt time.Time `json:"last_message_at"`
	}
	result := make([]sessionSummary, len(sessions))
	for i, s := range sessions {
		result[i] = sessionSummary{
			SessionID:     s.ID,
			Agent:         s.Agent,
			Model:         s.Model,
			Project:       s.Project,
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
		writeJSON(w, http.StatusNotFound, core.ErrorResponse{
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
const idleTurnTimeout = 500 * time.Millisecond

// streamOutput reads from the attached container and sends output/done to the WebSocket.
// For claude agent: demultiplexes Docker stream (non-TTY) then parses NDJSON.
// For kiro agent: reads raw TTY stream as plain text lines.
func (h *SessionHandler) streamOutput(ctx context.Context, sessionID string, attached dockertypes.HijackedResponse) {
	sess := h.store.Get(sessionID)
	if sess == nil {
		return
	}

	if sess.Agent == "claude" {
		// Non-TTY: Docker uses multiplexed stream. Demux stdout via stdcopy.
		pr, pw := io.Pipe()
		go func() {
			defer pw.Close()
			stdcopy.StdCopy(pw, pw, attached.Reader)
		}()
		h.streamOutputClaude(ctx, sessionID, pr)
	} else {
		// TTY mode: raw stream, read directly.
		h.streamOutputPlainText(ctx, sessionID, attached.Reader)
	}
}

// streamOutputClaude handles NDJSON protocol output from claude CLI (stream-json mode).
func (h *SessionHandler) streamOutputClaude(ctx context.Context, sessionID string, reader interface{ Read([]byte) (int, error) }) {
	lineCh := make(chan string, 32)
	go func() {
		defer close(lineCh)
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer for large JSON lines
		for scanner.Scan() {
			lineCh <- scanner.Text()
		}
	}()

	// Send initialization control request
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

			var msg map[string]interface{}
			if err := json.Unmarshal([]byte(line), &msg); err != nil {
				// Not JSON — pass through as raw output
				h.writeWSToSession(sessionID, WSServerMessage{Type: "output", Data: line + "\n"})
				continue
			}

			msgType, _ := msg["type"].(string)

			switch msgType {
			case "control_response":
				if !initialized {
					initialized = true
					h.logger.Info(ctx, fmt.Sprintf("session %s: claude CLI initialized", sessionID))
				}

			case "system":
				// System messages (session info etc.) — skip or log
				continue

			case "assistant":
				// Extract text content from assistant message
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
				// End of turn — emit "done"
				// Check if context was compacted
				subtype, _ := msg["subtype"].(string)
				if subtype == "compact_boundary" {
					h.writeWSToSession(sessionID, WSServerMessage{Type: "context_compacted"})
				}
				h.writeWSToSession(sessionID, WSServerMessage{Type: "done"})

			case "stream_event":
				// Partial streaming delta — extract text
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
func (h *SessionHandler) streamOutputPlainText(ctx context.Context, sessionID string, reader interface{ Read([]byte) (int, error) }) {
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
func (h *SessionHandler) readClient(ctx context.Context, sessionID string, conn *websocket.Conn) {
	defer func() {
		h.store.Update(sessionID, func(s *store.Session) {
			s.ConnMu.Lock()
			if s.Conn == conn {
				s.Conn = nil
			}
			s.ConnMu.Unlock()
		})
	}()

	// Set up ping/pong keepalive to detect dead connections early.
	conn.SetReadDeadline(time.Now().Add(pingInterval + pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pingInterval + pongWait))
		return nil
	})

	pingDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				sess := h.store.Get(sessionID)
				if sess == nil {
					return
				}
				sess.ConnMu.Lock()
				if sess.Conn == conn {
					err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
					sess.ConnMu.Unlock()
					if err != nil {
						return
					}
				} else {
					sess.ConnMu.Unlock()
					return
				}
			case <-pingDone:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	defer close(pingDone)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msgType, data, err := conn.ReadMessage()
		if err != nil {
			// Client disconnected without sending "terminate" (tab closed, network
			// drop, navigated away). The container keeps running for reconnect, but
			// give back the chat slot immediately instead of holding it until the
			// idle timeout — otherwise a client that just wanders off between
			// projects can quietly exhaust the whole chat slot pool.
			h.releaseSlotIfHeld(sessionID)
			return
		}

		if msgType == websocket.BinaryMessage {
			filename, fileData, err := DecodeBinaryFrame(data)
			if err != nil {
				h.writeWS(conn, WSServerMessage{Type: "error", Message: err.Error()})
				continue
			}
			uploaded, err := SaveSingleFile(filename, fileData, h.config.WorkspaceDir, sessionID, h.config.MaxUploadSize)
			if err != nil {
				h.writeWS(conn, WSServerMessage{Type: "error", Message: err.Error()})
				continue
			}
			h.addSessionFile(sessionID, *uploaded)
			h.store.Update(sessionID, func(s *store.Session) {
				s.LastMessageAt = time.Now()
			})
			h.writeWS(conn, WSServerMessage{Type: "file_uploaded", Data: uploaded.OriginalName})
			continue
		}

		var msg WSClientMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			h.writeWS(conn, WSServerMessage{Type: "error", Message: "invalid message format"})
			continue
		}

		h.store.Update(sessionID, func(s *store.Session) {
			s.LastMessageAt = time.Now()
		})

		sess := h.store.Get(sessionID)
		if sess == nil {
			return
		}

		switch msg.Type {
		case "message":
			if sess.Stdin != nil && msg.Prompt != "" {
				prompt := msg.Prompt
				if files := h.getSessionFiles(sessionID); len(files) > 0 {
					prompt = BuildUploadMetadata(files) + prompt
				}
				if sess.Agent == "claude" {
					// Send NDJSON user message for claude stream-json protocol
					userMsg := map[string]interface{}{
						"type":               "user",
						"session_id":         nil,
						"message":            map[string]interface{}{"role": "user", "content": prompt},
						"parent_tool_use_id": nil,
					}
					data, _ := json.Marshal(userMsg)
					fmt.Fprintf(sess.Stdin, "%s\n", data)
				} else {
					// Plain text for kiro
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

package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"remote/core"
	"remote/store"
)

// validateAssistantAgent checks that the agent query parameter is one of the
// supported agent types. Returns an error response if invalid, or nil if valid.
func (h *AssistantHandler) validateAssistantAgent(agent string) *core.ErrorResponse {
	if agent != "kiro" && agent != "claude" {
		return &core.ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: `agent query parameter must be "kiro" or "claude"`,
		}
	}
	return nil
}

// validateUploadFileSize checks that dataLen does not exceed MaxUploadFileSize.
// Returns nil when the size is within the limit, or a descriptive error otherwise.
func validateUploadFileSize(filename string, dataLen int) error {
	if dataLen > MaxUploadFileSize {
		return fmt.Errorf("file %q exceeds maximum size of %d bytes", filename, MaxUploadFileSize)
	}
	return nil
}

// assistantDataDirReady reports whether ASSISTANT_DATA_DIR is configured and
// points at an accessible directory.
func (h *AssistantHandler) assistantDataDirReady() bool {
	if h.config.AssistantDataDir == "" {
		return false
	}
	info, err := os.Stat(h.config.AssistantDataDir)
	return err == nil && info.IsDir()
}

// writeWS sends a WSServerMessage to a connection.
func (h *AssistantHandler) writeWS(conn *websocket.Conn, msg WSServerMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	conn.WriteMessage(websocket.TextMessage, data)
}

// writeWSToSession safely sends a message to the session's current connection, if any.
func (h *AssistantHandler) writeWSToSession(sessionID string, msg WSServerMessage) {
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

// wsAuth reads the first WebSocket message and validates it as an auth message.
// Returns true if auth succeeded. On failure, sends an error and closes the connection.
func (h *AssistantHandler) wsAuth(conn *websocket.Conn) bool {
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

// saveState persists server state to disk, logging any error.
func (h *AssistantHandler) saveState(ctx context.Context) {
	if h.stateMgr == nil {
		return
	}
	if err := h.stateMgr.Save(); err != nil && h.logger != nil {
		h.logger.Error(ctx, "state save error: "+err.Error())
	}
}

// releaseSlotIfHeld releases the session's chat slot exactly once, if it
// currently holds one. Safe to call from multiple code paths (client
// disconnect, session termination/timeout) without double-releasing.
func (h *AssistantHandler) releaseSlotIfHeld(sessionID string) {
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

// terminateSessionImmediately releases the session's chat slot and removes
// it from the store synchronously with the termination request, mirroring
// ProjectAgentHandler.terminateSessionImmediately.
func (h *AssistantHandler) terminateSessionImmediately(sessionID string) {
	h.releaseSlotIfHeld(sessionID)
	h.store.Remove(sessionID)
}

// touchLastMessageAt refreshes the session's idle timer.
func (h *AssistantHandler) touchLastMessageAt(sessionID string) {
	h.store.Update(sessionID, func(s *store.Session) {
		s.LastMessageAt = time.Now()
	})
}

// HandleAssistantChat handles WS /assistant/chat.
func (h *AssistantHandler) HandleAssistantChat(w http.ResponseWriter, r *http.Request) {
	if !h.assistantDataDirReady() {
		writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
			Error:   "CONFIGURATION_ERROR",
			Message: "assistant data directory is not configured or accessible",
		})
		return
	}

	agent := r.URL.Query().Get("agent")
	if err := h.validateAssistantAgent(agent); err != nil {
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
		Project:       assistantProject,
		CreatedAt:     now,
		LastMessageAt: now,
		Conn:          conn,
		Active:        true,
		Ctx:           ctx,
		CancelFunc:    cancel,
		SlotHeld:      true,
	}
	h.store.Add(sess)
	h.logger.Info(r.Context(), fmt.Sprintf("assistant session %s created (agent: %s)", sessionID, agent))
	h.saveState(r.Context())

	containerID, err := h.cm.StartAssistantChatContainer(ctx, agent, model, system, sessionID)
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
	}()
}

// HandleAssistantChatReconnect handles WS /assistant/chat/{id}, resuming an
// existing assistant session and replaying its message transcript.
func (h *AssistantHandler) HandleAssistantChatReconnect(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess := h.store.Get(id)
	if sess == nil || !sess.Active || sess.Project != assistantProject {
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

	ctx := sess.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	h.store.Update(id, func(s *store.Session) {
		s.LastMessageAt = time.Now()
		s.SlotHeld = true
	})

	h.writeWS(conn, WSServerMessage{Type: "session_resumed", SessionID: id, Agent: sess.Agent, Model: sess.Model})
	h.writeWS(conn, WSServerMessage{Type: "history", Messages: toHistoryMessages(sess.Messages)})
	go h.readClient(ctx, id, conn)
}

// streamOutput reads from the attached container and sends output/done to the WebSocket.
// It also refreshes sess.LastMessageAt on each chunk of agent output so the idle
// timeout resets on agent activity as well as client activity.
func (h *AssistantHandler) streamOutput(ctx context.Context, sessionID string, attached dockertypes.HijackedResponse) {
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

// streamOutputClaude handles NDJSON protocol output from claude CLI (stream-json mode).
func (h *AssistantHandler) streamOutputClaude(ctx context.Context, sessionID string, reader interface{ Read([]byte) (int, error) }) {
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
		if _, err := fmt.Fprintf(sess.Stdin, "%s\n", data); err != nil {
			h.logger.Error(ctx, fmt.Sprintf(
				"assistant session %s: failed to write init control_request to container stdin: %s", sessionID, err.Error()))
		}
	}

	initialized := false
	for {
		select {
		case line, ok := <-lineCh:
			if !ok {
				h.writeWSToSession(sessionID, WSServerMessage{Type: "done"})
				h.store.MarkAgentDone(sessionID)
				return
			}
			h.touchLastMessageAt(sessionID)

			var msg map[string]interface{}
			if err := json.Unmarshal([]byte(line), &msg); err != nil {
				h.writeWSToSession(sessionID, WSServerMessage{Type: "output", Data: line + "\n"})
				h.store.AppendAgentOutput(sessionID, line+"\n")
				continue
			}

			msgType, _ := msg["type"].(string)

			switch msgType {
			case "control_response":
				if !initialized {
					initialized = true
					h.logger.Info(ctx, fmt.Sprintf("assistant session %s: claude CLI initialized", sessionID))
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
							h.store.AppendAgentOutput(sessionID, text)
						}
					}
				}

			case "result":
				subtype, _ := msg["subtype"].(string)
				if subtype == "compact_boundary" {
					h.writeWSToSession(sessionID, WSServerMessage{Type: "context_compacted"})
				}
				h.writeWSToSession(sessionID, WSServerMessage{Type: "done"})
				h.store.MarkAgentDone(sessionID)

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
								h.store.AppendAgentOutput(sessionID, text)
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
func (h *AssistantHandler) streamOutputPlainText(ctx context.Context, sessionID string, reader interface{ Read([]byte) (int, error) }) {
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
					h.store.MarkAgentDone(sessionID)
					return
				}
				h.touchLastMessageAt(sessionID)
				if isContextCompaction(line) {
					h.writeWSToSession(sessionID, WSServerMessage{Type: "context_compacted"})
				}
				h.writeWSToSession(sessionID, WSServerMessage{Type: "output", Data: line + "\n"})
				h.store.AppendAgentOutput(sessionID, line+"\n")
			case <-time.After(idleTurnTimeout):
				h.writeWSToSession(sessionID, WSServerMessage{Type: "done"})
				h.store.MarkAgentDone(sessionID)
				pendingDone = false
			case <-ctx.Done():
				return
			}
		} else {
			select {
			case line, ok := <-lineCh:
				if !ok {
					h.writeWSToSession(sessionID, WSServerMessage{Type: "done"})
					h.store.MarkAgentDone(sessionID)
					return
				}
				h.touchLastMessageAt(sessionID)
				if isContextCompaction(line) {
					h.writeWSToSession(sessionID, WSServerMessage{Type: "context_compacted"})
				}
				h.writeWSToSession(sessionID, WSServerMessage{Type: "output", Data: line + "\n"})
				h.store.AppendAgentOutput(sessionID, line+"\n")
				pendingDone = true
			case <-ctx.Done():
				return
			}
		}
	}
}

// readClient reads WebSocket messages from the client and processes them.
func (h *AssistantHandler) readClient(ctx context.Context, sessionID string, conn *websocket.Conn) {
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

		messageType, data, err := conn.ReadMessage()
		if err != nil {
			// Client disconnected without sending "terminate". The container
			// keeps running for reconnect, but give back the chat slot
			// immediately instead of holding it until the idle timeout.
			h.releaseSlotIfHeld(sessionID)
			return
		}

		h.touchLastMessageAt(sessionID)

		if messageType == websocket.BinaryMessage {
			h.handleFileUpload(ctx, sessionID, conn, data)
			continue
		}

		var msg WSClientMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			h.writeWS(conn, WSServerMessage{Type: "error", Message: "invalid message format"})
			continue
		}

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
				if len(sess.UploadedFiles) > 0 {
					prompt = buildProjectUploadMetadata(sess.UploadedFiles) + prompt
					h.store.Update(sessionID, func(s *store.Session) {
						s.UploadedFiles = nil
					})
				}
				var writeErr error
				if sess.Agent == "claude" {
					userMsg := map[string]interface{}{
						"type":               "user",
						"session_id":         nil,
						"message":            map[string]interface{}{"role": "user", "content": prompt},
						"parent_tool_use_id": nil,
					}
					data, _ := json.Marshal(userMsg)
					_, writeErr = fmt.Fprintf(sess.Stdin, "%s\n", data)
				} else {
					_, writeErr = fmt.Fprintf(sess.Stdin, "%s\n", prompt)
				}
				if writeErr != nil {
					h.logger.Error(ctx, fmt.Sprintf(
						"assistant session %s: failed to write prompt to container stdin: %s", sessionID, writeErr.Error()))
					h.writeWS(conn, WSServerMessage{Type: "error", Message: "failed to send message to agent"})
				} else {
					h.store.AppendUserMessage(sessionID, msg.Prompt)
				}
			}
		case "interrupt":
			if sess.ContainerID != "" {
				if err := h.cm.KillContainer(ctx, sess.ContainerID, "SIGINT"); err != nil {
					h.writeWS(conn, WSServerMessage{Type: "error", Message: "failed to send interrupt: " + err.Error()})
				}
			}
		case "terminate":
			h.logger.Info(ctx, fmt.Sprintf("assistant session %s terminated: user-initiated", sessionID))
			h.writeWS(conn, WSServerMessage{Type: "terminated"})
			conn.Close()
			h.terminateSessionImmediately(sessionID)
			if sess.CancelFunc != nil {
				sess.CancelFunc()
			}
			return
		}
	}
}

// handleFileUpload decodes a WebSocket binary frame, validates it, and writes the
// file into the session's running container at projectUploadDir.
func (h *AssistantHandler) handleFileUpload(ctx context.Context, sessionID string, conn *websocket.Conn, frame []byte) {
	filename, data, err := DecodeBinaryFrame(frame)
	if err != nil {
		h.writeWS(conn, WSServerMessage{Type: "error", Message: err.Error()})
		return
	}
	if err := validateUploadFileSize(filename, len(data)); err != nil {
		h.writeWS(conn, WSServerMessage{Type: "error", Message: err.Error()})
		return
	}

	sess := h.store.Get(sessionID)
	if sess == nil || sess.ContainerID == "" {
		h.writeWS(conn, WSServerMessage{Type: "error", Message: "no active agent container"})
		return
	}

	safeName := sanitizeFilename(filename)
	if err := h.cm.CopyFileToContainer(ctx, sess.ContainerID, projectUploadDir, safeName, data); err != nil {
		h.writeWS(conn, WSServerMessage{Type: "error", Message: "failed to upload file: " + err.Error()})
		return
	}

	h.store.Update(sessionID, func(s *store.Session) {
		s.UploadedFiles = append(s.UploadedFiles, store.UploadedFile{OriginalName: filename, SafeName: safeName})
	})

	h.writeWS(conn, WSServerMessage{Type: "file_uploaded", Data: filename})
}

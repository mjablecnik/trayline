package api

// WSClientMessage is a message sent from the WebSocket client to the server.
type WSClientMessage struct {
	Type   string `json:"type"`   // "message", "interrupt", "terminate"
	Prompt string `json:"prompt,omitempty"`
}

// WSServerMessage is a message sent from the server to the WebSocket client.
type WSServerMessage struct {
	Type      string `json:"type"`             // "session_started", "session_resumed", "output", "done", "error", "terminated", "context_compacted", "file_uploaded"
	SessionID string `json:"sessionId,omitempty"`
	Data      string `json:"data,omitempty"`
	Message   string `json:"message,omitempty"`
}

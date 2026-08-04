package api

import "time"

// assistantSessionSummary describes a single assistant session, as returned
// by GET /assistant/sessions.
type assistantSessionSummary struct {
	SessionID     string    `json:"session_id"`
	Agent         string    `json:"agent"`
	Model         string    `json:"model,omitempty"`
	IsAssistant   bool      `json:"is_assistant"`
	CreatedAt     time.Time `json:"created_at"`
	LastMessageAt time.Time `json:"last_message_at"`
}

// putPromptRequest is the request body for PUT /assistant/prompts/{filename}.
type putPromptRequest struct {
	Content string `json:"content"`
}

// directoryResponse describes the contents of a directory within the
// assistant folder, as returned by the file browser endpoints.
type directoryResponse struct {
	Path    string      `json:"path"`
	Entries []fileEntry `json:"entries"`
}

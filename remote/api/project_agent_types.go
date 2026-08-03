package api

import "time"

// projectSessionSummary is the REST response shape for a project agent session.
type projectSessionSummary struct {
	SessionID     string    `json:"session_id"`
	Agent         string    `json:"agent"`
	Model         string    `json:"model,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	LastMessageAt time.Time `json:"last_message_at"`
}

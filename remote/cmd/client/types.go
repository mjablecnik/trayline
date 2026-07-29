package main

import "time"

// Config holds resolved, validated connection settings.
type Config struct {
	ServerURL string
	Token     string
	Verbose   bool
	Quiet     bool
}

// RunRequest mirrors the server's POST /run body.
type RunRequest struct {
	Prompt       string `json:"prompt"`
	Agent        string `json:"agent"`
	Model        string `json:"model,omitempty"`
	System       string `json:"system,omitempty"`
	OutputFormat string `json:"output_format,omitempty"`
}

// RunResponse mirrors a completed task response from GET /run/{id} or HTTP 200 on POST /run.
type RunResponse struct {
	ID          string     `json:"id"`
	Status      string     `json:"status"`
	Agent       string     `json:"agent"`
	Result      string     `json:"result,omitempty"`
	Error       string     `json:"error,omitempty"`
	Valid       *bool      `json:"valid,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// RunAcceptedResponse mirrors a 202 Accepted response on POST /run.
type RunAcceptedResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// TaskSummary mirrors one item from GET /runs.
type TaskSummary struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	Agent     string    `json:"agent"`
	CreatedAt time.Time `json:"created_at"`
}

// SessionSummary mirrors one item from GET /sessions.
type SessionSummary struct {
	SessionID     string    `json:"session_id"`
	Agent         string    `json:"agent"`
	Model         string    `json:"model,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	LastMessageAt time.Time `json:"last_message_at"`
}

// ErrorResponse mirrors the server's error envelope.
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// WSClientMessage is sent from client to server over WebSocket.
type WSClientMessage struct {
	Type   string `json:"type"`
	Prompt string `json:"prompt,omitempty"`
}

// WSServerMessage is received from server over WebSocket.
type WSServerMessage struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionId,omitempty"`
	Data      string `json:"data,omitempty"`
	Message   string `json:"message,omitempty"`
}

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

// ScheduleWorkflowRequest is the request body for POST /projects/{name}/workflows.
type ScheduleWorkflowRequest struct {
	Pipeline  string            `json:"pipeline"`
	Variables map[string]string `json:"variables"`
}

// WorkflowSummary mirrors one item from GET /projects/{name}/workflows.
type WorkflowSummary struct {
	ID          string            `json:"id"`
	Pipeline    string            `json:"pipeline"`
	Variables   map[string]string `json:"variables"`
	Status      string            `json:"status"`
	CreatedAt   time.Time         `json:"created_at"`
	StartedAt   *time.Time        `json:"started_at,omitempty"`
	CompletedAt *time.Time        `json:"completed_at,omitempty"`
	Error       string            `json:"error,omitempty"`
	ExitCode    *int              `json:"exit_code,omitempty"`
	Log         string            `json:"log,omitempty"`
	Truncated   bool              `json:"truncated,omitempty"`
}

// WSLogMessage is a message received from the workflow log WebSocket.
type WSLogMessage struct {
	Type      string `json:"type"`
	Data      string `json:"data,omitempty"`
	Status    string `json:"status,omitempty"`
	ExitCode  *int   `json:"exit_code,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

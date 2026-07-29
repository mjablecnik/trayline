package api

import (
	"time"

	"remote/store"
)

// RunRequest is the body for POST /run.
type RunRequest struct {
	Prompt       string `json:"prompt"`
	Agent        string `json:"agent"`
	Model        string `json:"model,omitempty"`
	System       string `json:"system,omitempty"`
	OutputFormat string `json:"output_format,omitempty"`
}

// RunResponse is returned when the task completes within the 30-second long-poll window.
type RunResponse struct {
	ID          string           `json:"id"`
	Status      store.TaskStatus `json:"status"`
	Agent       string           `json:"agent"`
	Result      string           `json:"result,omitempty"`
	Error       string           `json:"error,omitempty"`
	Valid        *bool            `json:"valid,omitempty"`
	CreatedAt   time.Time        `json:"created_at"`
	CompletedAt *time.Time       `json:"completed_at,omitempty"`
}

// RunAcceptedResponse is returned when the task is still running after 30 seconds.
type RunAcceptedResponse struct {
	ID     string           `json:"id"`
	Status store.TaskStatus `json:"status"`
}

// TaskSummary is one item in the GET /runs response array.
type TaskSummary struct {
	ID        string           `json:"id"`
	Status    store.TaskStatus `json:"status"`
	Agent     string           `json:"agent"`
	CreatedAt time.Time        `json:"created_at"`
}

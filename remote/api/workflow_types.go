package api

import (
	"time"

	"remote/store"
)

// ScheduleWorkflowRequest is the request body for POST /projects/{name}/workflows.
type ScheduleWorkflowRequest struct {
	Pipeline  string            `json:"pipeline"`
	Variables map[string]string `json:"variables"`
}

// EditWorkflowRequest is the request body for PUT /projects/{name}/workflows/{id}.
// Pipeline is optional (omitted keeps the workflow's current pipeline);
// Variables fully replaces the existing variables map, per Requirement 6.2.
type EditWorkflowRequest struct {
	Pipeline  string            `json:"pipeline,omitempty"`
	Variables map[string]string `json:"variables"`
}

// WorkflowResponse is the JSON representation of a workflow returned by the
// schedule/list/detail/edit/cancel endpoints. Log is only populated by the
// detail endpoint (Requirement 5.3) — list responses omit it (Requirement 5.2).
type WorkflowResponse struct {
	ID          string               `json:"id"`
	Pipeline    string               `json:"pipeline"`
	Variables   map[string]string    `json:"variables"`
	Status      store.WorkflowStatus `json:"status"`
	CreatedAt   time.Time            `json:"created_at"`
	StartedAt   *time.Time           `json:"started_at,omitempty"`
	CompletedAt *time.Time           `json:"completed_at,omitempty"`
	Error       string               `json:"error,omitempty"`
	ExitCode    *int                 `json:"exit_code,omitempty"`
	Log         string               `json:"log,omitempty"`
	Truncated   bool                 `json:"truncated,omitempty"`
}

// workflowToResponse builds a WorkflowResponse from a workflow snapshot.
// includeLog controls whether the captured log output is included (detail
// endpoint) or omitted (list endpoint).
func workflowToResponse(w store.Workflow, includeLog bool) WorkflowResponse {
	resp := WorkflowResponse{
		ID:          w.ID,
		Pipeline:    w.Pipeline,
		Variables:   w.Variables,
		Status:      w.Status,
		CreatedAt:   w.CreatedAt,
		StartedAt:   w.StartedAt,
		CompletedAt: w.CompletedAt,
		Error:       w.Error,
		ExitCode:    w.ExitCode,
	}
	if includeLog && w.LogBuffer != nil {
		resp.Log = w.LogBuffer.String()
		resp.Truncated = w.LogBuffer.Wrapped()
	}
	return resp
}

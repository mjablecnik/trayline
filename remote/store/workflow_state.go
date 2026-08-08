package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"remote/core"
)

const (
	workflowStateFileName = "workflows.json"
	// WorkflowLogBufferSize is the maximum number of log bytes retained per
	// workflow (1 MB), per Requirement 7.6.
	WorkflowLogBufferSize = 1 * 1024 * 1024
)

// persistedWorkflow holds the fields needed to reconstruct a Workflow after restart.
type persistedWorkflow struct {
	ID          string            `json:"id"`
	Project     string            `json:"project"`
	Pipeline    string            `json:"pipeline"`
	Variables   map[string]string `json:"variables"`
	Status      WorkflowStatus    `json:"status"`
	CreatedAt   time.Time         `json:"created_at"`
	StartedAt   *time.Time        `json:"started_at,omitempty"`
	CompletedAt *time.Time        `json:"completed_at,omitempty"`
	Error       string            `json:"error,omitempty"`
	ExitCode    *int              `json:"exit_code,omitempty"`
	NotBefore   *time.Time        `json:"not_before,omitempty"`
	Log         string            `json:"log,omitempty"`
}

// workflowState is the full workflow queue state written atomically to disk.
type workflowState struct {
	Workflows []persistedWorkflow `json:"workflows"`
}

// WorkflowStateManager handles workflow state persistence to disk and
// startup recovery.
type WorkflowStateManager struct {
	stateDir      string
	workflowStore *WorkflowStore
	logger        *core.Logger
}

// NewWorkflowStateManager creates a WorkflowStateManager.
func NewWorkflowStateManager(stateDir string, workflowStore *WorkflowStore, logger *core.Logger) *WorkflowStateManager {
	return &WorkflowStateManager{
		stateDir:      stateDir,
		workflowStore: workflowStore,
		logger:        logger,
	}
}

// Save atomically writes the current workflow state to disk. If the write
// fails, the error is logged and the server continues operating with the
// in-memory state (Requirement 8.7).
func (sm *WorkflowStateManager) Save() error {
	if err := sm.save(); err != nil {
		if sm.logger != nil {
			sm.logger.Error(context.Background(), fmt.Sprintf("failed to persist workflow state: %v", err))
		}
		return err
	}
	return nil
}

func (sm *WorkflowStateManager) save() error {
	state := sm.buildState()

	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal workflow state: %w", err)
	}

	tmpPath := filepath.Join(sm.stateDir, workflowStateFileName+".tmp")
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write workflow state temp file: %w", err)
	}

	targetPath := filepath.Join(sm.stateDir, workflowStateFileName)
	if err := os.Rename(tmpPath, targetPath); err != nil {
		return fmt.Errorf("failed to rename workflow state file: %w", err)
	}

	return nil
}

// buildState constructs a workflowState snapshot from the current in-memory store.
func (sm *WorkflowStateManager) buildState() workflowState {
	workflows := sm.workflowStore.All()
	pworkflows := make([]persistedWorkflow, 0, len(workflows))
	for _, w := range workflows {
		var log string
		if w.LogBuffer != nil {
			log = w.LogBuffer.String()
		}
		pworkflows = append(pworkflows, persistedWorkflow{
			ID:          w.ID,
			Project:     w.Project,
			Pipeline:    w.Pipeline,
			Variables:   w.Variables,
			Status:      w.Status,
			CreatedAt:   w.CreatedAt,
			StartedAt:   w.StartedAt,
			CompletedAt: w.CompletedAt,
			Error:       w.Error,
			ExitCode:    w.ExitCode,
			NotBefore:   w.NotBefore,
			Log:         log,
		})
	}
	return workflowState{Workflows: pworkflows}
}

// Load reads STATE_DIR/workflows.json (if it exists) and restores the
// WorkflowStore. A missing file initializes an empty store without error
// (Requirement 8.3). A corrupt file is renamed to workflows.json.corrupt and
// an empty store is initialized (Requirement 8.5).
func (sm *WorkflowStateManager) Load() error {
	path := filepath.Join(sm.stateDir, workflowStateFileName)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read workflow state file: %w", err)
	}

	var state workflowState
	if err := json.Unmarshal(data, &state); err != nil {
		if sm.logger != nil {
			sm.logger.Error(context.Background(), fmt.Sprintf("workflow state file is corrupt, quarantining: %v", err))
		}
		corruptPath := filepath.Join(sm.stateDir, workflowStateFileName+".corrupt")
		if renameErr := os.Rename(path, corruptPath); renameErr != nil && sm.logger != nil {
			sm.logger.Error(context.Background(), fmt.Sprintf("failed to quarantine corrupt workflow state file: %v", renameErr))
		}
		return nil
	}

	for _, pw := range state.Workflows {
		buf := NewRingBuffer(WorkflowLogBufferSize)
		if pw.Log != "" {
			buf.Write([]byte(pw.Log))
		}
		sm.workflowStore.Add(&Workflow{
			ID:          pw.ID,
			Project:     pw.Project,
			Pipeline:    pw.Pipeline,
			Variables:   pw.Variables,
			Status:      pw.Status,
			CreatedAt:   pw.CreatedAt,
			StartedAt:   pw.StartedAt,
			CompletedAt: pw.CompletedAt,
			Error:       pw.Error,
			ExitCode:    pw.ExitCode,
			NotBefore:   pw.NotBefore,
			LogBuffer:   buf,
		})
	}

	return nil
}

// Recover marks any workflow left in "running" status as "failed" (the
// container was lost across the restart, Requirement 8.6), then persists the
// change. Workflows left "queued" are untouched here — resuming their
// execution is the caller's responsibility (the queue manager), invoked
// after Recover returns, so it can enqueue them in original creation order.
func (sm *WorkflowStateManager) Recover() error {
	for _, w := range sm.workflowStore.All() {
		if w.Status != WorkflowRunning {
			continue
		}
		id := w.ID
		sm.workflowStore.Update(id, func(w *Workflow) {
			w.Status = WorkflowFailed
			w.Error = "server restarted and container was lost"
			now := time.Now()
			w.CompletedAt = &now
		})
	}

	return sm.Save()
}

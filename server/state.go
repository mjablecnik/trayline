package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const stateFileName = "state.json"

// persistedTask holds the fields needed to reconstruct a Task after restart.
type persistedTask struct {
	ID           string     `json:"id"`
	Status       TaskStatus `json:"status"`
	Agent        string     `json:"agent"`
	Prompt       string     `json:"prompt"`
	Model        string     `json:"model,omitempty"`
	System       string     `json:"system,omitempty"`
	OutputFormat string     `json:"output_format,omitempty"`
	Result       string     `json:"result,omitempty"`
	Error        string     `json:"error,omitempty"`
	Valid        *bool      `json:"valid,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	ContainerID  string     `json:"container_id,omitempty"`
}

// persistedSession holds the fields needed to reconstruct a Session after restart.
type persistedSession struct {
	ID            string    `json:"session_id"`
	Agent         string    `json:"agent"`
	Model         string    `json:"model,omitempty"`
	System        string    `json:"system,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	LastMessageAt time.Time `json:"last_message_at"`
	ContainerID   string    `json:"container_id"`
}

// serverState is the full server state written atomically to disk.
type serverState struct {
	Tasks    []persistedTask    `json:"tasks"`
	Sessions []persistedSession `json:"sessions"`
}

// StateManager handles state persistence to disk and startup recovery.
type StateManager struct {
	stateDir     string
	taskStore    *TaskStore
	sessionStore *SessionStore
	cm           *ContainerManager
	logger       *Logger
}

// NewStateManager creates a StateManager.
func NewStateManager(stateDir string, tasks *TaskStore, sessions *SessionStore, cm *ContainerManager, logger *Logger) *StateManager {
	return &StateManager{
		stateDir:     stateDir,
		taskStore:    tasks,
		sessionStore: sessions,
		cm:           cm,
		logger:       logger,
	}
}

// EnsureStateDir verifies that the state directory exists and is writable,
// creating it if necessary. Returns an error if it cannot be created or written to.
func EnsureStateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cannot create STATE_DIR %q: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".write-check-*")
	if err != nil {
		return fmt.Errorf("STATE_DIR %q is not writable: %w", dir, err)
	}
	tmp.Close()
	os.Remove(tmp.Name())
	return nil
}

// Save atomically writes the current task and session state to disk.
func (sm *StateManager) Save() error {
	state := sm.buildState()

	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	tmpPath := filepath.Join(sm.stateDir, stateFileName+".tmp")
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write state temp file: %w", err)
	}

	targetPath := filepath.Join(sm.stateDir, stateFileName)
	if err := os.Rename(tmpPath, targetPath); err != nil {
		return fmt.Errorf("failed to rename state file: %w", err)
	}

	return nil
}

// buildState constructs a serverState snapshot from the current in-memory stores.
func (sm *StateManager) buildState() serverState {
	tasks := sm.taskStore.All()
	ptasks := make([]persistedTask, 0, len(tasks))
	for _, t := range tasks {
		ptasks = append(ptasks, persistedTask{
			ID:           t.ID,
			Status:       t.Status,
			Agent:        t.Agent,
			Prompt:       t.Prompt,
			Model:        t.Model,
			System:       t.System,
			OutputFormat: t.OutputFormat,
			Result:       t.Result,
			Error:        t.Error,
			Valid:         t.Valid,
			CreatedAt:    t.CreatedAt,
			CompletedAt:  t.CompletedAt,
			ContainerID:  t.ContainerID,
		})
	}

	sessions := sm.sessionStore.All()
	psessions := make([]persistedSession, 0, len(sessions))
	for _, s := range sessions {
		psessions = append(psessions, persistedSession{
			ID:            s.ID,
			Agent:         s.Agent,
			Model:         s.Model,
			System:        s.System,
			CreatedAt:     s.CreatedAt,
			LastMessageAt: s.LastMessageAt,
			ContainerID:   s.ContainerID,
		})
	}

	return serverState{Tasks: ptasks, Sessions: psessions}
}

// Recover reads the state file (if it exists) and reconciles persisted tasks and
// sessions against the live Docker environment. On return the in-memory stores
// are populated and the state file reflects the reconciled state.
func (sm *StateManager) Recover(ctx context.Context) error {
	path := filepath.Join(sm.stateDir, stateFileName)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil // no prior state — start clean
	}
	if err != nil {
		return fmt.Errorf("failed to read state file: %w", err)
	}

	var state serverState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("failed to parse state file: %w", err)
	}

	sm.recoverTasks(ctx, state.Tasks)
	sm.recoverSessions(ctx, state.Sessions)

	// Persist reconciled state so stale entries don't reappear on the next start.
	_ = sm.Save()
	return nil
}

// recoverTasks reconciles persisted tasks against Docker.
func (sm *StateManager) recoverTasks(ctx context.Context, ptasks []persistedTask) {
	for _, pt := range ptasks {
		t := &Task{
			ID:           pt.ID,
			Status:       pt.Status,
			Agent:        pt.Agent,
			Prompt:       pt.Prompt,
			Model:        pt.Model,
			System:       pt.System,
			OutputFormat: pt.OutputFormat,
			Result:       pt.Result,
			Error:        pt.Error,
			Valid:         pt.Valid,
			CreatedAt:    pt.CreatedAt,
			CompletedAt:  pt.CompletedAt,
			ContainerID:  pt.ContainerID,
			Done:         make(chan struct{}),
		}

		switch t.Status {
		case TaskCompleted, TaskFailed, TaskCancelled:
			// Already terminal — restore as-is.
			close(t.Done)

		case TaskQueued:
			// Never started a container — fail it.
			t.Status = TaskFailed
			t.Error = "server restarted before task could be executed"
			now := time.Now()
			t.CompletedAt = &now
			close(t.Done)

		case TaskRunning:
			// Check if the container still exists.
			if t.ContainerID == "" {
				t.Status = TaskFailed
				t.Error = "server restarted and container was lost"
				now := time.Now()
				t.CompletedAt = &now
				close(t.Done)
				break
			}

			result, err := sm.cm.CaptureContainerOutput(ctx, t.ContainerID)
			if err != nil {
				t.Status = TaskFailed
				t.Error = "server restarted and container was lost"
				if sm.logger != nil {
					sm.logger.Info(ctx, fmt.Sprintf("task %s: container %s not found after restart", t.ID, t.ContainerID))
				}
			} else if result.ExitCode == 0 {
				t.Status = TaskCompleted
				t.Result = result.Stdout
			} else {
				t.Status = TaskFailed
				t.Error = result.Stderr
				if t.Error == "" {
					t.Error = fmt.Sprintf("container exited with code %d", result.ExitCode)
				}
			}
			now := time.Now()
			t.CompletedAt = &now
			close(t.Done)

			// Clean up the container regardless of outcome.
			if t.ContainerID != "" {
				_ = sm.cm.StopAndRemoveContainer(ctx, t.ContainerID)
			}
		}

		sm.taskStore.Add(t)
	}
}

// recoverSessions reconciles persisted sessions against Docker.
func (sm *StateManager) recoverSessions(ctx context.Context, psessions []persistedSession) {
	for _, ps := range psessions {
		info, err := sm.cm.InspectContainer(ctx, ps.ContainerID)
		if err != nil || info.State == nil || !info.State.Running {
			// Container gone — session is terminated, don't restore.
			if sm.logger != nil {
				sm.logger.Info(ctx, fmt.Sprintf("session %s: container %s not running after restart, discarding", ps.ID, ps.ContainerID))
			}
			continue
		}

		// Container still running — re-register the session as active (no WebSocket yet).
		sess := &Session{
			ID:            ps.ID,
			Agent:         ps.Agent,
			Model:         ps.Model,
			System:        ps.System,
			CreatedAt:     ps.CreatedAt,
			LastMessageAt: ps.LastMessageAt,
			ContainerID:   ps.ContainerID,
			Active:        true,
		}
		sm.sessionStore.Add(sess)
		if sm.logger != nil {
			sm.logger.Info(ctx, fmt.Sprintf("session %s: container %s re-attached after restart", ps.ID, ps.ContainerID))
		}
	}
}

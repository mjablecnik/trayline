package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"server/core"
	"server/docker"
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

// OutputStreamer is implemented by the session handler to re-attach output streaming
// after a server restart. Declared here to avoid an import cycle.
type OutputStreamer interface {
	StreamOutput(ctx context.Context, sessionID string, attached interface{})
}

// StateManager handles state persistence to disk and startup recovery.
type StateManager struct {
	stateDir     string
	taskStore    *TaskStore
	sessionStore *SessionStore
	cm           *docker.ContainerManager
	logger       *core.Logger
	sessionH     SessionOutputStreamer // set via SetSessionHandler before Recover is called
}

// SessionOutputStreamer is the minimal interface StateManager needs from the session handler.
type SessionOutputStreamer interface {
	StreamOutputForRecovery(ctx context.Context, sessionID string, attached interface{})
}

// NewStateManager creates a StateManager.
func NewStateManager(stateDir string, tasks *TaskStore, sessions *SessionStore, cm *docker.ContainerManager, logger *core.Logger) *StateManager {
	return &StateManager{
		stateDir:     stateDir,
		taskStore:    tasks,
		sessionStore: sessions,
		cm:           cm,
		logger:       logger,
	}
}

// SetSessionHandler injects the session handler used to re-attach recovered sessions.
// Must be called before Recover.
func (sm *StateManager) SetSessionHandler(h SessionOutputStreamer) {
	sm.sessionH = h
}

// EnsureStateDir verifies that the state directory exists and is writable,
// creating it if necessary.
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
			Valid:        t.Valid,
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
// sessions against the live Docker environment.
func (sm *StateManager) Recover(ctx context.Context) error {
	path := filepath.Join(sm.stateDir, stateFileName)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
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
			Valid:        pt.Valid,
			CreatedAt:    pt.CreatedAt,
			CompletedAt:  pt.CompletedAt,
			ContainerID:  pt.ContainerID,
			Done:         make(chan struct{}),
		}

		switch t.Status {
		case TaskCompleted, TaskFailed, TaskCancelled:
			close(t.Done)

		case TaskQueued:
			t.Status = TaskFailed
			t.Error = "server restarted before task could be executed"
			now := time.Now()
			t.CompletedAt = &now
			close(t.Done)

		case TaskRunning:
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
			if sm.logger != nil {
				sm.logger.Info(ctx, fmt.Sprintf("session %s: container %s not running after restart, discarding", ps.ID, ps.ContainerID))
			}
			continue
		}

		sessCtx, cancel := context.WithCancel(context.Background())

		attached, err := sm.cm.AttachChatContainer(sessCtx, ps.ContainerID)
		if err != nil {
			cancel()
			if sm.logger != nil {
				sm.logger.Info(ctx, fmt.Sprintf("session %s: failed to re-attach container %s after restart: %v", ps.ID, ps.ContainerID, err))
			}
			continue
		}

		slotAcquired := sm.cm.TryAcquireSlot()

		sess := &Session{
			ID:            ps.ID,
			Agent:         ps.Agent,
			Model:         ps.Model,
			System:        ps.System,
			CreatedAt:     ps.CreatedAt,
			LastMessageAt: ps.LastMessageAt,
			ContainerID:   ps.ContainerID,
			Active:        true,
			Ctx:           sessCtx,
			CancelFunc:    cancel,
			Stdin:         attached.Conn,
		}
		sm.sessionStore.Add(sess)

		if sm.sessionH != nil {
			sm.sessionH.StreamOutputForRecovery(sessCtx, ps.ID, attached)
		}

		go func(sessID, containerID string, releaseSlot bool) {
			<-sessCtx.Done()
			attached.Close()
			sm.cm.StopAndRemoveContainer(context.Background(), containerID)
			if releaseSlot {
				sm.cm.ReleaseChatSlot()
			}
			sm.sessionStore.Remove(sessID)
			_ = sm.Save()
		}(ps.ID, ps.ContainerID, slotAcquired)

		if sm.logger != nil {
			sm.logger.Info(ctx, fmt.Sprintf("session %s: container %s re-attached after restart", ps.ID, ps.ContainerID))
		}
	}
}

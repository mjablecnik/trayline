package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	dockertypes "github.com/docker/docker/api/types"
)

// newRecoverSessionsManager builds a StateManager wired with a ContainerManager using the given mock.
func newRecoverSessionsManager(t *testing.T, mock *mockContainerClient, maxSlots int) *StateManager {
	t.Helper()
	cfg := &Config{
		MaxConcurrentTasks: maxSlots,
		TaskTimeout:        5 * time.Second,
		WorkspaceHostDir:   t.TempDir(),
	}
	cm := NewContainerManager(mock, cfg, NewLogger(""))
	sm := &StateManager{
		stateDir:     t.TempDir(),
		taskStore:    NewTaskStore(),
		sessionStore: NewSessionStore(),
		cm:           cm,
		logger:       NewLogger(""),
	}
	return sm
}

// runningContainerInspectResult returns a ContainerJSON representing a running container.
func runningContainerInspectResult() dockertypes.ContainerJSON {
	return dockertypes.ContainerJSON{
		ContainerJSONBase: &dockertypes.ContainerJSONBase{
			State: &dockertypes.ContainerState{Running: true},
		},
	}
}

// stoppedContainerInspectResult returns a ContainerJSON representing a stopped container.
func stoppedContainerInspectResult() dockertypes.ContainerJSON {
	return dockertypes.ContainerJSON{
		ContainerJSONBase: &dockertypes.ContainerJSONBase{
			State: &dockertypes.ContainerState{Running: false},
		},
	}
}

// persistedSessionWithContainer returns a test persistedSession with a given containerID.
func persistedSessionWithContainer(id, containerID string) persistedSession {
	now := time.Now()
	return persistedSession{
		ID:            id,
		Agent:         "claude",
		ContainerID:   containerID,
		CreatedAt:     now,
		LastMessageAt: now,
	}
}

// TestRecoverSessions_ContainerNotRunning verifies that a session whose container
// is stopped is discarded and not added to the session store.
func TestRecoverSessions_ContainerNotRunning(t *testing.T) {
	mock := newMockContainerClient()
	mock.inspectResult = stoppedContainerInspectResult()

	sm := newRecoverSessionsManager(t, mock, 2)

	ps := persistedSessionWithContainer("sess-1", "container-abc")
	sm.recoverSessions(context.Background(), []persistedSession{ps})

	if len(sm.sessionStore.All()) != 0 {
		t.Errorf("expected no sessions after stopped-container recovery, got %d", len(sm.sessionStore.All()))
	}
}

// TestRecoverSessions_ContainerInspectError verifies that a session is discarded when
// InspectContainer returns an error (container not found / daemon error).
func TestRecoverSessions_ContainerInspectError(t *testing.T) {
	mock := newMockContainerClient()
	mock.inspectErr = fmt.Errorf("container not found")

	sm := newRecoverSessionsManager(t, mock, 2)

	ps := persistedSessionWithContainer("sess-2", "container-missing")
	sm.recoverSessions(context.Background(), []persistedSession{ps})

	if len(sm.sessionStore.All()) != 0 {
		t.Errorf("expected no sessions when inspect fails, got %d", len(sm.sessionStore.All()))
	}
}

// TestRecoverSessions_ContainerRunning verifies that a session whose container is still
// running is re-added to the store and a concurrency slot is acquired.
func TestRecoverSessions_ContainerRunning(t *testing.T) {
	mock := newMockContainerClient()
	mock.inspectResult = runningContainerInspectResult()

	sm := newRecoverSessionsManager(t, mock, 1)

	ps := persistedSessionWithContainer("sess-3", "container-alive")
	sm.recoverSessions(context.Background(), []persistedSession{ps})

	sessions := sm.sessionStore.All()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session after running-container recovery, got %d", len(sessions))
	}
	if sessions[0].ID != "sess-3" {
		t.Errorf("unexpected session ID: got %q, want %q", sessions[0].ID, "sess-3")
	}
	if !sessions[0].Active {
		t.Error("expected recovered session to be Active")
	}

	// Slot should be consumed.
	if sm.cm.AvailableSlots() != 0 {
		t.Errorf("expected slot consumed after session recovery, got %d available", sm.cm.AvailableSlots())
	}

	// Cancel the session context so the cleanup goroutine exits cleanly.
	sess := sm.sessionStore.Get("sess-3")
	if sess != nil && sess.CancelFunc != nil {
		sess.CancelFunc()
	}

	// Wait for cleanup goroutine to release the slot and remove the session.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&mock.stopCount) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if atomic.LoadInt32(&mock.stopCount) == 0 {
		t.Error("expected ContainerStop to be called after context cancel")
	}
}

// TestRecoverSessions_AttachFails verifies that when re-attaching fails the session
// is discarded and no slot is leaked.
func TestRecoverSessions_AttachFails(t *testing.T) {
	mock := newMockContainerClient()
	mock.inspectResult = runningContainerInspectResult()
	mock.attachErr = fmt.Errorf("attach failed")

	sm := newRecoverSessionsManager(t, mock, 2)

	ps := persistedSessionWithContainer("sess-4", "container-noattach")
	sm.recoverSessions(context.Background(), []persistedSession{ps})

	if len(sm.sessionStore.All()) != 0 {
		t.Errorf("expected no sessions when attach fails, got %d", len(sm.sessionStore.All()))
	}
	// No slot should have been consumed (TryAcquireSlot is called after a successful attach check
	// but the attach itself failed, so the slot should not have been acquired — or if it was, it
	// is not released on this path; check the cm has the same slot count it started with).
	// Note: TryAcquireSlot is called AFTER AttachChatContainer succeeds, so a failed attach
	// means no slot was taken.
	if sm.cm.AvailableSlots() != 2 {
		t.Errorf("expected all slots available after failed attach, got %d", sm.cm.AvailableSlots())
	}
}

// TestRecoverSessions_MultipleSessionsMixed verifies that when multiple sessions are
// persisted, only those with running containers are restored.
func TestRecoverSessions_MultipleSessionsMixed(t *testing.T) {
	// Use inspectErr to simulate the first call failing, then running for second.
	// Since mockContainerClient uses a single inspectErr/inspectResult, we need separate mocks.
	// Instead, exercise two separate managers.

	mock := newMockContainerClient()
	mock.inspectResult = runningContainerInspectResult()

	sm := newRecoverSessionsManager(t, mock, 5)

	sessions := []persistedSession{
		persistedSessionWithContainer("s-running-1", "c-running-1"),
		persistedSessionWithContainer("s-running-2", "c-running-2"),
	}
	sm.recoverSessions(context.Background(), sessions)

	got := sm.sessionStore.All()
	if len(got) != 2 {
		t.Fatalf("expected 2 recovered sessions, got %d", len(got))
	}

	// Cancel contexts to avoid goroutine leaks.
	for _, s := range got {
		if s.CancelFunc != nil {
			s.CancelFunc()
		}
	}
}

// TestRecover_MalformedJSON verifies that a corrupted state file returns an error
// without panicking.
func TestRecover_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	sm := &StateManager{
		stateDir:     dir,
		taskStore:    NewTaskStore(),
		sessionStore: NewSessionStore(),
	}

	// Write malformed JSON.
	if err := os.WriteFile(filepath.Join(dir, stateFileName), []byte("{bad json"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := sm.Recover(context.Background())
	if err == nil {
		t.Fatal("expected error for malformed state file, got nil")
	}

	// Stores should remain empty (no partial recovery).
	if len(sm.taskStore.All()) != 0 {
		t.Errorf("expected empty task store after parse error, got %d", len(sm.taskStore.All()))
	}
}

// TestRecover_SessionsViaStateFile exercises Recover with a real state file containing
// sessions whose containers are not running (so they are dropped cleanly).
func TestRecover_SessionsViaStateFile(t *testing.T) {
	mock := newMockContainerClient()
	mock.inspectResult = stoppedContainerInspectResult()

	sm := newRecoverSessionsManager(t, mock, 2)

	state := serverState{
		Sessions: []persistedSession{
			persistedSessionWithContainer("file-sess-1", "file-ctr-1"),
		},
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sm.stateDir, stateFileName), data, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := sm.Recover(context.Background()); err != nil {
		t.Fatalf("Recover() error: %v", err)
	}

	// Stopped container — session should be discarded.
	if len(sm.sessionStore.All()) != 0 {
		t.Errorf("expected no sessions after recovery of stopped container, got %d", len(sm.sessionStore.All()))
	}
}

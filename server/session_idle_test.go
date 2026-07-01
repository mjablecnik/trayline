package main

import (
	"context"
	"testing"
	"time"
)

func newIdleTestHandler(t *testing.T, timeout time.Duration) (*SessionStore, *SessionHandler) {
	t.Helper()
	cfg := &Config{
		SessionTimeout:     timeout,
		MaxConcurrentTasks: 2,
		TaskTimeout:        5 * time.Second,
		WorkspaceHostDir:   t.TempDir(),
	}
	store := NewSessionStore()
	mock := newMockContainerClient()
	cm := NewContainerManager(mock, cfg, NewLogger(""))
	h := NewSessionHandler(store, cm, NewLogger(""), cfg)
	return store, h
}

// TestCheckIdleSessions_StaleTerminated verifies that a session older than
// SessionTimeout has its context cancelled.
func TestCheckIdleSessions_StaleTerminated(t *testing.T) {
	store, h := newIdleTestHandler(t, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	sess := &Session{
		ID:            "stale-session",
		Agent:         "claude",
		CreatedAt:     time.Now().Add(-3 * time.Hour),
		LastMessageAt: time.Now().Add(-2 * time.Hour), // 2h ago, exceeds 1h timeout
		Active:        true,
		Ctx:           ctx,
		CancelFunc:    cancel,
	}
	store.Add(sess)

	h.checkIdleSessions()

	select {
	case <-ctx.Done():
		// expected: context was cancelled for the stale session
	default:
		t.Error("expected stale session's context to be cancelled")
	}
}

// TestCheckIdleSessions_ActiveUntouched verifies that a recently-active session
// is not terminated.
func TestCheckIdleSessions_ActiveUntouched(t *testing.T) {
	store, h := newIdleTestHandler(t, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sess := &Session{
		ID:            "active-session",
		Agent:         "kiro",
		CreatedAt:     time.Now().Add(-30 * time.Minute),
		LastMessageAt: time.Now().Add(-10 * time.Minute), // 10 min ago, within 1h timeout
		Active:        true,
		Ctx:           ctx,
		CancelFunc:    cancel,
	}
	store.Add(sess)

	h.checkIdleSessions()

	select {
	case <-ctx.Done():
		t.Error("expected active session's context to remain alive after checkIdleSessions")
	default:
		// expected
	}
}

// TestCheckIdleSessions_Mixed verifies mixed-age sessions: only stale ones are terminated.
func TestCheckIdleSessions_Mixed(t *testing.T) {
	store, h := newIdleTestHandler(t, time.Hour)

	staleCtx, staleCancel := context.WithCancel(context.Background())
	staleSess := &Session{
		ID:            "stale",
		Agent:         "claude",
		CreatedAt:     time.Now().Add(-2 * time.Hour),
		LastMessageAt: time.Now().Add(-90 * time.Minute), // 90 min ago > 1h timeout
		Active:        true,
		Ctx:           staleCtx,
		CancelFunc:    staleCancel,
	}
	store.Add(staleSess)

	activeCtx, activeCancel := context.WithCancel(context.Background())
	defer activeCancel()
	activeSess := &Session{
		ID:            "active",
		Agent:         "kiro",
		CreatedAt:     time.Now().Add(-30 * time.Minute),
		LastMessageAt: time.Now().Add(-5 * time.Minute), // 5 min ago, within timeout
		Active:        true,
		Ctx:           activeCtx,
		CancelFunc:    activeCancel,
	}
	store.Add(activeSess)

	h.checkIdleSessions()

	select {
	case <-staleCtx.Done():
		// expected
	default:
		t.Error("expected stale session to have its context cancelled")
	}

	select {
	case <-activeCtx.Done():
		t.Error("expected active session to remain alive")
	default:
		// expected
	}
}

// TestCheckIdleSessions_Empty verifies that an empty session store is a no-op (no panic).
func TestCheckIdleSessions_Empty(t *testing.T) {
	_, h := newIdleTestHandler(t, time.Hour)
	h.checkIdleSessions() // must not panic
}

// TestCheckIdleSessions_NilCancelFunc verifies no panic when CancelFunc is nil.
func TestCheckIdleSessions_NilCancelFunc(t *testing.T) {
	store, h := newIdleTestHandler(t, time.Hour)

	sess := &Session{
		ID:            "no-cancel",
		Agent:         "claude",
		CreatedAt:     time.Now().Add(-3 * time.Hour),
		LastMessageAt: time.Now().Add(-2 * time.Hour),
		Active:        true,
		CancelFunc:    nil, // no cancel func
	}
	store.Add(sess)

	h.checkIdleSessions() // must not panic
}

// TestCheckIdleSessions_MultipleStale verifies all stale sessions are terminated.
func TestCheckIdleSessions_MultipleStale(t *testing.T) {
	store, h := newIdleTestHandler(t, 30*time.Minute)

	ctxA, cancelA := context.WithCancel(context.Background())
	ctxB, cancelB := context.WithCancel(context.Background())

	for _, sess := range []*Session{
		{
			ID:            "stale-a",
			Agent:         "claude",
			LastMessageAt: time.Now().Add(-60 * time.Minute),
			Active:        true,
			Ctx:           ctxA,
			CancelFunc:    cancelA,
		},
		{
			ID:            "stale-b",
			Agent:         "kiro",
			LastMessageAt: time.Now().Add(-45 * time.Minute),
			Active:        true,
			Ctx:           ctxB,
			CancelFunc:    cancelB,
		},
	} {
		store.Add(sess)
	}

	h.checkIdleSessions()

	for _, ctx := range []context.Context{ctxA, ctxB} {
		select {
		case <-ctx.Done():
			// expected
		default:
			t.Error("expected stale session context to be cancelled")
		}
	}
}

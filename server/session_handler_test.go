package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// --- test helpers ---

func newTestSessionSetup(t *testing.T) (*SessionStore, *SessionHandler) {
	t.Helper()
	store := NewSessionStore()
	mock := newMockContainerClient()
	mock.createErr = context.DeadlineExceeded
	cfg := &Config{
		MaxConcurrentTasks: 2,
		TaskTimeout:        5 * time.Second,
		SessionTimeout:     24 * time.Hour,
		WorkspaceHostDir:   t.TempDir(),
	}
	cm := NewContainerManager(mock, cfg, NewLogger(""))
	h := NewSessionHandler(store, cm, NewLogger(""), cfg)
	return store, h
}

func newTestSessionServer(t *testing.T) (*httptest.Server, *SessionStore) {
	t.Helper()
	store, h := newTestSessionSetup(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /sessions", h.HandleGetSessions)
	mux.HandleFunc("POST /sessions/{id}/terminate", h.HandleTerminateSession)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, store
}

// --- Property 9: Session listing is ordered by last activity ---
// Feature: agent-api-server, Property 9: Session listing ordered by last activity

func TestPropertySessionListingOrderedByLastActivity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		store := NewSessionStore()
		cfg := &Config{
			MaxConcurrentTasks: 2,
			TaskTimeout:        5 * time.Second,
			SessionTimeout:     24 * time.Hour,
			WorkspaceHostDir:   "/tmp",
		}
		mock := newMockContainerClient()
		cm := NewContainerManager(mock, cfg, NewLogger(""))
		h := NewSessionHandler(store, cm, NewLogger(""), cfg)

		n := rapid.IntRange(0, 20).Draw(t, "n")
		base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

		for i := 0; i < n; i++ {
			offset := rapid.Int64Range(0, 1_000_000).Draw(t, "offset")
			sess := &Session{
				ID:            rapid.String().Draw(t, "id") + "_" + string(rune('A'+i)),
				Agent:         rapid.SampledFrom([]string{"kiro", "claude"}).Draw(t, "agent"),
				Model:         rapid.String().Draw(t, "model"),
				CreatedAt:     base,
				LastMessageAt: base.Add(time.Duration(offset) * time.Second),
				Active:        true,
			}
			store.Add(sess)
		}

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/sessions", nil)
		h.HandleGetSessions(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		type sessionSummary struct {
			SessionID     string    `json:"session_id"`
			Agent         string    `json:"agent"`
			Model         string    `json:"model,omitempty"`
			CreatedAt     time.Time `json:"created_at"`
			LastMessageAt time.Time `json:"last_message_at"`
		}
		var result []sessionSummary
		if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
			t.Fatalf("invalid JSON response: %v", err)
		}

		// Must have same count as sessions added.
		if len(result) != n {
			t.Fatalf("expected %d sessions, got %d", n, len(result))
		}

		// Must be sorted by last_message_at descending (ties OK in any order).
		for i := 1; i < len(result); i++ {
			if result[i].LastMessageAt.After(result[i-1].LastMessageAt) {
				t.Fatalf("sessions not sorted descending at index %d: %v > %v",
					i, result[i].LastMessageAt, result[i-1].LastMessageAt)
			}
		}

		// Each entry must have all required fields.
		for _, s := range result {
			if s.SessionID == "" {
				t.Fatal("session entry missing session_id")
			}
			if s.Agent == "" {
				t.Fatal("session entry missing agent")
			}
			if s.CreatedAt.IsZero() {
				t.Fatal("session entry missing created_at")
			}
			if s.LastMessageAt.IsZero() {
				t.Fatal("session entry missing last_message_at")
			}
		}
	})
}

// --- Unit tests ---

func TestGetSessionsEmpty(t *testing.T) {
	srv, _ := newTestSessionServer(t)

	resp, err := http.Get(srv.URL + "/sessions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result []interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected empty array, got %v", result)
	}
}

func TestTerminateSessionNotFound(t *testing.T) {
	srv, _ := newTestSessionServer(t)

	resp, err := http.Post(srv.URL+"/sessions/nonexistent/terminate", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestTerminateSessionNoConn(t *testing.T) {
	srv, store := newTestSessionServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sess := &Session{
		ID:            "test-session-1",
		Agent:         "kiro",
		CreatedAt:     time.Now(),
		LastMessageAt: time.Now(),
		Active:        true,
		Ctx:           ctx,
		CancelFunc:    cancel,
	}
	store.Add(sess)

	resp, err := http.Post(srv.URL+"/sessions/test-session-1/terminate", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["session_id"] != "test-session-1" {
		t.Fatalf("unexpected session_id: %v", result)
	}
	if result["status"] != "terminated" {
		t.Fatalf("unexpected status: %v", result)
	}
}

// --- isContextCompaction unit tests ---

func TestIsContextCompaction(t *testing.T) {
	trueInputs := []string{
		"context compacted",
		"Context Compacted",
		"CONTEXT COMPACTED",
		"compacting context",
		"Compacting Context",
		"now compacting context for session",
		"[info] context compacted (tokens saved: 5000)",
	}
	for _, input := range trueInputs {
		if !isContextCompaction(input) {
			t.Errorf("expected isContextCompaction(%q) = true, got false", input)
		}
	}

	falseInputs := []string{
		"",
		"ordinary output line",
		"context saved",
		"compaction done",
		"context: processing",
		"hello world",
	}
	for _, input := range falseInputs {
		if isContextCompaction(input) {
			t.Errorf("expected isContextCompaction(%q) = false, got true", input)
		}
	}
}

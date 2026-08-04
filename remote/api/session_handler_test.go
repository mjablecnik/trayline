package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"remote/core"
	"remote/docker"
	"remote/store"
)

// Regression: the dashboard's cross-project "all sessions" view needs to know
// which project each session belongs to, so GET /sessions must include it.
func TestHandleGetSessions_IncludesProject(t *testing.T) {
	logger := core.NewLogger("test-token")
	cfg := &core.Config{}
	cm := docker.NewContainerManager(noopContainerClient{}, cfg, logger)
	sessionStore := store.NewSessionStore()
	h := NewSessionHandler(sessionStore, cm, logger, cfg, nil)

	sessionStore.Add(&store.Session{ID: "sess-1", Agent: "claude", Project: "myproject"})
	sessionStore.Add(&store.Session{ID: "sess-2", Agent: "kiro"})

	req := httptest.NewRequest(http.MethodGet, "/sessions", nil)
	rec := httptest.NewRecorder()

	h.HandleGetSessions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var got []struct {
		SessionID string `json:"session_id"`
		Project   string `json:"project"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(got))
	}

	byID := map[string]string{}
	for _, s := range got {
		byID[s.SessionID] = s.Project
	}
	if byID["sess-1"] != "myproject" {
		t.Errorf("expected sess-1 project %q, got %q", "myproject", byID["sess-1"])
	}
	if byID["sess-2"] != "" {
		t.Errorf("expected sess-2 to have no project, got %q", byID["sess-2"])
	}
}

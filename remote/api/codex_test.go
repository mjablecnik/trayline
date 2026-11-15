package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"remote/core"
	"remote/docker"
	"remote/store"
	"strings"
	"testing"
)

func TestCodexTaskAccepted(t *testing.T) {
	h, _ := newTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/run", strings.NewReader(`{"agent":"codex","prompt":"Fix tests"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.HandlePostRun(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Codex task rejected: %d %s", rec.Code, rec.Body.String())
	}
}

func TestCodexSSEFiltersPrivateEventsAndReportsFailures(t *testing.T) {
	for _, test := range []struct {
		name, end string
		wantErr   bool
	}{
		{"completed", `{"type":"turn.completed"}`, false},
		{"failure", `{"type":"turn.failed","error":{"message":"limit"}}`, true},
		{"truncated", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := `{"type":"thread.started","thread_id":"private"}
{"type":"item.completed","item":{"type":"reasoning","text":"private reasoning"}}
{"type":"item.completed","item":{"type":"command_execution","text":"private command"}}
{"type":"item.completed","item":{"type":"agent_message","text":"hello"}}
` + test.end + "\n"
			rec := httptest.NewRecorder()
			writer, err := NewSSEWriter(rec, "test", "codex", 0)
			if err != nil {
				t.Fatal(err)
			}
			err = streamCodexChunks(context.Background(), strings.NewReader(input), writer)
			if (err != nil) != test.wantErr {
				t.Fatalf("err=%v, want error=%v", err, test.wantErr)
			}
			body := rec.Body.String()
			if !strings.Contains(body, "hello") || strings.Contains(body, "private") {
				t.Fatalf("unexpected SSE: %s", body)
			}
		})
	}
}

func TestCodexOpenAIModelRunsBufferedCompletion(t *testing.T) {
	srv := newOpenAITestServer(t, openaiServerOpts{})
	resp := chatRequest(t, srv, map[string]any{"model": "codex", "messages": userMessage("hi")})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Codex completion returned %d", resp.StatusCode)
	}
}

// A supported agent must reach the capacity check instead of a validation error.
func TestCodexGlobalChatPassesAgentValidation(t *testing.T) {
	cfg := &core.Config{}
	logger := core.NewLogger("")
	cm := docker.NewContainerManager(noopContainerClient{}, cfg, logger)
	h := NewSessionHandler(store.NewSessionStore(), cm, logger, cfg, nil)
	rec := httptest.NewRecorder()
	h.HandleChat(rec, httptest.NewRequest(http.MethodGet, "/chat?agent=codex", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want capacity response, got %d: %s", rec.Code, rec.Body.String())
	}
}

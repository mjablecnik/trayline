package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"remote/core"
	"remote/docker"
)

// --- ComposeMessages ---

func TestComposeMessages_SingleUser(t *testing.T) {
	system, prompt := ComposeMessages([]OpenAIMessage{
		{Role: "user", Content: "hello there"},
	})
	if system != "" {
		t.Errorf("expected empty system, got %q", system)
	}
	if prompt != "hello there" {
		t.Errorf("expected prompt to pass through directly, got %q", prompt)
	}
}

func TestComposeMessages_SystemExtraction(t *testing.T) {
	system, prompt := ComposeMessages([]OpenAIMessage{
		{Role: "system", Content: "be concise"},
		{Role: "system", Content: "answer in English"},
		{Role: "user", Content: "hi"},
	})
	if system != "be concise\nanswer in English" {
		t.Errorf("expected concatenated system messages, got %q", system)
	}
	if prompt != "hi" {
		t.Errorf("expected single user message passed through, got %q", prompt)
	}
}

func TestComposeMessages_MultiTurn(t *testing.T) {
	_, prompt := ComposeMessages([]OpenAIMessage{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "second"},
		{Role: "user", Content: "third"},
	})
	expected := "User:\nfirst\n\nAssistant:\nsecond\n\nUser:\nthird"
	if prompt != expected {
		t.Errorf("expected %q, got %q", expected, prompt)
	}
}

func TestComposeMessages_AdjacentSameRole(t *testing.T) {
	_, prompt := ComposeMessages([]OpenAIMessage{
		{Role: "user", Content: "one"},
		{Role: "user", Content: "two"},
	})
	expected := "User:\none\n\nUser:\ntwo"
	if prompt != expected {
		t.Errorf("expected %q, got %q", expected, prompt)
	}
}

// --- ModelRegistry ---

func TestModelRegistry_Resolve(t *testing.T) {
	r := NewModelRegistry("kiro:kiro:,claude-sonnet:claude:sonnet")
	entry, ok := r.Resolve("Claude-Sonnet")
	if !ok {
		t.Fatal("expected case-insensitive resolve to succeed")
	}
	if entry.Agent != "claude" || entry.Model != "sonnet" {
		t.Errorf("unexpected entry: %+v", entry)
	}
}

func TestModelRegistry_ResolveNotFound(t *testing.T) {
	r := NewModelRegistry("kiro:kiro:")
	if _, ok := r.Resolve("nonexistent"); ok {
		t.Error("expected resolve of unknown model to fail")
	}
}

func TestModelRegistry_EmptyConfig(t *testing.T) {
	r := NewModelRegistry("")
	entries := r.List()
	if len(entries) != 3 {
		t.Fatalf("expected 3 default entries, got %d", len(entries))
	}
	if _, ok := r.Resolve("kiro"); !ok {
		t.Error("expected default 'kiro' entry")
	}
	if _, ok := r.Resolve("claude"); !ok {
		t.Error("expected default 'claude' entry")
	}
	if _, ok := r.Resolve("claude-sonnet"); !ok {
		t.Error("expected default 'claude-sonnet' entry")
	}
}

// --- HandleChatCompletions: validation ---

func newTestOpenAIHandler(runner ContainerRunner) *OpenAIHandler {
	registry := NewModelRegistry("kiro:kiro:,claude:claude:")
	logger := core.NewLogger("")
	return NewOpenAIHandler(registry, runner, logger, 30*time.Second)
}

func doChatCompletionsRequest(h *OpenAIHandler, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(b))
	rec := httptest.NewRecorder()
	h.HandleChatCompletions(rec, req)
	return rec
}

func decodeOpenAIError(t *testing.T, rec *httptest.ResponseRecorder) OpenAIErrorResponse {
	t.Helper()
	var errResp OpenAIErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to decode error response: %v (body: %s)", err, rec.Body.String())
	}
	return errResp
}

func TestHandleChatCompletions_MissingModel(t *testing.T) {
	h := newTestOpenAIHandler(&fakeRunner{})
	rec := doChatCompletionsRequest(h, map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	errResp := decodeOpenAIError(t, rec)
	if errResp.Error.Param == nil || *errResp.Error.Param != "model" {
		t.Errorf("expected param \"model\", got %+v", errResp.Error.Param)
	}
}

func TestHandleChatCompletions_EmptyMessages(t *testing.T) {
	h := newTestOpenAIHandler(&fakeRunner{})
	rec := doChatCompletionsRequest(h, map[string]any{
		"model":    "kiro",
		"messages": []map[string]string{},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	errResp := decodeOpenAIError(t, rec)
	if errResp.Error.Param == nil || *errResp.Error.Param != "messages" {
		t.Errorf("expected param \"messages\", got %+v", errResp.Error.Param)
	}
}

func TestHandleChatCompletions_InvalidRole(t *testing.T) {
	h := newTestOpenAIHandler(&fakeRunner{})
	// Note: "developer" is *not* used here — it is a supported alias for
	// "system" (see TestIntegration_DeveloperRole), so it would not fail
	// validation.
	rec := doChatCompletionsRequest(h, map[string]any{
		"model":    "kiro",
		"messages": []map[string]string{{"role": "wizard", "content": "hi"}},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	errResp := decodeOpenAIError(t, rec)
	if errResp.Error.Param == nil || *errResp.Error.Param != "messages[0].role" {
		t.Errorf("expected param \"messages[0].role\", got %+v", errResp.Error.Param)
	}
}

func TestHandleChatCompletions_UnknownModel(t *testing.T) {
	h := newTestOpenAIHandler(&fakeRunner{})
	rec := doChatCompletionsRequest(h, map[string]any{
		"model":    "does-not-exist",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	errResp := decodeOpenAIError(t, rec)
	if errResp.Error.Code == nil || *errResp.Error.Code != "model_not_found" {
		t.Errorf("expected code \"model_not_found\", got %+v", errResp.Error.Code)
	}
}

func TestHandleChatCompletions_NonStreaming(t *testing.T) {
	runner := &fakeRunner{result: &docker.ContainerResult{Stdout: "hello from agent", ExitCode: 0}}
	h := newTestOpenAIHandler(runner)
	rec := doChatCompletionsRequest(h, map[string]any{
		"model":    "kiro",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	var resp OpenAIChatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Object != "chat.completion" {
		t.Errorf("expected object \"chat.completion\", got %q", resp.Object)
	}
	if !strings.HasPrefix(resp.ID, "chatcmpl-") {
		t.Errorf("expected ID to start with \"chatcmpl-\", got %q", resp.ID)
	}
	if resp.Model != "kiro" {
		t.Errorf("expected model \"kiro\", got %q", resp.Model)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	if resp.Choices[0].Message.Role != "assistant" {
		t.Errorf("expected assistant role, got %q", resp.Choices[0].Message.Role)
	}
	if resp.Choices[0].Message.Content != "hello from agent" {
		t.Errorf("expected content \"hello from agent\", got %q", resp.Choices[0].Message.Content)
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Errorf("expected finish_reason \"stop\", got %q", resp.Choices[0].FinishReason)
	}
	if resp.Usage.TotalTokens != resp.Usage.PromptTokens+resp.Usage.CompletionTokens {
		t.Errorf("expected total tokens to be the sum of prompt+completion, got %+v", resp.Usage)
	}
}

func TestHandleChatCompletions_IgnoredParams(t *testing.T) {
	runner := &fakeRunner{result: &docker.ContainerResult{Stdout: "ok", ExitCode: 0}}
	h := newTestOpenAIHandler(runner)
	temp := 0.9
	rec := doChatCompletionsRequest(h, map[string]any{
		"model":             "kiro",
		"messages":          []map[string]string{{"role": "user", "content": "hi"}},
		"temperature":       temp,
		"top_p":             0.5,
		"max_tokens":        100,
		"n":                 1,
		"presence_penalty":  0.1,
		"frequency_penalty": 0.1,
		"user":              "test-user",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (unrecognized OpenAI params should be accepted and ignored), got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

// --- Models endpoints ---

func TestHandleListModels(t *testing.T) {
	h := newTestOpenAIHandler(&fakeRunner{})
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	h.HandleListModels(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var list OpenAIModelList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if list.Object != "list" {
		t.Errorf("expected object \"list\", got %q", list.Object)
	}
	if len(list.Data) != 2 {
		t.Fatalf("expected 2 models, got %d", len(list.Data))
	}
	for _, m := range list.Data {
		if m.OwnedBy != "trayline" {
			t.Errorf("expected owned_by \"trayline\", got %q", m.OwnedBy)
		}
		if m.Object != "model" {
			t.Errorf("expected object \"model\", got %q", m.Object)
		}
	}
}

func TestHandleGetModel(t *testing.T) {
	h := newTestOpenAIHandler(&fakeRunner{})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models/{model_id}", h.HandleGetModel)

	req := httptest.NewRequest(http.MethodGet, "/v1/models/kiro", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	var model OpenAIModel
	if err := json.Unmarshal(rec.Body.Bytes(), &model); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if model.ID != "kiro" {
		t.Errorf("expected ID \"kiro\", got %q", model.ID)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/models/nonexistent", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	errResp := decodeOpenAIError(t, rec)
	if errResp.Error.Type != "invalid_request_error" {
		t.Errorf("expected type \"invalid_request_error\", got %q", errResp.Error.Type)
	}
}

// --- Helpers ---

func TestEstimateTokens(t *testing.T) {
	cases := []struct {
		text     string
		expected int
	}{
		{"", 0},
		{"ab", 1},
		{"abcd", 1},
		{"hello world", 3},
	}
	for _, c := range cases {
		if got := estimateTokens(c.text); got != c.expected {
			t.Errorf("estimateTokens(%q) = %d, want %d", c.text, got, c.expected)
		}
	}
}

func TestGenerateCompletionID(t *testing.T) {
	id := generateCompletionID()
	if !strings.HasPrefix(id, "chatcmpl-") {
		t.Fatalf("expected ID to start with \"chatcmpl-\", got %q", id)
	}
	suffix := strings.TrimPrefix(id, "chatcmpl-")
	if len(suffix) != 24 {
		t.Errorf("expected 24-character suffix, got %d chars (%q)", len(suffix), suffix)
	}

	id2 := generateCompletionID()
	if id == id2 {
		t.Error("expected two generated IDs to differ")
	}
}

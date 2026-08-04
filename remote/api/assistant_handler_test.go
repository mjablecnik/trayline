package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"remote/core"
)

func newTestAssistantHandler(t *testing.T) (*AssistantHandler, string) {
	t.Helper()
	dataDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataDir, "prompts"), 0755); err != nil {
		t.Fatalf("setup prompts dir: %v", err)
	}
	folderMgr := NewAssistantFolderManager(dataDir, core.NewLogger("test-token"))
	return &AssistantHandler{
		logger:    core.NewLogger("test-token"),
		folderMgr: folderMgr,
	}, dataDir
}

func TestHandleListPrompts_ReturnsSortedPrompts(t *testing.T) {
	h, dataDir := newTestAssistantHandler(t)
	promptsDir := filepath.Join(dataDir, "prompts")
	if err := os.WriteFile(filepath.Join(promptsDir, "b-second.md"), []byte("second"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(promptsDir, "a-first.txt"), []byte("first"), 0644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/assistant/prompts", nil)
	rec := httptest.NewRecorder()
	h.HandleListPrompts(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var prompts []starterPrompt
	if err := json.Unmarshal(rec.Body.Bytes(), &prompts); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(prompts) != 2 {
		t.Fatalf("expected 2 prompts, got %d", len(prompts))
	}
	if prompts[0].Filename != "a-first.txt" || prompts[1].Filename != "b-second.md" {
		t.Fatalf("expected alphabetical order, got %+v", prompts)
	}
	if prompts[0].DisplayName != "a first" {
		t.Fatalf("expected display name %q, got %q", "a first", prompts[0].DisplayName)
	}
}

func TestHandleGetPrompt_ReturnsContent(t *testing.T) {
	h, dataDir := newTestAssistantHandler(t)
	if err := os.WriteFile(filepath.Join(dataDir, "prompts", "hello.md"), []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/assistant/prompts/hello.md", nil)
	req.SetPathValue("filename", "hello.md")
	rec := httptest.NewRecorder()
	h.HandleGetPrompt(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var prompt starterPrompt
	if err := json.Unmarshal(rec.Body.Bytes(), &prompt); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if prompt.Content != "hello world" {
		t.Fatalf("expected content %q, got %q", "hello world", prompt.Content)
	}
}

func TestHandleGetPrompt_NotFound(t *testing.T) {
	h, _ := newTestAssistantHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/assistant/prompts/missing.md", nil)
	req.SetPathValue("filename", "missing.md")
	rec := httptest.NewRecorder()
	h.HandleGetPrompt(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleGetPrompt_InvalidFilename(t *testing.T) {
	h, _ := newTestAssistantHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/assistant/prompts/..%2Fetc%2Fpasswd", nil)
	req.SetPathValue("filename", "../etc/passwd")
	rec := httptest.NewRecorder()
	h.HandleGetPrompt(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandlePutPrompt_CreatesAndUpdates(t *testing.T) {
	h, dataDir := newTestAssistantHandler(t)

	body, _ := json.Marshal(putPromptRequest{Content: "my content"})
	req := httptest.NewRequest(http.MethodPut, "/assistant/prompts/new-prompt.md", bytes.NewReader(body))
	req.SetPathValue("filename", "new-prompt.md")
	rec := httptest.NewRecorder()
	h.HandlePutPrompt(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	written, err := os.ReadFile(filepath.Join(dataDir, "prompts", "new-prompt.md"))
	if err != nil {
		t.Fatalf("expected file to be written: %v", err)
	}
	if string(written) != "my content" {
		t.Fatalf("expected written content %q, got %q", "my content", written)
	}

	var prompt starterPrompt
	if err := json.Unmarshal(rec.Body.Bytes(), &prompt); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if prompt.DisplayName != "new prompt" {
		t.Fatalf("expected display name %q, got %q", "new prompt", prompt.DisplayName)
	}
}

func TestHandlePutPrompt_RejectsOversizedContent(t *testing.T) {
	h, _ := newTestAssistantHandler(t)

	body, _ := json.Marshal(putPromptRequest{Content: strings.Repeat("x", maxPromptContentLen+1)})
	req := httptest.NewRequest(http.MethodPut, "/assistant/prompts/big.md", bytes.NewReader(body))
	req.SetPathValue("filename", "big.md")
	rec := httptest.NewRecorder()
	h.HandlePutPrompt(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandlePutPrompt_InvalidFilename(t *testing.T) {
	h, _ := newTestAssistantHandler(t)

	body, _ := json.Marshal(putPromptRequest{Content: "x"})
	req := httptest.NewRequest(http.MethodPut, "/assistant/prompts/bad%2Fname.md", bytes.NewReader(body))
	req.SetPathValue("filename", "bad/name.md")
	rec := httptest.NewRecorder()
	h.HandlePutPrompt(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleDeletePrompt_RemovesFile(t *testing.T) {
	h, dataDir := newTestAssistantHandler(t)
	promptPath := filepath.Join(dataDir, "prompts", "to-delete.md")
	if err := os.WriteFile(promptPath, []byte("bye"), 0644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/assistant/prompts/to-delete.md", nil)
	req.SetPathValue("filename", "to-delete.md")
	rec := httptest.NewRecorder()
	h.HandleDeletePrompt(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(promptPath); !os.IsNotExist(err) {
		t.Fatalf("expected file to be removed, stat err = %v", err)
	}
}

func TestHandleDeletePrompt_NotFound(t *testing.T) {
	h, _ := newTestAssistantHandler(t)

	req := httptest.NewRequest(http.MethodDelete, "/assistant/prompts/missing.md", nil)
	req.SetPathValue("filename", "missing.md")
	rec := httptest.NewRecorder()
	h.HandleDeletePrompt(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

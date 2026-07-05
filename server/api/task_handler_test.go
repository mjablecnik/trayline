package api

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"server/core"
	"server/docker"
	"server/store"
)

// fakeRunner is a ContainerRunner that returns a fixed result immediately.
type fakeRunner struct {
	result *docker.ContainerResult
	err    error
}

func (f *fakeRunner) RunOneShot(_ context.Context, _, _, _, _ string, _ time.Time) (*docker.ContainerResult, error) {
	return f.result, f.err
}

func newTestHandler(t *testing.T) (*TaskHandler, string) {
	t.Helper()
	dir := t.TempDir()
	ts := store.NewTaskStore()
	logger := core.NewLogger("test-token")
	runner := &fakeRunner{result: &docker.ContainerResult{Stdout: "ok", ExitCode: 0}}
	return NewTaskHandler(ts, runner, logger, nil, dir, MaxUploadFileSize, MaxUploadFileCount), dir
}

// buildMultipartBody creates a multipart/form-data body with given fields and optional file content.
func buildMultipartBody(t *testing.T, fields map[string]string, filename, fileContent string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatalf("WriteField %q: %v", k, err)
		}
	}
	if filename != "" {
		fw, err := mw.CreateFormFile("files", filename)
		if err != nil {
			t.Fatalf("CreateFormFile: %v", err)
		}
		fw.Write([]byte(fileContent))
	}
	mw.Close()
	return &buf, mw.FormDataContentType()
}

// TestHandlePostRun_MultipartHappyPath: multipart with prompt, agent, and one file
// should return 200 (task completed inline) with upload metadata prepended to the prompt.
func TestHandlePostRun_MultipartHappyPath(t *testing.T) {
	h, _ := newTestHandler(t)

	body, ct := buildMultipartBody(t, map[string]string{
		"prompt": "Analyze this file",
		"agent":  "kiro",
	}, "data.txt", "col1,col2\n1,2")

	req := httptest.NewRequest(http.MethodPost, "/run", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	h.HandlePostRun(rec, req)

	if rec.Code != http.StatusOK && rec.Code != http.StatusAccepted {
		t.Fatalf("expected 200 or 202, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if rec.Code == http.StatusOK {
		// Task completed inline — verify the task's prompt had metadata prepended.
		// We check via the task store: find the single task.
		all := h.store.List()
		if len(all) != 1 {
			t.Fatalf("expected 1 task in store, got %d", len(all))
		}
		task := all[0]
		if !strings.Contains(task.Prompt, "[Uploaded Files]") {
			t.Fatalf("expected prompt to contain upload metadata, got: %q", task.Prompt)
		}
		if !strings.Contains(task.Prompt, "data.txt") {
			t.Fatalf("expected prompt to contain filename 'data.txt', got: %q", task.Prompt)
		}
		if !strings.HasSuffix(task.Prompt, "Analyze this file") {
			t.Fatalf("expected original prompt at end, got: %q", task.Prompt)
		}
	}
}

// TestHandlePostRun_MultipartMissingPrompt: multipart without prompt should return 400.
func TestHandlePostRun_MultipartMissingPrompt(t *testing.T) {
	h, _ := newTestHandler(t)

	body, ct := buildMultipartBody(t, map[string]string{"agent": "kiro"}, "", "")

	req := httptest.NewRequest(http.MethodPost, "/run", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	h.HandlePostRun(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	var errResp core.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp.Error != "VALIDATION_ERROR" {
		t.Fatalf("expected VALIDATION_ERROR, got %q", errResp.Error)
	}
	if !strings.Contains(errResp.Message, "prompt is required") {
		t.Fatalf("expected 'prompt is required' in message, got %q", errResp.Message)
	}
}

// TestHandlePostRun_JSONBackwardCompat: application/json body should behave exactly as before.
func TestHandlePostRun_JSONBackwardCompat(t *testing.T) {
	h, _ := newTestHandler(t)

	body, _ := json.Marshal(RunRequest{Prompt: "Hello", Agent: "claude"})
	req := httptest.NewRequest(http.MethodPost, "/run", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.HandlePostRun(rec, req)

	if rec.Code != http.StatusOK && rec.Code != http.StatusAccepted {
		t.Fatalf("expected 200 or 202, got %d: %s", rec.Code, rec.Body.String())
	}
}

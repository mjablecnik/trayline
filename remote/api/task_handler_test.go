package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"remote/core"
	"remote/docker"
	"remote/store"
)

// fakeRunner is a ContainerRunner that returns a fixed result immediately.
type fakeRunner struct {
	result      *docker.ContainerResult
	err         error
	containerID string // if set, reported to onStart before returning
	panic       any    // if set, RunOneShot panics with this value instead of returning

	stoppedContainerIDs []string
}

func (f *fakeRunner) RunOneShot(_ context.Context, _, _, _, _ string, _ time.Time, onStart func(string)) (*docker.ContainerResult, error) {
	if f.containerID != "" && onStart != nil {
		onStart(f.containerID)
	}
	if f.panic != nil {
		panic(f.panic)
	}
	return f.result, f.err
}

func (f *fakeRunner) RunOneShotStreaming(_ context.Context, _, _, _, _ string, _ time.Time) (*docker.OneShotStream, error) {
	return nil, errors.New("fakeRunner: RunOneShotStreaming not implemented")
}

func (f *fakeRunner) StopAndRemoveContainer(_ context.Context, containerID string) error {
	f.stoppedContainerIDs = append(f.stoppedContainerIDs, containerID)
	return nil
}

func newTestHandler(t *testing.T) (*TaskHandler, string) {
	t.Helper()
	dir := t.TempDir()
	ts := store.NewTaskStore()
	logger := core.NewLogger("test-token")
	runner := &fakeRunner{result: &docker.ContainerResult{Stdout: "ok", ExitCode: 0}}
	return NewTaskHandler(ts, runner, logger, nil, dir, MaxUploadFileSize, MaxUploadFileCount, 32000), dir
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

// TestExecuteTask_PersistsContainerIDOnStart verifies that the container ID reported by
// RunOneShot's onStart callback is written to the task, so a server restart mid-run can
// find and clean up the container (see store.recoverTasks).
func TestExecuteTask_PersistsContainerIDOnStart(t *testing.T) {
	dir := t.TempDir()
	ts := store.NewTaskStore()
	logger := core.NewLogger("test-token")
	runner := &fakeRunner{result: &docker.ContainerResult{Stdout: "ok", ExitCode: 0}, containerID: "container-123"}
	h := NewTaskHandler(ts, runner, logger, nil, dir, MaxUploadFileSize, MaxUploadFileCount, 32000)

	task := &store.Task{ID: "task-1", Status: store.TaskQueued, Agent: "claude", Prompt: "hi", CreatedAt: time.Now(), Done: make(chan struct{})}
	ts.Add(task)

	h.executeTask(context.Background(), task)

	got := ts.Get("task-1")
	if got.Status != store.TaskCompleted {
		t.Fatalf("expected completed, got %s (%s)", got.Status, got.Error)
	}
	if got.ContainerID != "container-123" {
		t.Fatalf("expected container id to be persisted on the task, got %q", got.ContainerID)
	}
}

// TestExecuteTask_RecoversFromPanicAndCleansUpContainer verifies that a panic inside
// RunOneShot doesn't crash the server: the task is marked failed, task.Done is still
// closed (so HTTP long-pollers don't hang), and the already-started container is stopped.
func TestExecuteTask_RecoversFromPanicAndCleansUpContainer(t *testing.T) {
	dir := t.TempDir()
	ts := store.NewTaskStore()
	logger := core.NewLogger("test-token")
	runner := &fakeRunner{containerID: "container-456", panic: "boom"}
	h := NewTaskHandler(ts, runner, logger, nil, dir, MaxUploadFileSize, MaxUploadFileCount, 32000)

	task := &store.Task{ID: "task-2", Status: store.TaskQueued, Agent: "claude", Prompt: "hi", CreatedAt: time.Now(), Done: make(chan struct{})}
	ts.Add(task)

	h.executeTask(context.Background(), task)

	got := ts.Get("task-2")
	if got.Status != store.TaskFailed {
		t.Fatalf("expected failed after panic, got %s", got.Status)
	}
	if !strings.Contains(got.Error, "boom") {
		t.Fatalf("expected error to mention panic value, got %q", got.Error)
	}
	select {
	case <-task.Done:
	default:
		t.Fatal("expected task.Done to be closed after panic recovery")
	}
	if len(runner.stoppedContainerIDs) != 1 || runner.stoppedContainerIDs[0] != "container-456" {
		t.Fatalf("expected container-456 to be stopped/removed after panic, got %v", runner.stoppedContainerIDs)
	}
}

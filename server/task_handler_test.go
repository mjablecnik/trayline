package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"pgregory.net/rapid"
)

// --- test helpers ---

// newTestHandlerSetup creates a TaskStore and TaskHandler backed by a failing mock container.
// The mock container fails to create (simulating no Docker daemon), so tasks fail fast,
// allowing the long-poll to return 200 quickly in handler tests.
func newTestHandlerSetup(t *testing.T) (*TaskStore, *TaskHandler) {
	t.Helper()
	store := NewTaskStore()
	mock := newMockContainerClient()
	// Make containers fail immediately so tasks complete (failed) before the 30s poll.
	mock.createErr = context.DeadlineExceeded // non-Canceled error → "failed" status
	cfg := &Config{
		MaxConcurrentTasks: 2,
		TaskTimeout:        5 * time.Second,
		WorkspaceHostDir:   t.TempDir(),
	}
	cm := NewContainerManager(mock, cfg, NewLogger(""))
	h := NewTaskHandler(store, cm, NewLogger(""))
	return store, h
}

// newTestServer creates a test HTTP server wired to all task handler routes.
func newTestServer(t *testing.T) (*httptest.Server, *TaskStore) {
	t.Helper()
	store, h := newTestHandlerSetup(t)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /run", h.HandlePostRun)
	mux.HandleFunc("GET /run/{id}", h.HandleGetRun)
	mux.HandleFunc("GET /runs", h.HandleGetRuns)
	mux.HandleFunc("POST /run/{id}/cancel", h.HandleCancelRun)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, store
}

// doPostRun posts a RunRequest body and returns the response.
func doPostRun(t *testing.T, srv *httptest.Server, body interface{}) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(srv.URL+"/run", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// --- Property 2: Request validation accepts valid requests and rejects invalid ones ---
// Feature: agent-api-server, Property 2: Request validation accepts valid and rejects invalid

func TestProperty2_RequestValidation(t *testing.T) {
	t.Run("valid requests accepted", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) {
			srv, _ := newTestServer(t)

			agent := rapid.SampledFrom([]string{"kiro", "claude"}).Draw(rt, "agent")
			prompt := rapid.StringN(1, 100, -1).Draw(rt, "prompt")

			resp := doPostRun(t, srv, RunRequest{Prompt: prompt, Agent: agent})
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
				t.Fatalf("valid request: expected 200 or 202, got %d", resp.StatusCode)
			}
		})
	})

	t.Run("empty prompt rejected", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) {
			srv, _ := newTestServer(t)

			agent := rapid.SampledFrom([]string{"kiro", "claude"}).Draw(rt, "agent")

			resp := doPostRun(t, srv, RunRequest{Prompt: "", Agent: agent})
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("empty prompt: expected 400, got %d", resp.StatusCode)
			}
		})
	})

	t.Run("prompt too long rejected", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) {
			srv, _ := newTestServer(t)

			agent := rapid.SampledFrom([]string{"kiro", "claude"}).Draw(rt, "agent")
			// Generate prompt length between maxPromptLen+1 and maxPromptLen+1000.
			extraLen := rapid.IntRange(1, 1000).Draw(rt, "extra")
			prompt := strings.Repeat("x", maxPromptLen+extraLen)

			resp := doPostRun(t, srv, RunRequest{Prompt: prompt, Agent: agent})
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("prompt too long: expected 400, got %d", resp.StatusCode)
			}
		})
	})

	t.Run("invalid agent rejected", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) {
			srv, _ := newTestServer(t)

			// Generate agent string that is neither "kiro" nor "claude".
			agent := rapid.StringN(0, 20, -1).Draw(rt, "agent")
			if agent == "kiro" || agent == "claude" {
				return // skip valid agents
			}

			resp := doPostRun(t, srv, RunRequest{Prompt: "hello", Agent: agent})
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("invalid agent %q: expected 400, got %d", agent, resp.StatusCode)
			}
		})
	})

	t.Run("invalid JSON rejected", func(t *testing.T) {
		srv, _ := newTestServer(t)

		resp, err := http.Post(srv.URL+"/run", "application/json", strings.NewReader("{not json}"))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid JSON: expected 400, got %d", resp.StatusCode)
		}
	})
}

// --- Property 3: Valid task submission creates a queued task ---
// Feature: agent-api-server, Property 3: Valid task submission creates queued task

func TestProperty3_ValidTaskCreation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		srv, _ := newTestServer(t)

		agent := rapid.SampledFrom([]string{"kiro", "claude"}).Draw(rt, "agent")
		prompt := rapid.StringN(1, 50, -1).Draw(rt, "prompt")

		before := time.Now()
		resp := doPostRun(t, srv, RunRequest{Prompt: prompt, Agent: agent})
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
			rt.Fatalf("expected 200 or 202, got %d", resp.StatusCode)
		}

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			rt.Fatalf("failed to decode response: %v", err)
		}

		// Property: ID is a non-empty string (UUID format).
		id, ok := result["id"].(string)
		if !ok || id == "" {
			rt.Fatal("expected non-empty string 'id' in response")
		}
		if _, err := uuid.Parse(id); err != nil {
			rt.Fatalf("id %q is not a valid UUID: %v", id, err)
		}

		// Property: returned agent matches the request.
		if gotAgent, ok := result["agent"].(string); !ok || gotAgent != agent {
			rt.Fatalf("expected agent %q, got %q", agent, gotAgent)
		}

		// Property: created_at is parseable and not before request time.
		createdAtStr, _ := result["created_at"].(string)
		createdAt, err := time.Parse(time.RFC3339Nano, createdAtStr)
		if err != nil {
			// Try RFC3339 without nanoseconds.
			createdAt, err = time.Parse(time.RFC3339, createdAtStr)
			if err != nil {
				rt.Fatalf("could not parse created_at %q: %v", createdAtStr, err)
			}
		}
		if createdAt.Before(before.Add(-time.Second)) {
			rt.Fatalf("created_at %v is before request time %v", createdAt, before)
		}
	})
}

// --- Property 4: Output format validation ---
// Feature: agent-api-server, Property 4: Output format validation

func TestProperty4_OutputFormatValidation(t *testing.T) {
	t.Run("json format", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) {
			output := rapid.String().Draw(rt, "output")
			valid := validateOutputFormat("json", output)

			var v interface{}
			expected := json.Unmarshal([]byte(output), &v) == nil
			if valid != expected {
				rt.Fatalf("json validation mismatch for output %q: got valid=%v, expected valid=%v",
					output, valid, expected)
			}
		})
	})

	t.Run("text format always valid", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) {
			output := rapid.String().Draw(rt, "output")
			if !validateOutputFormat("text", output) {
				rt.Fatalf("expected valid=true for text format, got false (output=%q)", output)
			}
		})
	})

	t.Run("markdown format always valid", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) {
			output := rapid.String().Draw(rt, "output")
			if !validateOutputFormat("markdown", output) {
				rt.Fatalf("expected valid=true for markdown format, got false (output=%q)", output)
			}
		})
	})
}

// --- Property 6: Task retrieval returns correct fields based on status ---
// Feature: agent-api-server, Property 6: Task retrieval returns correct fields

func TestProperty6_TaskRetrievalFields(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		_, h := newTestHandlerSetup(t)
		store := NewTaskStore()
		h.store = store

		status := rapid.SampledFrom([]TaskStatus{
			TaskQueued, TaskRunning, TaskCompleted, TaskFailed, TaskCancelled,
		}).Draw(rt, "status")
		outputFormat := rapid.SampledFrom([]string{"", "json", "text", "markdown"}).Draw(rt, "output_format")

		task := &Task{
			ID:           uuid.NewString(),
			Status:       status,
			Agent:        "claude",
			OutputFormat: outputFormat,
			CreatedAt:    time.Now(),
		}
		if isTerminalStatus(status) {
			now := time.Now()
			task.CompletedAt = &now
		}
		if status == TaskCompleted {
			task.Result = "some result"
			if outputFormat != "" {
				v := true
				task.Valid = &v
			}
		}
		if status == TaskFailed {
			task.Error = "some error"
		}
		store.Add(task)

		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/run/"+task.ID, nil)
		r.SetPathValue("id", task.ID)
		h.HandleGetRun(w, r)

		if w.Code != http.StatusOK {
			rt.Fatalf("expected 200, got %d", w.Code)
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			rt.Fatalf("failed to decode response: %v", err)
		}

		// Property: result is present only for completed tasks.
		_, hasResult := resp["result"]
		if status == TaskCompleted && !hasResult {
			rt.Fatal("expected 'result' field for completed task")
		}
		if status != TaskCompleted && hasResult {
			rt.Fatalf("unexpected 'result' field for status=%q", status)
		}

		// Property: error is present only for failed tasks.
		_, hasError := resp["error"]
		if status == TaskFailed && !hasError {
			rt.Fatal("expected 'error' field for failed task")
		}
		if status != TaskFailed && hasError {
			rt.Fatalf("unexpected 'error' field for status=%q", status)
		}

		// Property: valid is present only when completed AND output_format was specified.
		_, hasValid := resp["valid"]
		if status == TaskCompleted && outputFormat != "" && !hasValid {
			rt.Fatal("expected 'valid' field for completed task with output_format")
		}
		if (status != TaskCompleted || outputFormat == "") && hasValid {
			rt.Fatalf("unexpected 'valid' field for status=%q, output_format=%q", status, outputFormat)
		}

		// Property: completed_at is present only for terminal statuses.
		_, hasCompletedAt := resp["completed_at"]
		if isTerminalStatus(status) && !hasCompletedAt {
			rt.Fatalf("expected 'completed_at' for terminal status=%q", status)
		}
		if !isTerminalStatus(status) && hasCompletedAt {
			rt.Fatalf("unexpected 'completed_at' for non-terminal status=%q", status)
		}
	})
}

// --- Property 7: Task listing is ordered and bounded ---
// Feature: agent-api-server, Property 7: Task listing ordered and bounded

func TestProperty7_TaskListing(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		_, h := newTestHandlerSetup(t)
		store := NewTaskStore()
		h.store = store

		n := rapid.IntRange(0, 150).Draw(rt, "n")

		baseTime := time.Now()
		for i := 0; i < n; i++ {
			store.Add(&Task{
				ID:        uuid.NewString(),
				Status:    TaskQueued,
				Agent:     "claude",
				CreatedAt: baseTime.Add(time.Duration(i) * time.Millisecond),
			})
		}

		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/runs", nil)
		h.HandleGetRuns(w, r)

		if w.Code != http.StatusOK {
			rt.Fatalf("expected 200, got %d", w.Code)
		}

		var summaries []TaskSummary
		if err := json.Unmarshal(w.Body.Bytes(), &summaries); err != nil {
			rt.Fatalf("failed to decode response: %v", err)
		}

		// Property: at most 100 tasks returned.
		if len(summaries) > 100 {
			rt.Fatalf("expected at most 100 tasks, got %d", len(summaries))
		}

		// Property: correct count (capped at 100).
		expected := n
		if expected > 100 {
			expected = 100
		}
		if len(summaries) != expected {
			rt.Fatalf("expected %d tasks, got %d", expected, len(summaries))
		}

		// Property: ordered by created_at descending.
		for i := 1; i < len(summaries); i++ {
			if summaries[i-1].CreatedAt.Before(summaries[i].CreatedAt) {
				rt.Fatalf("tasks not ordered by created_at desc: index %d (%v) < index %d (%v)",
					i-1, summaries[i-1].CreatedAt, i, summaries[i].CreatedAt)
			}
		}
	})
}

// --- Property 8: Cancellation of terminal tasks is rejected ---
// Feature: agent-api-server, Property 8: Cancellation of terminal tasks rejected

func TestProperty8_CancelTerminalTasks(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		_, h := newTestHandlerSetup(t)
		store := NewTaskStore()
		h.store = store

		status := rapid.SampledFrom([]TaskStatus{
			TaskCompleted, TaskFailed, TaskCancelled,
		}).Draw(rt, "status")

		now := time.Now()
		task := &Task{
			ID:          uuid.NewString(),
			Status:      status,
			Agent:       "claude",
			CreatedAt:   time.Now().Add(-time.Minute),
			CompletedAt: &now,
		}
		store.Add(task)

		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/run/"+task.ID+"/cancel", nil)
		r.SetPathValue("id", task.ID)
		h.HandleCancelRun(w, r)

		// Property: cancelling a terminal task returns 409.
		if w.Code != http.StatusConflict {
			rt.Fatalf("expected 409 for terminal task with status=%q, got %d", status, w.Code)
		}

		// Property: task status is not modified.
		updated := store.Get(task.ID)
		if updated.Status != status {
			rt.Fatalf("expected status %q to remain unchanged, got %q", status, updated.Status)
		}
	})
}

// --- Unit tests for specific edge cases ---

func TestGetRunNotFound(t *testing.T) {
	_, h := newTestHandlerSetup(t)
	store := NewTaskStore()
	h.store = store

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/run/nonexistent", nil)
	r.SetPathValue("id", "nonexistent")
	h.HandleGetRun(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestCancelRunNotFound(t *testing.T) {
	_, h := newTestHandlerSetup(t)
	store := NewTaskStore()
	h.store = store

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/run/nonexistent/cancel", nil)
	r.SetPathValue("id", "nonexistent")
	h.HandleCancelRun(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestGetRunsEmpty(t *testing.T) {
	_, h := newTestHandlerSetup(t)
	store := NewTaskStore()
	h.store = store

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/runs", nil)
	h.HandleGetRuns(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result []interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected empty array, got %d items", len(result))
	}
}

func TestCancelQueuedTask(t *testing.T) {
	_, h := newTestHandlerSetup(t)
	store := NewTaskStore()
	h.store = store

	ctx, cancel := context.WithCancel(context.Background())
	task := &Task{
		ID:         uuid.NewString(),
		Status:     TaskQueued,
		Agent:      "claude",
		CreatedAt:  time.Now(),
		CancelFunc: cancel,
		Done:       make(chan struct{}),
	}
	store.Add(task)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/run/"+task.ID+"/cancel", nil)
	r.SetPathValue("id", task.ID)
	h.HandleCancelRun(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != string(TaskCancelled) {
		t.Fatalf("expected status 'cancelled', got %q", resp["status"])
	}
	if resp["id"] != task.ID {
		t.Fatalf("expected id %q, got %q", task.ID, resp["id"])
	}

	updated := store.Get(task.ID)
	if updated.Status != TaskCancelled {
		t.Fatalf("expected task status 'cancelled', got %q", updated.Status)
	}
	// ctx should have been cancelled.
	if ctx.Err() == nil {
		t.Fatal("expected context to be cancelled")
	}
}

// --- executeTask direct tests ---

// newExecuteTaskHandler builds a TaskHandler with a ContainerManager backed by the given mock.
func newExecuteTaskHandler(t *testing.T, mock *mockContainerClient) (*TaskStore, *TaskHandler) {
	t.Helper()
	cfg := &Config{
		MaxConcurrentTasks: 2,
		TaskTimeout:        5 * time.Second,
		WorkspaceHostDir:   t.TempDir(),
	}
	store := NewTaskStore()
	cm := NewContainerManager(mock, cfg, NewLogger(""))
	h := NewTaskHandler(store, cm, NewLogger(""))
	return store, h
}

// addAndRun creates a task, adds it to the store, runs executeTask in a goroutine,
// and waits for it to complete (Done closed).
func addAndRun(t *testing.T, h *TaskHandler, store *TaskStore, task *Task) {
	t.Helper()
	store.Add(task)
	go h.executeTask(context.Background(), task)
	select {
	case <-task.Done:
	case <-time.After(10 * time.Second):
		t.Fatal("executeTask did not complete within timeout")
	}
}

// TestExecuteTask_HappyPath verifies that a successful container run produces a
// completed task with Done closed.
func TestExecuteTask_HappyPath(t *testing.T) {
	mock := newMockContainerClient()
	mock.autoComplete = true // container exits immediately with code 0
	store, h := newExecuteTaskHandler(t, mock)

	task := &Task{
		ID:        "et-happy",
		Status:    TaskQueued,
		Agent:     "claude",
		Prompt:    "hello",
		CreatedAt: time.Now(),
		Done:      make(chan struct{}),
	}
	addAndRun(t, h, store, task)

	got := store.Get(task.ID)
	if got.Status != TaskCompleted {
		t.Errorf("expected TaskCompleted, got %s", got.Status)
	}
	if got.CompletedAt == nil {
		t.Error("expected CompletedAt set")
	}
}

// TestExecuteTask_NonZeroExit verifies that a non-zero container exit code produces a
// failed task with a non-empty error.
func TestExecuteTask_NonZeroExit(t *testing.T) {
	mock := newMockContainerClient()
	mock.autoComplete = true
	mock.autoExitCode = 1
	store, h := newExecuteTaskHandler(t, mock)

	task := &Task{
		ID:        "et-nz",
		Status:    TaskQueued,
		Agent:     "claude",
		Prompt:    "fail me",
		CreatedAt: time.Now(),
		Done:      make(chan struct{}),
	}
	addAndRun(t, h, store, task)

	got := store.Get(task.ID)
	if got.Status != TaskFailed {
		t.Errorf("expected TaskFailed, got %s", got.Status)
	}
	if got.Error == "" {
		t.Error("expected non-empty Error for non-zero exit")
	}
}

// TestExecuteTask_ContainerCreateError verifies that a container creation failure
// produces a failed task.
func TestExecuteTask_ContainerCreateError(t *testing.T) {
	mock := newMockContainerClient()
	mock.createErr = fmt.Errorf("docker disk full")
	store, h := newExecuteTaskHandler(t, mock)

	task := &Task{
		ID:        "et-cre",
		Status:    TaskQueued,
		Agent:     "claude",
		Prompt:    "hello",
		CreatedAt: time.Now(),
		Done:      make(chan struct{}),
	}
	addAndRun(t, h, store, task)

	got := store.Get(task.ID)
	if got.Status != TaskFailed {
		t.Errorf("expected TaskFailed after create error, got %s", got.Status)
	}
}

// TestExecuteTask_AlreadyCancelled verifies that a task already in Cancelled status
// exits executeTask immediately without transitioning to Running.
func TestExecuteTask_AlreadyCancelled(t *testing.T) {
	mock := newMockContainerClient()
	mock.autoComplete = true
	store, h := newExecuteTaskHandler(t, mock)

	task := &Task{
		ID:        "et-pre-cancel",
		Status:    TaskCancelled, // already cancelled before executeTask runs
		Agent:     "claude",
		Prompt:    "never runs",
		CreatedAt: time.Now(),
		Done:      make(chan struct{}),
	}
	addAndRun(t, h, store, task)

	got := store.Get(task.ID)
	if got.Status != TaskCancelled {
		t.Errorf("expected TaskCancelled, got %s", got.Status)
	}
}

// TestExecuteTask_JSONFormat verifies that output_format "json" causes validateOutputFormat
// to be invoked: Valid is set to &false when output is empty (not valid JSON).
func TestExecuteTask_JSONFormat(t *testing.T) {
	mock := newMockContainerClient()
	mock.autoComplete = true
	store, h := newExecuteTaskHandler(t, mock)

	task := &Task{
		ID:           "et-json",
		Status:       TaskQueued,
		Agent:        "claude",
		Prompt:       "return json",
		OutputFormat: "json",
		CreatedAt:    time.Now(),
		Done:         make(chan struct{}),
	}
	addAndRun(t, h, store, task)

	got := store.Get(task.ID)
	if got.Status != TaskCompleted {
		t.Errorf("expected TaskCompleted, got %s", got.Status)
	}
	if got.Valid == nil {
		t.Error("expected Valid to be set when OutputFormat is 'json'")
	}
	// Empty stdout is not valid JSON → Valid == false.
	if *got.Valid != false {
		t.Errorf("expected Valid=false for empty output with json format, got %v", *got.Valid)
	}
}

// TestExecuteTask_TextFormat verifies that output_format "text" sets Valid=&true
// (plain text always passes validation).
func TestExecuteTask_TextFormat(t *testing.T) {
	mock := newMockContainerClient()
	mock.autoComplete = true
	store, h := newExecuteTaskHandler(t, mock)

	task := &Task{
		ID:           "et-text",
		Status:       TaskQueued,
		Agent:        "claude",
		Prompt:       "return text",
		OutputFormat: "text",
		CreatedAt:    time.Now(),
		Done:         make(chan struct{}),
	}
	addAndRun(t, h, store, task)

	got := store.Get(task.ID)
	if got.Status != TaskCompleted {
		t.Errorf("expected TaskCompleted, got %s", got.Status)
	}
	if got.Valid == nil {
		t.Error("expected Valid to be set when OutputFormat is 'text'")
	}
	if !*got.Valid {
		t.Error("expected Valid=true for text format")
	}
}

// TestExecuteTask_DoneChannelCloses verifies that the Done channel is closed for all
// terminal states (completed, failed, already-cancelled).
func TestExecuteTask_DoneChannelCloses(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      TaskStatus
		createErr   error
		autoComplete bool
	}{
		{"completed", TaskQueued, nil, true},
		{"failed-create", TaskQueued, fmt.Errorf("err"), false},
		{"pre-cancelled", TaskCancelled, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mock := newMockContainerClient()
			mock.autoComplete = tc.autoComplete
			mock.createErr = tc.createErr
			store, h := newExecuteTaskHandler(t, mock)

			task := &Task{
				ID:        "et-done-" + tc.name,
				Status:    tc.status,
				Agent:     "claude",
				Prompt:    "p",
				CreatedAt: time.Now(),
				Done:      make(chan struct{}),
			}
			addAndRun(t, h, store, task)

			// Done already closed by addAndRun; verify non-blocking receive.
			select {
			case <-task.Done:
			default:
				t.Error("expected Done channel closed")
			}
		})
	}
}

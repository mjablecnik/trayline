package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// newTestHandler returns a Handler wired to a fresh in-memory Queue and a
// Worker backed by a fakeRunner, along with the ServeMux it registered on.
// stateFile is left empty so no test writes to disk.
func newTestHandler() (*Handler, *Queue, *Worker, *fakeRunner, *http.ServeMux) {
	q := newTestQueue()
	runner := newFakeRunner()
	notifier := &fakeNotifier{}
	w := NewWorker(q, runner, notifier, "", &bytes.Buffer{})
	h := NewHandler(q, w, "")

	mux := http.NewServeMux()
	h.Register(mux)
	return h, q, w, runner, mux
}

// pollUntil polls cond until it returns true or timeout elapses, returning
// the final result. Used inside rapid.Check where only a *rapid.T (not a
// *testing.T) is available.
func pollUntil(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return cond()
}

func doRequest(mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	var reqBody *bytes.Reader
	if body == "" {
		reqBody = bytes.NewReader(nil)
	} else {
		reqBody = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, reqBody)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// Feature: taskline, Property 4: Command validation rejects empty and whitespace
//
// For any string composed entirely of whitespace characters (or empty),
// submitting it as the command field is rejected with HTTP 400. For any
// string containing at least one non-whitespace character, the command is
// accepted.
func TestProperty_CommandValidationRejectsEmptyAndWhitespace(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		useWhitespace := rapid.Bool().Draw(t, "useWhitespace")

		var command string
		if useWhitespace {
			command = rapid.SampledFrom([]string{"", " ", "\t", "\n", "  \t\n  "}).Draw(t, "command")
		} else {
			command = genCommand(t, "command")
		}

		_, _, _, _, mux := newTestHandler()
		body, err := json.Marshal(createTaskRequest{Command: command})
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		rec := doRequest(mux, http.MethodPost, "/tasks", string(body))

		if strings.TrimSpace(command) == "" {
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for command %q, got %d: %s", command, rec.Code, rec.Body.String())
			}
		} else {
			if rec.Code != http.StatusCreated {
				t.Fatalf("expected 201 for command %q, got %d: %s", command, rec.Code, rec.Body.String())
			}
		}
	})
}

// Feature: taskline, Property 12: Queue status response structure
//
// For any queue state, the status response includes state and pendingCount.
// If running, currentTask is present. If halted, failedTask is present. If
// idle, neither object is present and pendingCount is 0.
func TestProperty_QueueStatusResponseStructure(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		scenario := rapid.SampledFrom([]string{"idle", "running", "halted"}).Draw(t, "scenario")
		pendingExtra := rapid.IntRange(0, 4).Draw(t, "pendingExtra")

		_, q, w, runner, mux := newTestHandler()

		switch scenario {
		case "running":
			proc := newFakeProcess(0)
			runner.enqueue("sleep 100", proc)
			if _, err := q.AddTask("sleep 100", "", nil); err != nil {
				t.Fatalf("AddTask: %v", err)
			}
			go w.Run()
			t.Cleanup(w.Shutdown)
			if !pollUntil(time.Second, func() bool {
				return q.CurrentState() == QueueRunning && q.CurrentTask() != nil
			}) {
				t.Fatalf("task never started running")
			}
			for i := 0; i < pendingExtra; i++ {
				proc := newFakeProcess(0)
				cmd := rapid.SampledFrom([]string{"a", "b", "c", "d"}).Draw(t, "extraCmd")
				runner.enqueue(cmd, proc)
				_, _ = q.AddTask(cmd, "", nil)
			}
		case "halted":
			proc := newFakeProcess(1)
			proc.finish()
			runner.enqueue("false", proc)
			if _, err := q.AddTask("false", "", nil); err != nil {
				t.Fatalf("AddTask: %v", err)
			}
			go w.Run()
			t.Cleanup(w.Shutdown)
			if !pollUntil(time.Second, func() bool {
				return q.CurrentState() == QueueHalted
			}) {
				t.Fatalf("task never halted")
			}
		case "idle":
			// leave the queue empty
		}

		rec := doRequest(mux, http.MethodGet, "/queue/status", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp queueStatusResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}

		switch resp.State {
		case string(QueueRunning):
			if resp.CurrentTask == nil {
				t.Fatalf("expected currentTask present when running, body=%s", rec.Body.String())
			}
			if resp.FailedTask != nil {
				t.Fatalf("expected no failedTask when running, body=%s", rec.Body.String())
			}
		case string(QueueHalted):
			if resp.FailedTask == nil {
				t.Fatalf("expected failedTask present when halted, body=%s", rec.Body.String())
			}
			if resp.CurrentTask != nil {
				t.Fatalf("expected no currentTask when halted, body=%s", rec.Body.String())
			}
		case string(QueueIdle):
			if resp.CurrentTask != nil || resp.FailedTask != nil {
				t.Fatalf("expected no task objects when idle, body=%s", rec.Body.String())
			}
			if resp.PendingCount != 0 {
				t.Fatalf("expected pendingCount 0 when idle, got %d", resp.PendingCount)
			}
		default:
			t.Fatalf("unexpected state %q", resp.State)
		}
		if resp.PendingCount < 0 {
			t.Fatalf("pendingCount must be non-negative, got %d", resp.PendingCount)
		}
	})
}

func TestHandleCreateTask_MalformedJSON(t *testing.T) {
	_, _, _, _, mux := newTestHandler()
	rec := doRequest(mux, http.MethodPost, "/tasks", "{not json")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	var resp errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if resp.Error != "VALIDATION_ERROR" {
		t.Fatalf("expected VALIDATION_ERROR, got %q", resp.Error)
	}
}

func TestHandleCreateTask_DuplicateNameConflict(t *testing.T) {
	_, _, _, _, mux := newTestHandler()
	body := `{"command":"echo hi","name":"brave-tiger"}`
	rec := doRequest(mux, http.MethodPost, "/tasks", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doRequest(mux, http.MethodPost, "/tasks", body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleListTasks_EmptyQueueReturnsEmptyArray(t *testing.T) {
	_, _, _, _, mux := newTestHandler()
	rec := doRequest(mux, http.MethodGet, "/tasks", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("expected empty JSON array, got %q", rec.Body.String())
	}
}

func TestHandleDeleteTask_NotFound(t *testing.T) {
	_, _, _, _, mux := newTestHandler()
	rec := doRequest(mux, http.MethodDelete, "/tasks/does-not-exist", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandleDeleteTask_RunningTaskConflict(t *testing.T) {
	_, q, w, runner, mux := newTestHandler()
	proc := newFakeProcess(0)
	runner.enqueue("sleep 100", proc)
	task, err := q.AddTask("sleep 100", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	go w.Run()
	defer w.Shutdown()
	waitFor(t, time.Second, func() bool {
		got, err := q.Snapshot(task.ID)
		return err == nil && got.Status == TaskRunning
	})

	rec := doRequest(mux, http.MethodDelete, "/tasks/"+task.ID, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleUpdateTask_NoFieldsRejected(t *testing.T) {
	_, q, _, _, mux := newTestHandler()
	task, err := q.AddTask("echo hi", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	rec := doRequest(mux, http.MethodPatch, "/tasks/"+task.ID, `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleUpdateTask_AppliesFields(t *testing.T) {
	_, q, _, _, mux := newTestHandler()
	task, err := q.AddTask("echo hi", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	rec := doRequest(mux, http.MethodPatch, "/tasks/"+task.ID, `{"command":"echo bye"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp taskResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Command != "echo bye" {
		t.Fatalf("expected updated command, got %q", resp.Command)
	}
	if resp.Name != task.Name {
		t.Fatalf("expected unchanged name %q, got %q", task.Name, resp.Name)
	}
}

func TestHandleRetry_NoFailedTaskConflict(t *testing.T) {
	_, _, _, _, mux := newTestHandler()
	rec := doRequest(mux, http.MethodPost, "/tasks/retry", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSkip_NoFailedTaskConflict(t *testing.T) {
	_, _, _, _, mux := newTestHandler()
	rec := doRequest(mux, http.MethodPost, "/tasks/skip", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleResume_EmptyQueueReturnsIdle(t *testing.T) {
	_, _, _, _, mux := newTestHandler()
	rec := doRequest(mux, http.MethodPost, "/queue/resume", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp queueActionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.State != string(QueueIdle) {
		t.Fatalf("expected idle state, got %q", resp.State)
	}
}

func TestHandleResume_AlreadyRunningConflict(t *testing.T) {
	_, _, _, runner, mux := newTestHandler()
	runner.enqueue("sleep 100", newFakeProcess(0))
	rec := doRequest(mux, http.MethodPost, "/tasks", `{"command":"sleep 100"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doRequest(mux, http.MethodPost, "/queue/resume", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleStop_Success(t *testing.T) {
	_, q, w, runner, mux := newTestHandler()
	proc := newFakeProcess(0)
	proc.exitOnTerm = true
	runner.enqueue("sleep 100", proc)
	task, err := q.AddTask("sleep 100", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	go w.Run()
	defer w.Shutdown()
	waitFor(t, time.Second, func() bool {
		got, err := q.Snapshot(task.ID)
		return err == nil && got.Status == TaskRunning
	})

	rec := doRequest(mux, http.MethodPost, "/tasks/stop", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp taskResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.ID != task.ID {
		t.Fatalf("expected stopped task %s, got %s", task.ID, resp.ID)
	}
	if resp.Status != string(TaskFailed) {
		t.Fatalf("expected status failed, got %q", resp.Status)
	}
	if resp.ExitCode == nil || *resp.ExitCode != ExitCodeStopped {
		t.Fatalf("expected exit code %d, got %v", ExitCodeStopped, resp.ExitCode)
	}
}

func TestHandleStop_NoRunningTaskConflict(t *testing.T) {
	_, _, _, _, mux := newTestHandler()
	rec := doRequest(mux, http.MethodPost, "/tasks/stop", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleHealth(t *testing.T) {
	_, _, _, _, mux := newTestHandler()
	rec := doRequest(mux, http.MethodGet, "/health", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) != `{"status":"ok"}` {
		t.Fatalf("expected status ok body, got %q", rec.Body.String())
	}
}

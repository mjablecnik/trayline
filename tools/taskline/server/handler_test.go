package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// testProject is the project name used by every test in this file that
// doesn't care about multi-project behavior specifically (that's covered by
// registry_test.go and the project-scoped tests below).
const testProject = "testproject"

// projectPath prefixes suffix with /projects/{testProject}, mirroring how
// the real API scopes every task/queue/logs route to a project (FR-4.1,
// FR-4.2).
func projectPath(suffix string) string {
	return "/projects/" + testProject + suffix
}

// setInstance registers inst directly in r's project map, bypassing
// GetOrCreate's directory creation, state loading, and automatic
// Worker.Run goroutine start, so tests can drive a ProjectInstance's Queue
// and Worker directly (same pattern the pre-multi-project tests used).
func setInstance(r *Registry, name string, inst *ProjectInstance) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.projects[name] = inst
}

// newTestRegistryAndInstance builds a Registry backed by throwaway temp
// directories plus a single ProjectInstance registered under testProject,
// wrapping queue and worker (whose Run loop is NOT started automatically).
// It has no *testing.T dependency so it can also be called from inside a
// rapid.Check callback, which only has a *rapid.T in scope.
func newTestRegistryAndInstance(queue *Queue, worker *Worker, stateFile string) *Registry {
	stateDir, err := os.MkdirTemp("", "taskline-test-state-*")
	if err != nil {
		panic(err)
	}
	logDir, err := os.MkdirTemp("", "taskline-test-log-*")
	if err != nil {
		panic(err)
	}
	r := NewRegistry(stateDir, logDir, NewNameGenerator(), &fakeNotifier{})

	logWriter, err := NewProjectLog(r.LogPath(testProject))
	if err != nil {
		panic(err)
	}
	setInstance(r, testProject, &ProjectInstance{
		Name: testProject, Queue: queue, Worker: worker, LogWriter: logWriter, StateFile: stateFile,
	})
	return r
}

// newTestHandler returns a Handler wired to a fresh in-memory Queue and a
// Worker backed by a fakeRunner, registered under testProject, along with
// the ServeMux it registered on. No state file is configured, so no test
// writes queue state to disk.
func newTestHandler() (*Handler, *Queue, *Worker, *fakeRunner, *http.ServeMux) {
	q := newTestQueue()
	runner := newFakeRunner()
	notifier := &fakeNotifier{}
	w := NewWorker(q, runner, notifier, "", &bytes.Buffer{})

	r := newTestRegistryAndInstance(q, w, "")
	h := NewHandler(r)

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
		rec := doRequest(mux, http.MethodPost, projectPath("/tasks"), string(body))

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
			if _, err := q.AddTask("sleep 100", "", "", nil); err != nil {
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
				_, _ = q.AddTask(cmd, "", "", nil)
			}
		case "halted":
			proc := newFakeProcess(1)
			proc.finish()
			runner.enqueue("false", proc)
			if _, err := q.AddTask("false", "", "", nil); err != nil {
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

		rec := doRequest(mux, http.MethodGet, projectPath("/queue/status"), "")
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
	rec := doRequest(mux, http.MethodPost, projectPath("/tasks"), "{not json")
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
	rec := doRequest(mux, http.MethodPost, projectPath("/tasks"), body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doRequest(mux, http.MethodPost, projectPath("/tasks"), body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleListTasks_EmptyQueueReturnsEmptyArray(t *testing.T) {
	_, _, _, _, mux := newTestHandler()
	rec := doRequest(mux, http.MethodGet, projectPath("/tasks"), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("expected empty JSON array, got %q", rec.Body.String())
	}
}

func TestHandleDeleteTask_NotFound(t *testing.T) {
	_, _, _, _, mux := newTestHandler()
	rec := doRequest(mux, http.MethodDelete, projectPath("/tasks/does-not-exist"), "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandleDeleteTask_RunningTaskConflict(t *testing.T) {
	_, q, w, runner, mux := newTestHandler()
	proc := newFakeProcess(0)
	runner.enqueue("sleep 100", proc)
	task, err := q.AddTask("sleep 100", "", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	go w.Run()
	defer w.Shutdown()
	waitFor(t, time.Second, func() bool {
		got, err := q.Snapshot(task.ID)
		return err == nil && got.Status == TaskRunning
	})

	rec := doRequest(mux, http.MethodDelete, projectPath("/tasks/"+task.ID), "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleUpdateTask_NoFieldsRejected(t *testing.T) {
	_, q, _, _, mux := newTestHandler()
	task, err := q.AddTask("echo hi", "", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	rec := doRequest(mux, http.MethodPatch, projectPath("/tasks/"+task.ID), `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleUpdateTask_AppliesFields(t *testing.T) {
	_, q, _, _, mux := newTestHandler()
	task, err := q.AddTask("echo hi", "", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	rec := doRequest(mux, http.MethodPatch, projectPath("/tasks/"+task.ID), `{"command":"echo bye"}`)
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
	rec := doRequest(mux, http.MethodPost, projectPath("/tasks/retry"), "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSkip_NoFailedTaskConflict(t *testing.T) {
	_, _, _, _, mux := newTestHandler()
	rec := doRequest(mux, http.MethodPost, projectPath("/tasks/skip"), "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleResume_EmptyQueueReturnsIdle(t *testing.T) {
	_, _, _, _, mux := newTestHandler()
	rec := doRequest(mux, http.MethodPost, projectPath("/queue/resume"), "")
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
	rec := doRequest(mux, http.MethodPost, projectPath("/tasks"), `{"command":"sleep 100"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doRequest(mux, http.MethodPost, projectPath("/queue/resume"), "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleStop_Success(t *testing.T) {
	_, q, w, runner, mux := newTestHandler()
	proc := newFakeProcess(0)
	proc.exitOnTerm = true
	runner.enqueue("sleep 100", proc)
	task, err := q.AddTask("sleep 100", "", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	go w.Run()
	defer w.Shutdown()
	waitFor(t, time.Second, func() bool {
		got, err := q.Snapshot(task.ID)
		return err == nil && got.Status == TaskRunning
	})

	rec := doRequest(mux, http.MethodPost, projectPath("/tasks/stop"), "")
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
	rec := doRequest(mux, http.MethodPost, projectPath("/tasks/stop"), "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandlerPersist_WritesStateFileAfterCreate(t *testing.T) {
	q := newTestQueue()
	runner := newFakeRunner()
	notifier := &fakeNotifier{}
	w := NewWorker(q, runner, notifier, "", &bytes.Buffer{})
	statePath := t.TempDir() + "/state.json"
	r := newTestRegistryAndInstance(q, w, statePath)
	h := NewHandler(r)
	mux := http.NewServeMux()
	h.Register(mux)

	rec := doRequest(mux, http.MethodPost, projectPath("/tasks"), `{"command":"echo hi"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	loaded, err := LoadState(statePath, NewNameGenerator())
	if err != nil {
		t.Fatalf("unexpected error loading persisted state: %v", err)
	}
	if len(loaded.Tasks) != 1 || loaded.Tasks[0].Command != "echo hi" {
		t.Fatalf("expected persisted state to contain the created task, got %+v", loaded.Tasks)
	}
}

func TestHandleRetry_Success(t *testing.T) {
	_, q, w, runner, mux := newTestHandler()
	task, err := q.AddTask("will-fail", "", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if running := q.StartNext(); running == nil {
		t.Fatal("expected StartNext to start the task")
	}
	if _, err := q.MarkFailed(1); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	proc := newFakeProcess(0)
	proc.finish()
	runner.enqueue("will-fail", proc)

	// NOTE: the worker is deliberately started only after the response body
	// has been read, not before. handleRetry (handler.go) reads the *Task
	// returned by queue.Retry() via toTaskResponse without holding q.mu, so
	// starting the worker earlier races the Worker goroutine's concurrent
	// mutation of that same Task's Status/ExitCode fields under `-race`. See
	// MEMORY.md "handleRetry/handleSkip read Task pointers unsynchronized
	// with the Worker".
	rec := doRequest(mux, http.MethodPost, projectPath("/tasks/retry"), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp taskResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.ID != task.ID {
		t.Fatalf("expected retried task %s, got %s", task.ID, resp.ID)
	}
	if resp.Status != string(TaskPending) {
		t.Fatalf("expected status pending in response, got %q", resp.Status)
	}

	// The retry notified the worker (buffered wake signal); once started, it
	// picks up and completes the re-queued task using the process enqueued
	// above without needing that signal (StartNext is checked unconditionally
	// on the first loop iteration).
	go w.Run()
	t.Cleanup(w.Shutdown)
	if !pollUntil(time.Second, func() bool {
		return q.CurrentState() == QueueIdle
	}) {
		t.Fatalf("expected worker to complete the retried task, queue state=%q", q.CurrentState())
	}
}

func TestHandleSkip_Success(t *testing.T) {
	_, q, _, _, mux := newTestHandler()
	task, err := q.AddTask("will-fail", "", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if running := q.StartNext(); running == nil {
		t.Fatal("expected StartNext to start the task")
	}
	if _, err := q.MarkFailed(1); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	rec := doRequest(mux, http.MethodPost, projectPath("/tasks/skip"), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp idNameResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.ID != task.ID || resp.Name != task.Name {
		t.Fatalf("expected skipped task %s/%s, got %s/%s", task.ID, task.Name, resp.ID, resp.Name)
	}
	if _, err := q.Snapshot(task.ID); err != ErrTaskNotFound {
		t.Fatalf("expected skipped task to be removed from the queue, got err=%v", err)
	}
}

func TestHandleCreateTask_NegativePositionRejected(t *testing.T) {
	_, _, _, _, mux := newTestHandler()
	rec := doRequest(mux, http.MethodPost, projectPath("/tasks"), `{"command":"echo hi","position":-1}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if resp.Error != "VALIDATION_ERROR" {
		t.Fatalf("expected VALIDATION_ERROR, got %q", resp.Error)
	}
}

func TestHandleUpdateTask_MalformedJSON(t *testing.T) {
	_, q, _, _, mux := newTestHandler()
	task, err := q.AddTask("echo hi", "", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	rec := doRequest(mux, http.MethodPatch, projectPath("/tasks/"+task.ID), "{not json")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if resp.Error != "VALIDATION_ERROR" {
		t.Fatalf("expected VALIDATION_ERROR, got %q", resp.Error)
	}
}

func TestHandleResume_RunningWhenPendingTasksRemain(t *testing.T) {
	// Constructed directly (rather than via AddTask, which always makes an
	// idle queue running) to exercise Resume's "idle with pending tasks"
	// branch, e.g. as it would appear right after loading a persisted state.
	pending := &Task{ID: "abc12345", Name: "brave-tiger", Command: "echo hi", Status: TaskPending, CreatedAt: time.Now()}
	q := &Queue{State: QueueIdle, Tasks: []*Task{pending}, names: NewNameGenerator()}
	runner := newFakeRunner()
	notifier := &fakeNotifier{}
	w := NewWorker(q, runner, notifier, "", &bytes.Buffer{})
	r := newTestRegistryAndInstance(q, w, "")
	h := NewHandler(r)
	mux := http.NewServeMux()
	h.Register(mux)

	rec := doRequest(mux, http.MethodPost, projectPath("/queue/resume"), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp queueActionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.State != string(QueueRunning) {
		t.Fatalf("expected running state, got %q", resp.State)
	}
	if resp.Message != "" {
		t.Fatalf("expected no message, got %q", resp.Message)
	}
}

func TestTaskPosition_UnknownIdentifierReturnsNegativeOne(t *testing.T) {
	h, _, _, _, _ := newTestHandler()
	inst, err := h.registry.GetOrCreate(testProject)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	if pos := h.taskPosition(inst, "does-not-exist"); pos != -1 {
		t.Fatalf("expected -1 for an unknown identifier, got %d", pos)
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

func TestHandleListProjects_ReturnsAllKnownProjects(t *testing.T) {
	h, _, _, _, mux := newTestHandler()
	otherInst, err := h.registry.GetOrCreate("otherproject")
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	t.Cleanup(otherInst.Worker.Shutdown)

	rec := doRequest(mux, http.MethodGet, "/projects", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var items []projectListItem
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	names := make(map[string]bool)
	for _, it := range items {
		names[it.Name] = true
	}
	if !names[testProject] || !names["otherproject"] {
		t.Fatalf("expected both %q and %q listed, got %+v", testProject, "otherproject", items)
	}
}

func TestHandleCreateTask_InvalidProjectNameRejected(t *testing.T) {
	_, _, _, _, mux := newTestHandler()
	rec := doRequest(mux, http.MethodPost, "/projects/Not-Valid-UPPER/tasks", `{"command":"echo hi"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if resp.Error != "VALIDATION_ERROR" {
		t.Fatalf("expected VALIDATION_ERROR, got %q", resp.Error)
	}
}

func TestHandleGetLogs_ReturnsTailLines(t *testing.T) {
	h, _, _, _, mux := newTestHandler()
	inst, err := h.registry.GetOrCreate(testProject)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	inst.LogWriter.SetCurrentTask("t")
	for _, line := range []string{"one", "two", "three"} {
		if _, err := inst.LogWriter.Write([]byte(line + "\n")); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	rec := doRequest(mux, http.MethodGet, projectPath("/logs?tail=2"), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "one") {
		t.Errorf("expected tail=2 to exclude the first line, got %q", body)
	}
	if !strings.Contains(body, "two") || !strings.Contains(body, "three") {
		t.Errorf("expected tail=2 to include the last two lines, got %q", body)
	}
}

func TestHandleGetLogs_InvalidTailRejected(t *testing.T) {
	_, _, _, _, mux := newTestHandler()
	rec := doRequest(mux, http.MethodGet, projectPath("/logs?tail=notanumber"), "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestHandleStreamLogs_StreamsNewLines drives the SSE endpoint through a real
// httptest.Server and streaming HTTP client rather than httptest.Recorder:
// the handler and the test goroutine would otherwise race on the same
// bytes.Buffer, since ResponseRecorder is not safe for concurrent
// write-while-read.
func TestHandleStreamLogs_StreamsNewLines(t *testing.T) {
	h, _, _, _, mux := newTestHandler()
	inst, err := h.registry.GetOrCreate(testProject)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}

	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+projectPath("/logs/stream"), nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	lines := make(chan string, 8)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()

	if !pollUntil(time.Second, func() bool {
		inst.LogWriter.mu.Lock()
		n := len(inst.LogWriter.subs)
		inst.LogWriter.mu.Unlock()
		return n > 0
	}) {
		t.Fatal("expected a subscriber to be registered before the write")
	}

	inst.LogWriter.SetCurrentTask("t")
	if _, err := inst.LogWriter.Write([]byte("hello stream\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	select {
	case line := <-lines:
		if !strings.Contains(line, "hello stream") {
			t.Fatalf("expected streamed line to contain %q, got %q", "hello stream", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected a streamed line, got none")
	}

	cancel()
	_ = resp.Body.Close()
	if !pollUntil(time.Second, func() bool {
		inst.LogWriter.mu.Lock()
		n := len(inst.LogWriter.subs)
		inst.LogWriter.mu.Unlock()
		return n == 0
	}) {
		t.Fatal("expected the subscriber to be unregistered after the client disconnects")
	}
}

package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"pgregory.net/rapid"

	"remote/core"
	"remote/docker"
	"remote/store"
)

// newTestWorkflowHandler builds a WorkflowHandler wired to a real
// WorkflowQueueManager/WorkflowStore, backed by client (a docker.ContainerClient
// test double), with a project "proj" and a pipeline "processes/demo" already
// present on disk so schedule/edit validation succeeds against them.
func newTestWorkflowHandler(t *testing.T, client docker.ContainerClient) (*WorkflowHandler, *store.WorkflowStore) {
	t.Helper()

	projectsDir := t.TempDir()
	pipelinesDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectsDir, "proj"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pipelinesDir, "processes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pipelinesDir, "processes", "demo.yaml"), []byte("variables:\n  path: \"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &core.Config{
		ProjectsDir:  projectsDir,
		PipelinesDir: pipelinesDir,
	}
	s := store.NewWorkflowStore()
	cm := docker.NewContainerManager(client, cfg, nil)
	q := NewWorkflowQueueManager(s, cm, cfg, nil, nil)
	h := NewWorkflowHandler(s, cfg, nil, nil, q)
	return h, s
}

func doWorkflowRequest(handler http.HandlerFunc, method, target string, body any, pathValues map[string]string) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, target, reader)
	for k, v := range pathValues {
		req.SetPathValue(k, v)
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func TestHandleSchedule_Success(t *testing.T) {
	h, s := newTestWorkflowHandler(t, newFakeQueueContainerClient())

	rec := doWorkflowRequest(h.HandleSchedule, http.MethodPost, "/projects/proj/workflows",
		ScheduleWorkflowRequest{Pipeline: "processes/demo", Variables: map[string]string{"path": "."}},
		map[string]string{"name": "proj"})

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp WorkflowResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.ID == "" {
		t.Fatal("expected non-empty workflow ID")
	}
	if resp.Status != store.WorkflowQueued {
		t.Fatalf("expected status queued, got %q", resp.Status)
	}
	if resp.CreatedAt.IsZero() || resp.CreatedAt.After(time.Now()) {
		t.Fatalf("expected a non-zero, non-future created_at, got %v", resp.CreatedAt)
	}
	if len(s.ListByProjectSnapshot("proj")) != 1 {
		t.Fatal("expected workflow to be stored under project proj")
	}
}

func TestHandleSchedule_ProjectNotFound(t *testing.T) {
	h, _ := newTestWorkflowHandler(t, newFakeQueueContainerClient())

	rec := doWorkflowRequest(h.HandleSchedule, http.MethodPost, "/projects/missing/workflows",
		ScheduleWorkflowRequest{Pipeline: "processes/demo", Variables: map[string]string{}},
		map[string]string{"name": "missing"})

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandleSchedule_InvalidPipeline(t *testing.T) {
	h, _ := newTestWorkflowHandler(t, newFakeQueueContainerClient())

	cases := []ScheduleWorkflowRequest{
		{Pipeline: "", Variables: map[string]string{}},
		{Pipeline: "bogus-type/demo", Variables: map[string]string{}},
		{Pipeline: "processes/does-not-exist", Variables: map[string]string{}},
	}
	for _, req := range cases {
		rec := doWorkflowRequest(h.HandleSchedule, http.MethodPost, "/projects/proj/workflows", req, map[string]string{"name": "proj"})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("pipeline %q: expected 400, got %d: %s", req.Pipeline, rec.Code, rec.Body.String())
		}
	}
}

func TestHandleSchedule_InvalidVariables(t *testing.T) {
	h, _ := newTestWorkflowHandler(t, newFakeQueueContainerClient())

	req := ScheduleWorkflowRequest{Pipeline: "processes/demo", Variables: map[string]string{"bad key!": "x"}}
	rec := doWorkflowRequest(h.HandleSchedule, http.MethodPost, "/projects/proj/workflows", req, map[string]string{"name": "proj"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Feature: 010-dashboard-workflow-runner, Property 3: Workflow creation invariants
func TestHandleSchedule_CreationInvariants(t *testing.T) {
	h, _ := newTestWorkflowHandler(t, newFakeQueueContainerClient())
	seen := make(map[string]bool)

	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(0, 5).Draw(t, "n")
		variables := make(map[string]string, n)
		for i := 0; i < n; i++ {
			key := fmt.Sprintf("k%d", i)
			value := rapid.StringMatching(`[a-zA-Z0-9]{0,20}`).Draw(t, fmt.Sprintf("v%d", i))
			variables[key] = value
		}

		before := time.Now()
		rec := doWorkflowRequest(h.HandleSchedule, http.MethodPost, "/projects/proj/workflows",
			ScheduleWorkflowRequest{Pipeline: "processes/demo", Variables: variables},
			map[string]string{"name": "proj"})
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp WorkflowResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if _, err := uuid.Parse(resp.ID); err != nil {
			t.Fatalf("expected ID to be a non-empty, valid UUID, got %q: %v", resp.ID, err)
		}
		if seen[resp.ID] {
			t.Fatalf("expected a unique UUID per created workflow, got duplicate %q", resp.ID)
		}
		seen[resp.ID] = true

		if resp.Status != store.WorkflowQueued {
			t.Fatalf("expected status queued, got %q", resp.Status)
		}
		if resp.CreatedAt.IsZero() || resp.CreatedAt.Before(before) || resp.CreatedAt.After(time.Now()) {
			t.Fatalf("expected created_at within [%v, now], got %v", before, resp.CreatedAt)
		}
	})
}

func TestHandleList(t *testing.T) {
	h, s := newTestWorkflowHandler(t, newFakeQueueContainerClient())
	addQueuedWorkflow(s, "w1", "proj", "processes/demo", time.Now())
	addQueuedWorkflow(s, "w2", "proj", "processes/demo", time.Now().Add(time.Second))

	rec := doWorkflowRequest(h.HandleList, http.MethodGet, "/projects/proj/workflows", nil, map[string]string{"name": "proj"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp []WorkflowResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp) != 2 || resp[0].ID != "w2" || resp[1].ID != "w1" {
		t.Fatalf("expected [w2 w1] (desc by created_at), got %+v", resp)
	}
}

func TestHandleList_ProjectNotFound(t *testing.T) {
	h, _ := newTestWorkflowHandler(t, newFakeQueueContainerClient())
	rec := doWorkflowRequest(h.HandleList, http.MethodGet, "/projects/missing/workflows", nil, map[string]string{"name": "missing"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandleDetail(t *testing.T) {
	h, s := newTestWorkflowHandler(t, newFakeQueueContainerClient())
	w := addQueuedWorkflow(s, "w1", "proj", "processes/demo", time.Now())
	w.LogBuffer = store.NewRingBuffer(1024)
	w.LogBuffer.Write([]byte("hello\n"))

	rec := doWorkflowRequest(h.HandleDetail, http.MethodGet, "/projects/proj/workflows/w1", nil, map[string]string{"name": "proj", "id": "w1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp WorkflowResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Log != "hello\n" {
		t.Fatalf("expected log %q, got %q", "hello\n", resp.Log)
	}
}

func TestHandleDetail_NotFound(t *testing.T) {
	h, s := newTestWorkflowHandler(t, newFakeQueueContainerClient())
	addQueuedWorkflow(s, "w1", "other-project", "processes/demo", time.Now())

	// "w1" exists but belongs to a different project; "missing-id" doesn't exist at all.
	for _, id := range []string{"missing-id", "w1"} {
		rec := doWorkflowRequest(h.HandleDetail, http.MethodGet, "/projects/proj/workflows/"+id, nil, map[string]string{"name": "proj", "id": id})
		if rec.Code != http.StatusNotFound {
			t.Fatalf("id %q: expected 404, got %d", id, rec.Code)
		}
	}
}

func TestHandleEdit_Success(t *testing.T) {
	h, s := newTestWorkflowHandler(t, newFakeQueueContainerClient())
	addQueuedWorkflow(s, "w1", "proj", "processes/demo", time.Now())

	req := EditWorkflowRequest{Variables: map[string]string{"path": "updated"}}
	rec := doWorkflowRequest(h.HandleEdit, http.MethodPut, "/projects/proj/workflows/w1", req, map[string]string{"name": "proj", "id": "w1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp WorkflowResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Variables["path"] != "updated" {
		t.Fatalf("expected updated variables, got %+v", resp.Variables)
	}

	// Editing must not change the workflow's queue position: it's still the
	// oldest queued workflow for its project (Property 8).
	if next := s.NextQueued("proj"); next == nil || next.ID != "w1" {
		t.Fatalf("expected w1 to remain the next queued workflow")
	}
}

func TestHandleEdit_ConflictWhenNotQueued(t *testing.T) {
	h, s := newTestWorkflowHandler(t, newFakeQueueContainerClient())
	w := addQueuedWorkflow(s, "w1", "proj", "processes/demo", time.Now())
	s.Update("w1", func(wf *store.Workflow) { wf.Status = store.WorkflowCompleted })
	_ = w

	req := EditWorkflowRequest{Variables: map[string]string{"path": "updated"}}
	rec := doWorkflowRequest(h.HandleEdit, http.MethodPut, "/projects/proj/workflows/w1", req, map[string]string{"name": "proj", "id": "w1"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleCancel_Queued(t *testing.T) {
	h, s := newTestWorkflowHandler(t, newFakeQueueContainerClient())
	addQueuedWorkflow(s, "w1", "proj", "processes/demo", time.Now())

	rec := doWorkflowRequest(h.HandleCancel, http.MethodDelete, "/projects/proj/workflows/w1", nil, map[string]string{"name": "proj", "id": "w1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp WorkflowResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Status != store.WorkflowCancelled {
		t.Fatalf("expected cancelled, got %q", resp.Status)
	}
}

func TestHandleCancel_ConflictWhenTerminal(t *testing.T) {
	h, s := newTestWorkflowHandler(t, newFakeQueueContainerClient())
	addQueuedWorkflow(s, "w1", "proj", "processes/demo", time.Now())
	s.Update("w1", func(wf *store.Workflow) { wf.Status = store.WorkflowCompleted })

	rec := doWorkflowRequest(h.HandleCancel, http.MethodDelete, "/projects/proj/workflows/w1", nil, map[string]string{"name": "proj", "id": "w1"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleCancel_DeleteFailed(t *testing.T) {
	h, s := newTestWorkflowHandler(t, newFakeQueueContainerClient())
	addQueuedWorkflow(s, "w1", "proj", "processes/demo", time.Now())
	s.Update("w1", func(wf *store.Workflow) { wf.Status = store.WorkflowFailed })

	rec := doWorkflowRequest(h.HandleCancel, http.MethodDelete, "/projects/proj/workflows/w1", nil, map[string]string{"name": "proj", "id": "w1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for deleting failed workflow, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleCancel_NotFound(t *testing.T) {
	h, _ := newTestWorkflowHandler(t, newFakeQueueContainerClient())
	rec := doWorkflowRequest(h.HandleCancel, http.MethodDelete, "/projects/proj/workflows/missing", nil, map[string]string{"name": "proj", "id": "missing"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandleCancel_Running(t *testing.T) {
	client := newFakeQueueContainerClient()
	client.results["processes/demo"] = fakeQueueResult{exitCode: 0}
	client.gates["processes/demo"] = make(chan struct{})

	h, s := newTestWorkflowHandler(t, client)

	rec := doWorkflowRequest(h.HandleSchedule, http.MethodPost, "/projects/proj/workflows",
		ScheduleWorkflowRequest{Pipeline: "processes/demo", Variables: map[string]string{}},
		map[string]string{"name": "proj"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var scheduled WorkflowResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &scheduled); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	id := scheduled.ID

	deadline := time.Now().Add(2 * time.Second)
	for workflowStatus(s, id) != store.WorkflowRunning {
		if time.Now().After(deadline) {
			t.Fatalf("workflow did not reach running status in time")
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancelRec := doWorkflowRequest(h.HandleCancel, http.MethodDelete, "/projects/proj/workflows/"+id, nil, map[string]string{"name": "proj", "id": id})
	if cancelRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", cancelRec.Code, cancelRec.Body.String())
	}

	// Simulate the container actually exiting in response to CancelRunning's SIGTERM.
	close(client.gates["processes/demo"])

	w := waitForTerminal(t, s, id, 2*time.Second)
	if w.Status != store.WorkflowCancelled {
		t.Fatalf("expected cancelled, got %q (error: %s)", w.Status, w.Error)
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	found := false
	for _, p := range client.killed {
		if p == "processes/demo" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected container to receive a kill signal, got killed=%v", client.killed)
	}
}

// Feature: 010-dashboard-workflow-runner, Property 7: State machine edit/cancel constraints
func TestWorkflowHandler_StateMachineEditCancelConstraints(t *testing.T) {
	h, s := newTestWorkflowHandler(t, newFakeQueueContainerClient())
	iteration := 0

	rapid.Check(t, func(t *rapid.T) {
		status := rapid.SampledFrom([]store.WorkflowStatus{
			store.WorkflowQueued, store.WorkflowRunning, store.WorkflowCompleted, store.WorkflowFailed, store.WorkflowCancelled,
		}).Draw(t, "status")
		iteration++

		editID := fmt.Sprintf("edit-wf-%d", iteration)
		addQueuedWorkflow(s, editID, "proj", "processes/demo", time.Now())
		s.Update(editID, func(wf *store.Workflow) { wf.Status = status })

		editReq := EditWorkflowRequest{Variables: map[string]string{"path": "x"}}
		editRec := doWorkflowRequest(h.HandleEdit, http.MethodPut, "/projects/proj/workflows/"+editID, editReq, map[string]string{"name": "proj", "id": editID})
		if status != store.WorkflowQueued && editRec.Code != http.StatusConflict {
			t.Fatalf("status %q: expected edit to be rejected with 409, got %d: %s", status, editRec.Code, editRec.Body.String())
		}

		cancelID := fmt.Sprintf("cancel-wf-%d", iteration)
		addQueuedWorkflow(s, cancelID, "proj", "processes/demo", time.Now())
		s.Update(cancelID, func(wf *store.Workflow) { wf.Status = status })

		cancelRec := doWorkflowRequest(h.HandleCancel, http.MethodDelete, "/projects/proj/workflows/"+cancelID, nil, map[string]string{"name": "proj", "id": cancelID})
		// Failed workflows can be deleted (unblocks queue); completed and cancelled reject with 409.
		if status == store.WorkflowCompleted || status == store.WorkflowCancelled {
			if cancelRec.Code != http.StatusConflict {
				t.Fatalf("status %q: expected cancel to be rejected with 409, got %d: %s", status, cancelRec.Code, cancelRec.Body.String())
			}
		}
		if status == store.WorkflowFailed {
			if cancelRec.Code != http.StatusOK {
				t.Fatalf("status %q: expected cancel (delete failed) to succeed with 200, got %d: %s", status, cancelRec.Code, cancelRec.Body.String())
			}
		}
	})
}

// newTestLogsServer starts an httptest server exposing only HandleLogs, wired
// to a real ServeMux so r.PathValue("name")/r.PathValue("id") resolve exactly
// as they do through the real router.
func newTestLogsServer(t *testing.T, h *WorkflowHandler) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /projects/{name}/workflows/{id}/logs", h.HandleLogs)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// readLogMsg reads and decodes the next message from conn, failing the test
// if it doesn't arrive within 2 seconds or isn't valid JSON.
func readLogMsg(t *testing.T, conn *websocket.Conn) workflowLogMessage {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read message: %v", err)
	}
	var msg workflowLogMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("unmarshal message %q: %v", data, err)
	}
	return msg
}

func TestHandleLogs_ProjectNotFound(t *testing.T) {
	h, _ := newTestWorkflowHandler(t, newFakeQueueContainerClient())
	wsBase := newTestLogsServer(t, h)

	resp, err := http.Get("http" + strings.TrimPrefix(wsBase, "ws") + "/projects/missing/workflows/w1/logs")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestHandleLogs_WorkflowNotFound(t *testing.T) {
	h, _ := newTestWorkflowHandler(t, newFakeQueueContainerClient())
	wsBase := newTestLogsServer(t, h)

	resp, err := http.Get("http" + strings.TrimPrefix(wsBase, "ws") + "/projects/proj/workflows/missing/logs")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestHandleLogs_InvalidBearerToken(t *testing.T) {
	h, s := newTestWorkflowHandler(t, newFakeQueueContainerClient())
	h.config.APIToken = "correct-token"
	addQueuedWorkflow(s, "w1", "proj", "processes/demo", time.Now())
	wsBase := newTestLogsServer(t, h)

	req, _ := http.NewRequest(http.MethodGet, "http"+strings.TrimPrefix(wsBase, "ws")+"/projects/proj/workflows/w1/logs", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestHandleLogs_TerminalOnConnect(t *testing.T) {
	h, s := newTestWorkflowHandler(t, newFakeQueueContainerClient())
	h.config.APIToken = "test-token"

	w := addQueuedWorkflow(s, "w1", "proj", "processes/demo", time.Now())
	w.LogBuffer = store.NewRingBuffer(1024)
	w.LogBuffer.Write([]byte("hello\n"))
	exitCode := 1
	s.Update("w1", func(wf *store.Workflow) {
		wf.Status = store.WorkflowFailed
		wf.ExitCode = &exitCode
	})
	wsBase := newTestLogsServer(t, h)

	conn, _, err := websocket.DefaultDialer.Dial(wsBase+"/projects/proj/workflows/w1/logs", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]string{"type": "auth", "token": "test-token"}); err != nil {
		t.Fatalf("write auth: %v", err)
	}

	msg := readLogMsg(t, conn)
	if msg.Type != "output" || msg.Data != "hello\n" {
		t.Fatalf("expected output %q, got %+v", "hello\n", msg)
	}
	msg = readLogMsg(t, conn)
	if msg.Type != "finished" || msg.Status != "failed" || msg.ExitCode == nil || *msg.ExitCode != 1 {
		t.Fatalf("expected finished/failed/exit_code=1, got %+v", msg)
	}
}

func TestHandleLogs_QueuedThenRunningThenFinished(t *testing.T) {
	h, s := newTestWorkflowHandler(t, newFakeQueueContainerClient())
	h.config.APIToken = "test-token"
	addQueuedWorkflow(s, "w1", "proj", "processes/demo", time.Now())
	wsBase := newTestLogsServer(t, h)

	header := http.Header{"Authorization": []string{"Bearer test-token"}}
	conn, _, err := websocket.DefaultDialer.Dial(wsBase+"/projects/proj/workflows/w1/logs", header)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if msg := readLogMsg(t, conn); msg.Type != "waiting" {
		t.Fatalf("expected waiting, got %+v", msg)
	}

	buf := store.NewRingBuffer(1024)
	buf.Write([]byte("hi\n"))
	s.Update("w1", func(wf *store.Workflow) {
		wf.Status = store.WorkflowRunning
		wf.LogBuffer = buf
	})

	if msg := readLogMsg(t, conn); msg.Type != "output" || msg.Data != "hi\n" {
		t.Fatalf("expected output %q, got %+v", "hi\n", msg)
	}

	// Exercise the live subscriber path the same way workflowOutputWriter.Write does.
	s.Update("w1", func(wf *store.Workflow) {
		for _, sub := range wf.LogSubs {
			sub <- "more\n"
		}
	})
	if msg := readLogMsg(t, conn); msg.Type != "output" || msg.Data != "more\n" {
		t.Fatalf("expected output %q, got %+v", "more\n", msg)
	}

	exitCode := 0
	s.Update("w1", func(wf *store.Workflow) {
		wf.Status = store.WorkflowCompleted
		wf.ExitCode = &exitCode
	})

	if msg := readLogMsg(t, conn); msg.Type != "finished" || msg.Status != "completed" || msg.ExitCode == nil || *msg.ExitCode != 0 {
		t.Fatalf("expected finished/completed/exit_code=0, got %+v", msg)
	}

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatalf("expected connection closed after finished message")
	}
}

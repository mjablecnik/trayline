package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func newTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *Client) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, NewClient(srv.URL)
}

func TestNewClient_TrimsTrailingSlash(t *testing.T) {
	c := NewClient("http://example.com/")
	if c.baseURL != "http://example.com" {
		t.Fatalf("expected trailing slash trimmed, got %q", c.baseURL)
	}
}

func TestAPIError_ErrorReturnsMessage(t *testing.T) {
	err := &APIError{Code: "NOT_FOUND", Message: "task not found"}
	if err.Error() != "task not found" {
		t.Fatalf("expected Error() to return Message, got %q", err.Error())
	}
}

func TestClient_CreateTask_SendsMethodPathAndBody(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody struct {
		Command  string `json:"command"`
		Name     string `json:"name,omitempty"`
		Position *int   `json:"position,omitempty"`
	}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CreateTaskResponse{
			ID: "abc123", Name: "brave-tiger", Command: gotBody.Command, Status: "pending", Position: 0,
		})
	})

	pos := 2
	resp, err := c.CreateTask("echo hi", "my-task", &pos)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("expected POST, got %s", gotMethod)
	}
	if gotPath != "/tasks" {
		t.Errorf("expected path /tasks, got %s", gotPath)
	}
	if gotBody.Command != "echo hi" || gotBody.Name != "my-task" || gotBody.Position == nil || *gotBody.Position != 2 {
		t.Errorf("unexpected request body: %+v", gotBody)
	}
	if resp.ID != "abc123" || resp.Name != "brave-tiger" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestClient_CreateTask_OmitsNameAndPositionWhenUnset(t *testing.T) {
	var rawBody map[string]interface{}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&rawBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CreateTaskResponse{ID: "x", Name: "y", Command: "echo hi", Status: "pending"})
	})

	if _, err := c.CreateTask("echo hi", "", nil); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, ok := rawBody["name"]; ok {
		t.Errorf("expected name to be omitted, got %v", rawBody["name"])
	}
	if _, ok := rawBody["position"]; ok {
		t.Errorf("expected position to be omitted, got %v", rawBody["position"])
	}
}

func TestClient_ListTasks_DecodesArray(t *testing.T) {
	var gotMethod, gotPath string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]TaskListItem{
			{Position: 0, ID: "a", Name: "n1", Command: "c1", Status: "running"},
			{Position: 1, ID: "b", Name: "n2", Command: "c2", Status: "pending"},
		})
	})

	tasks, err := c.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if gotMethod != http.MethodGet || gotPath != "/tasks" {
		t.Errorf("expected GET /tasks, got %s %s", gotMethod, gotPath)
	}
	if len(tasks) != 2 || tasks[0].ID != "a" || tasks[1].ID != "b" {
		t.Errorf("unexpected tasks: %+v", tasks)
	}
}

func TestClient_ListTasks_EmptyArray(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]TaskListItem{})
	})

	tasks, err := c.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected empty slice, got %+v", tasks)
	}
}

func TestClient_DeleteTask_EscapesIdentifierPath(t *testing.T) {
	var gotMethod, gotEscapedPath, gotDecodedPath string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotEscapedPath = r.URL.EscapedPath()
		gotDecodedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Task{ID: "id with space/slash", Name: "n", Command: "c", Status: "pending"})
	})

	task, err := c.DeleteTask("id with space/slash")
	if err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("expected DELETE, got %s", gotMethod)
	}
	wantEscaped := "/tasks/" + url.PathEscape("id with space/slash")
	if gotEscapedPath != wantEscaped {
		t.Errorf("expected escaped path %q, got %q", wantEscaped, gotEscapedPath)
	}
	if gotDecodedPath != "/tasks/id with space/slash" {
		t.Errorf("expected decoded path %q, got %q", "/tasks/id with space/slash", gotDecodedPath)
	}
	if task.ID != "id with space/slash" {
		t.Errorf("unexpected task: %+v", task)
	}
}

func TestClient_UpdateTask_SendsPatchWithBody(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody struct {
		Command string `json:"command,omitempty"`
		Name    string `json:"name,omitempty"`
	}
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Task{ID: "abc", Name: gotBody.Name, Command: gotBody.Command, Status: "pending"})
	})

	task, err := c.UpdateTask("abc", "echo updated", "new-name")
	if err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	if gotMethod != http.MethodPatch || gotPath != "/tasks/abc" {
		t.Errorf("expected PATCH /tasks/abc, got %s %s", gotMethod, gotPath)
	}
	if gotBody.Command != "echo updated" || gotBody.Name != "new-name" {
		t.Errorf("unexpected body: %+v", gotBody)
	}
	if task.Command != "echo updated" {
		t.Errorf("unexpected task: %+v", task)
	}
}

func TestClient_Retry_DecodesTask(t *testing.T) {
	var gotMethod, gotPath string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Task{ID: "abc", Name: "n", Command: "c", Status: "pending"})
	})

	task, err := c.Retry()
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/tasks/retry" {
		t.Errorf("expected POST /tasks/retry, got %s %s", gotMethod, gotPath)
	}
	if task.ID != "abc" {
		t.Errorf("unexpected task: %+v", task)
	}
}

func TestClient_Skip_DecodesIDNameResult(t *testing.T) {
	var gotMethod, gotPath string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(IDNameResult{ID: "abc", Name: "n"})
	})

	result, err := c.Skip()
	if err != nil {
		t.Fatalf("Skip: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/tasks/skip" {
		t.Errorf("expected POST /tasks/skip, got %s %s", gotMethod, gotPath)
	}
	if result.ID != "abc" || result.Name != "n" {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestClient_Stop_DecodesTask(t *testing.T) {
	var gotMethod, gotPath string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Task{ID: "abc", Name: "n", Command: "c", Status: "failed"})
	})

	task, err := c.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/tasks/stop" {
		t.Errorf("expected POST /tasks/stop, got %s %s", gotMethod, gotPath)
	}
	if task.Status != "failed" {
		t.Errorf("unexpected task: %+v", task)
	}
}

func TestClient_Resume_DecodesQueueActionResult(t *testing.T) {
	var gotMethod, gotPath string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(QueueActionResult{State: "running", Message: "resumed"})
	})

	result, err := c.Resume()
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/queue/resume" {
		t.Errorf("expected POST /queue/resume, got %s %s", gotMethod, gotPath)
	}
	if result.State != "running" || result.Message != "resumed" {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestClient_Status_DecodesQueueStatusResult(t *testing.T) {
	var gotMethod, gotPath string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(QueueStatusResult{
			State:        "running",
			PendingCount: 3,
			CurrentTask:  &TaskBrief{ID: "a", Name: "n", Command: "c"},
			FailedTask:   &FailedInfo{ID: "b", Name: "m", Command: "d", ExitCode: 1},
		})
	})

	result, err := c.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if gotMethod != http.MethodGet || gotPath != "/queue/status" {
		t.Errorf("expected GET /queue/status, got %s %s", gotMethod, gotPath)
	}
	if result.State != "running" || result.PendingCount != 3 {
		t.Errorf("unexpected result: %+v", result)
	}
	if result.CurrentTask == nil || result.CurrentTask.ID != "a" {
		t.Errorf("unexpected current task: %+v", result.CurrentTask)
	}
	if result.FailedTask == nil || result.FailedTask.ExitCode != 1 {
		t.Errorf("unexpected failed task: %+v", result.FailedTask)
	}
}

func TestClient_Do_4xxParsesAPIError(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": "CONFLICT", "message": "name already taken"})
	})

	_, err := c.Retry()
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T (%v)", err, err)
	}
	if apiErr.Code != "CONFLICT" || apiErr.Message != "name already taken" {
		t.Errorf("unexpected APIError: %+v", apiErr)
	}
}

func TestClient_Do_5xxParsesAPIError(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "INTERNAL", "message": "something broke"})
	})

	_, err := c.Retry()
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T (%v)", err, err)
	}
	if apiErr.Code != "INTERNAL" || apiErr.Message != "something broke" {
		t.Errorf("unexpected APIError: %+v", apiErr)
	}
}

func TestClient_Do_MalformedErrorBodyFallsBackToUnknown(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("  not json at all  "))
	})

	_, err := c.Retry()
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T (%v)", err, err)
	}
	if apiErr.Code != "UNKNOWN" {
		t.Errorf("expected Code UNKNOWN, got %q", apiErr.Code)
	}
	if apiErr.Message != "not json at all" {
		t.Errorf("expected trimmed raw text, got %q", apiErr.Message)
	}
}

func TestClient_Do_EmptyErrorBodyFallsBackToUnknown(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	_, err := c.Retry()
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T (%v)", err, err)
	}
	if apiErr.Code != "UNKNOWN" || apiErr.Message != "" {
		t.Errorf("unexpected APIError: %+v", apiErr)
	}
}

func TestClient_Do_EmptySuccessBodySkipsDecode(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	task, err := c.Retry()
	if err != nil {
		t.Fatalf("expected no error for empty success body, got %v", err)
	}
	if task == nil || task.ID != "" {
		t.Errorf("expected zero-value Task, got %+v", task)
	}
}

func TestClient_Do_ConnectionErrorIsWrapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	c := NewClient(srv.URL)
	srv.Close() // ensure nothing is listening on this address anymore

	_, err := c.Retry()
	if err == nil {
		t.Fatal("expected a connection error, got nil")
	}
	if _, ok := err.(*APIError); ok {
		t.Fatalf("expected a wrapped connection error, not *APIError: %v", err)
	}
	if !strings.Contains(err.Error(), "connecting to") {
		t.Errorf("expected error to mention connecting to, got %q", err.Error())
	}
}

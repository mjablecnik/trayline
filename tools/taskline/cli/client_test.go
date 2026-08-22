package main

import (
	"encoding/json"
	"fmt"
	"io"
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
	return srv, NewClient(srv.URL, "proj", "")
}

func TestClient_SendsBearerTokenWhenConfigured(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]TaskListItem{})
	}))
	t.Cleanup(srv.Close)

	c := NewClient(srv.URL, "proj", "secret-token")
	if _, err := c.ListTasks(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "Bearer secret-token" {
		t.Fatalf("expected Authorization: Bearer secret-token, got %q", gotAuth)
	}
}

func TestClient_OmitsAuthHeaderWhenNoTokenConfigured(t *testing.T) {
	var gotAuth string
	sawRequest := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawRequest = true
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]TaskListItem{})
	}))
	t.Cleanup(srv.Close)

	c := NewClient(srv.URL, "proj", "")
	if _, err := c.ListTasks(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sawRequest {
		t.Fatalf("expected request to reach server")
	}
	if gotAuth != "" {
		t.Fatalf("expected no Authorization header, got %q", gotAuth)
	}
}

func TestNewClient_TrimsTrailingSlash(t *testing.T) {
	c := NewClient("http://example.com/", "proj", "")
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
	resp, err := c.CreateTask("echo hi", "my-task", "/some/path", &pos)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("expected POST, got %s", gotMethod)
	}
	if gotPath != "/projects/proj/tasks" {
		t.Errorf("expected path /projects/proj/tasks, got %s", gotPath)
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

	if _, err := c.CreateTask("echo hi", "", "", nil); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, ok := rawBody["name"]; ok {
		t.Errorf("expected name to be omitted, got %v", rawBody["name"])
	}
	if _, ok := rawBody["position"]; ok {
		t.Errorf("expected position to be omitted, got %v", rawBody["position"])
	}
}

func TestClient_ProjectNameIsEscapedInPath(t *testing.T) {
	var gotRawPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRawPath = r.URL.EscapedPath()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]TaskListItem{})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "my project/x", "")
	if _, err := c.ListTasks(); err != nil {
		t.Fatalf("ListTasks: %v", err)
	}

	want := "/projects/" + url.PathEscape("my project/x") + "/tasks"
	if gotRawPath != want {
		t.Errorf("expected escaped path %q, got %q", want, gotRawPath)
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
	if gotMethod != http.MethodGet || gotPath != "/projects/proj/tasks" {
		t.Errorf("expected GET /projects/proj/tasks, got %s %s", gotMethod, gotPath)
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
	wantEscaped := "/projects/proj/tasks/" + url.PathEscape("id with space/slash")
	if gotEscapedPath != wantEscaped {
		t.Errorf("expected escaped path %q, got %q", wantEscaped, gotEscapedPath)
	}
	if gotDecodedPath != "/projects/proj/tasks/id with space/slash" {
		t.Errorf("expected decoded path %q, got %q", "/projects/proj/tasks/id with space/slash", gotDecodedPath)
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
	if gotMethod != http.MethodPatch || gotPath != "/projects/proj/tasks/abc" {
		t.Errorf("expected PATCH /projects/proj/tasks/abc, got %s %s", gotMethod, gotPath)
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
	if gotMethod != http.MethodPost || gotPath != "/projects/proj/tasks/retry" {
		t.Errorf("expected POST /projects/proj/tasks/retry, got %s %s", gotMethod, gotPath)
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
	if gotMethod != http.MethodPost || gotPath != "/projects/proj/tasks/skip" {
		t.Errorf("expected POST /projects/proj/tasks/skip, got %s %s", gotMethod, gotPath)
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
	if gotMethod != http.MethodPost || gotPath != "/projects/proj/tasks/stop" {
		t.Errorf("expected POST /projects/proj/tasks/stop, got %s %s", gotMethod, gotPath)
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
	if gotMethod != http.MethodPost || gotPath != "/projects/proj/queue/resume" {
		t.Errorf("expected POST /projects/proj/queue/resume, got %s %s", gotMethod, gotPath)
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
	if gotMethod != http.MethodGet || gotPath != "/projects/proj/queue/status" {
		t.Errorf("expected GET /projects/proj/queue/status, got %s %s", gotMethod, gotPath)
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
	c := NewClient(srv.URL, "proj", "")
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

func TestClient_ListProjects_DecodesArrayFromUnscopedPath(t *testing.T) {
	var gotMethod, gotPath string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]ProjectListItem{
			{Name: "dashboard", State: "running", PendingCount: 3},
			{Name: "backend", State: "idle", PendingCount: 0},
		})
	})

	projects, err := c.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if gotMethod != http.MethodGet || gotPath != "/projects" {
		t.Errorf("expected GET /projects (unscoped), got %s %s", gotMethod, gotPath)
	}
	if len(projects) != 2 || projects[0].Name != "dashboard" || projects[1].State != "idle" {
		t.Errorf("unexpected projects: %+v", projects)
	}
}

func TestClient_GetLogs_OmitsTailQueryWhenNonPositive(t *testing.T) {
	var gotPath, gotQuery string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "line1\nline2\n")
	})

	content, err := c.GetLogs(0)
	if err != nil {
		t.Fatalf("GetLogs: %v", err)
	}
	if gotPath != "/projects/proj/logs" || gotQuery != "" {
		t.Errorf("expected GET /projects/proj/logs with no query, got %s?%s", gotPath, gotQuery)
	}
	if content != "line1\nline2\n" {
		t.Errorf("unexpected content: %q", content)
	}
}

func TestClient_GetLogs_IncludesTailQuery(t *testing.T) {
	var gotQuery string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		fmt.Fprint(w, "last line\n")
	})

	if _, err := c.GetLogs(50); err != nil {
		t.Fatalf("GetLogs: %v", err)
	}
	if gotQuery != "tail=50" {
		t.Errorf("expected query tail=50, got %q", gotQuery)
	}
}

func TestClient_GetLogs_APIErrorOnNonSuccessStatus(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "NOT_FOUND", "message": "no such project"})
	})

	_, err := c.GetLogs(0)
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T (%v)", err, err)
	}
	if apiErr.Message != "no such project" {
		t.Errorf("unexpected APIError: %+v", apiErr)
	}
}

func TestClient_StreamLogs_ReturnsReadableBody(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/proj/logs/stream" {
			t.Errorf("expected GET /projects/proj/logs/stream, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: hello\n\n")
	})

	body, err := c.StreamLogs()
	if err != nil {
		t.Fatalf("StreamLogs: %v", err)
	}
	defer body.Close()

	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("reading stream body: %v", err)
	}
	if !strings.Contains(string(data), "data: hello") {
		t.Errorf("expected stream body to contain event data, got %q", data)
	}
}

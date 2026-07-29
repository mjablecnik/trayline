package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// tsTestConfig creates a Config pointing at the given test server with quiet mode.
func tsTestConfig(ts *httptest.Server) *Config {
	return &Config{ServerURL: ts.URL, Token: "tok", Quiet: true}
}

// captureStdout captures everything written to os.Stdout during fn().
func captureStdout(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

// captureStderr captures everything written to os.Stderr during fn().
func captureStderr(fn func()) string {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	fn()
	w.Close()
	os.Stderr = old
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

// --- handleTasks ---

func TestHandleTasks_ListsTable(t *testing.T) {
	now := time.Now()
	tasks := []TaskSummary{
		{ID: "abc-1", Status: "completed", Agent: "claude", CreatedAt: now},
		{ID: "abc-2", Status: "running", Agent: "kiro", CreatedAt: now},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/runs" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("auth header: got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tasks)
	}))
	defer ts.Close()

	var code int
	out := captureStdout(func() {
		code = handleTasks([]string{}, tsTestConfig(ts))
	})

	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	if !strings.Contains(out, "abc-1") {
		t.Errorf("expected task ID in output, got: %q", out)
	}
	if !strings.Contains(out, "completed") {
		t.Errorf("expected status in output, got: %q", out)
	}
	if !strings.Contains(out, "claude") {
		t.Errorf("expected agent in output, got: %q", out)
	}
	if !strings.Contains(out, "abc-2") {
		t.Errorf("expected second task in output, got: %q", out)
	}
	// Header row must be present
	if !strings.Contains(out, "ID") || !strings.Contains(out, "STATUS") {
		t.Errorf("expected header row in output, got: %q", out)
	}
}

func TestHandleTasks_EmptyList(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, "[]")
	}))
	defer ts.Close()

	var code int
	out := captureStdout(func() {
		code = handleTasks([]string{}, tsTestConfig(ts))
	})

	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	// Header row only (no data rows) — should still have the header.
	if !strings.Contains(out, "ID") {
		t.Errorf("expected header row even for empty list, got: %q", out)
	}
}

func TestHandleTasks_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, `{"message":"internal error"}`)
	}))
	defer ts.Close()

	code := handleTasks([]string{}, tsTestConfig(ts))
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
}

func TestHandleTasks_UnexpectedArg(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()
	code := handleTasks([]string{"extra"}, tsTestConfig(ts))
	if code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

func TestHandleTasks_HelpFlag(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()
	code := handleTasks([]string{"--help"}, tsTestConfig(ts))
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
}

// --- handleTask ---

func TestHandleTask_DisplaysAllFields(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	completed := now.Add(5 * time.Second)
	task := RunResponse{
		ID:          "task-detail",
		Status:      "completed",
		Agent:       "claude",
		Result:      "the result text",
		CreatedAt:   now,
		CompletedAt: &completed,
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/run/task-detail" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(task)
	}))
	defer ts.Close()

	var code int
	out := captureStdout(func() {
		code = handleTask([]string{"task-detail"}, tsTestConfig(ts))
	})

	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	for _, want := range []string{"task-detail", "completed", "claude", "the result text"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got: %q", want, out)
		}
	}
}

func TestHandleTask_FailedStatusShowsError(t *testing.T) {
	now := time.Now()
	task := RunResponse{
		ID: "task-fail", Status: "failed", Agent: "kiro",
		Error: "agent crashed hard", CreatedAt: now,
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(task)
	}))
	defer ts.Close()

	var code int
	out := captureStdout(func() {
		code = handleTask([]string{"task-fail"}, tsTestConfig(ts))
	})

	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	if !strings.Contains(out, "agent crashed hard") {
		t.Errorf("expected error message in output, got: %q", out)
	}
}

func TestHandleTask_MissingID(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()
	code := handleTask([]string{}, tsTestConfig(ts))
	if code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

func TestHandleTask_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintln(w, `{"message":"task not found"}`)
	}))
	defer ts.Close()

	code := handleTask([]string{"missing-id"}, tsTestConfig(ts))
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
}

// --- handleCancel ---

func TestHandleCancel_Success(t *testing.T) {
	task := RunResponse{ID: "task-abc", Status: "cancelled", Agent: "claude", CreatedAt: time.Now()}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/run/task-abc/cancel" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(task)
	}))
	defer ts.Close()

	var code int
	out := captureStdout(func() {
		code = handleCancel([]string{"task-abc"}, tsTestConfig(ts))
	})

	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	if !strings.Contains(out, "task-abc") {
		t.Errorf("expected task ID in output, got: %q", out)
	}
	if !strings.Contains(out, "cancelled") {
		t.Errorf("expected status in output, got: %q", out)
	}
}

func TestHandleCancel_ConflictAlreadyTerminal(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		fmt.Fprintln(w, `{"message":"task is already in a terminal state"}`)
	}))
	defer ts.Close()

	code := handleCancel([]string{"task-done"}, tsTestConfig(ts))
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
}

func TestHandleCancel_MissingID(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()
	code := handleCancel([]string{}, tsTestConfig(ts))
	if code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

func TestHandleCancel_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintln(w, `{"message":"task not found"}`)
	}))
	defer ts.Close()

	code := handleCancel([]string{"no-such-task"}, tsTestConfig(ts))
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
}

// --- handleSessions ---

func TestHandleSessions_ListsTable(t *testing.T) {
	now := time.Now()
	sessions := []SessionSummary{
		{SessionID: "sess-1", Agent: "claude", Model: "claude-3", CreatedAt: now, LastMessageAt: now},
		{SessionID: "sess-2", Agent: "kiro", Model: "", CreatedAt: now, LastMessageAt: now},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/sessions" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("auth header: got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sessions)
	}))
	defer ts.Close()

	var code int
	out := captureStdout(func() {
		code = handleSessions([]string{}, tsTestConfig(ts))
	})

	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	if !strings.Contains(out, "sess-1") || !strings.Contains(out, "sess-2") {
		t.Errorf("expected session IDs in output, got: %q", out)
	}
	if !strings.Contains(out, "SESSION ID") {
		t.Errorf("expected header row, got: %q", out)
	}
}

func TestHandleSessions_EmptyList(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, "[]")
	}))
	defer ts.Close()

	var code int
	out := captureStdout(func() {
		code = handleSessions([]string{}, tsTestConfig(ts))
	})

	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	if !strings.Contains(out, "No active sessions") {
		t.Errorf("expected informational message for empty list, got: %q", out)
	}
}

func TestHandleSessions_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintln(w, `{"message":"server unavailable"}`)
	}))
	defer ts.Close()

	code := handleSessions([]string{}, tsTestConfig(ts))
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
}

func TestHandleSessions_UnexpectedArg(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()
	code := handleSessions([]string{"extra"}, tsTestConfig(ts))
	if code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

// --- handleTerminate ---

func TestHandleTerminate_Success(t *testing.T) {
	session := SessionSummary{
		SessionID: "sess-xyz", Agent: "claude",
		CreatedAt: time.Now(), LastMessageAt: time.Now(),
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/sessions/sess-xyz/terminate" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(session)
	}))
	defer ts.Close()

	var code int
	out := captureStdout(func() {
		code = handleTerminate([]string{"sess-xyz"}, tsTestConfig(ts))
	})

	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	if !strings.Contains(out, "sess-xyz") {
		t.Errorf("expected session ID in output, got: %q", out)
	}
	if !strings.Contains(out, "terminated") {
		t.Errorf("expected 'terminated' in output, got: %q", out)
	}
}

func TestHandleTerminate_MissingID(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()
	code := handleTerminate([]string{}, tsTestConfig(ts))
	if code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

func TestHandleTerminate_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintln(w, `{"message":"session not found"}`)
	}))
	defer ts.Close()

	code := handleTerminate([]string{"no-such-session"}, tsTestConfig(ts))
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
}

func TestHandleTerminate_ServerUnreachable(t *testing.T) {
	cfg := &Config{ServerURL: "http://localhost:19998", Token: "tok", Quiet: true}
	code := handleTerminate([]string{"sess-xyz"}, cfg)
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
}

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestClient creates an APIClient pointed at the given test server with verbose output
// captured in the supplied buffer.
func newTestClient(server *httptest.Server, verboseBuf *bytes.Buffer) *APIClient {
	cfg := &Config{
		ServerURL: server.URL,
		Token:     "test-token",
		Verbose:   verboseBuf != nil,
	}
	c := NewAPIClient(cfg)
	if verboseBuf != nil {
		c.verboseOut = verboseBuf
	}
	return c
}

// --- Auth header injection ---

func TestAPIClient_AuthHeaderInjected(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv, nil)
	c.Health()

	if gotAuth != "Bearer test-token" {
		t.Errorf("expected 'Bearer test-token', got %q", gotAuth)
	}
}

func TestAPIClient_AuthHeaderInjected_GetRuns(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := newTestClient(srv, nil)
	c.GetRuns()

	if gotAuth != "Bearer test-token" {
		t.Errorf("expected 'Bearer test-token', got %q", gotAuth)
	}
}

// --- Health ---

func TestAPIClient_Health_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" || r.Method != http.MethodGet {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv, nil)
	if err := c.Health(); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestAPIClient_Health_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":"shutting_down","message":"Server is shutting down."}`))
	}))
	defer srv.Close()

	c := newTestClient(srv, nil)
	err := c.Health()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "shutting down") {
		t.Errorf("unexpected error message: %v", err)
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", apiErr.StatusCode)
	}
}

func TestAPIClient_Health_Unreachable(t *testing.T) {
	cfg := &Config{ServerURL: "http://127.0.0.1:19999", Token: "tok"}
	c := NewAPIClient(cfg)
	err := c.Health()
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
	if !strings.Contains(err.Error(), "unreachable") {
		t.Errorf("expected 'unreachable' in error, got %v", err)
	}
}

// --- PostRun ---

func TestAPIClient_PostRun_200(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	run := RunResponse{ID: "run1", Status: "completed", Agent: "claude", Result: "hello", CreatedAt: now}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/run" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(run)
	}))
	defer srv.Close()

	c := newTestClient(srv, nil)
	got, accepted, err := c.PostRun(RunRequest{Prompt: "hi", Agent: "claude"})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if accepted != nil {
		t.Fatal("expected nil accepted response on 200")
	}
	if got.ID != "run1" || got.Status != "completed" {
		t.Errorf("unexpected response: %+v", got)
	}
}

func TestAPIClient_PostRun_202(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(RunAcceptedResponse{ID: "run2", Status: "pending"})
	}))
	defer srv.Close()

	c := newTestClient(srv, nil)
	run, accepted, err := c.PostRun(RunRequest{Prompt: "hi", Agent: "claude"})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if run != nil {
		t.Fatal("expected nil run on 202")
	}
	if accepted == nil || accepted.ID != "run2" {
		t.Errorf("unexpected accepted response: %+v", accepted)
	}
}

func TestAPIClient_PostRun_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"bad_request","message":"Prompt is required."}`))
	}))
	defer srv.Close()

	c := newTestClient(srv, nil)
	_, _, err := c.PostRun(RunRequest{Agent: "claude"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "Prompt is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- GetRun ---

func TestAPIClient_GetRun(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	run := RunResponse{ID: "abc", Status: "completed", Agent: "claude", CreatedAt: now}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/run/abc" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(run)
	}))
	defer srv.Close()

	c := newTestClient(srv, nil)
	got, err := c.GetRun("abc")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got.ID != "abc" {
		t.Errorf("unexpected run ID: %s", got.ID)
	}
}

func TestAPIClient_GetRun_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not_found","message":"Task not found."}`))
	}))
	defer srv.Close()

	c := newTestClient(srv, nil)
	_, err := c.GetRun("missing")
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", apiErr.StatusCode)
	}
	if !strings.Contains(err.Error(), "Task not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- GetRuns ---

func TestAPIClient_GetRuns(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	tasks := []TaskSummary{{ID: "t1", Status: "completed", Agent: "claude", CreatedAt: now}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/runs" || r.Method != http.MethodGet {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(tasks)
	}))
	defer srv.Close()

	c := newTestClient(srv, nil)
	got, err := c.GetRuns()
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(got) != 1 || got[0].ID != "t1" {
		t.Errorf("unexpected tasks: %+v", got)
	}
}

// --- CancelRun ---

func TestAPIClient_CancelRun(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	run := RunResponse{ID: "r1", Status: "cancelled", Agent: "claude", CreatedAt: now}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/run/r1/cancel" || r.Method != http.MethodPost {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(run)
	}))
	defer srv.Close()

	c := newTestClient(srv, nil)
	got, err := c.CancelRun("r1")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got.Status != "cancelled" {
		t.Errorf("unexpected status: %s", got.Status)
	}
}

func TestAPIClient_CancelRun_Conflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error":"conflict","message":"Task is already in a terminal status."}`))
	}))
	defer srv.Close()

	c := newTestClient(srv, nil)
	_, err := c.CancelRun("r1")
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, _ := err.(*APIError)
	if apiErr.StatusCode != http.StatusConflict {
		t.Errorf("expected 409, got %d", apiErr.StatusCode)
	}
}

// --- GetSessions ---

func TestAPIClient_GetSessions(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	sessions := []SessionSummary{{SessionID: "s1", Agent: "kiro", CreatedAt: now, LastMessageAt: now}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sessions" || r.Method != http.MethodGet {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(sessions)
	}))
	defer srv.Close()

	c := newTestClient(srv, nil)
	got, err := c.GetSessions()
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(got) != 1 || got[0].SessionID != "s1" {
		t.Errorf("unexpected sessions: %+v", got)
	}
}

// --- TerminateSession ---

func TestAPIClient_TerminateSession(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	session := SessionSummary{SessionID: "s2", Agent: "claude", CreatedAt: now, LastMessageAt: now}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sessions/s2/terminate" || r.Method != http.MethodPost {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(session)
	}))
	defer srv.Close()

	c := newTestClient(srv, nil)
	got, err := c.TerminateSession("s2")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got.SessionID != "s2" {
		t.Errorf("unexpected session ID: %s", got.SessionID)
	}
}

func TestAPIClient_TerminateSession_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not_found","message":"Session not found."}`))
	}))
	defer srv.Close()

	c := newTestClient(srv, nil)
	_, err := c.TerminateSession("missing")
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, _ := err.(*APIError)
	if apiErr.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", apiErr.StatusCode)
	}
}

// --- Verbose output ---

func TestAPIClient_VerboseOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	var buf bytes.Buffer
	c := newTestClient(srv, &buf)
	c.GetRuns()

	out := buf.String()
	if !strings.Contains(out, "GET") || !strings.Contains(out, "/runs") {
		t.Errorf("expected verbose log with GET /runs, got: %q", out)
	}
	if !strings.Contains(out, "200") {
		t.Errorf("expected status code 200 in verbose log, got: %q", out)
	}
	if !strings.Contains(out, "ms") {
		t.Errorf("expected timing in verbose log, got: %q", out)
	}
}

func TestAPIClient_NoVerboseOutput_WhenDisabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := newTestClient(srv, nil) // verbose disabled
	c.GetRuns()
	// No output to check — the test passes as long as no panic occurs.
}

// --- Structured error parsing fallback ---

func TestAPIClient_ParseError_Fallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	c := newTestClient(srv, nil)
	_, err := c.GetRun("x")
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, _ := err.(*APIError)
	if apiErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", apiErr.StatusCode)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected '500' in fallback error, got %v", err)
	}
}

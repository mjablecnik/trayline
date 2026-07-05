package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fastPoll is a very short interval used in tests to avoid slow polling.
const fastPoll = 5 * time.Millisecond

// runTestConfig creates a Config pointing at the given test server.
func runTestConfig(ts *httptest.Server) *Config {
	return &Config{ServerURL: ts.URL, Token: "tok", Quiet: true}
}

// --- handleRun flag parsing ---

func TestHandleRun_MissingAgent(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()
	code := handleRun([]string{"--prompt", "hello"}, runTestConfig(ts))
	if code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

func TestHandleRun_MissingPrompt(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()
	code := handleRun([]string{"--agent", "claude"}, runTestConfig(ts))
	if code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

func TestHandleRun_UnexpectedPositionalArg(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()
	code := handleRun([]string{"--agent", "claude", "--prompt", "hi", "extra"}, runTestConfig(ts))
	if code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

func TestHandleRun_HelpFlag(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()
	code := handleRun([]string{"--help"}, runTestConfig(ts))
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
}

// --- HTTP 200 immediate result ---

func TestHandleRun_Immediate200_DisplaysResult(t *testing.T) {
	now := time.Now()
	completed := now.Add(2 * time.Second)
	result := "Hello, result!"
	run := RunResponse{
		ID:          "task-200",
		Status:      "completed",
		Agent:       "claude",
		Result:      result,
		CreatedAt:   now,
		CompletedAt: &completed,
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/run" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("auth header: got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(run)
	}))
	defer ts.Close()

	var stdout bytes.Buffer
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := handleRun([]string{"--agent", "claude", "--prompt", "Hello"}, runTestConfig(ts))

	w.Close()
	os.Stdout = old
	io.Copy(&stdout, r)

	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), result) {
		t.Errorf("expected %q in stdout, got %q", result, stdout.String())
	}
}

func TestHandleRun_Immediate200_RequestBodyFields(t *testing.T) {
	now := time.Now()
	run := RunResponse{ID: "t1", Status: "completed", Agent: "kiro", CreatedAt: now}

	var gotBody RunRequest
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(run)
	}))
	defer ts.Close()

	handleRun([]string{
		"--agent", "kiro", "--prompt", "my prompt",
		"--model", "gpt-4", "--system", "be helpful",
		"--format", "markdown",
	}, runTestConfig(ts))

	if gotBody.Agent != "kiro" {
		t.Errorf("agent: got %q", gotBody.Agent)
	}
	if gotBody.Prompt != "my prompt" {
		t.Errorf("prompt: got %q", gotBody.Prompt)
	}
	if gotBody.Model != "gpt-4" {
		t.Errorf("model: got %q", gotBody.Model)
	}
	if gotBody.System != "be helpful" {
		t.Errorf("system: got %q", gotBody.System)
	}
	if gotBody.OutputFormat != "markdown" {
		t.Errorf("output_format: got %q", gotBody.OutputFormat)
	}
}

func TestHandleRun_Immediate200_ElapsedTimeOnStderr(t *testing.T) {
	now := time.Now()
	completed := now.Add(3 * time.Second)
	run := RunResponse{
		ID: "t2", Status: "completed", Agent: "claude",
		Result: "res", CreatedAt: now, CompletedAt: &completed,
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(run)
	}))
	defer ts.Close()

	r, w, _ := os.Pipe()
	oldErr := os.Stderr
	os.Stderr = w

	cfg := runTestConfig(ts)
	cfg.Quiet = false
	handleRun([]string{"--agent", "claude", "--prompt", "hi"}, cfg)

	w.Close()
	os.Stderr = oldErr
	var buf bytes.Buffer
	io.Copy(&buf, r)
	stderr := buf.String()

	if !strings.Contains(stderr, "t2") {
		t.Errorf("expected task ID in stderr, got: %q", stderr)
	}
	if !strings.Contains(stderr, "completed") {
		t.Errorf("expected status in stderr, got: %q", stderr)
	}
	if !strings.Contains(stderr, "elapsed") {
		t.Errorf("expected elapsed in stderr, got: %q", stderr)
	}
}

// --- HTTP 202 polling ---

func TestHandleRun_202_PollsUntilCompleted(t *testing.T) {
	now := time.Now()
	completed := now.Add(1 * time.Second)
	taskID := "task-poll"
	pollCount := 0

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/run":
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(RunAcceptedResponse{ID: taskID, Status: "running"})
		case r.Method == http.MethodGet && r.URL.Path == "/run/"+taskID:
			pollCount++
			if pollCount < 3 {
				json.NewEncoder(w).Encode(RunResponse{ID: taskID, Status: "running", Agent: "claude", CreatedAt: now})
			} else {
				json.NewEncoder(w).Encode(RunResponse{
					ID: taskID, Status: "completed", Agent: "claude",
					Result: "done!", CreatedAt: now, CompletedAt: &completed,
				})
			}
		default:
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
			http.Error(w, "not found", 404)
		}
	}))
	defer ts.Close()

	req := RunRequest{Agent: "claude", Prompt: "hi"}
	code := executeRun(req, "", runTestConfig(ts), fastPoll, time.Minute)
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	if pollCount < 3 {
		t.Errorf("expected >=3 polls, got %d", pollCount)
	}
}

func TestHandleRun_202_FailedStatus(t *testing.T) {
	now := time.Now()
	taskID := "task-fail"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/run" {
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(RunAcceptedResponse{ID: taskID, Status: "running"})
			return
		}
		json.NewEncoder(w).Encode(RunResponse{
			ID: taskID, Status: "failed", Agent: "claude",
			Error: "agent crashed", CreatedAt: now,
		})
	}))
	defer ts.Close()

	req := RunRequest{Agent: "claude", Prompt: "hi"}
	code := executeRun(req, "", runTestConfig(ts), fastPoll, time.Minute)
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
}

func TestHandleRun_202_CancelledStatus(t *testing.T) {
	now := time.Now()
	taskID := "task-cancel"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/run" {
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(RunAcceptedResponse{ID: taskID, Status: "running"})
			return
		}
		json.NewEncoder(w).Encode(RunResponse{
			ID: taskID, Status: "cancelled", Agent: "claude", CreatedAt: now,
		})
	}))
	defer ts.Close()

	req := RunRequest{Agent: "claude", Prompt: "hi"}
	code := executeRun(req, "", runTestConfig(ts), fastPoll, time.Minute)
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
}

func TestHandleRun_202_PollingTimeout(t *testing.T) {
	taskID := "task-timeout"
	now := time.Now()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/run" {
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(RunAcceptedResponse{ID: taskID, Status: "running"})
			return
		}
		// Always returns running — will eventually trigger timeout.
		json.NewEncoder(w).Encode(RunResponse{ID: taskID, Status: "running", Agent: "claude", CreatedAt: now})
	}))
	defer ts.Close()

	req := RunRequest{Agent: "claude", Prompt: "hi"}
	// Use a very short timeout so test finishes quickly.
	code := executeRun(req, "", runTestConfig(ts), fastPoll, 20*time.Millisecond)
	if code != 1 {
		t.Errorf("expected exit 1 (timeout), got %d", code)
	}
}

// --- valid=false warning ---

func TestHandleRun_ValidFalse_JSONFormatWarning(t *testing.T) {
	now := time.Now()
	validFalse := false
	run := RunResponse{
		ID: "t3", Status: "completed", Agent: "claude",
		Result: `{"bad": json}`, Valid: &validFalse, CreatedAt: now,
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(run)
	}))
	defer ts.Close()

	rr, ww, _ := os.Pipe()
	oldErr := os.Stderr
	os.Stderr = ww

	cfg := runTestConfig(ts)
	cfg.Quiet = false
	handleRun([]string{"--agent", "claude", "--prompt", "hi", "--format", "json"}, cfg)

	ww.Close()
	os.Stderr = oldErr
	var buf bytes.Buffer
	io.Copy(&buf, rr)
	stderr := buf.String()

	if !strings.Contains(stderr, "Warning") {
		t.Errorf("expected validation warning in stderr, got: %q", stderr)
	}
}

func TestHandleRun_ValidFalse_NonJSONFormatNoWarning(t *testing.T) {
	now := time.Now()
	validFalse := false
	run := RunResponse{
		ID: "t4", Status: "completed", Agent: "claude",
		Result: "text result", Valid: &validFalse, CreatedAt: now,
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(run)
	}))
	defer ts.Close()

	rr, ww, _ := os.Pipe()
	oldErr := os.Stderr
	os.Stderr = ww

	cfg := runTestConfig(ts)
	cfg.Quiet = false
	handleRun([]string{"--agent", "claude", "--prompt", "hi", "--format", "text"}, cfg)

	ww.Close()
	os.Stderr = oldErr
	var buf bytes.Buffer
	io.Copy(&buf, rr)
	stderr := buf.String()

	if strings.Contains(stderr, "Warning") {
		t.Errorf("unexpected validation warning for non-json format: %q", stderr)
	}
}

// --- server error handling ---

func TestHandleRun_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, `{"error":"internal","message":"server exploded"}`)
	}))
	defer ts.Close()

	code := handleRun([]string{"--agent", "claude", "--prompt", "hi"}, runTestConfig(ts))
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
}

func TestHandleRun_ServerUnreachable(t *testing.T) {
	cfg := &Config{ServerURL: "http://localhost:19999", Token: "tok", Quiet: true}
	code := handleRun([]string{"--agent", "claude", "--prompt", "hi"}, cfg)
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
}

// --- --file flag: parsing and validation ---

func TestHandleRun_FileFlag_NonExistentFile(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()
	code := handleRun([]string{
		"--agent", "claude", "--prompt", "hi",
		"--file", "/nonexistent/path/file.txt",
	}, runTestConfig(ts))
	if code != 2 {
		t.Errorf("expected exit 2 for non-existent file, got %d", code)
	}
}

func TestHandleRun_FileFlag_ValidFile_SendsMultipart(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "data.txt")
	if err := os.WriteFile(tmpFile, []byte("file content"), 0644); err != nil {
		t.Fatal(err)
	}

	var gotContentType string
	now := time.Now()
	run := RunResponse{ID: "t-mp", Status: "completed", Agent: "claude", CreatedAt: now}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(run)
	}))
	defer ts.Close()

	code := handleRun([]string{
		"--agent", "claude", "--prompt", "hello", "--file", tmpFile,
	}, runTestConfig(ts))
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	if !strings.HasPrefix(gotContentType, "multipart/form-data") {
		t.Errorf("expected multipart content type, got %q", gotContentType)
	}
}

func TestHandleRun_FileFlag_MultipleFiles_FormFieldsAndFiles(t *testing.T) {
	dir := t.TempDir()
	f1 := filepath.Join(dir, "a.txt")
	f2 := filepath.Join(dir, "b.csv")
	if err := os.WriteFile(f1, []byte("aaa"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f2, []byte("bbb"), 0644); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	run := RunResponse{ID: "t-mp2", Status: "completed", Agent: "kiro", CreatedAt: now}

	var gotPrompt, gotAgent string
	var gotFileNames []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(10 << 20); err == nil {
			gotPrompt = r.FormValue("prompt")
			gotAgent = r.FormValue("agent")
			for _, fhs := range r.MultipartForm.File {
				for _, fh := range fhs {
					gotFileNames = append(gotFileNames, fh.Filename)
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(run)
	}))
	defer ts.Close()

	code := handleRun([]string{
		"--agent", "kiro", "--prompt", "analyze", "--file", f1, "--file", f2,
	}, runTestConfig(ts))
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	if gotPrompt != "analyze" {
		t.Errorf("prompt: got %q", gotPrompt)
	}
	if gotAgent != "kiro" {
		t.Errorf("agent: got %q", gotAgent)
	}
	if len(gotFileNames) != 2 {
		t.Errorf("expected 2 files, got %d: %v", len(gotFileNames), gotFileNames)
	}
}

func TestHandleRun_NoFileFlagUsesJSON(t *testing.T) {
	var gotContentType string
	now := time.Now()
	run := RunResponse{ID: "t-json", Status: "completed", Agent: "claude", CreatedAt: now}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(run)
	}))
	defer ts.Close()

	code := handleRun([]string{"--agent", "claude", "--prompt", "hi"}, runTestConfig(ts))
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	if gotContentType != "application/json" {
		t.Errorf("expected application/json content type, got %q", gotContentType)
	}
}

func TestHandleRun_FileFlag_SecondFileNonExistent(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "real.txt")
	if err := os.WriteFile(tmpFile, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()

	code := handleRun([]string{
		"--agent", "claude", "--prompt", "hi",
		"--file", tmpFile,
		"--file", "/no/such/file.pdf",
	}, runTestConfig(ts))
	if code != 2 {
		t.Errorf("expected exit 2 for non-existent second file, got %d", code)
	}
}

func TestHandleRun_PollServerError(t *testing.T) {
	taskID := "task-poll-err"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/run" {
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(RunAcceptedResponse{ID: taskID, Status: "running"})
			return
		}
		// Polling returns 500
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, `{"message":"poll error"}`)
	}))
	defer ts.Close()

	req := RunRequest{Agent: "claude", Prompt: "hi"}
	code := executeRun(req, "", runTestConfig(ts), fastPoll, time.Minute)
	if code != 1 {
		t.Errorf("expected exit 1 on poll server error, got %d", code)
	}
}

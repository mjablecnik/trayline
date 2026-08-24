package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseArgs_PositionalOnly(t *testing.T) {
	pa, err := parseArgs([]string{"echo hi"}, map[string]bool{"name": true})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if len(pa.positional) != 1 || pa.positional[0] != "echo hi" {
		t.Errorf("unexpected positional: %+v", pa.positional)
	}
	if len(pa.flags) != 0 {
		t.Errorf("expected no flags, got %+v", pa.flags)
	}
}

func TestParseArgs_FlagWithSeparateValue(t *testing.T) {
	pa, err := parseArgs([]string{"--name", "my-task"}, map[string]bool{"name": true})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if pa.flags["name"] != "my-task" {
		t.Errorf("expected name=my-task, got %+v", pa.flags)
	}
}

func TestParseArgs_FlagWithEqualsValue(t *testing.T) {
	pa, err := parseArgs([]string{"--name=my-task"}, map[string]bool{"name": true})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if pa.flags["name"] != "my-task" {
		t.Errorf("expected name=my-task, got %+v", pa.flags)
	}
}

func TestParseArgs_FlagInMiddleOfPositionals(t *testing.T) {
	pa, err := parseArgs([]string{"echo hi", "--name", "my-task", "extra"}, map[string]bool{"name": true})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if len(pa.positional) != 2 || pa.positional[0] != "echo hi" || pa.positional[1] != "extra" {
		t.Errorf("unexpected positional: %+v", pa.positional)
	}
	if pa.flags["name"] != "my-task" {
		t.Errorf("expected name=my-task, got %+v", pa.flags)
	}
}

func TestParseArgs_UnknownFlagReturnsError(t *testing.T) {
	_, err := parseArgs([]string{"--bogus", "value"}, map[string]bool{"name": true})
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

func TestParseArgs_UnknownFlagWithEqualsReturnsError(t *testing.T) {
	_, err := parseArgs([]string{"--bogus=value"}, map[string]bool{"name": true})
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

func TestParseArgs_FlagMissingValueReturnsError(t *testing.T) {
	_, err := parseArgs([]string{"--name"}, map[string]bool{"name": true})
	if err == nil {
		t.Fatal("expected error for flag missing a value")
	}
}

func TestUsageError_PrintsAndReturnsExit2(t *testing.T) {
	var buf bytes.Buffer
	code := usageError(&buf, "bad input")
	if code != 2 {
		t.Errorf("expected exit code 2, got %d", code)
	}
	if !strings.Contains(buf.String(), "Error: bad input") {
		t.Errorf("expected error message on stderr, got %q", buf.String())
	}
}

func TestServerError_PrintsAndReturnsExit1(t *testing.T) {
	var buf bytes.Buffer
	code := serverError(&buf, &APIError{Code: "CONFLICT", Message: "boom"})
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(buf.String(), "Error: boom") {
		t.Errorf("expected error message on stderr, got %q", buf.String())
	}
}

func TestExecute_UnknownSubcommandReturnsUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Execute("bogus", nil, NewClient("http://unused", "proj", ""), &stdout, &stderr)
	if code != 2 {
		t.Errorf("expected exit code 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), `unknown subcommand "bogus"`) {
		t.Errorf("expected unknown subcommand message, got %q", stderr.String())
	}
}

// jsonHandler builds an httptest.Server that always responds with status and
// the JSON encoding of body, and a Client pointed at it.
func jsonHandler(t *testing.T, status int, body interface{}) (*httptest.Server, *Client) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if body != nil {
			json.NewEncoder(w).Encode(body)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, NewClient(srv.URL, "proj", "")
}

func errorHandler(t *testing.T, status int, code, message string) (*httptest.Server, *Client) {
	t.Helper()
	return jsonHandler(t, status, map[string]string{"error": code, "message": message})
}

func TestCmdAdd_NoCommandArgReturnsExit2(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cmdAdd(NewClient("http://unused", "proj", ""), nil, &stdout, &stderr)
	if code != 2 {
		t.Errorf("expected exit code 2, got %d", code)
	}
}

func TestCmdAdd_MultiplePositionalsReturnsExit2(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cmdAdd(NewClient("http://unused", "proj", ""), []string{"echo", "hi"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("expected exit code 2, got %d", code)
	}
}

func TestCmdAdd_NonIntegerPositionReturnsExit2(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cmdAdd(NewClient("http://unused", "proj", ""), []string{"echo hi", "--position", "abc"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("expected exit code 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "--position must be an integer") {
		t.Errorf("expected position error, got %q", stderr.String())
	}
}

func TestCmdAdd_SuccessPrintsCreatedLine(t *testing.T) {
	_, c := jsonHandler(t, http.StatusOK, CreateTaskResponse{
		ID: "abc", Name: "brave-tiger", Command: "echo hi", Status: "pending", Position: 0,
	})
	var stdout, stderr bytes.Buffer
	code := cmdAdd(c, []string{"echo hi"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, stderr.String())
	}
	want := "Task brave-tiger (abc) created: echo hi [pending] at position 0\n"
	if stdout.String() != want {
		t.Errorf("expected %q, got %q", want, stdout.String())
	}
}

func TestCmdAdd_ServerErrorReturnsExit1(t *testing.T) {
	_, c := errorHandler(t, http.StatusConflict, "CONFLICT", "name taken")
	var stdout, stderr bytes.Buffer
	code := cmdAdd(c, []string{"echo hi"}, &stdout, &stderr)
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "name taken") {
		t.Errorf("expected server error message, got %q", stderr.String())
	}
}

func TestCmdList_ExtraArgsReturnsExit2(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cmdList(NewClient("http://unused", "proj", ""), []string{"extra"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("expected exit code 2, got %d", code)
	}
}

func TestCmdList_SuccessPrintsTable(t *testing.T) {
	_, c := jsonHandler(t, http.StatusOK, []TaskListItem{
		{Position: 0, ID: "a", Name: "n1", Command: "echo hi", Status: "running"},
	})
	var stdout, stderr bytes.Buffer
	code := cmdList(c, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "n1") || !strings.Contains(stdout.String(), "echo hi") {
		t.Errorf("expected table containing task, got %q", stdout.String())
	}
}

func TestCmdDelete_WrongArgCountReturnsExit2(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cmdDelete(NewClient("http://unused", "proj", ""), nil, &stdout, &stderr)
	if code != 2 {
		t.Errorf("expected exit code 2, got %d", code)
	}
}

func TestCmdDelete_SuccessPrintsLine(t *testing.T) {
	_, c := jsonHandler(t, http.StatusOK, Task{ID: "abc", Name: "brave-tiger", Command: "echo hi", Status: "pending"})
	var stdout, stderr bytes.Buffer
	code := cmdDelete(c, []string{"abc"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, stderr.String())
	}
	want := "Task brave-tiger (abc) deleted\n"
	if stdout.String() != want {
		t.Errorf("expected %q, got %q", want, stdout.String())
	}
}

func TestCmdUpdate_WrongArgCountReturnsExit2(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cmdUpdate(NewClient("http://unused", "proj", ""), nil, &stdout, &stderr)
	if code != 2 {
		t.Errorf("expected exit code 2, got %d", code)
	}
}

func TestCmdUpdate_NeitherCommandNorNameReturnsExit2(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cmdUpdate(NewClient("http://unused", "proj", ""), []string{"abc"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("expected exit code 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "at least one of --command or --name") {
		t.Errorf("expected missing-fields message, got %q", stderr.String())
	}
}

func TestCmdUpdate_SuccessPrintsLine(t *testing.T) {
	_, c := jsonHandler(t, http.StatusOK, Task{ID: "abc", Name: "brave-tiger", Command: "echo updated", Status: "pending"})
	var stdout, stderr bytes.Buffer
	code := cmdUpdate(c, []string{"abc", "--command", "echo updated"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, stderr.String())
	}
	want := "Task brave-tiger (abc) updated: echo updated\n"
	if stdout.String() != want {
		t.Errorf("expected %q, got %q", want, stdout.String())
	}
}

func TestCmdRetry_ExtraArgsReturnsExit2(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cmdRetry(NewClient("http://unused", "proj", ""), []string{"extra"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("expected exit code 2, got %d", code)
	}
}

func TestCmdRetry_SuccessPrintsLine(t *testing.T) {
	_, c := jsonHandler(t, http.StatusOK, Task{ID: "abc", Name: "brave-tiger", Command: "echo hi", Status: "pending"})
	var stdout, stderr bytes.Buffer
	code := cmdRetry(c, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, stderr.String())
	}
	want := "Task brave-tiger (abc) retried: pending\n"
	if stdout.String() != want {
		t.Errorf("expected %q, got %q", want, stdout.String())
	}
}

func TestCmdSkip_ExtraArgsReturnsExit2(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cmdSkip(NewClient("http://unused", "proj", ""), []string{"extra"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("expected exit code 2, got %d", code)
	}
}

func TestCmdSkip_SuccessPrintsLine(t *testing.T) {
	_, c := jsonHandler(t, http.StatusOK, IDNameResult{ID: "abc", Name: "brave-tiger"})
	var stdout, stderr bytes.Buffer
	code := cmdSkip(c, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, stderr.String())
	}
	want := "Task brave-tiger (abc) skipped\n"
	if stdout.String() != want {
		t.Errorf("expected %q, got %q", want, stdout.String())
	}
}

func TestCmdStop_ExtraArgsReturnsExit2(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cmdStop(NewClient("http://unused", "proj", ""), []string{"extra"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("expected exit code 2, got %d", code)
	}
}

func TestCmdStop_SuccessPrintsLine(t *testing.T) {
	_, c := jsonHandler(t, http.StatusOK, Task{ID: "abc", Name: "brave-tiger", Command: "echo hi", Status: "failed"})
	var stdout, stderr bytes.Buffer
	code := cmdStop(c, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, stderr.String())
	}
	want := "Task brave-tiger (abc) stopped: echo hi\n"
	if stdout.String() != want {
		t.Errorf("expected %q, got %q", want, stdout.String())
	}
}

func TestCmdResume_ExtraArgsReturnsExit2(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cmdResume(NewClient("http://unused", "proj", ""), []string{"extra"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("expected exit code 2, got %d", code)
	}
}

func TestCmdResume_WithMessage(t *testing.T) {
	_, c := jsonHandler(t, http.StatusOK, QueueActionResult{State: "idle", Message: "no pending tasks"})
	var stdout, stderr bytes.Buffer
	code := cmdResume(c, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, stderr.String())
	}
	want := "Queue state: idle (no pending tasks)\n"
	if stdout.String() != want {
		t.Errorf("expected %q, got %q", want, stdout.String())
	}
}

func TestCmdResume_WithoutMessage(t *testing.T) {
	_, c := jsonHandler(t, http.StatusOK, QueueActionResult{State: "running"})
	var stdout, stderr bytes.Buffer
	code := cmdResume(c, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, stderr.String())
	}
	want := "Queue state: running\n"
	if stdout.String() != want {
		t.Errorf("expected %q, got %q", want, stdout.String())
	}
}

func TestCmdStatus_ExtraArgsReturnsExit2(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cmdStatus(NewClient("http://unused", "proj", ""), []string{"extra"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("expected exit code 2, got %d", code)
	}
}

func TestCmdStatus_PrintsCurrentAndFailedTaskLines(t *testing.T) {
	_, c := jsonHandler(t, http.StatusOK, QueueStatusResult{
		State:        "halted",
		PendingCount: 2,
		CurrentTask:  &TaskBrief{ID: "a", Name: "run-task", Command: "echo running"},
		FailedTask:   &FailedInfo{ID: "b", Name: "fail-task", Command: "echo fail", ExitCode: 1},
	})
	var stdout, stderr bytes.Buffer
	code := cmdStatus(c, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"State: halted\n",
		"Pending: 2\n",
		"Current task: run-task (a) - echo running\n",
		"Failed task: fail-task (b) - echo fail [exit 1]\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got %q", want, out)
		}
	}
}

func TestCmdStatus_OmitsCurrentAndFailedTaskLinesWhenNil(t *testing.T) {
	_, c := jsonHandler(t, http.StatusOK, QueueStatusResult{State: "idle", PendingCount: 0})
	var stdout, stderr bytes.Buffer
	code := cmdStatus(c, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "Current task:") || strings.Contains(stdout.String(), "Failed task:") {
		t.Errorf("expected no task lines, got %q", stdout.String())
	}
}

func TestCmdProjects_ExtraArgsReturnsExit2(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cmdProjects(NewClient("http://unused", "proj", ""), []string{"extra"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("expected exit code 2, got %d", code)
	}
}

func TestCmdProjects_SuccessPrintsTable(t *testing.T) {
	_, c := jsonHandler(t, http.StatusOK, []ProjectListItem{
		{Name: "dashboard", State: "running", PendingCount: 3},
		{Name: "backend", State: "idle", PendingCount: 0},
	})
	var stdout, stderr bytes.Buffer
	code := cmdProjects(c, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "dashboard") || !strings.Contains(out, "running") || !strings.Contains(out, "backend") {
		t.Errorf("expected table containing projects, got %q", out)
	}
}

func TestCmdProjects_EmptyPrintsMessage(t *testing.T) {
	_, c := jsonHandler(t, http.StatusOK, []ProjectListItem{})
	var stdout, stderr bytes.Buffer
	code := cmdProjects(c, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "No projects") {
		t.Errorf("expected empty-state message, got %q", stdout.String())
	}
}

func TestCmdLogs_UnknownFlagReturnsExit2(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cmdLogs(NewClient("http://unused", "proj", ""), []string{"--bogus"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("expected exit code 2, got %d", code)
	}
}

func TestCmdLogs_NonIntegerTailReturnsExit2(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cmdLogs(NewClient("http://unused", "proj", ""), []string{"--tail", "abc"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("expected exit code 2, got %d", code)
	}
}

func TestCmdLogs_TailWithoutFollowPrintsContentAndExitsWithoutStreaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/proj/logs" {
			t.Errorf("expected only the tail endpoint to be hit, got %s", r.URL.Path)
		}
		if r.URL.RawQuery != "tail=2" {
			t.Errorf("expected tail=2 query, got %q", r.URL.RawQuery)
		}
		fmt.Fprint(w, "line1\nline2\n")
	}))
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, "proj", "")

	var stdout, stderr bytes.Buffer
	code := cmdLogs(c, []string{"--tail", "2"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, stderr.String())
	}
	if stdout.String() != "line1\nline2\n" {
		t.Errorf("unexpected output: %q", stdout.String())
	}
}

func TestCmdLogs_NoFlagsFollowsStreamByDefault(t *testing.T) {
	var hitTail, hitStream bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/projects/proj/logs":
			hitTail = true
			if r.URL.Query().Get("tail") != "300" {
				t.Errorf("expected tail=300, got %q", r.URL.Query().Get("tail"))
			}
			fmt.Fprint(w, "history line\n")
		case "/projects/proj/logs/stream":
			hitStream = true
			fmt.Fprint(w, "data: hello world\n\n")
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, "proj", "")

	var stdout, stderr bytes.Buffer
	code := cmdLogs(c, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, stderr.String())
	}
	if !hitTail {
		t.Error("expected tail endpoint to be hit for history")
	}
	if !hitStream {
		t.Error("expected stream endpoint to be hit")
	}
	if !strings.Contains(stdout.String(), "history line") {
		t.Errorf("expected history content, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "hello world") {
		t.Errorf("expected streamed line, got %q", stdout.String())
	}
}

func TestCmdLogs_TailThenFollowHitsBothEndpoints(t *testing.T) {
	var hitTail, hitStream bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/projects/proj/logs":
			hitTail = true
			fmt.Fprint(w, "old line\n")
		case "/projects/proj/logs/stream":
			hitStream = true
			fmt.Fprint(w, "data: new line\n\n")
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, "proj", "")

	var stdout, stderr bytes.Buffer
	code := cmdLogs(c, []string{"--follow", "--tail", "10"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, stderr.String())
	}
	if !hitTail || !hitStream {
		t.Errorf("expected both endpoints to be hit, tail=%v stream=%v", hitTail, hitStream)
	}
	if !strings.Contains(stdout.String(), "old line") || !strings.Contains(stdout.String(), "new line") {
		t.Errorf("expected both tail and streamed content, got %q", stdout.String())
	}
}

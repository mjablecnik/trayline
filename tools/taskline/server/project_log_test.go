package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

var projectLogLineRE = regexp.MustCompile(`^\[[^\]]+\] \[([^\]]*)\] (.*)$`)

func newTestProjectLog(t *testing.T) (*ProjectLog, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "project.log")
	l, err := NewProjectLog(path)
	if err != nil {
		t.Fatalf("NewProjectLog: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l, path
}

func TestProjectLog_WriteFormatsCompleteLine(t *testing.T) {
	l, path := newTestProjectLog(t)
	l.SetCurrentTask("my-task")

	if _, err := l.Write([]byte("hello world\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	line := strings.TrimSuffix(string(data), "\n")
	m := projectLogLineRE.FindStringSubmatch(line)
	if m == nil {
		t.Fatalf("line %q does not match expected format", line)
	}
	if m[1] != "my-task" {
		t.Errorf("expected task prefix %q, got %q", "my-task", m[1])
	}
	if m[2] != "hello world" {
		t.Errorf("expected message %q, got %q", "hello world", m[2])
	}
}

func TestProjectLog_WriteBuffersPartialLineAcrossCalls(t *testing.T) {
	l, path := newTestProjectLog(t)
	l.SetCurrentTask("t")

	if _, err := l.Write([]byte("hel")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if data, _ := os.ReadFile(path); len(data) != 0 {
		t.Fatalf("expected nothing written before a newline is seen, got %q", data)
	}

	if _, err := l.Write([]byte("lo\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "hello") {
		t.Fatalf("expected the completed line to contain %q, got %q", "hello", data)
	}
}

func TestProjectLog_WriteHandlesMultipleLinesInOneCall(t *testing.T) {
	l, path := newTestProjectLog(t)
	l.SetCurrentTask("t")

	if _, err := l.Write([]byte("line1\nline2\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 emitted lines, got %d: %q", len(lines), data)
	}
	if !strings.HasSuffix(lines[0], "line1") || !strings.HasSuffix(lines[1], "line2") {
		t.Fatalf("expected lines to end with line1/line2, got %q", lines)
	}
}

func TestProjectLog_CloseFlushesTrailingPartialLine(t *testing.T) {
	l, path := newTestProjectLog(t)
	l.SetCurrentTask("t")

	if _, err := l.Write([]byte("no newline yet")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "no newline yet") {
		t.Fatalf("expected buffered partial line to be flushed on Close, got %q", data)
	}
}

func TestProjectLog_SubscribeReceivesBroadcastLines(t *testing.T) {
	l, _ := newTestProjectLog(t)
	l.SetCurrentTask("t")

	ch := l.Subscribe()
	defer l.Unsubscribe(ch)

	if _, err := l.Write([]byte("broadcast me\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	select {
	case data := <-ch:
		if !strings.Contains(string(data), "broadcast me") {
			t.Errorf("expected broadcast line to contain %q, got %q", "broadcast me", data)
		}
	case <-time.After(time.Second):
		t.Fatal("expected a broadcast line, got none")
	}
}

func TestProjectLog_UnsubscribeStopsDelivery(t *testing.T) {
	l, _ := newTestProjectLog(t)
	l.SetCurrentTask("t")

	ch := l.Subscribe()
	l.Unsubscribe(ch)

	if _, err := l.Write([]byte("after unsubscribe\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if _, ok := <-ch; ok {
		t.Fatal("expected channel to be closed after Unsubscribe")
	}
}

func TestProjectLog_SlowSubscriberDoesNotBlockWrite(t *testing.T) {
	l, _ := newTestProjectLog(t)
	l.SetCurrentTask("t")

	ch := l.Subscribe()
	defer l.Unsubscribe(ch)

	// Fill the subscriber's buffered channel without draining it, then make
	// sure a further Write still returns promptly instead of blocking on a
	// full channel.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			_, _ = l.Write([]byte("line\n"))
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Write blocked on a slow/non-draining subscriber")
	}
}

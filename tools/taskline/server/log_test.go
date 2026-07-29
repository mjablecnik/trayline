package main

import (
	"bufio"
	"os"
	"regexp"
	"testing"
)

var logLineRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} \[(INFO|WARN|ERROR)\] (.+)$`)

// captureStdout redirects os.Stdout to a pipe for the duration of fn and
// returns the single line written to it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	os.Stdout = orig

	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		t.Fatal("expected a line written to stdout, got none")
	}
	line := scanner.Text()
	if scanner.Scan() {
		t.Fatalf("expected exactly one line, got extra: %q", scanner.Text())
	}
	return line
}

func TestLogInfo_EmitsFormattedLine(t *testing.T) {
	line := captureStdout(t, func() {
		logInfo("task %s started", "abc123")
	})

	m := logLineRE.FindStringSubmatch(line)
	if m == nil {
		t.Fatalf("line %q does not match expected format", line)
	}
	if m[1] != "INFO" {
		t.Errorf("expected level INFO, got %s", m[1])
	}
	if m[2] != "task abc123 started" {
		t.Errorf("expected message %q, got %q", "task abc123 started", m[2])
	}
}

func TestLogWarn_EmitsFormattedLine(t *testing.T) {
	line := captureStdout(t, func() {
		logWarn("queue %s is halted", "main")
	})

	m := logLineRE.FindStringSubmatch(line)
	if m == nil {
		t.Fatalf("line %q does not match expected format", line)
	}
	if m[1] != "WARN" {
		t.Errorf("expected level WARN, got %s", m[1])
	}
	if m[2] != "queue main is halted" {
		t.Errorf("expected message %q, got %q", "queue main is halted", m[2])
	}
}

func TestLogError_EmitsFormattedLine(t *testing.T) {
	line := captureStdout(t, func() {
		logError("task %s failed with exit %d", "abc123", 1)
	})

	m := logLineRE.FindStringSubmatch(line)
	if m == nil {
		t.Fatalf("line %q does not match expected format", line)
	}
	if m[1] != "ERROR" {
		t.Errorf("expected level ERROR, got %s", m[1])
	}
	if m[2] != "task abc123 failed with exit 1" {
		t.Errorf("expected message %q, got %q", "task abc123 failed with exit 1", m[2])
	}
}

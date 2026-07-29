package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun_NoArgsPrintsUsageOnStderrAndReturnsExit2(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)
	if code != 2 {
		t.Errorf("expected exit code 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "Usage: taskline") {
		t.Errorf("expected usage text on stderr, got %q", stderr.String())
	}
	if stdout.String() != "" {
		t.Errorf("expected no stdout output, got %q", stdout.String())
	}
}

func TestRun_HelpFlagPrintsUsageOnStdoutAndReturnsExit0(t *testing.T) {
	for _, flag := range []string{"-h", "--help"} {
		t.Run(flag, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run([]string{flag}, &stdout, &stderr)
			if code != 0 {
				t.Errorf("expected exit code 0, got %d", code)
			}
			if !strings.Contains(stdout.String(), "Usage: taskline") {
				t.Errorf("expected usage text on stdout, got %q", stdout.String())
			}
			if stderr.String() != "" {
				t.Errorf("expected no stderr output, got %q", stderr.String())
			}
		})
	}
}

func TestRun_VersionFlagPrintsVersionAndReturnsExit0(t *testing.T) {
	for _, flag := range []string{"-v", "--version"} {
		t.Run(flag, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run([]string{flag}, &stdout, &stderr)
			if code != 0 {
				t.Errorf("expected exit code 0, got %d", code)
			}
			want := "taskline 1.0.0\n"
			if stdout.String() != want {
				t.Errorf("expected %q, got %q", want, stdout.String())
			}
		})
	}
}

func TestRun_InvalidTasklineURLReturnsExit2(t *testing.T) {
	clearTasklineURLEnv(t)
	t.Setenv("TASKLINE_URL", "not-a-url")

	var stdout, stderr bytes.Buffer
	code := run([]string{"status"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("expected exit code 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "Error:") {
		t.Errorf("expected error message on stderr, got %q", stderr.String())
	}
}

func TestRun_ValidSubcommandDispatchesToExecute(t *testing.T) {
	clearTasklineURLEnv(t)
	// No server listens on this port, so Execute reaches the client and
	// returns a connection-error exit code (1) rather than a usage error (2)
	// — proving dispatch occurred instead of failing at arg parsing.
	t.Setenv("TASKLINE_URL", "http://127.0.0.1:1")

	var stdout, stderr bytes.Buffer
	code := run([]string{"status"}, &stdout, &stderr)
	if code != 1 {
		t.Errorf("expected exit code 1 (connection error), got %d (stderr=%q)", code, stderr.String())
	}
}

package main

import (
	"bytes"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// requireSh skips the test if no "sh" binary is available on PATH, since
// ShellRunner shells out to it directly (Requirement 3.4).
func requireSh(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available on PATH")
	}
}

func TestShellRunner_StartRunsCommandViaShell(t *testing.T) {
	requireSh(t)

	var out bytes.Buffer
	proc, err := ShellRunner{}.Start("echo hi", &out)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	exitCode, err := proc.Wait()
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if got := strings.TrimSpace(out.String()); got != "hi" {
		t.Fatalf("expected captured output %q, got %q", "hi", got)
	}
}

func TestShellRunner_NonZeroExitCodePropagated(t *testing.T) {
	requireSh(t)

	var out bytes.Buffer
	proc, err := ShellRunner{}.Start("exit 7", &out)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	exitCode, err := proc.Wait()
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if exitCode != 7 {
		t.Fatalf("expected exit code 7, got %d", exitCode)
	}
}

func TestShellRunner_StdoutAndStderrBothGoToOutput(t *testing.T) {
	requireSh(t)

	var out bytes.Buffer
	proc, err := ShellRunner{}.Start("echo out-line; echo err-line 1>&2", &out)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if _, err := proc.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "out-line") {
		t.Fatalf("expected output to contain stdout line, got %q", got)
	}
	if !strings.Contains(got, "err-line") {
		t.Fatalf("expected output to contain stderr line, got %q", got)
	}
}

func TestShellRunner_SignalTerminatesProcess(t *testing.T) {
	requireSh(t)

	var out bytes.Buffer
	proc, err := ShellRunner{}.Start("sleep 100", &out)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := proc.Signal(syscall.SIGKILL); err != nil {
		t.Fatalf("Signal: %v", err)
	}

	done := make(chan struct{})
	go func() {
		proc.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("process did not terminate after SIGKILL within 5s")
	}
}

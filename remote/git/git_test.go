package git

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// initRepo creates a fresh git repository in a temp directory with one commit.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(cmd.Environ(),
			"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	return dir
}

func TestRunReturnsStdout(t *testing.T) {
	dir := initRepo(t)
	r := NewRunner()

	out, err := r.Run(dir, "status", "--porcelain")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "" {
		t.Errorf("expected empty status for fresh repo, got %q", out)
	}
}

func TestRunPrependsNoPager(t *testing.T) {
	// git --no-pager log on a repo with no commits yet exits non-zero with a
	// specific stderr message; asserting on that confirms --no-pager was
	// accepted as a valid global flag rather than passed through as a bad arg.
	dir := initRepo(t)
	r := NewRunner()

	_, err := r.Run(dir, "log")
	if err == nil {
		t.Fatal("expected error for log on empty repo")
	}
	var gitErr *Error
	if !asError(err, &gitErr) {
		t.Fatalf("expected *git.Error, got %T: %v", err, err)
	}
	if !strings.Contains(gitErr.Stderr, "does not have any commits yet") {
		t.Errorf("expected stderr to mention missing commits, got %q", gitErr.Stderr)
	}
}

func TestRunNonZeroExitReturnsStructuredError(t *testing.T) {
	dir := initRepo(t)
	r := NewRunner()

	_, err := r.Run(dir, "this-is-not-a-git-command")
	if err == nil {
		t.Fatal("expected error for invalid git subcommand")
	}
	var gitErr *Error
	if !asError(err, &gitErr) {
		t.Fatalf("expected *git.Error, got %T: %v", err, err)
	}
	if gitErr.Timeout {
		t.Error("did not expect a timeout error")
	}
}

func TestRunTimesOut(t *testing.T) {
	dir := initRepo(t)
	r := &Runner{Timeout: 1 * time.Nanosecond}

	_, err := r.Run(dir, "status")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	var gitErr *Error
	if !asError(err, &gitErr) {
		t.Fatalf("expected *git.Error, got %T: %v", err, err)
	}
	if !gitErr.Timeout {
		t.Errorf("expected Timeout=true, got error: %v", gitErr)
	}
}

func TestNewRunnerDefaultTimeout(t *testing.T) {
	r := NewRunner()
	if r.Timeout != DefaultTimeout {
		t.Errorf("expected default timeout %v, got %v", DefaultTimeout, r.Timeout)
	}
}

// asError is a small helper mirroring errors.As without importing it twice
// at call sites in this test file.
func asError(err error, target **Error) bool {
	if e, ok := err.(*Error); ok {
		*target = e
		return true
	}
	return false
}

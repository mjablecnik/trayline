// Package git executes git CLI commands against project repositories for
// the dashboard API, with bounded timeouts and structured errors.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// DefaultTimeout is used when Runner.Timeout is unset.
const DefaultTimeout = 5 * time.Second

// Runner executes git commands with a bounded timeout.
type Runner struct {
	// Timeout bounds how long a single git command may run. If zero,
	// DefaultTimeout is used.
	Timeout time.Duration
}

// NewRunner returns a Runner configured with DefaultTimeout.
func NewRunner() *Runner {
	return &Runner{Timeout: DefaultTimeout}
}

// Error wraps a failed git invocation with its arguments and stderr output.
type Error struct {
	Args    []string
	Stderr  string
	Timeout bool
	Err     error
}

func (e *Error) Error() string {
	if e.Timeout {
		return fmt.Sprintf("git %s: timed out", strings.Join(e.Args, " "))
	}
	msg := fmt.Sprintf("git %s: %v", strings.Join(e.Args, " "), e.Err)
	if e.Stderr != "" {
		msg += ": " + e.Stderr
	}
	return msg
}

func (e *Error) Unwrap() error { return e.Err }

// Run executes `git --no-pager <args...>` in repoPath and returns trimmed
// stdout. On a non-zero exit or timeout, it returns a *Error.
func (r *Runner) Run(repoPath string, args ...string) (string, error) {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	fullArgs := append([]string{"--no-pager"}, args...)
	cmd := exec.CommandContext(ctx, "git", fullArgs...)
	cmd.Dir = repoPath

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", &Error{Args: args, Timeout: true, Err: ctx.Err()}
		}
		return "", &Error{Args: args, Stderr: strings.TrimSpace(stderr.String()), Err: err}
	}

	return stdout.String(), nil
}

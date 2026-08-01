package main

import (
	"errors"
	"io"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// stopGraceTimeout is how long Stop waits after SIGTERM before escalating to
// SIGKILL. Requirement 5.2. Declared as a var (not const) so tests can
// shrink it to keep the SIGKILL-escalation path fast.
var stopGraceTimeout = 5 * time.Second

// Exit codes the Worker assigns itself, distinct from any code a command's
// own process could report.
const (
	// ExitCodeStopped marks a Task terminated via Stop (Requirement 5.3).
	ExitCodeStopped = -1
	// ExitCodeSpawnFailure marks a Task whose command process could not be
	// started at all (Requirement 3.6).
	ExitCodeSpawnFailure = -3
	// ExitCodeWaitFailure marks a Task whose process could not be waited on
	// (e.g. an I/O error unrelated to the command's own exit status).
	ExitCodeWaitFailure = -4
)

// Process abstracts a running OS process so tests can substitute a fake
// implementation without spawning real shells.
type Process interface {
	// Wait blocks until the process exits and returns its exit code.
	Wait() (exitCode int, err error)
	// Signal delivers sig to the process.
	Signal(sig syscall.Signal) error
}

// CommandRunner abstracts command execution for testability.
type CommandRunner interface {
	// Start spawns command in dir, piping its stdout and stderr to output, and
	// returns a handle to the running Process. If dir is empty, the process
	// inherits the server's working directory.
	Start(command, dir string, output io.Writer) (Process, error)
}

// ShellRunner is the production CommandRunner: it executes commands via
// "sh -c" to support shell features such as pipes, redirects, and chaining
// (Requirement 3.4).
type ShellRunner struct{}

// Start implements CommandRunner.
func (ShellRunner) Start(command, dir string, output io.Writer) (Process, error) {
	cmd := exec.Command("sh", "-c", command)
	cmd.Stdout = output
	cmd.Stderr = output
	if dir != "" {
		cmd.Dir = dir
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &osProcess{cmd: cmd}, nil
}

type osProcess struct {
	cmd *exec.Cmd
}

func (p *osProcess) Wait() (int, error) {
	err := p.cmd.Wait()
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return 0, err
}

func (p *osProcess) Signal(sig syscall.Signal) error {
	if p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Signal(sig)
}

// runningTask tracks the Process backing the Task currently executing so
// Stop can signal it and the run loop can report back once it has fully
// exited.
type runningTask struct {
	task    *Task
	process Process
	stopped bool
	done    chan struct{}
}

// Worker is the single goroutine that pulls pending Tasks from a Queue and
// executes them one at a time via a CommandRunner (Requirement 3.1),
// persisting state and sending failure notifications as their status
// changes.
type Worker struct {
	queue     *Queue
	runner    CommandRunner
	notifier  Notifier
	stateFile string
	output    io.Writer

	wake chan struct{}
	quit chan struct{}

	mu      sync.Mutex
	running *runningTask
}

// NewWorker returns a Worker ready to be started with Run. output receives
// the piped stdout/stderr of every executed command (Requirement 3.5).
// notifier is invoked whenever a Task transitions to "failed". stateFile is
// where the Queue is persisted after every status change; persistence is
// skipped if stateFile is empty.
func NewWorker(queue *Queue, runner CommandRunner, notifier Notifier, stateFile string, output io.Writer) *Worker {
	return &Worker{
		queue:     queue,
		runner:    runner,
		notifier:  notifier,
		stateFile: stateFile,
		output:    output,
		wake:      make(chan struct{}, 1),
		quit:      make(chan struct{}),
	}
}

// Notify wakes the run loop so it re-checks the Queue for work, e.g. after a
// Task is added or the Queue is resumed, retried, or skipped.
func (w *Worker) Notify() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Shutdown stops the run loop once it is next idle. It does not interrupt a
// Task that is currently executing; callers that need to do that should call
// Stop first.
func (w *Worker) Shutdown() {
	close(w.quit)
}

// Run pulls the next pending Task from the Queue and executes it, blocking
// until Shutdown is called. It is intended to be run in its own goroutine.
func (w *Worker) Run() {
	for {
		select {
		case <-w.quit:
			return
		default:
		}

		task := w.queue.StartNext()
		if task == nil {
			select {
			case <-w.wake:
				continue
			case <-w.quit:
				return
			}
		}
		w.executeTask(task)
	}
}

// Stop terminates the currently running Task's process: SIGTERM, wait up to
// stopGraceTimeout, then SIGKILL if it hasn't exited (Requirements 5.1,
// 5.2). It blocks until the Task has fully transitioned to "failed" with
// exit code ExitCodeStopped and returns the updated Task (Requirement 5.3).
// It returns ErrNoRunningTask if no Task is currently running.
func (w *Worker) Stop() (*Task, error) {
	w.mu.Lock()
	rt := w.running
	if rt == nil {
		w.mu.Unlock()
		return nil, ErrNoRunningTask
	}
	rt.stopped = true
	proc := rt.process
	task := rt.task
	w.mu.Unlock()

	_ = proc.Signal(syscall.SIGTERM)

	select {
	case <-rt.done:
	case <-time.After(stopGraceTimeout):
		_ = proc.Signal(syscall.SIGKILL)
		<-rt.done
	}
	return task, nil
}

// ForceKill immediately sends SIGKILL to the currently running Task's
// process, skipping the SIGTERM grace period Stop uses, and blocks until it
// has fully transitioned to "failed" with exit code ExitCodeStopped. Used by
// the server's shutdown sequence once the 30-second grace period for a
// running Task to finish on its own has already elapsed (Requirement 1.5).
// It returns ErrNoRunningTask if no Task is currently running.
func (w *Worker) ForceKill() (*Task, error) {
	w.mu.Lock()
	rt := w.running
	if rt == nil {
		w.mu.Unlock()
		return nil, ErrNoRunningTask
	}
	rt.stopped = true
	proc := rt.process
	task := rt.task
	w.mu.Unlock()

	_ = proc.Signal(syscall.SIGKILL)
	<-rt.done
	return task, nil
}

func (w *Worker) executeTask(task *Task) {
	proc, err := w.runner.Start(task.Command, task.Cwd, w.output)
	if err != nil {		logError("task %s (%s): failed to spawn command: %v", task.ID, task.Name, err)
		w.finishTask(task, ExitCodeSpawnFailure)
		return
	}

	rt := &runningTask{task: task, process: proc, done: make(chan struct{})}
	w.mu.Lock()
	w.running = rt
	w.mu.Unlock()

	logInfo("task %s (%s) started: %s", task.ID, task.Name, task.Command)

	exitCode, waitErr := proc.Wait()

	w.mu.Lock()
	stopped := rt.stopped
	w.running = nil
	w.mu.Unlock()

	switch {
	case stopped:
		exitCode = ExitCodeStopped
	case waitErr != nil:
		logError("task %s (%s): wait error: %v", task.ID, task.Name, waitErr)
		exitCode = ExitCodeWaitFailure
	}

	w.finishTask(task, exitCode)
	close(rt.done)
}

// finishTask transitions task in the Queue based on exitCode, persists
// state, and sends a failure notification if applicable. It is the single
// place responsible for reacting to a Task's completion, whether the
// command exited on its own, failed to spawn, or was stopped.
func (w *Worker) finishTask(task *Task, exitCode int) {
	if exitCode == 0 {
		if _, err := w.queue.MarkComplete(); err != nil {
			logError("task %s (%s): failed to mark complete: %v", task.ID, task.Name, err)
		}
		logInfo("task %s (%s) completed", task.ID, task.Name)
	} else {
		if _, err := w.queue.MarkFailed(exitCode); err != nil {
			logError("task %s (%s): failed to mark failed: %v", task.ID, task.Name, err)
		}
		logError("task %s (%s) failed with exit code %d", task.ID, task.Name, exitCode)

		if err := w.notifier.NotifyFailure(task); err != nil {
			logError("task %s (%s): failed to send failure notification: %v", task.ID, task.Name, err)
		}
	}

	if w.stateFile != "" {
		if err := SaveState(w.queue, w.stateFile); err != nil {
			logError("failed to persist state: %v", err)
		}
	}
}

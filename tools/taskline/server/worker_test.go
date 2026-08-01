package main

import (
	"bytes"
	"fmt"
	"io"
	"sync"
	"syscall"
	"testing"
	"time"
)

// fakeProcess is a Process test double whose exit behavior and signal
// handling are fully controlled by the test.
type fakeProcess struct {
	exitCode int
	waitErr  error
	waitCh   chan struct{}

	mu         sync.Mutex
	signals    []syscall.Signal
	exitOnTerm bool // simulate the process exiting once it receives SIGTERM or SIGKILL
	closeOnce  sync.Once
}

func newFakeProcess(exitCode int) *fakeProcess {
	return &fakeProcess{exitCode: exitCode, waitCh: make(chan struct{})}
}

func (p *fakeProcess) Wait() (int, error) {
	<-p.waitCh
	return p.exitCode, p.waitErr
}

func (p *fakeProcess) Signal(sig syscall.Signal) error {
	p.mu.Lock()
	p.signals = append(p.signals, sig)
	shouldExit := p.exitOnTerm
	p.mu.Unlock()

	if shouldExit {
		p.finish()
	}
	return nil
}

// finish unblocks Wait, simulating the underlying process exiting.
func (p *fakeProcess) finish() {
	p.closeOnce.Do(func() { close(p.waitCh) })
}

func (p *fakeProcess) signalsReceived() []syscall.Signal {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]syscall.Signal, len(p.signals))
	copy(out, p.signals)
	return out
}

// fakeRunner is a CommandRunner test double backed by a queue of canned
// processes (or errors) to return, one per Start call, keyed by command.
type fakeRunner struct {
	mu    sync.Mutex
	procs map[string][]*fakeProcess
	errs  map[string][]error
	calls []string
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{procs: map[string][]*fakeProcess{}, errs: map[string][]error{}}
}

func (r *fakeRunner) enqueue(command string, proc *fakeProcess) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.procs[command] = append(r.procs[command], proc)
}

func (r *fakeRunner) enqueueErr(command string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errs[command] = append(r.errs[command], err)
}

func (r *fakeRunner) Start(command, dir string, output io.Writer) (Process, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, command)

	if errs := r.errs[command]; len(errs) > 0 {
		err := errs[0]
		r.errs[command] = errs[1:]
		return nil, err
	}
	procs := r.procs[command]
	if len(procs) == 0 {
		panic(fmt.Sprintf("fakeRunner: no process enqueued for command %q", command))
	}
	proc := procs[0]
	r.procs[command] = procs[1:]
	if output != nil {
		io.WriteString(output, "")
	}
	return proc, nil
}

// fakeNotifier is a Notifier test double recording every call.
type fakeNotifier struct {
	mu    sync.Mutex
	calls []*Task
}

func (n *fakeNotifier) NotifyFailure(task *Task) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls = append(n.calls, task)
	return nil
}

func (n *fakeNotifier) callCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.calls)
}

func newTestQueue() *Queue {
	return NewQueue(NewNameGenerator())
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	if !cond() {
		t.Fatalf("condition not met within %s", timeout)
	}
}

func TestWorker_SuccessfulTaskRemovedAndQueueIdles(t *testing.T) {
	q := newTestQueue()
	task, err := q.AddTask("echo hi", "", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	runner := newFakeRunner()
	proc := newFakeProcess(0)
	runner.enqueue("echo hi", proc)
	proc.finish()

	notifier := &fakeNotifier{}
	w := NewWorker(q, runner, notifier, "", &bytes.Buffer{})

	go w.Run()
	defer w.Shutdown()

	waitFor(t, time.Second, func() bool {
		_, err := q.FindTask(task.ID)
		return err == ErrTaskNotFound
	})

	if q.CurrentState() != QueueIdle {
		t.Fatalf("expected queue idle, got %s", q.CurrentState())
	}
	if notifier.callCount() != 0 {
		t.Fatalf("expected no notifications on success, got %d", notifier.callCount())
	}
}

func TestWorker_FailedTaskHaltsQueueAndNotifies(t *testing.T) {
	q := newTestQueue()
	task, err := q.AddTask("false", "", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	runner := newFakeRunner()
	proc := newFakeProcess(1)
	runner.enqueue("false", proc)
	proc.finish()

	notifier := &fakeNotifier{}
	w := NewWorker(q, runner, notifier, "", &bytes.Buffer{})

	go w.Run()
	defer w.Shutdown()

	waitFor(t, time.Second, func() bool {
		got, err := q.Snapshot(task.ID)
		return err == nil && got.Status == TaskFailed
	})

	if q.CurrentState() != QueueHalted {
		t.Fatalf("expected queue halted, got %s", q.CurrentState())
	}
	got, _ := q.Snapshot(task.ID)
	if got.ExitCode == nil || *got.ExitCode != 1 {
		t.Fatalf("expected exit code 1, got %v", got.ExitCode)
	}
	if notifier.callCount() != 1 {
		t.Fatalf("expected 1 notification, got %d", notifier.callCount())
	}
}

func TestWorker_SpawnFailureMarksTaskFailed(t *testing.T) {
	q := newTestQueue()
	task, err := q.AddTask("nonexistent-binary", "", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	runner := newFakeRunner()
	runner.enqueueErr("nonexistent-binary", fmt.Errorf("exec: no such file"))

	notifier := &fakeNotifier{}
	w := NewWorker(q, runner, notifier, "", &bytes.Buffer{})

	go w.Run()
	defer w.Shutdown()

	waitFor(t, time.Second, func() bool {
		got, err := q.Snapshot(task.ID)
		return err == nil && got.Status == TaskFailed
	})

	got, _ := q.Snapshot(task.ID)
	if got.ExitCode == nil || *got.ExitCode != ExitCodeSpawnFailure {
		t.Fatalf("expected exit code %d, got %v", ExitCodeSpawnFailure, got.ExitCode)
	}
	if q.CurrentState() != QueueHalted {
		t.Fatalf("expected queue halted, got %s", q.CurrentState())
	}
}

func TestWorker_SequentialExecutionOrder(t *testing.T) {
	q := newTestQueue()
	first, err := q.AddTask("cmd-a", "", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	_, err = q.AddTask("cmd-b", "", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	runner := newFakeRunner()
	procA := newFakeProcess(0)
	procB := newFakeProcess(0)
	runner.enqueue("cmd-a", procA)
	runner.enqueue("cmd-b", procB)

	notifier := &fakeNotifier{}
	w := NewWorker(q, runner, notifier, "", &bytes.Buffer{})

	go w.Run()
	defer w.Shutdown()

	// cmd-b must not start until cmd-a finishes.
	time.Sleep(20 * time.Millisecond)
	if got, err := q.Snapshot(first.ID); err != nil || got.Status != TaskRunning {
		t.Fatalf("expected cmd-a running, got %+v (err=%v)", got, err)
	}
	runner.mu.Lock()
	calls := append([]string(nil), runner.calls...)
	runner.mu.Unlock()
	if len(calls) != 1 || calls[0] != "cmd-a" {
		t.Fatalf("expected only cmd-a started so far, got %v", calls)
	}

	procA.finish()
	waitFor(t, time.Second, func() bool {
		runner.mu.Lock()
		defer runner.mu.Unlock()
		return len(runner.calls) == 2
	})
	procB.finish()

	waitFor(t, time.Second, func() bool {
		return q.CurrentState() == QueueIdle
	})
}

func TestWorker_StopSendsSigtermAndMarksStopped(t *testing.T) {
	q := newTestQueue()
	task, err := q.AddTask("sleep 100", "", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	runner := newFakeRunner()
	proc := newFakeProcess(0)
	proc.exitOnTerm = true
	runner.enqueue("sleep 100", proc)

	notifier := &fakeNotifier{}
	w := NewWorker(q, runner, notifier, "", &bytes.Buffer{})

	go w.Run()
	defer w.Shutdown()

	waitFor(t, time.Second, func() bool {
		got, err := q.Snapshot(task.ID)
		return err == nil && got.Status == TaskRunning
	})

	stopped, err := w.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if stopped.ID != task.ID {
		t.Fatalf("expected stopped task %s, got %s", task.ID, stopped.ID)
	}

	got, _ := q.Snapshot(task.ID)
	if got.Status != TaskFailed {
		t.Fatalf("expected task failed after stop, got %s", got.Status)
	}
	if got.ExitCode == nil || *got.ExitCode != ExitCodeStopped {
		t.Fatalf("expected exit code %d, got %v", ExitCodeStopped, got.ExitCode)
	}
	if q.CurrentState() != QueueHalted {
		t.Fatalf("expected queue halted after stop, got %s", q.CurrentState())
	}

	signals := proc.signalsReceived()
	if len(signals) == 0 || signals[0] != syscall.SIGTERM {
		t.Fatalf("expected SIGTERM sent first, got %v", signals)
	}
	if notifier.callCount() != 1 {
		t.Fatalf("expected 1 notification after stop, got %d", notifier.callCount())
	}
}

func TestWorker_StopEscalatesToSigkillAfterGraceTimeout(t *testing.T) {
	q := newTestQueue()
	task, err := q.AddTask("sleep 100", "", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	runner := newFakeRunner()
	proc := newFakeProcess(0)
	// Does not react to SIGTERM; only exits once SIGKILL is received.
	runner.enqueue("sleep 100", proc)

	origTimeout := stopGraceTimeout
	stopGraceTimeout = 20 * time.Millisecond
	defer func() { stopGraceTimeout = origTimeout }()

	notifier := &fakeNotifier{}
	w := NewWorker(q, runner, notifier, "", &bytes.Buffer{})

	go w.Run()
	defer w.Shutdown()

	waitFor(t, time.Second, func() bool {
		got, err := q.Snapshot(task.ID)
		return err == nil && got.Status == TaskRunning
	})

	go func() {
		time.Sleep(50 * time.Millisecond)
		proc.finish()
	}()

	if _, err := w.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	signals := proc.signalsReceived()
	if len(signals) < 2 || signals[0] != syscall.SIGTERM || signals[1] != syscall.SIGKILL {
		t.Fatalf("expected SIGTERM then SIGKILL, got %v", signals)
	}
}

func TestWorker_StopWithNoRunningTaskReturnsError(t *testing.T) {
	q := newTestQueue()
	runner := newFakeRunner()
	notifier := &fakeNotifier{}
	w := NewWorker(q, runner, notifier, "", &bytes.Buffer{})

	go w.Run()
	defer w.Shutdown()

	if _, err := w.Stop(); err != ErrNoRunningTask {
		t.Fatalf("expected ErrNoRunningTask, got %v", err)
	}
}

func TestWorker_HaltedQueueDoesNotStartNextTask(t *testing.T) {
	q := newTestQueue()
	_, err := q.AddTask("false", "", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	_, err = q.AddTask("echo second", "", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	runner := newFakeRunner()
	failProc := newFakeProcess(1)
	runner.enqueue("false", failProc)
	failProc.finish()

	notifier := &fakeNotifier{}
	w := NewWorker(q, runner, notifier, "", &bytes.Buffer{})

	go w.Run()
	defer w.Shutdown()

	waitFor(t, time.Second, func() bool {
		return q.CurrentState() == QueueHalted
	})

	time.Sleep(20 * time.Millisecond)
	runner.mu.Lock()
	calls := append([]string(nil), runner.calls...)
	runner.mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("expected only the failing command started, got %v", calls)
	}
}

func TestWorker_ForceKillSendsOnlySigkillAndMarksStopped(t *testing.T) {
	q := newTestQueue()
	task, err := q.AddTask("sleep 100", "", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	runner := newFakeRunner()
	proc := newFakeProcess(0)
	proc.exitOnTerm = true // reacts to SIGKILL too, since SIGKILL sets shouldExit
	runner.enqueue("sleep 100", proc)

	notifier := &fakeNotifier{}
	w := NewWorker(q, runner, notifier, "", &bytes.Buffer{})

	go w.Run()
	defer w.Shutdown()

	waitFor(t, time.Second, func() bool {
		got, err := q.Snapshot(task.ID)
		return err == nil && got.Status == TaskRunning
	})

	killed, err := w.ForceKill()
	if err != nil {
		t.Fatalf("ForceKill: %v", err)
	}
	if killed.ID != task.ID {
		t.Fatalf("expected killed task %s, got %s", task.ID, killed.ID)
	}

	got, _ := q.Snapshot(task.ID)
	if got.Status != TaskFailed {
		t.Fatalf("expected task failed after ForceKill, got %s", got.Status)
	}
	if got.ExitCode == nil || *got.ExitCode != ExitCodeStopped {
		t.Fatalf("expected exit code %d, got %v", ExitCodeStopped, got.ExitCode)
	}

	signals := proc.signalsReceived()
	if len(signals) != 1 || signals[0] != syscall.SIGKILL {
		t.Fatalf("expected only SIGKILL sent, got %v", signals)
	}
	if notifier.callCount() != 1 {
		t.Fatalf("expected 1 notification after ForceKill, got %d", notifier.callCount())
	}
}

func TestWorker_ForceKillWithNoRunningTaskReturnsError(t *testing.T) {
	q := newTestQueue()
	runner := newFakeRunner()
	notifier := &fakeNotifier{}
	w := NewWorker(q, runner, notifier, "", &bytes.Buffer{})

	go w.Run()
	defer w.Shutdown()

	if _, err := w.ForceKill(); err != ErrNoRunningTask {
		t.Fatalf("expected ErrNoRunningTask, got %v", err)
	}
}

func TestWorker_FinishTask_SuccessSendsNoNotification(t *testing.T) {
	q := newTestQueue()
	task, err := q.AddTask("echo hi", "", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if q.StartNext() == nil {
		t.Fatal("StartNext: expected a task")
	}

	notifier := &fakeNotifier{}
	w := NewWorker(q, newFakeRunner(), notifier, "", &bytes.Buffer{})

	w.finishTask(task, 0)

	if notifier.callCount() != 0 {
		t.Fatalf("expected no notification on success, got %d", notifier.callCount())
	}
}

func TestWorker_FinishTask_FailureNotifiesOnce(t *testing.T) {
	q := newTestQueue()
	task, err := q.AddTask("false", "", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if q.StartNext() == nil {
		t.Fatal("StartNext: expected a task")
	}

	notifier := &fakeNotifier{}
	w := NewWorker(q, newFakeRunner(), notifier, "", &bytes.Buffer{})

	w.finishTask(task, 1)

	if notifier.callCount() != 1 {
		t.Fatalf("expected 1 notification on failure, got %d", notifier.callCount())
	}
}

func TestWorker_FinishTask_PersistErrorToUnwritableStateFileIsLoggedNotPanicked(t *testing.T) {
	q := newTestQueue()
	task, err := q.AddTask("echo hi", "", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if q.StartNext() == nil {
		t.Fatal("StartNext: expected a task")
	}

	notifier := &fakeNotifier{}
	// A state file inside a nonexistent directory makes SaveState fail at
	// os.CreateTemp; finishTask must log the error, not panic or block.
	w := NewWorker(q, newFakeRunner(), notifier, "/nonexistent-dir/state.json", &bytes.Buffer{})

	done := make(chan struct{})
	go func() {
		w.finishTask(task, 0)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("finishTask blocked on persist error")
	}
}

func TestWorker_PersistsStateAfterEachTask(t *testing.T) {
	dir := t.TempDir()
	statePath := dir + "/state.json"

	q := newTestQueue()
	task, err := q.AddTask("echo hi", "", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	runner := newFakeRunner()
	proc := newFakeProcess(0)
	runner.enqueue("echo hi", proc)
	proc.finish()

	notifier := &fakeNotifier{}
	w := NewWorker(q, runner, notifier, statePath, &bytes.Buffer{})

	go w.Run()
	defer w.Shutdown()

	waitFor(t, time.Second, func() bool {
		_, err := q.FindTask(task.ID)
		return err == ErrTaskNotFound
	})

	loaded, err := LoadState(statePath, NewNameGenerator())
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if loaded.State != QueueIdle {
		t.Fatalf("expected persisted state idle, got %s", loaded.State)
	}
	if len(loaded.Tasks) != 0 {
		t.Fatalf("expected no persisted tasks, got %d", len(loaded.Tasks))
	}
}

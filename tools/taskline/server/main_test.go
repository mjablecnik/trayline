package main

import (
	"testing"
	"time"
)

func TestRecoverRunningTask_RunningTaskMarkedFailedAndHalted(t *testing.T) {
	dir := t.TempDir()
	statePath := dir + "/state.json"

	q := newTestQueue()
	task, err := q.AddTask("sleep 100", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if running := q.StartNext(); running == nil {
		t.Fatal("expected StartNext to start the task")
	}

	notifier := &fakeNotifier{}
	recoverRunningTask(q, notifier, statePath)

	got, err := q.Snapshot(task.ID)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if got.Status != TaskFailed {
		t.Errorf("expected task status failed, got %s", got.Status)
	}
	if got.ExitCode == nil || *got.ExitCode != recoveredRunningExitCode {
		t.Errorf("expected exit code %d, got %v", recoveredRunningExitCode, got.ExitCode)
	}
	if q.CurrentState() != QueueHalted {
		t.Errorf("expected queue halted, got %s", q.CurrentState())
	}
	if notifier.callCount() != 1 {
		t.Errorf("expected 1 notification, got %d", notifier.callCount())
	}

	loaded, err := LoadState(statePath, NewNameGenerator())
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if loaded.State != QueueHalted {
		t.Errorf("expected persisted state halted, got %s", loaded.State)
	}
	if len(loaded.Tasks) != 1 || loaded.Tasks[0].Status != TaskFailed {
		t.Errorf("expected persisted failed task, got %+v", loaded.Tasks)
	}
}

func TestRecoverRunningTask_NoRunningTaskIsNoop(t *testing.T) {
	q := newTestQueue()
	task, err := q.AddTask("echo hi", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	notifier := &fakeNotifier{}
	recoverRunningTask(q, notifier, "")

	got, err := q.Snapshot(task.ID)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if got.Status != TaskPending {
		t.Errorf("expected task status unchanged (pending), got %s", got.Status)
	}
	if q.CurrentState() != QueueRunning {
		t.Errorf("expected queue state unchanged (running), got %s", q.CurrentState())
	}
	if notifier.callCount() != 0 {
		t.Errorf("expected no notifications, got %d", notifier.callCount())
	}
}

func TestRecoverRunningTask_EmptyStateFileDoesNotWriteOrPanic(t *testing.T) {
	q := newTestQueue()
	task, err := q.AddTask("sleep 100", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if running := q.StartNext(); running == nil {
		t.Fatal("expected StartNext to start the task")
	}

	notifier := &fakeNotifier{}
	recoverRunningTask(q, notifier, "")

	got, err := q.Snapshot(task.ID)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if got.Status != TaskFailed {
		t.Errorf("expected task status failed, got %s", got.Status)
	}
	if notifier.callCount() != 1 {
		t.Errorf("expected 1 notification, got %d", notifier.callCount())
	}
}

func TestWaitForIdle_ReturnsTrueImmediatelyWhenNoTaskRunning(t *testing.T) {
	q := newTestQueue()
	if _, err := q.AddTask("echo hi", "", nil); err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	start := time.Now()
	if !waitForIdle(q, time.Second) {
		t.Fatal("expected waitForIdle to return true when no task is running")
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("expected waitForIdle to return promptly, took %s", elapsed)
	}
}

func TestWaitForIdle_ReturnsFalseAfterTimeoutWhileTaskStillRunning(t *testing.T) {
	q := newTestQueue()
	if _, err := q.AddTask("sleep 100", "", nil); err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if running := q.StartNext(); running == nil {
		t.Fatal("expected StartNext to start the task")
	}

	if waitForIdle(q, 50*time.Millisecond) {
		t.Fatal("expected waitForIdle to return false while the task is still running")
	}
}

func TestEnabledLabel(t *testing.T) {
	if got := enabledLabel(true); got != "enabled" {
		t.Errorf(`expected "enabled", got %q`, got)
	}
	if got := enabledLabel(false); got != "disabled" {
		t.Errorf(`expected "disabled", got %q`, got)
	}
}

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	dir := t.TempDir()
	return NewRegistry(filepath.Join(dir, "state"), filepath.Join(dir, "logs"), NewNameGenerator(), noopNotifier{})
}

func TestValidateProjectName_AcceptsWellFormedNames(t *testing.T) {
	for _, name := range []string{"a", "dashboard", "back-end_2", "123", "a1_2-3"} {
		if err := ValidateProjectName(name); err != nil {
			t.Errorf("expected %q to be valid, got error: %v", name, err)
		}
	}
}

func TestValidateProjectName_RejectsEmpty(t *testing.T) {
	if err := ValidateProjectName(""); err == nil {
		t.Fatal("expected an error for an empty project name")
	}
}

func TestValidateProjectName_RejectsTooLong(t *testing.T) {
	name := ""
	for i := 0; i < 65; i++ {
		name += "a"
	}
	if err := ValidateProjectName(name); err == nil {
		t.Fatal("expected an error for a 65-character project name")
	}
}

func TestValidateProjectName_RejectsUppercaseAndSymbols(t *testing.T) {
	for _, name := range []string{"Dashboard", "back end", "back.end", "back/end"} {
		if err := ValidateProjectName(name); err == nil {
			t.Errorf("expected %q to be rejected", name)
		}
	}
}

func TestRegistry_GetOrCreate_InvalidNameReturnsError(t *testing.T) {
	r := newTestRegistry(t)
	if _, err := r.GetOrCreate("Not Valid"); err == nil {
		t.Fatal("expected an error for an invalid project name")
	}
}

func TestRegistry_GetOrCreate_ReturnsSameInstanceOnSecondCall(t *testing.T) {
	r := newTestRegistry(t)

	first, err := r.GetOrCreate("dashboard")
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	second, err := r.GetOrCreate("dashboard")
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	if first != second {
		t.Fatal("expected the second GetOrCreate call to return the same ProjectInstance")
	}
}

func TestRegistry_GetOrCreate_CreatesLogFileAndEmptyQueue(t *testing.T) {
	r := newTestRegistry(t)

	inst, err := r.GetOrCreate("dashboard")
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	defer inst.Worker.Shutdown()

	if inst.Name != "dashboard" {
		t.Errorf("expected instance name %q, got %q", "dashboard", inst.Name)
	}
	if inst.Queue.CurrentState() != QueueIdle || inst.Queue.PendingCount() != 0 {
		t.Errorf("expected a fresh, empty, idle queue, got state=%s pending=%d", inst.Queue.CurrentState(), inst.Queue.PendingCount())
	}
	if _, err := os.Stat(r.LogPath("dashboard")); err != nil {
		t.Errorf("expected log file to be created at %s: %v", r.LogPath("dashboard"), err)
	}
	if inst.StateFile != r.StatePath("dashboard") {
		t.Errorf("expected state file %q, got %q", r.StatePath("dashboard"), inst.StateFile)
	}
}

func TestRegistry_GetOrCreate_RestoresExistingState(t *testing.T) {
	r := newTestRegistry(t)

	seedQueue := NewQueue(NewNameGenerator())
	if _, err := seedQueue.AddTask("echo hi", "seed-task", "", nil); err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(r.StatePath("dashboard")), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := SaveState(seedQueue, r.StatePath("dashboard")); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	inst, err := r.GetOrCreate("dashboard")
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	defer inst.Worker.Shutdown()

	if len(inst.Queue.List()) != 1 || inst.Queue.List()[0].Name != "seed-task" {
		t.Fatalf("expected restored queue to contain the seeded task, got %+v", inst.Queue.List())
	}
}

func TestRegistry_GetOrCreate_ProjectsDoNotShareQueues(t *testing.T) {
	r := newTestRegistry(t)

	dashboard, err := r.GetOrCreate("dashboard")
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	defer dashboard.Worker.Shutdown()
	backend, err := r.GetOrCreate("backend")
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	defer backend.Worker.Shutdown()

	if _, err := dashboard.Queue.AddTask("echo hi", "", "", nil); err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if dashboard.Queue.PendingCount() != 1 {
		t.Errorf("expected dashboard queue to have 1 pending task, got %d", dashboard.Queue.PendingCount())
	}
	if backend.Queue.PendingCount() != 0 {
		t.Errorf("expected backend queue to be unaffected, got %d pending", backend.Queue.PendingCount())
	}
}

func TestRegistry_List_ReturnsSortedSummaries(t *testing.T) {
	r := newTestRegistry(t)

	for _, name := range []string{"frontend", "backend", "dashboard"} {
		inst, err := r.GetOrCreate(name)
		if err != nil {
			t.Fatalf("GetOrCreate(%q): %v", name, err)
		}
		defer inst.Worker.Shutdown()
	}
	backend, _ := r.GetOrCreate("backend")
	if _, err := backend.Queue.AddTask("echo hi", "", "", nil); err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	summaries := r.List()
	if len(summaries) != 3 {
		t.Fatalf("expected 3 project summaries, got %d", len(summaries))
	}
	names := []string{summaries[0].Name, summaries[1].Name, summaries[2].Name}
	want := []string{"backend", "dashboard", "frontend"}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("expected sorted names %v, got %v", want, names)
		}
	}
	if summaries[0].State != QueueRunning || summaries[0].PendingCount != 1 {
		t.Errorf("expected backend summary state=running pending=1, got state=%s pending=%d", summaries[0].State, summaries[0].PendingCount)
	}
}

func TestRegistry_Shutdown_PersistsStateAndClosesLog(t *testing.T) {
	r := newTestRegistry(t)

	inst, err := r.GetOrCreate("dashboard")
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	if _, err := inst.Queue.AddTask("sleep 5", "", "", nil); err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	inst.Worker.Notify()

	deadline := time.Now().Add(2 * time.Second)
	for inst.Queue.CurrentTask() == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if inst.Queue.CurrentTask() == nil {
		t.Fatal("expected the task to start running before Shutdown")
	}

	r.Shutdown(200 * time.Millisecond)

	loaded, err := LoadState(inst.StateFile, NewNameGenerator())
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if len(loaded.Tasks) != 1 || loaded.Tasks[0].Status != TaskFailed {
		t.Fatalf("expected the running task to be persisted as failed (stopped), got %+v", loaded.Tasks)
	}
	if loaded.Tasks[0].ExitCode == nil || *loaded.Tasks[0].ExitCode != ExitCodeStopped {
		t.Fatalf("expected exit code %d, got %v", ExitCodeStopped, loaded.Tasks[0].ExitCode)
	}

	if _, err := inst.LogWriter.file.Write([]byte("x")); err == nil {
		t.Fatal("expected the log file to be closed after Shutdown")
	}
}

func TestRegistry_Shutdown_NoProjectsIsNoop(t *testing.T) {
	r := newTestRegistry(t)
	done := make(chan struct{})
	go func() {
		r.Shutdown(time.Second)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected Shutdown with no projects to return promptly")
	}
}

func TestRegistry_TasksInDifferentProjectsExecuteInParallel(t *testing.T) {
	r := newTestRegistry(t)

	dashboard, err := r.GetOrCreate("dashboard")
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	defer dashboard.Worker.Shutdown()
	backend, err := r.GetOrCreate("backend")
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	defer backend.Worker.Shutdown()

	if _, err := dashboard.Queue.AddTask("sleep 0.3", "", "", nil); err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if _, err := backend.Queue.AddTask("sleep 0.3", "", "", nil); err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	dashboard.Worker.Notify()
	backend.Worker.Notify()

	// If the two projects' tasks were serialized onto a single worker
	// (FR-1.3 violation), the second task would not start running until
	// the first had already finished. Poll for a moment where both are
	// running at the same time to prove they execute concurrently.
	deadline := time.Now().Add(2 * time.Second)
	sawBothRunning := false
	for time.Now().Before(deadline) {
		if dashboard.Queue.CurrentTask() != nil && backend.Queue.CurrentTask() != nil {
			sawBothRunning = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !sawBothRunning {
		t.Fatal("expected both projects' tasks to be running at the same time")
	}
}

func TestRegistry_GetOrCreate_CorruptedStateStartsEmptyQueue(t *testing.T) {
	r := newTestRegistry(t)
	if err := os.MkdirAll(filepath.Dir(r.StatePath("dashboard")), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(r.StatePath("dashboard"), []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	inst, err := r.GetOrCreate("dashboard")
	if err != nil {
		t.Fatalf("expected corrupted state to be tolerated, got error: %v", err)
	}
	defer inst.Worker.Shutdown()
	if len(inst.Queue.List()) != 0 {
		t.Fatalf("expected an empty queue after corrupted state, got %+v", inst.Queue.List())
	}
}

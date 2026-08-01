package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"
)

func genTaskStatus(t *rapid.T, label string) TaskStatus {
	return rapid.SampledFrom([]TaskStatus{TaskPending, TaskRunning, TaskFailed}).Draw(t, label)
}

func genTask(t *rapid.T, label string) *Task {
	status := genTaskStatus(t, label+"Status")
	var exitCode *int
	if status == TaskFailed {
		code := rapid.IntRange(1, 255).Draw(t, label+"ExitCode")
		exitCode = &code
	}
	return &Task{
		ID:        rapid.StringMatching(`[a-z0-9]{8}`).Draw(t, label+"ID"),
		Name:      rapid.StringMatching(`[a-z][a-z0-9-]{0,20}`).Draw(t, label+"Name"),
		Command:   genCommand(t, label+"Command"),
		Status:    status,
		ExitCode:  exitCode,
		CreatedAt: time.Unix(rapid.Int64Range(0, 2000000000).Draw(t, label+"CreatedAt"), 0).UTC(),
	}
}

// Feature: taskline, Property 13: State persistence round-trip
//
// For any valid queue state (containing any combination of tasks with valid
// statuses, IDs, names, commands, and timestamps, plus a valid queue state),
// serializing to JSON and deserializing back shall produce an identical
// queue state — same tasks with same fields in the same order, same queue
// state value.
func TestProperty_StatePersistenceRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		state := rapid.SampledFrom([]QueueState{QueueIdle, QueueRunning, QueueHalted}).Draw(t, "state")
		n := rapid.IntRange(0, 8).Draw(t, "n")

		tasks := make([]*Task, 0, n)
		seenIDs := map[string]bool{}
		for i := 0; i < n; i++ {
			task := genTask(t, "task")
			if seenIDs[task.ID] {
				t.Skip("drew a duplicate task ID")
			}
			seenIDs[task.ID] = true
			tasks = append(tasks, task)
		}

		q := &Queue{State: state, Tasks: tasks, names: NewNameGenerator()}
		dir, err := os.MkdirTemp("", "taskline-state-test-*")
		if err != nil {
			t.Fatalf("unexpected error creating temp dir: %v", err)
		}
		defer os.RemoveAll(dir)
		path := filepath.Join(dir, "taskline-state.json")

		if err := SaveState(q, path); err != nil {
			t.Fatalf("unexpected error saving state: %v", err)
		}

		loaded, err := LoadState(path, NewNameGenerator())
		if err != nil {
			t.Fatalf("unexpected error loading state: %v", err)
		}

		if loaded.State != state {
			t.Fatalf("expected queue state %q, got %q", state, loaded.State)
		}
		if len(loaded.Tasks) != len(tasks) {
			t.Fatalf("expected %d tasks, got %d", len(tasks), len(loaded.Tasks))
		}
		for i, want := range tasks {
			got := loaded.Tasks[i]
			if got.ID != want.ID || got.Name != want.Name || got.Command != want.Command || got.Status != want.Status {
				t.Fatalf("task at index %d: expected %+v, got %+v", i, want, got)
			}
			if !got.CreatedAt.Equal(want.CreatedAt) {
				t.Fatalf("task at index %d: expected CreatedAt %v, got %v", i, want.CreatedAt, got.CreatedAt)
			}
			switch {
			case want.ExitCode == nil && got.ExitCode != nil:
				t.Fatalf("task at index %d: expected nil ExitCode, got %v", i, *got.ExitCode)
			case want.ExitCode != nil && (got.ExitCode == nil || *got.ExitCode != *want.ExitCode):
				t.Fatalf("task at index %d: expected ExitCode %v, got %v", i, *want.ExitCode, got.ExitCode)
			}
		}
	})
}

func TestSaveState_AtomicWriteAndReload(t *testing.T) {
	q := &Queue{State: QueueIdle, Tasks: []*Task{}, names: NewNameGenerator()}
	if _, err := q.AddTask("echo hi", "", "", nil); err != nil {
		t.Fatalf("unexpected error adding task: %v", err)
	}

	path := filepath.Join(t.TempDir(), "taskline-state.json")
	if err := SaveState(q, path); err != nil {
		t.Fatalf("unexpected error saving state: %v", err)
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("unexpected error reading state dir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(path) {
			t.Fatalf("expected no leftover temp files, found %q", e.Name())
		}
	}

	loaded, err := LoadState(path, NewNameGenerator())
	if err != nil {
		t.Fatalf("unexpected error loading state: %v", err)
	}
	if len(loaded.Tasks) != 1 || loaded.Tasks[0].Command != "echo hi" {
		t.Fatalf("expected loaded queue to contain the saved task, got %+v", loaded.Tasks)
	}
}

func TestLoadState_MissingFileReturnsEmptyIdleQueue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")

	q, err := LoadState(path, NewNameGenerator())
	if err != nil {
		t.Fatalf("expected no error for missing state file, got %v", err)
	}
	if q.State != QueueIdle {
		t.Fatalf("expected idle queue state, got %q", q.State)
	}
	if len(q.Tasks) != 0 {
		t.Fatalf("expected empty task list, got %d tasks", len(q.Tasks))
	}
}

func TestLoadState_CorruptedJSONIsRenamedAndReturnsEmptyQueue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "taskline-state.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("unexpected error writing corrupted file: %v", err)
	}

	q, err := LoadState(path, NewNameGenerator())
	if !errors.Is(err, ErrCorruptedState) {
		t.Fatalf("expected ErrCorruptedState, got %v", err)
	}
	if q.State != QueueIdle || len(q.Tasks) != 0 {
		t.Fatalf("expected empty idle queue, got state=%q tasks=%d", q.State, len(q.Tasks))
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected original corrupted file to be moved away, stat err: %v", err)
	}
	if _, err := os.Stat(path + ".corrupted"); err != nil {
		t.Fatalf("expected corrupted file renamed with .corrupted suffix: %v", err)
	}
}

func TestLoadState_SchemaMismatchIsTreatedAsCorrupted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "taskline-state.json")
	data, err := json.Marshal(map[string]any{"state": "bogus", "tasks": []any{}})
	if err != nil {
		t.Fatalf("unexpected error marshaling fixture: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("unexpected error writing fixture: %v", err)
	}

	q, err := LoadState(path, NewNameGenerator())
	if !errors.Is(err, ErrCorruptedState) {
		t.Fatalf("expected ErrCorruptedState for unrecognized queue state, got %v", err)
	}
	if q.State != QueueIdle || len(q.Tasks) != 0 {
		t.Fatalf("expected empty idle queue, got state=%q tasks=%d", q.State, len(q.Tasks))
	}
}

func TestSaveState_UnwritableDirectoryReturnsError(t *testing.T) {
	q := &Queue{State: QueueIdle, Tasks: []*Task{}, names: NewNameGenerator()}
	path := filepath.Join(t.TempDir(), "does-not-exist", "taskline-state.json")

	if err := SaveState(q, path); err == nil {
		t.Fatal("expected an error saving state to a nonexistent directory")
	}
}

func TestLoadState_PathIsDirectoryReturnsReadError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "taskline-state.json")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("unexpected error creating directory fixture: %v", err)
	}

	q, err := LoadState(path, NewNameGenerator())
	if err == nil {
		t.Fatal("expected an error loading state from a path that is a directory")
	}
	if errors.Is(err, ErrCorruptedState) {
		t.Fatalf("expected a plain read error (not ErrCorruptedState) for a directory path, got %v", err)
	}
	if q.State != QueueIdle || len(q.Tasks) != 0 {
		t.Fatalf("expected empty idle queue, got state=%q tasks=%d", q.State, len(q.Tasks))
	}
}

func TestLoadState_CorruptedJSONRenameFailureWrapsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "taskline-state.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("unexpected error writing corrupted file: %v", err)
	}
	// Pre-create the ".corrupted" target as a directory so os.Rename (file
	// onto existing directory) fails, forcing LoadState's rename-error branch.
	if err := os.Mkdir(path+".corrupted", 0o755); err != nil {
		t.Fatalf("unexpected error creating rename-target directory: %v", err)
	}

	q, err := LoadState(path, NewNameGenerator())
	if !errors.Is(err, ErrCorruptedState) {
		t.Fatalf("expected error to wrap ErrCorruptedState, got %v", err)
	}
	if !strings.Contains(err.Error(), "also failed to rename corrupted file") {
		t.Fatalf("expected error to mention the rename failure, got %v", err)
	}
	if q.State != QueueIdle || len(q.Tasks) != 0 {
		t.Fatalf("expected empty idle queue, got state=%q tasks=%d", q.State, len(q.Tasks))
	}
}

func TestStateFileValid_RejectsUnknownQueueState(t *testing.T) {
	sf := stateFile{State: QueueState("bogus"), Tasks: nil}
	if sf.valid() {
		t.Fatal("expected an unrecognized queue state to be rejected")
	}
}

func TestStateFileValid_RejectsNilTask(t *testing.T) {
	sf := stateFile{State: QueueIdle, Tasks: []*Task{nil}}
	if sf.valid() {
		t.Fatal("expected a nil task to be rejected")
	}
}

func TestStateFileValid_RejectsEmptyID(t *testing.T) {
	sf := stateFile{State: QueueIdle, Tasks: []*Task{{ID: "", Command: "echo hi", Status: TaskPending}}}
	if sf.valid() {
		t.Fatal("expected a task with an empty ID to be rejected")
	}
}

func TestStateFileValid_RejectsWhitespaceOnlyCommand(t *testing.T) {
	sf := stateFile{State: QueueIdle, Tasks: []*Task{{ID: "abc123", Command: "   ", Status: TaskPending}}}
	if sf.valid() {
		t.Fatal("expected a task with a whitespace-only command to be rejected")
	}
}

func TestStateFileValid_RejectsUnknownTaskStatus(t *testing.T) {
	sf := stateFile{State: QueueIdle, Tasks: []*Task{{ID: "abc123", Command: "echo hi", Status: TaskStatus("bogus")}}}
	if sf.valid() {
		t.Fatal("expected a task with an unrecognized status to be rejected")
	}
}

func TestStateFileValid_AcceptsWellFormedState(t *testing.T) {
	sf := stateFile{State: QueueRunning, Tasks: []*Task{{ID: "abc123", Command: "echo hi", Status: TaskRunning}}}
	if !sf.valid() {
		t.Fatal("expected a well-formed state file to be accepted")
	}
}

func TestLoadState_RestoredIdentifiersAreNotReused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "taskline-state.json")

	seedNames := NewNameGenerator()
	q := &Queue{State: QueueIdle, Tasks: []*Task{}, names: seedNames}
	task, err := q.AddTask("echo hi", "", "", nil)
	if err != nil {
		t.Fatalf("unexpected error adding task: %v", err)
	}
	if err := SaveState(q, path); err != nil {
		t.Fatalf("unexpected error saving state: %v", err)
	}

	names := NewNameGenerator()
	if _, err := LoadState(path, names); err != nil {
		t.Fatalf("unexpected error loading state: %v", err)
	}

	if !names.usedIDs[task.ID] {
		t.Fatalf("expected restored task ID %q to be marked used", task.ID)
	}
	if !names.usedNames[task.Name] {
		t.Fatalf("expected restored task name %q to be marked used", task.Name)
	}
}

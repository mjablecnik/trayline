package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// --- Property 15: State persistence round-trip ---

// TestProperty15_StatePersistenceRoundTrip verifies that writing state to disk
// and reading it back produces an identical state structure.
//
// Feature: agent-api-server, Property 15: State persistence round-trip
func TestProperty15_StatePersistenceRoundTrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		dir := t.TempDir()

		// Generate a random set of tasks.
		numTasks := rapid.IntRange(0, 20).Draw(rt, "numTasks")
		taskStore := NewTaskStore()
		for i := 0; i < numTasks; i++ {
			pt := genPersistedTask(rt, i)
			task := persistedTaskToTask(pt)
			taskStore.Add(task)
		}

		// Generate a random set of sessions.
		numSessions := rapid.IntRange(0, 10).Draw(rt, "numSessions")
		sessionStore := NewSessionStore()
		for i := 0; i < numSessions; i++ {
			ps := genPersistedSession(rt, i)
			sess := persistedSessionToSession(ps)
			sessionStore.Add(sess)
		}

		sm := &StateManager{
			stateDir:     dir,
			taskStore:    taskStore,
			sessionStore: sessionStore,
		}

		if err := sm.Save(); err != nil {
			rt.Fatalf("Save() failed: %v", err)
		}

		statePath := filepath.Join(dir, stateFileName)
		data, err := os.ReadFile(statePath)
		if err != nil {
			rt.Fatalf("state file not found after Save(): %v", err)
		}

		var loaded serverState
		if err := json.Unmarshal(data, &loaded); err != nil {
			rt.Fatalf("state file is not valid JSON: %v", err)
		}

		originalTasks := taskStore.All()
		if len(loaded.Tasks) != len(originalTasks) {
			rt.Fatalf("task count mismatch: got %d, want %d", len(loaded.Tasks), len(originalTasks))
		}

		origTaskMap := make(map[string]*Task, len(originalTasks))
		for _, task := range originalTasks {
			origTaskMap[task.ID] = task
		}
		for _, pt := range loaded.Tasks {
			orig, ok := origTaskMap[pt.ID]
			if !ok {
				rt.Fatalf("loaded task %q not found in original store", pt.ID)
			}
			assertTaskRoundTrip(rt, orig, pt)
		}

		originalSessions := sessionStore.All()
		if len(loaded.Sessions) != len(originalSessions) {
			rt.Fatalf("session count mismatch: got %d, want %d", len(loaded.Sessions), len(originalSessions))
		}

		origSessMap := make(map[string]*Session, len(originalSessions))
		for _, sess := range originalSessions {
			origSessMap[sess.ID] = sess
		}
		for _, ps := range loaded.Sessions {
			orig, ok := origSessMap[ps.ID]
			if !ok {
				rt.Fatalf("loaded session %q not found in original store", ps.ID)
			}
			assertSessionRoundTrip(rt, orig, ps)
		}
	})
}

// TestStateSave_AtomicWrite verifies that the tmp file is removed after a
// successful save and only the final state file remains.
func TestStateSave_AtomicWrite(t *testing.T) {
	dir := t.TempDir()

	sm := &StateManager{
		stateDir:     dir,
		taskStore:    NewTaskStore(),
		sessionStore: NewSessionStore(),
	}

	if err := sm.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	tmpPath := filepath.Join(dir, stateFileName+".tmp")
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("tmp file still exists after Save()")
	}

	finalPath := filepath.Join(dir, stateFileName)
	if _, err := os.Stat(finalPath); err != nil {
		t.Errorf("state file missing after Save(): %v", err)
	}
}

// TestStateRecover_NoFile verifies that Recover succeeds with empty state when
// no state file exists.
func TestStateRecover_NoFile(t *testing.T) {
	dir := t.TempDir()

	sm := &StateManager{
		stateDir:     dir,
		taskStore:    NewTaskStore(),
		sessionStore: NewSessionStore(),
	}

	if err := sm.Recover(context.Background()); err != nil {
		t.Fatalf("Recover() with no file should succeed, got: %v", err)
	}
	if len(sm.taskStore.All()) != 0 {
		t.Error("expected empty task store after Recover with no state file")
	}
	if len(sm.sessionStore.All()) != 0 {
		t.Error("expected empty session store after Recover with no state file")
	}
}

// TestStateRecover_TerminalTasks verifies that terminal tasks are restored as-is.
func TestStateRecover_TerminalTasks(t *testing.T) {
	dir := t.TempDir()

	sm := &StateManager{
		stateDir:     dir,
		taskStore:    NewTaskStore(),
		sessionStore: NewSessionStore(),
	}

	now := time.Now().UTC().Truncate(time.Second)
	state := serverState{
		Tasks: []persistedTask{
			{ID: "task-1", Status: TaskCompleted, Agent: "kiro", CreatedAt: now, Result: "hello"},
			{ID: "task-2", Status: TaskFailed, Agent: "claude", CreatedAt: now, Error: "oops"},
			{ID: "task-3", Status: TaskCancelled, Agent: "kiro", CreatedAt: now},
		},
	}

	data, _ := json.Marshal(state)
	os.WriteFile(filepath.Join(dir, stateFileName), data, 0o644)

	if err := sm.Recover(context.Background()); err != nil {
		t.Fatalf("Recover() error: %v", err)
	}

	tasks := sm.taskStore.All()
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}
	taskMap := make(map[string]*Task)
	for _, task := range tasks {
		taskMap[task.ID] = task
	}

	if taskMap["task-1"].Status != TaskCompleted || taskMap["task-1"].Result != "hello" {
		t.Errorf("task-1 not restored correctly: %+v", taskMap["task-1"])
	}
	if taskMap["task-2"].Status != TaskFailed || taskMap["task-2"].Error != "oops" {
		t.Errorf("task-2 not restored correctly: %+v", taskMap["task-2"])
	}
	if taskMap["task-3"].Status != TaskCancelled {
		t.Errorf("task-3 not restored correctly: %+v", taskMap["task-3"])
	}
}

// TestStateRecover_QueuedTaskFailed verifies that queued tasks are failed on recovery.
func TestStateRecover_QueuedTaskFailed(t *testing.T) {
	dir := t.TempDir()

	sm := &StateManager{
		stateDir:     dir,
		taskStore:    NewTaskStore(),
		sessionStore: NewSessionStore(),
	}

	now := time.Now().UTC()
	state := serverState{
		Tasks: []persistedTask{
			{ID: "queued-1", Status: TaskQueued, Agent: "kiro", CreatedAt: now},
		},
	}
	data, _ := json.Marshal(state)
	os.WriteFile(filepath.Join(dir, stateFileName), data, 0o644)

	if err := sm.Recover(context.Background()); err != nil {
		t.Fatalf("Recover() error: %v", err)
	}

	task := sm.taskStore.Get("queued-1")
	if task == nil {
		t.Fatal("queued task not found after recovery")
	}
	if task.Status != TaskFailed {
		t.Errorf("expected queued task to be failed after recovery, got %s", task.Status)
	}
	if task.CompletedAt == nil {
		t.Error("expected CompletedAt to be set after recovery")
	}
}

// TestEnsureStateDir verifies directory creation and writability check.
func TestEnsureStateDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state-dir-test")

	if err := EnsureStateDir(dir); err != nil {
		t.Fatalf("EnsureStateDir() error: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("directory not created: %v", err)
	}

	// Second call should succeed (already exists).
	if err := EnsureStateDir(dir); err != nil {
		t.Errorf("EnsureStateDir() error on second call: %v", err)
	}
}

// --- Helpers ---

func genPersistedTask(t *rapid.T, index int) persistedTask {
	id := rapid.StringMatching(`[a-z0-9]{8}-[a-z0-9]{4}-[a-z0-9]{4}-[a-z0-9]{4}-[a-z0-9]{12}`).Draw(t, "taskID")
	statuses := []TaskStatus{TaskQueued, TaskRunning, TaskCompleted, TaskFailed, TaskCancelled}
	statusIdx := rapid.IntRange(0, len(statuses)-1).Draw(t, "taskStatusIdx")
	status := statuses[statusIdx]
	agentIdx := rapid.IntRange(0, 1).Draw(t, "taskAgentIdx")
	agent := []string{"kiro", "claude"}[agentIdx]
	model := rapid.StringOf(rapid.RuneFrom([]rune("abcdefghijklmnopqrstuvwxyz0123456789-"))).Draw(t, "taskModel")
	createdAt := time.Unix(rapid.Int64Range(1_000_000_000, 2_000_000_000).Draw(t, "taskCreatedAt"), 0).UTC()

	pt := persistedTask{
		ID:        id,
		Status:    status,
		Agent:     agent,
		Model:     model,
		CreatedAt: createdAt,
	}

	if status == TaskCompleted {
		pt.Result = rapid.String().Draw(t, "taskResult")
	}
	if status == TaskFailed {
		pt.Error = rapid.String().Draw(t, "taskError")
	}

	return pt
}

func genPersistedSession(t *rapid.T, index int) persistedSession {
	id := rapid.StringMatching(`[a-z0-9]{8}-[a-z0-9]{4}-[a-z0-9]{4}-[a-z0-9]{4}-[a-z0-9]{12}`).Draw(t, "sessID")
	agentIdx := rapid.IntRange(0, 1).Draw(t, "sessAgentIdx")
	agent := []string{"kiro", "claude"}[agentIdx]
	createdAt := time.Unix(rapid.Int64Range(1_000_000_000, 2_000_000_000).Draw(t, "sessCreatedAt"), 0).UTC()
	lastMsgAt := time.Unix(rapid.Int64Range(1_000_000_000, 2_000_000_000).Draw(t, "sessLastMsgAt"), 0).UTC()
	containerID := rapid.StringMatching(`[a-f0-9]{64}`).Draw(t, "sessContainerID")

	return persistedSession{
		ID:            id,
		Agent:         agent,
		CreatedAt:     createdAt,
		LastMessageAt: lastMsgAt,
		ContainerID:   containerID,
	}
}

func persistedTaskToTask(pt persistedTask) *Task {
	task := &Task{
		ID:           pt.ID,
		Status:       pt.Status,
		Agent:        pt.Agent,
		Prompt:       pt.Prompt,
		Model:        pt.Model,
		System:       pt.System,
		OutputFormat:  pt.OutputFormat,
		Result:       pt.Result,
		Error:        pt.Error,
		Valid:         pt.Valid,
		CreatedAt:    pt.CreatedAt,
		CompletedAt:  pt.CompletedAt,
		ContainerID:  pt.ContainerID,
		Done:         make(chan struct{}),
	}
	switch task.Status {
	case TaskCompleted, TaskFailed, TaskCancelled:
		close(task.Done)
	}
	return task
}

func persistedSessionToSession(ps persistedSession) *Session {
	return &Session{
		ID:            ps.ID,
		Agent:         ps.Agent,
		Model:         ps.Model,
		System:        ps.System,
		CreatedAt:     ps.CreatedAt,
		LastMessageAt: ps.LastMessageAt,
		ContainerID:   ps.ContainerID,
		Active:        true,
	}
}

func assertTaskRoundTrip(t *rapid.T, orig *Task, loaded persistedTask) {
	t.Helper()
	if orig.ID != loaded.ID {
		t.Errorf("task ID mismatch: got %q, want %q", loaded.ID, orig.ID)
	}
	if orig.Status != loaded.Status {
		t.Errorf("task %q status mismatch: got %q, want %q", orig.ID, loaded.Status, orig.Status)
	}
	if orig.Agent != loaded.Agent {
		t.Errorf("task %q agent mismatch: got %q, want %q", orig.ID, loaded.Agent, orig.Agent)
	}
	if orig.Model != loaded.Model {
		t.Errorf("task %q model mismatch: got %q, want %q", orig.ID, loaded.Model, orig.Model)
	}
	if orig.Result != loaded.Result {
		t.Errorf("task %q result mismatch", orig.ID)
	}
	if orig.Error != loaded.Error {
		t.Errorf("task %q error mismatch", orig.ID)
	}
	if !orig.CreatedAt.Equal(loaded.CreatedAt) {
		t.Errorf("task %q created_at mismatch: got %v, want %v", orig.ID, loaded.CreatedAt, orig.CreatedAt)
	}
}

func assertSessionRoundTrip(t *rapid.T, orig *Session, loaded persistedSession) {
	t.Helper()
	if orig.ID != loaded.ID {
		t.Errorf("session ID mismatch: got %q, want %q", loaded.ID, orig.ID)
	}
	if orig.Agent != loaded.Agent {
		t.Errorf("session %q agent mismatch: got %q, want %q", orig.ID, loaded.Agent, orig.Agent)
	}
	if orig.Model != loaded.Model {
		t.Errorf("session %q model mismatch: got %q, want %q", orig.ID, loaded.Model, orig.Model)
	}
	if orig.ContainerID != loaded.ContainerID {
		t.Errorf("session %q container_id mismatch", orig.ID)
	}
	if !orig.CreatedAt.Equal(loaded.CreatedAt) {
		t.Errorf("session %q created_at mismatch", orig.ID)
	}
	if !orig.LastMessageAt.Equal(loaded.LastMessageAt) {
		t.Errorf("session %q last_message_at mismatch", orig.ID)
	}
}

package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	"pgregory.net/rapid"

	"server/core"
	"server/docker"
)

func newTestContainerManager(t *testing.T, mock docker.ContainerClient) *docker.ContainerManager {
	t.Helper()
	cfg := &core.Config{MaxConcurrentTasks: 1, TaskTimeout: 5 * time.Second, WorkspaceHostDir: t.TempDir()}
	return docker.NewContainerManager(mock, cfg, core.NewLogger(""))
}

// --- Property 15: State persistence round-trip ---

func TestProperty15_StatePersistenceRoundTrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		dir := t.TempDir()

		numTasks := rapid.IntRange(0, 20).Draw(rt, "numTasks")
		taskStore := NewTaskStore()
		for i := 0; i < numTasks; i++ {
			pt := genPersistedTask(rt, i)
			task := persistedTaskToTask(pt)
			taskStore.Add(task)
		}

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

		data, err := os.ReadFile(filepath.Join(dir, stateFileName))
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

func TestStateSave_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	sm := &StateManager{stateDir: dir, taskStore: NewTaskStore(), sessionStore: NewSessionStore()}

	if err := sm.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, stateFileName+".tmp")); !os.IsNotExist(err) {
		t.Errorf("tmp file still exists after Save()")
	}
	if _, err := os.Stat(filepath.Join(dir, stateFileName)); err != nil {
		t.Errorf("state file missing after Save(): %v", err)
	}
}

func TestStateRecover_NoFile(t *testing.T) {
	dir := t.TempDir()
	sm := &StateManager{stateDir: dir, taskStore: NewTaskStore(), sessionStore: NewSessionStore()}

	if err := sm.Recover(context.Background()); err != nil {
		t.Fatalf("Recover() with no file should succeed, got: %v", err)
	}
	if len(sm.taskStore.All()) != 0 {
		t.Error("expected empty task store after Recover with no state file")
	}
}

func TestStateRecover_TerminalTasks(t *testing.T) {
	dir := t.TempDir()
	sm := &StateManager{stateDir: dir, taskStore: NewTaskStore(), sessionStore: NewSessionStore()}

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
		t.Errorf("task-1 not restored correctly")
	}
	if taskMap["task-2"].Status != TaskFailed || taskMap["task-2"].Error != "oops" {
		t.Errorf("task-2 not restored correctly")
	}
	if taskMap["task-3"].Status != TaskCancelled {
		t.Errorf("task-3 not restored correctly")
	}
}

func TestStateRecover_QueuedTaskFailed(t *testing.T) {
	dir := t.TempDir()
	sm := &StateManager{stateDir: dir, taskStore: NewTaskStore(), sessionStore: NewSessionStore()}

	now := time.Now().UTC()
	state := serverState{Tasks: []persistedTask{{ID: "queued-1", Status: TaskQueued, Agent: "kiro", CreatedAt: now}}}
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
}

func TestStateRecover_RunningTask_ExitZero(t *testing.T) {
	dir := t.TempDir()
	mock := newMockForStateTest(false, 0, nil, nil)
	cm := newTestContainerManager(t, mock)
	sm := &StateManager{stateDir: dir, taskStore: NewTaskStore(), sessionStore: NewSessionStore(), cm: cm}

	now := time.Now().UTC()
	state := serverState{Tasks: []persistedTask{{ID: "run-0", Status: TaskRunning, Agent: "claude", ContainerID: "ctr-0", CreatedAt: now}}}
	data, _ := json.Marshal(state)
	os.WriteFile(filepath.Join(dir, stateFileName), data, 0o644)

	if err := sm.Recover(context.Background()); err != nil {
		t.Fatalf("Recover() error: %v", err)
	}

	task := sm.taskStore.Get("run-0")
	if task == nil {
		t.Fatal("task not found after recovery")
	}
	if task.Status != TaskCompleted {
		t.Errorf("expected TaskCompleted, got %s", task.Status)
	}
}

func TestStateRecover_RunningTask_ContainerInspectError(t *testing.T) {
	dir := t.TempDir()
	mock := newMockForStateTest(false, 0, fmt.Errorf("container not found"), nil)
	cm := newTestContainerManager(t, mock)
	sm := &StateManager{stateDir: dir, taskStore: NewTaskStore(), sessionStore: NewSessionStore(), cm: cm}

	now := time.Now().UTC()
	state := serverState{Tasks: []persistedTask{{ID: "run-err", Status: TaskRunning, Agent: "claude", ContainerID: "ctr-gone", CreatedAt: now}}}
	data, _ := json.Marshal(state)
	os.WriteFile(filepath.Join(dir, stateFileName), data, 0o644)

	if err := sm.Recover(context.Background()); err != nil {
		t.Fatalf("Recover() error: %v", err)
	}

	task := sm.taskStore.Get("run-err")
	if task == nil {
		t.Fatal("task not found")
	}
	if task.Status != TaskFailed {
		t.Errorf("expected TaskFailed, got %s", task.Status)
	}
}

func TestEnsureStateDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state-dir-test")

	if err := EnsureStateDir(dir); err != nil {
		t.Fatalf("EnsureStateDir() error: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("directory not created: %v", err)
	}
	if err := EnsureStateDir(dir); err != nil {
		t.Errorf("EnsureStateDir() error on second call: %v", err)
	}
}

// --- mock for state tests ---

type stateTestMock struct {
	running    bool
	exitCode   int
	inspectErr error
	attachErr  error
}

func newMockForStateTest(running bool, exitCode int, inspectErr, attachErr error) *stateTestMock {
	return &stateTestMock{running: running, exitCode: exitCode, inspectErr: inspectErr, attachErr: attachErr}
}

func (m *stateTestMock) ContainerCreate(_ context.Context, _ interface{}, _ interface{}, _ interface{}, _ string) (interface{}, error) {
	return nil, nil
}

func (m *stateTestMock) ContainerInspect(_ context.Context, _ string) (dockertypes.ContainerJSON, error) {
	if m.inspectErr != nil {
		return dockertypes.ContainerJSON{}, m.inspectErr
	}
	return dockertypes.ContainerJSON{
		ContainerJSONBase: &dockertypes.ContainerJSONBase{
			State: &dockertypes.ContainerState{Running: m.running, ExitCode: m.exitCode},
		},
	}, nil
}

// stateTestMock must implement docker.ContainerClient — use the real mock from docker package via embedding trick.
// Since we can't import docker_test, we implement the full interface here.

func (m *stateTestMock) ContainerStart(_ context.Context, _ string, _ dockertypes.ContainerStartOptions) error {
	return nil
}
func (m *stateTestMock) ContainerLogs(_ context.Context, _ string, _ dockertypes.ContainerLogsOptions) (interface{}, error) {
	return nil, nil
}

// --- Helpers ---

func genPersistedTask(t *rapid.T, index int) persistedTask {
	id := rapid.StringMatching(`[a-z0-9]{8}-[a-z0-9]{4}-[a-z0-9]{4}-[a-z0-9]{4}-[a-z0-9]{12}`).Draw(t, "taskID")
	statuses := []TaskStatus{TaskQueued, TaskRunning, TaskCompleted, TaskFailed, TaskCancelled}
	status := statuses[rapid.IntRange(0, len(statuses)-1).Draw(t, "taskStatusIdx")]
	agent := []string{"kiro", "claude"}[rapid.IntRange(0, 1).Draw(t, "taskAgentIdx")]
	model := rapid.StringOf(rapid.RuneFrom([]rune("abcdefghijklmnopqrstuvwxyz0123456789-"))).Draw(t, "taskModel")
	createdAt := time.Unix(rapid.Int64Range(1_000_000_000, 2_000_000_000).Draw(t, "taskCreatedAt"), 0).UTC()

	pt := persistedTask{ID: id, Status: status, Agent: agent, Model: model, CreatedAt: createdAt}
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
	agent := []string{"kiro", "claude"}[rapid.IntRange(0, 1).Draw(t, "sessAgentIdx")]
	createdAt := time.Unix(rapid.Int64Range(1_000_000_000, 2_000_000_000).Draw(t, "sessCreatedAt"), 0).UTC()
	lastMsgAt := time.Unix(rapid.Int64Range(1_000_000_000, 2_000_000_000).Draw(t, "sessLastMsgAt"), 0).UTC()
	containerID := rapid.StringMatching(`[a-f0-9]{64}`).Draw(t, "sessContainerID")
	return persistedSession{ID: id, Agent: agent, CreatedAt: createdAt, LastMessageAt: lastMsgAt, ContainerID: containerID}
}

func persistedTaskToTask(pt persistedTask) *Task {
	task := &Task{
		ID: pt.ID, Status: pt.Status, Agent: pt.Agent, Prompt: pt.Prompt,
		Model: pt.Model, System: pt.System, OutputFormat: pt.OutputFormat,
		Result: pt.Result, Error: pt.Error, Valid: pt.Valid,
		CreatedAt: pt.CreatedAt, CompletedAt: pt.CompletedAt, ContainerID: pt.ContainerID,
		Done: make(chan struct{}),
	}
	switch task.Status {
	case TaskCompleted, TaskFailed, TaskCancelled:
		close(task.Done)
	}
	return task
}

func persistedSessionToSession(ps persistedSession) *Session {
	return &Session{
		ID: ps.ID, Agent: ps.Agent, Model: ps.Model, System: ps.System,
		CreatedAt: ps.CreatedAt, LastMessageAt: ps.LastMessageAt,
		ContainerID: ps.ContainerID, Active: true,
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
		t.Errorf("task %q agent mismatch", orig.ID)
	}
	if orig.Result != loaded.Result {
		t.Errorf("task %q result mismatch", orig.ID)
	}
	if !orig.CreatedAt.Equal(loaded.CreatedAt) {
		t.Errorf("task %q created_at mismatch", orig.ID)
	}
}

func assertSessionRoundTrip(t *rapid.T, orig *Session, loaded persistedSession) {
	t.Helper()
	if orig.ID != loaded.ID {
		t.Errorf("session ID mismatch: got %q, want %q", loaded.ID, orig.ID)
	}
	if orig.Agent != loaded.Agent {
		t.Errorf("session %q agent mismatch", orig.ID)
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

package store

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pgregory.net/rapid"
)

func TestWorkflowStateManager_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	s1 := NewWorkflowStore()
	now := time.Now().Truncate(time.Second).UTC()
	exitCode := 0
	buf := NewRingBuffer(WorkflowLogBufferSize)
	buf.Write([]byte("hello output"))
	s1.Add(&Workflow{
		ID:          "w1",
		Project:     "proj",
		Pipeline:    "processes/4-create-code",
		Variables:   map[string]string{"path": "."},
		Status:      WorkflowCompleted,
		CreatedAt:   now,
		StartedAt:   &now,
		CompletedAt: &now,
		ExitCode:    &exitCode,
		LogBuffer:   buf,
	})
	sm1 := NewWorkflowStateManager(dir, s1, nil)
	if err := sm1.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "workflows.json")); err != nil {
		t.Fatalf("expected workflows.json to exist: %v", err)
	}

	s2 := NewWorkflowStore()
	sm2 := NewWorkflowStateManager(dir, s2, nil)
	if err := sm2.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	w := s2.Get("w1")
	if w == nil {
		t.Fatal("expected workflow w1 to be restored")
	}
	if w.Pipeline != "processes/4-create-code" || w.Status != WorkflowCompleted {
		t.Errorf("unexpected restored workflow: %+v", w)
	}
	if w.Variables["path"] != "." {
		t.Errorf("expected variables preserved, got %+v", w.Variables)
	}
	if w.LogBuffer == nil || w.LogBuffer.String() != "hello output" {
		t.Errorf("expected log buffer restored with content, got %+v", w.LogBuffer)
	}
	if w.ExitCode == nil || *w.ExitCode != 0 {
		t.Errorf("expected exit code 0 restored, got %+v", w.ExitCode)
	}
}

func TestWorkflowStateManager_LoadMissingFile(t *testing.T) {
	dir := t.TempDir()
	s := NewWorkflowStore()
	sm := NewWorkflowStateManager(dir, s, nil)
	if err := sm.Load(); err != nil {
		t.Fatalf("expected no error for missing state file, got: %v", err)
	}
	if len(s.All()) != 0 {
		t.Errorf("expected empty store, got %d workflows", len(s.All()))
	}
}

func TestWorkflowStateManager_LoadCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workflows.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("failed to write corrupt file: %v", err)
	}

	s := NewWorkflowStore()
	sm := NewWorkflowStateManager(dir, s, nil)
	if err := sm.Load(); err != nil {
		t.Fatalf("expected no error for corrupt state file, got: %v", err)
	}
	if len(s.All()) != 0 {
		t.Errorf("expected empty store after corrupt load, got %d workflows", len(s.All()))
	}
	if _, err := os.Stat(filepath.Join(dir, "workflows.json.corrupt")); err != nil {
		t.Errorf("expected corrupt file to be quarantined: %v", err)
	}
}

func TestWorkflowStateManager_Recover_RunningToFailed(t *testing.T) {
	dir := t.TempDir()
	s := NewWorkflowStore()
	now := time.Now()
	s.Add(&Workflow{ID: "w-running", Project: "proj", Status: WorkflowRunning, CreatedAt: now, LogBuffer: NewRingBuffer(WorkflowLogBufferSize)})
	s.Add(&Workflow{ID: "w-queued", Project: "proj", Status: WorkflowQueued, CreatedAt: now, LogBuffer: NewRingBuffer(WorkflowLogBufferSize)})

	sm := NewWorkflowStateManager(dir, s, nil)
	if err := sm.Recover(); err != nil {
		t.Fatalf("Recover failed: %v", err)
	}

	running := s.Get("w-running")
	if running.Status != WorkflowFailed {
		t.Errorf("expected running workflow to become failed, got %q", running.Status)
	}
	if running.Error != "server restarted and container was lost" {
		t.Errorf("unexpected error message: %q", running.Error)
	}
	if running.CompletedAt == nil {
		t.Error("expected CompletedAt to be set")
	}

	queued := s.Get("w-queued")
	if queued.Status != WorkflowQueued {
		t.Errorf("expected queued workflow to remain queued, got %q", queued.Status)
	}
}

// Feature: 010-dashboard-workflow-runner, Property 10: Persistence round-trip
func TestWorkflowStateManager_PersistenceRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dir, err := os.MkdirTemp("", "workflow-roundtrip-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(dir)
		n := rapid.IntRange(0, 10).Draw(t, "n")

		s1 := NewWorkflowStore()
		type expected struct {
			id, project, pipeline string
			variables             map[string]string
			status                WorkflowStatus
			createdAt             time.Time
			startedAt             *time.Time
			completedAt           *time.Time
			errMsg                string
			exitCode              *int
			log                   string
		}
		var wants []expected

		for i := 0; i < n; i++ {
			id := fmt.Sprintf("%s-%d", rapid.StringMatching(`[a-zA-Z0-9-]{1,20}`).Draw(t, "id"), i)
			project := rapid.StringMatching(`[a-z]{1,10}`).Draw(t, "project")
			pipeline := rapid.StringMatching(`[a-z]{1,10}/[a-z0-9-]{1,20}`).Draw(t, "pipeline")
			status := rapid.SampledFrom([]WorkflowStatus{
				WorkflowQueued, WorkflowRunning, WorkflowCompleted, WorkflowFailed, WorkflowCancelled,
			}).Draw(t, "status")
			createdAt := time.Unix(rapid.Int64Range(0, 2000000000).Draw(t, "createdAt"), 0).UTC()
			errMsg := rapid.StringMatching(`[a-zA-Z ]{0,30}`).Draw(t, "errMsg")
			logContent := rapid.StringMatching(`[a-zA-Z0-9 \n]{0,100}`).Draw(t, "log")

			numVars := rapid.IntRange(0, 5).Draw(t, "numVars")
			variables := make(map[string]string, numVars)
			for j := 0; j < numVars; j++ {
				k := rapid.StringMatching(`[a-zA-Z0-9_-]{1,20}`).Draw(t, "varKey")
				v := rapid.StringMatching(`[a-zA-Z0-9]{0,20}`).Draw(t, "varVal")
				variables[k] = v
			}

			var startedAt, completedAt *time.Time
			if rapid.Bool().Draw(t, "hasStarted") {
				ts := createdAt.Add(time.Minute)
				startedAt = &ts
			}
			var exitCode *int
			if rapid.Bool().Draw(t, "hasExitCode") {
				ec := rapid.IntRange(0, 255).Draw(t, "exitCode")
				exitCode = &ec
				ts := createdAt.Add(2 * time.Minute)
				completedAt = &ts
			}

			buf := NewRingBuffer(WorkflowLogBufferSize)
			buf.Write([]byte(logContent))

			s1.Add(&Workflow{
				ID:          id,
				Project:     project,
				Pipeline:    pipeline,
				Variables:   variables,
				Status:      status,
				CreatedAt:   createdAt,
				StartedAt:   startedAt,
				CompletedAt: completedAt,
				Error:       errMsg,
				ExitCode:    exitCode,
				LogBuffer:   buf,
			})

			wants = append(wants, expected{
				id: id, project: project, pipeline: pipeline, variables: variables,
				status: status, createdAt: createdAt, startedAt: startedAt,
				completedAt: completedAt, errMsg: errMsg, exitCode: exitCode, log: logContent,
			})
		}

		sm1 := NewWorkflowStateManager(dir, s1, nil)
		if err := sm1.Save(); err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		s2 := NewWorkflowStore()
		sm2 := NewWorkflowStateManager(dir, s2, nil)
		if err := sm2.Load(); err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		for _, want := range wants {
			got := s2.Get(want.id)
			if got == nil {
				t.Fatalf("expected workflow %q to survive round-trip", want.id)
			}
			if got.Project != want.project || got.Pipeline != want.pipeline || got.Status != want.status {
				t.Fatalf("mismatch for %q: got %+v, want project=%q pipeline=%q status=%q", want.id, got, want.project, want.pipeline, want.status)
			}
			if !got.CreatedAt.Equal(want.createdAt) {
				t.Fatalf("created_at mismatch for %q: got %v, want %v", want.id, got.CreatedAt, want.createdAt)
			}
			if len(got.Variables) != len(want.variables) {
				t.Fatalf("variables mismatch for %q: got %+v, want %+v", want.id, got.Variables, want.variables)
			}
			for k, v := range want.variables {
				if got.Variables[k] != v {
					t.Fatalf("variable %q mismatch for %q: got %q, want %q", k, want.id, got.Variables[k], v)
				}
			}
			if got.Error != want.errMsg {
				t.Fatalf("error mismatch for %q: got %q, want %q", want.id, got.Error, want.errMsg)
			}
			if (got.ExitCode == nil) != (want.exitCode == nil) {
				t.Fatalf("exit_code presence mismatch for %q: got %+v, want %+v", want.id, got.ExitCode, want.exitCode)
			}
			if got.ExitCode != nil && *got.ExitCode != *want.exitCode {
				t.Fatalf("exit_code mismatch for %q: got %d, want %d", want.id, *got.ExitCode, *want.exitCode)
			}
			if (got.StartedAt == nil) != (want.startedAt == nil) {
				t.Fatalf("started_at presence mismatch for %q", want.id)
			}
			if got.StartedAt != nil && !got.StartedAt.Equal(*want.startedAt) {
				t.Fatalf("started_at mismatch for %q: got %v, want %v", want.id, got.StartedAt, want.startedAt)
			}
			if (got.CompletedAt == nil) != (want.completedAt == nil) {
				t.Fatalf("completed_at presence mismatch for %q", want.id)
			}
			if got.CompletedAt != nil && !got.CompletedAt.Equal(*want.completedAt) {
				t.Fatalf("completed_at mismatch for %q: got %v, want %v", want.id, got.CompletedAt, want.completedAt)
			}
			if got.LogBuffer == nil || got.LogBuffer.String() != want.log {
				t.Fatalf("log mismatch for %q: got %+v, want %q", want.id, got.LogBuffer, want.log)
			}
		}
	})
}

package store

import (
	"fmt"
	"testing"
	"time"

	"pgregory.net/rapid"
)

func TestWorkflowStore_AddAndGet(t *testing.T) {
	s := NewWorkflowStore()
	w := &Workflow{ID: "w1", Project: "proj", Status: WorkflowQueued, CreatedAt: time.Now()}
	s.Add(w)

	got := s.Get("w1")
	if got == nil {
		t.Fatal("expected to get workflow w1, got nil")
	}
	if got.ID != "w1" {
		t.Errorf("expected ID w1, got %q", got.ID)
	}
}

func TestWorkflowStore_GetMiss(t *testing.T) {
	s := NewWorkflowStore()
	if got := s.Get("nonexistent"); got != nil {
		t.Errorf("expected nil for missing id, got %+v", got)
	}
}

func TestWorkflowStore_UpdateFound(t *testing.T) {
	s := NewWorkflowStore()
	w := &Workflow{ID: "w2", Project: "proj", Status: WorkflowQueued, CreatedAt: time.Now()}
	s.Add(w)

	ok := s.Update("w2", func(w *Workflow) { w.Status = WorkflowRunning })
	if !ok {
		t.Fatal("expected Update to return true for existing workflow")
	}
	if s.Get("w2").Status != WorkflowRunning {
		t.Errorf("expected status running after update, got %q", s.Get("w2").Status)
	}
}

func TestWorkflowStore_UpdateNotFound(t *testing.T) {
	s := NewWorkflowStore()
	ok := s.Update("missing", func(w *Workflow) { w.Status = WorkflowRunning })
	if ok {
		t.Error("expected Update to return false for unknown id")
	}
}

func TestWorkflowStore_Remove(t *testing.T) {
	s := NewWorkflowStore()
	s.Add(&Workflow{ID: "w3", Project: "proj", Status: WorkflowQueued, CreatedAt: time.Now()})
	s.Remove("w3")
	if s.Get("w3") != nil {
		t.Error("expected workflow to be removed")
	}
	if len(s.ListByProject("proj")) != 0 {
		t.Error("expected project index to be cleared after removal")
	}
}

func TestWorkflowStore_NextQueued(t *testing.T) {
	s := NewWorkflowStore()
	base := time.Now()
	s.Add(&Workflow{ID: "w1", Project: "proj", Status: WorkflowCompleted, CreatedAt: base})
	s.Add(&Workflow{ID: "w2", Project: "proj", Status: WorkflowQueued, CreatedAt: base.Add(time.Second)})
	s.Add(&Workflow{ID: "w3", Project: "proj", Status: WorkflowQueued, CreatedAt: base.Add(2 * time.Second)})

	next := s.NextQueued("proj")
	if next == nil || next.ID != "w2" {
		t.Fatalf("expected w2 (oldest queued), got %+v", next)
	}
}

func TestWorkflowStore_NextQueuedNone(t *testing.T) {
	s := NewWorkflowStore()
	s.Add(&Workflow{ID: "w1", Project: "proj", Status: WorkflowCompleted, CreatedAt: time.Now()})
	if next := s.NextQueued("proj"); next != nil {
		t.Errorf("expected nil when no queued workflow, got %+v", next)
	}
}

func TestWorkflowStore_HasRunning(t *testing.T) {
	s := NewWorkflowStore()
	s.Add(&Workflow{ID: "w1", Project: "proj", Status: WorkflowQueued, CreatedAt: time.Now()})
	if s.HasRunning("proj") {
		t.Error("expected HasRunning false with only queued workflow")
	}
	s.Update("w1", func(w *Workflow) { w.Status = WorkflowRunning })
	if !s.HasRunning("proj") {
		t.Error("expected HasRunning true after status set to running")
	}
}

func TestWorkflowStore_AllCountMatchesInserts(t *testing.T) {
	s := NewWorkflowStore()
	for i := 0; i < 5; i++ {
		s.Add(&Workflow{ID: fmt.Sprintf("w%d", i), Project: "proj", Status: WorkflowQueued, CreatedAt: time.Now()})
	}
	all := s.All()
	if len(all) != 5 {
		t.Errorf("expected 5 workflows from All(), got %d", len(all))
	}
}

func TestWorkflowStore_Evict(t *testing.T) {
	s := NewWorkflowStore()
	base := time.Now()
	for i := 0; i < 25; i++ {
		s.Add(&Workflow{
			ID:        fmt.Sprintf("w%d", i),
			Project:   "proj",
			Status:    WorkflowCompleted,
			CreatedAt: base.Add(time.Duration(i) * time.Second),
		})
	}
	s.Evict("proj")

	if len(s.byProject["proj"]) != maxWorkflowsPerProject {
		t.Fatalf("expected %d workflows retained after evict, got %d", maxWorkflowsPerProject, len(s.byProject["proj"]))
	}
	// The 5 oldest (w0-w4) should have been evicted.
	for i := 0; i < 5; i++ {
		if s.Get(fmt.Sprintf("w%d", i)) != nil {
			t.Errorf("expected w%d to be evicted", i)
		}
	}
	for i := 5; i < 25; i++ {
		if s.Get(fmt.Sprintf("w%d", i)) == nil {
			t.Errorf("expected w%d to be retained", i)
		}
	}
}

func TestWorkflowStore_EvictSkipsNonTerminal(t *testing.T) {
	s := NewWorkflowStore()
	base := time.Now()
	// Oldest workflow is still queued; it must survive eviction even though
	// the project exceeds the cap.
	s.Add(&Workflow{ID: "w-queued", Project: "proj", Status: WorkflowQueued, CreatedAt: base})
	for i := 0; i < 20; i++ {
		s.Add(&Workflow{
			ID:        fmt.Sprintf("w%d", i),
			Project:   "proj",
			Status:    WorkflowCompleted,
			CreatedAt: base.Add(time.Duration(i+1) * time.Second),
		})
	}
	s.Evict("proj")

	if s.Get("w-queued") == nil {
		t.Error("expected non-terminal workflow to survive eviction")
	}
}

// Feature: 010-dashboard-workflow-runner, Property 4: Sequential execution per project
func TestWorkflowStore_SequentialExecutionPerProject(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		projects := rapid.SliceOfN(rapid.StringMatching(`[a-z]{1,8}`), 1, 5).Draw(t, "projects")
		s := NewWorkflowStore()

		numWorkflows := rapid.IntRange(0, 30).Draw(t, "numWorkflows")
		base := time.Now()
		for i := 0; i < numWorkflows; i++ {
			project := projects[rapid.IntRange(0, len(projects)-1).Draw(t, fmt.Sprintf("project%d", i))]
			status := rapid.SampledFrom([]WorkflowStatus{
				WorkflowQueued, WorkflowRunning, WorkflowCompleted, WorkflowFailed, WorkflowCancelled,
			}).Draw(t, fmt.Sprintf("status%d", i))

			// Never allow more than one running workflow to be manufactured per
			// project, mirroring the invariant a real queue manager enforces
			// when transitioning workflows to "running".
			if status == WorkflowRunning && s.HasRunning(project) {
				status = WorkflowQueued
			}

			s.Add(&Workflow{
				ID:        fmt.Sprintf("w%d", i),
				Project:   project,
				Status:    status,
				CreatedAt: base.Add(time.Duration(i) * time.Millisecond),
			})
		}

		uniqueProjects := make(map[string]bool)
		for _, project := range projects {
			uniqueProjects[project] = true
		}
		runningCount := make(map[string]int)
		for project := range uniqueProjects {
			for _, w := range s.ListByProject(project) {
				if w.Status == WorkflowRunning {
					runningCount[project]++
				}
			}
		}
		for project, count := range runningCount {
			if count > 1 {
				t.Fatalf("project %q has %d running workflows, expected at most 1", project, count)
			}
		}
	})
}

// Feature: 010-dashboard-workflow-runner, Property 6: Workflow listing is ordered and capped
func TestWorkflowStore_ListingOrderedAndCapped(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(0, 40).Draw(t, "n")
		s := NewWorkflowStore()
		base := time.Now()
		for i := 0; i < n; i++ {
			s.Add(&Workflow{
				ID:        fmt.Sprintf("w%d", i),
				Project:   "proj",
				Status:    WorkflowQueued,
				CreatedAt: base.Add(time.Duration(i) * time.Millisecond),
			})
		}

		list := s.ListByProject("proj")
		wantLen := n
		if wantLen > maxWorkflowsPerProject {
			wantLen = maxWorkflowsPerProject
		}
		if len(list) != wantLen {
			t.Fatalf("expected %d workflows, got %d", wantLen, len(list))
		}
		for i := 1; i < len(list); i++ {
			if list[i-1].CreatedAt.Before(list[i].CreatedAt) {
				t.Fatalf("list not sorted descending at index %d: %v before %v", i, list[i-1].CreatedAt, list[i].CreatedAt)
			}
		}
	})
}

// Feature: 010-dashboard-workflow-runner, Property 8: Edit preserves queue position
func TestWorkflowStore_EditPreservesQueuePosition(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 20).Draw(t, "n")
		s := NewWorkflowStore()
		base := time.Now()

		ids := make([]string, n)
		for i := 0; i < n; i++ {
			id := fmt.Sprintf("w%d", i)
			ids[i] = id
			s.Add(&Workflow{
				ID:        id,
				Project:   "proj",
				Pipeline:  "processes/original",
				Variables: map[string]string{"k": "original"},
				Status:    WorkflowQueued,
				CreatedAt: base.Add(time.Duration(i) * time.Millisecond),
			})
		}

		// Edit one workflow's pipeline/variables (simulating HandleEdit),
		// without touching its status or creation order.
		editIdx := rapid.IntRange(0, n-1).Draw(t, "editIdx")
		ok := s.Update(ids[editIdx], func(w *Workflow) {
			w.Pipeline = "processes/edited"
			w.Variables = map[string]string{"k": "edited"}
		})
		if !ok {
			t.Fatalf("expected Update to find workflow %q", ids[editIdx])
		}

		// Draining via NextQueued must still return every workflow in
		// original creation order, since editing content must not move a
		// workflow's position relative to the others.
		got := make([]string, 0, n)
		for {
			next := s.NextQueued("proj")
			if next == nil {
				break
			}
			got = append(got, next.ID)
			s.Update(next.ID, func(w *Workflow) { w.Status = WorkflowRunning })
			s.Update(next.ID, func(w *Workflow) { w.Status = WorkflowCompleted })
		}

		if len(got) != n {
			t.Fatalf("expected %d workflows drained in order, got %d: %v", n, len(got), got)
		}
		for i := range ids {
			if got[i] != ids[i] {
				t.Fatalf("queue position changed by edit: expected order %v, got %v", ids, got)
			}
		}
	})
}

package main

import (
	"fmt"
	"testing"
	"time"
)

func TestTaskStore_AddAndGet(t *testing.T) {
	s := NewTaskStore()
	task := &Task{ID: "t1", Status: TaskQueued, Agent: "claude", CreatedAt: time.Now()}
	s.Add(task)

	got := s.Get("t1")
	if got == nil {
		t.Fatal("expected to get task t1, got nil")
	}
	if got.ID != "t1" {
		t.Errorf("expected ID t1, got %q", got.ID)
	}
}

func TestTaskStore_GetMiss(t *testing.T) {
	s := NewTaskStore()
	if got := s.Get("nonexistent"); got != nil {
		t.Errorf("expected nil for missing id, got %+v", got)
	}
}

func TestTaskStore_UpdateFound(t *testing.T) {
	s := NewTaskStore()
	task := &Task{ID: "t2", Status: TaskQueued, Agent: "claude", CreatedAt: time.Now()}
	s.Add(task)

	ok := s.Update("t2", func(t *Task) { t.Status = TaskRunning })
	if !ok {
		t.Fatal("expected Update to return true for existing task")
	}
	if s.Get("t2").Status != TaskRunning {
		t.Errorf("expected status Running after update, got %q", s.Get("t2").Status)
	}
}

func TestTaskStore_UpdateNotFound(t *testing.T) {
	s := NewTaskStore()
	ok := s.Update("missing", func(t *Task) { t.Status = TaskRunning })
	if ok {
		t.Error("expected Update to return false for unknown id")
	}
}

func TestTaskStore_AllCountMatchesInserts(t *testing.T) {
	s := NewTaskStore()
	for i := 0; i < 5; i++ {
		s.Add(&Task{ID: fmt.Sprintf("t%d", i), Status: TaskQueued, Agent: "claude", CreatedAt: time.Now()})
	}
	all := s.All()
	if len(all) != 5 {
		t.Errorf("expected 5 tasks from All(), got %d", len(all))
	}
}

func TestTaskStore_EvictsOldestAt101(t *testing.T) {
	s := NewTaskStore()
	// Insert 101 tasks; oldest is "task-0"
	for i := 0; i < 101; i++ {
		s.Add(&Task{
			ID:        fmt.Sprintf("task-%d", i),
			Status:    TaskQueued,
			Agent:     "claude",
			CreatedAt: time.Now(),
		})
	}
	if len(s.All()) != 100 {
		t.Errorf("expected 100 tasks after eviction, got %d", len(s.All()))
	}
	if s.Get("task-0") != nil {
		t.Error("expected oldest task (task-0) to be evicted, but it is still present")
	}
	if s.Get("task-100") == nil {
		t.Error("expected newest task (task-100) to be present after eviction")
	}
}

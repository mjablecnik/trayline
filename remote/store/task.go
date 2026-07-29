package store

import (
	"context"
	"sort"
	"sync"
	"time"
)

// TaskStatus represents the lifecycle state of a one-shot task.
type TaskStatus string

const (
	TaskQueued    TaskStatus = "queued"
	TaskRunning   TaskStatus = "running"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
	TaskCancelled TaskStatus = "cancelled"
)

// IsTerminal reports whether a task status is in a terminal (non-progressing) state.
func IsTerminal(s TaskStatus) bool {
	return s == TaskCompleted || s == TaskFailed || s == TaskCancelled
}

// Task represents a one-shot agent execution unit.
type Task struct {
	ID           string             `json:"id"`
	Status       TaskStatus         `json:"status"`
	Agent        string             `json:"agent"`
	Prompt       string             `json:"-"`
	Model        string             `json:"model,omitempty"`
	System       string             `json:"-"`
	OutputFormat string             `json:"output_format,omitempty"`
	Result       string             `json:"result,omitempty"`
	Error        string             `json:"error,omitempty"`
	Valid        *bool              `json:"valid,omitempty"`
	CreatedAt    time.Time          `json:"created_at"`
	CompletedAt  *time.Time         `json:"completed_at,omitempty"`
	ContainerID  string             `json:"-"`
	CancelFunc   context.CancelFunc `json:"-"`
	Done         chan struct{}      `json:"-"` // closed when task reaches a terminal state
}

// TaskStore is a thread-safe store for one-shot tasks with a 100-task cap.
type TaskStore struct {
	mu    sync.RWMutex
	tasks map[string]*Task
	order []string // insertion order (FIFO), capped at 100
}

// NewTaskStore creates an empty TaskStore.
func NewTaskStore() *TaskStore {
	return &TaskStore{
		tasks: make(map[string]*Task),
	}
}

// Add inserts a task. If the store already holds 100 tasks, the oldest is evicted.
func (s *TaskStore) Add(t *Task) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.order) >= 100 {
		oldest := s.order[0]
		s.order = s.order[1:]
		delete(s.tasks, oldest)
	}
	s.tasks[t.ID] = t
	s.order = append(s.order, t.ID)
}

// Get returns a task by ID, or nil if not found.
func (s *TaskStore) Get(id string) *Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tasks[id]
}

// Update applies fn to the task with the given ID while holding a write lock.
// Returns false if the task was not found.
func (s *TaskStore) Update(id string, fn func(*Task)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return false
	}
	fn(t)
	return true
}

// List returns all tasks ordered by created_at descending (most recent first),
// capped at 100 entries.
func (s *TaskStore) List() []*Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	all := make([]*Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		all = append(all, t)
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].CreatedAt.After(all[j].CreatedAt)
	})
	if len(all) > 100 {
		all = all[:100]
	}
	return all
}

// All returns all tasks (unordered). Used for state persistence.
func (s *TaskStore) All() []*Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	all := make([]*Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		all = append(all, t)
	}
	return all
}

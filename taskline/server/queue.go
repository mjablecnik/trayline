package main

import (
	"errors"
	"strings"
	"sync"
	"time"
)

// TaskStatus represents the lifecycle state of a single Task.
type TaskStatus string

const (
	TaskPending TaskStatus = "pending"
	TaskRunning TaskStatus = "running"
	TaskFailed  TaskStatus = "failed"
)

// QueueState represents the overall processing state of the Queue.
type QueueState string

const (
	QueueIdle    QueueState = "idle"
	QueueRunning QueueState = "running"
	QueueHalted  QueueState = "halted"
)

// Task is a single shell command submitted to the queue.
type Task struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Command   string     `json:"command"`
	Status    TaskStatus `json:"status"`
	ExitCode  *int       `json:"exit_code,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

var (
	// ErrEmptyCommand is returned when a task is submitted with an empty or
	// whitespace-only command.
	ErrEmptyCommand = errors.New("command must not be empty")
	// ErrNameTaken is returned when a user-provided name collides with an
	// existing task in the queue.
	ErrNameTaken = errors.New("name is already in use")
	// ErrInvalidPosition is returned when a negative position is requested.
	ErrInvalidPosition = errors.New("position must be a non-negative integer")
	// ErrTaskNotFound is returned when an identifier matches no task.
	ErrTaskNotFound = errors.New("task not found")
	// ErrQueueAlreadyRunning is returned by Resume when the queue is already running.
	ErrQueueAlreadyRunning = errors.New("queue is already running")
	// ErrQueueHalted is returned by Resume when a failed task is blocking the queue.
	ErrQueueHalted = errors.New("queue is halted due to a failed task; retry or skip it first")
	// ErrNoFailedTask is returned by Retry and Skip when no task is failed.
	ErrNoFailedTask = errors.New("no failed task to retry or skip")
	// ErrNoRunningTask is returned by MarkFailed and MarkComplete when no task is running.
	ErrNoRunningTask = errors.New("no task is currently running")
	// ErrTaskRunning is returned by DeleteTask and UpdateTask for a running task.
	ErrTaskRunning = errors.New("running tasks cannot be modified")
	// ErrTaskFailedImmutable is returned by UpdateTask for a failed task.
	ErrTaskFailedImmutable = errors.New("failed tasks cannot be updated")
	// ErrNoUpdateFields is returned by UpdateTask when neither command nor name is provided.
	ErrNoUpdateFields = errors.New("at least one non-empty field must be provided")
)

// Queue is the thread-safe, ordered collection of Tasks processed by the
// Worker. The physical order of Tasks preserves FIFO execution order among
// pending tasks; running and failed tasks (at most one of either at a time)
// may sit anywhere in the slice and are repositioned for display separately.
type Queue struct {
	mu    sync.Mutex
	State QueueState
	Tasks []*Task

	names *NameGenerator
}

// NewQueue returns an empty, idle Queue backed by the given NameGenerator.
func NewQueue(names *NameGenerator) *Queue {
	return &Queue{
		State: QueueIdle,
		Tasks: []*Task{},
		names: names,
	}
}

// AddTask creates a new pending Task and inserts it into the queue.
//
// If name is empty, a Docker-style name is generated. If position is
// non-nil, the task is inserted at that index among the queue's current
// pending tasks (0 meaning first); a position at or beyond the number of
// pending tasks appends the task to the end of the queue. If the queue was
// idle, it transitions to running.
func (q *Queue) AddTask(command, name string, position *int) (*Task, error) {
	if strings.TrimSpace(command) == "" {
		return nil, ErrEmptyCommand
	}
	if position != nil && *position < 0 {
		return nil, ErrInvalidPosition
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	resolvedName := name
	if resolvedName != "" {
		if err := ValidateName(resolvedName); err != nil {
			return nil, err
		}
		if q.findByNameLocked(resolvedName) != nil {
			return nil, ErrNameTaken
		}
		q.names.ReserveName(resolvedName)
	} else {
		resolvedName = q.names.GenerateName()
	}

	task := &Task{
		ID:        q.names.GenerateID(),
		Name:      resolvedName,
		Command:   command,
		Status:    TaskPending,
		CreatedAt: time.Now().UTC(),
	}

	pendingPositions := q.pendingIndicesLocked()
	if position == nil || *position >= len(pendingPositions) {
		q.Tasks = append(q.Tasks, task)
	} else {
		insertAt := pendingPositions[*position]
		q.Tasks = append(q.Tasks, nil)
		copy(q.Tasks[insertAt+1:], q.Tasks[insertAt:])
		q.Tasks[insertAt] = task
	}

	if q.State == QueueIdle {
		q.State = QueueRunning
	}

	return task, nil
}

// RemoveTask removes the task matching identifier (resolved by ID, then by
// name) from the queue and returns it.
func (q *Queue) RemoveTask(identifier string) (*Task, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	task, idx := q.findLocked(identifier)
	if task == nil {
		return nil, ErrTaskNotFound
	}
	q.Tasks = append(q.Tasks[:idx], q.Tasks[idx+1:]...)
	return task, nil
}

// FindTask resolves identifier against Task_ID first, then Task_Name, both
// case-sensitive, and returns the matching task.
func (q *Queue) FindTask(identifier string) (*Task, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	task, _ := q.findLocked(identifier)
	if task == nil {
		return nil, ErrTaskNotFound
	}
	return task, nil
}

func (q *Queue) findLocked(identifier string) (*Task, int) {
	for i, t := range q.Tasks {
		if t.ID == identifier {
			return t, i
		}
	}
	for i, t := range q.Tasks {
		if t.Name == identifier {
			return t, i
		}
	}
	return nil, -1
}

func (q *Queue) findByNameLocked(name string) *Task {
	for _, t := range q.Tasks {
		if t.Name == name {
			return t
		}
	}
	return nil
}

func (q *Queue) pendingIndicesLocked() []int {
	var indices []int
	for i, t := range q.Tasks {
		if t.Status == TaskPending {
			indices = append(indices, i)
		}
	}
	return indices
}

func (q *Queue) runningTaskLocked() (*Task, int) {
	for i, t := range q.Tasks {
		if t.Status == TaskRunning {
			return t, i
		}
	}
	return nil, -1
}

func (q *Queue) failedTaskLocked() (*Task, int) {
	for i, t := range q.Tasks {
		if t.Status == TaskFailed {
			return t, i
		}
	}
	return nil, -1
}

// moveToFrontOfPendingLocked removes the task at idx and reinserts it
// immediately before the first pending task in the slice (or at the end if
// there are no other pending tasks), making it position 0 among pending
// tasks.
func (q *Queue) moveToFrontOfPendingLocked(idx int) *Task {
	task := q.Tasks[idx]
	q.Tasks = append(q.Tasks[:idx], q.Tasks[idx+1:]...)

	insertAt := len(q.Tasks)
	for i, t := range q.Tasks {
		if t.Status == TaskPending {
			insertAt = i
			break
		}
	}
	q.Tasks = append(q.Tasks, nil)
	copy(q.Tasks[insertAt+1:], q.Tasks[insertAt:])
	q.Tasks[insertAt] = task
	return task
}

// StartNext transitions the first pending Task to "running" and returns it,
// so the Worker can begin executing its command (Requirements 3.1, 3.2). It
// returns nil if the Queue is not "running", a Task is already running, or
// no pending Tasks remain.
func (q *Queue) StartNext() *Task {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.State != QueueRunning {
		return nil
	}
	if t, _ := q.runningTaskLocked(); t != nil {
		return nil
	}
	for _, t := range q.Tasks {
		if t.Status == TaskPending {
			t.Status = TaskRunning
			return t
		}
	}
	return nil
}

// MarkComplete removes the currently running Task (successful exit code 0)
// and, if no pending Tasks remain, transitions the Queue to "idle".
// Requirement 3.3.
func (q *Queue) MarkComplete() (*Task, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	task, idx := q.runningTaskLocked()
	if task == nil {
		return nil, ErrNoRunningTask
	}
	q.Tasks = append(q.Tasks[:idx], q.Tasks[idx+1:]...)
	if len(q.pendingIndicesLocked()) == 0 {
		q.State = QueueIdle
	}
	return task, nil
}

// MarkFailed transitions the currently running Task to "failed" with the
// given exit code and halts the Queue. Requirement 4.1.
func (q *Queue) MarkFailed(exitCode int) (*Task, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	task, _ := q.runningTaskLocked()
	if task == nil {
		return nil, ErrNoRunningTask
	}
	task.Status = TaskFailed
	task.ExitCode = &exitCode
	q.State = QueueHalted
	return task, nil
}

// Retry resets the failed Task to "pending", places it first among pending
// tasks, and transitions the Queue to "running". Returns ErrNoFailedTask if
// no task is currently failed. Requirement 6.1.
func (q *Queue) Retry() (*Task, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	task, idx := q.failedTaskLocked()
	if task == nil {
		return nil, ErrNoFailedTask
	}
	task.Status = TaskPending
	task.ExitCode = nil
	q.moveToFrontOfPendingLocked(idx)
	q.State = QueueRunning
	return task, nil
}

// Skip removes the failed Task from the Queue and transitions the Queue to
// "running". Returns ErrNoFailedTask if no task is currently failed.
// Requirement 6.3.
func (q *Queue) Skip() (*Task, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	task, idx := q.failedTaskLocked()
	if task == nil {
		return nil, ErrNoFailedTask
	}
	q.Tasks = append(q.Tasks[:idx], q.Tasks[idx+1:]...)
	q.State = QueueRunning
	return task, nil
}

// Resume transitions the Queue to "running" state so the Worker can begin
// executing pending Tasks. It returns ErrQueueAlreadyRunning if the Queue is
// already running, or ErrQueueHalted if a failed Task is blocking it. If no
// pending Tasks remain, the Queue transitions to "idle" instead and empty is
// true. Requirements 6.5, 6.6, 6.7, 6.8.
func (q *Queue) Resume() (empty bool, err error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.State == QueueRunning {
		return false, ErrQueueAlreadyRunning
	}
	if task, _ := q.failedTaskLocked(); task != nil {
		return false, ErrQueueHalted
	}
	if len(q.pendingIndicesLocked()) == 0 {
		q.State = QueueIdle
		return true, nil
	}
	q.State = QueueRunning
	return false, nil
}

// DeleteTask removes the Task matching identifier from the Queue. Running
// tasks cannot be deleted (ErrTaskRunning). Deleting a failed task
// transitions the Queue to "idle". Requirement 8.
func (q *Queue) DeleteTask(identifier string) (*Task, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	task, idx := q.findLocked(identifier)
	if task == nil {
		return nil, ErrTaskNotFound
	}
	if task.Status == TaskRunning {
		return nil, ErrTaskRunning
	}
	q.Tasks = append(q.Tasks[:idx], q.Tasks[idx+1:]...)
	if task.Status == TaskFailed {
		q.State = QueueIdle
	}
	return task, nil
}

// UpdateTask applies a non-empty command and/or name to the pending Task
// matching identifier. Running and failed tasks cannot be updated
// (ErrTaskRunning, ErrTaskFailedImmutable). A new name must pass ValidateName
// and be unique among the other tasks in the Queue. Requirement 9.
func (q *Queue) UpdateTask(identifier, command, name string) (*Task, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	task, _ := q.findLocked(identifier)
	if task == nil {
		return nil, ErrTaskNotFound
	}
	if task.Status == TaskRunning {
		return nil, ErrTaskRunning
	}
	if task.Status == TaskFailed {
		return nil, ErrTaskFailedImmutable
	}

	newCommand := strings.TrimSpace(command) != ""
	newName := strings.TrimSpace(name) != ""
	if !newCommand && !newName {
		return nil, ErrNoUpdateFields
	}

	if newName && name != task.Name {
		if err := ValidateName(name); err != nil {
			return nil, err
		}
		if existing := q.findByNameLocked(name); existing != nil && existing != task {
			return nil, ErrNameTaken
		}
	}

	if newCommand {
		task.Command = command
	}
	if newName {
		q.names.ReserveName(name)
		task.Name = name
	}
	return task, nil
}

// CurrentState returns the Queue's current state ("idle", "running", or
// "halted"). Safe for concurrent use with any other Queue method, unlike
// reading the State field directly.
func (q *Queue) CurrentState() QueueState {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.State
}

// CurrentTask returns a copy of the currently running Task, or nil if no
// Task is running. Used by the queue status endpoint (Requirement 10.4).
func (q *Queue) CurrentTask() *Task {
	q.mu.Lock()
	defer q.mu.Unlock()
	if t, _ := q.runningTaskLocked(); t != nil {
		cp := *t
		return &cp
	}
	return nil
}

// FailedTaskInfo returns a copy of the currently failed Task, or nil if no
// Task is failed. Used by the queue status endpoint (Requirement 10.3).
func (q *Queue) FailedTaskInfo() *Task {
	q.mu.Lock()
	defer q.mu.Unlock()
	if t, _ := q.failedTaskLocked(); t != nil {
		cp := *t
		return &cp
	}
	return nil
}

// PendingCount returns the number of pending Tasks in the Queue.
func (q *Queue) PendingCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pendingIndicesLocked())
}

// Snapshot returns a copy of the Task matching identifier (resolved by ID,
// then by name). Unlike FindTask, which returns a pointer into the live
// Queue, the copy is safe to read after Snapshot returns even while the
// Worker concurrently mutates the Task's Status and ExitCode.
func (q *Queue) Snapshot(identifier string) (Task, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	task, _ := q.findLocked(identifier)
	if task == nil {
		return Task{}, ErrTaskNotFound
	}
	return *task, nil
}

// List returns the Queue's Tasks in display order: the running task (if
// any) first, then pending tasks in execution order, then the failed task
// (if any) last. Requirement 7.4.
func (q *Queue) List() []*Task {
	q.mu.Lock()
	defer q.mu.Unlock()

	var running, failed *Task
	pending := make([]*Task, 0, len(q.Tasks))
	for _, t := range q.Tasks {
		switch t.Status {
		case TaskRunning:
			running = t
		case TaskFailed:
			failed = t
		default:
			pending = append(pending, t)
		}
	}

	result := make([]*Task, 0, len(q.Tasks))
	if running != nil {
		result = append(result, running)
	}
	result = append(result, pending...)
	if failed != nil {
		result = append(result, failed)
	}
	return result
}

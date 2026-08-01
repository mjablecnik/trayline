package main

import (
	"regexp"
	"testing"
	"time"

	"pgregory.net/rapid"
)

var taskIDPattern = regexp.MustCompile(`^[a-z0-9]{8}$`)

func genCommand(t *rapid.T, label string) string {
	return rapid.StringMatching(`[a-zA-Z0-9_/.-][a-zA-Z0-9 _/.-]{0,39}`).Draw(t, label)
}

// Feature: taskline, Property 2: Task creation produces valid structure
//
// For any non-empty, non-whitespace-only command string submitted to the
// queue, the resulting Task shall have a Task_ID matching [a-z0-9]{8},
// status "pending", a CreatedAt timestamp not before the submission time,
// and the original command stored verbatim.
func TestProperty_TaskCreationProducesValidStructure(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		command := genCommand(t, "command")

		q := NewQueue(NewNameGenerator())
		before := time.Now().UTC()
		task, err := q.AddTask(command, "", "", nil)
		if err != nil {
			t.Fatalf("unexpected error adding command %q: %v", command, err)
		}

		if !taskIDPattern.MatchString(task.ID) {
			t.Fatalf("task ID %q does not match [a-z0-9]{8}", task.ID)
		}
		if task.Status != TaskPending {
			t.Fatalf("expected status pending, got %q", task.Status)
		}
		if task.CreatedAt.Before(before) {
			t.Fatalf("CreatedAt %v is before submission time %v", task.CreatedAt, before)
		}
		if task.Command != command {
			t.Fatalf("expected command %q stored verbatim, got %q", command, task.Command)
		}
	})
}

// Feature: taskline, Property 3: Position insertion correctness
//
// For any queue with N pending tasks and a position value P, if P is a
// non-negative integer <= N, the new task is inserted at index P among
// pending tasks. If P > N, the task is appended at the end. If P is
// negative, the request is rejected.
func TestProperty_PositionInsertionCorrectness(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(0, 8).Draw(t, "n")

		q := NewQueue(NewNameGenerator())
		for i := 0; i < n; i++ {
			if _, err := q.AddTask(genCommand(t, "seedCommand"), "", "", nil); err != nil {
				t.Fatalf("unexpected error seeding pending task: %v", err)
			}
		}

		negative := rapid.Bool().Draw(t, "negative")
		if negative {
			p := -rapid.IntRange(1, 5).Draw(t, "negAmount")
			_, err := q.AddTask(genCommand(t, "command"), "", "", &p)
			if err != ErrInvalidPosition {
				t.Fatalf("expected ErrInvalidPosition for position %d, got %v", p, err)
			}
			return
		}

		p := rapid.IntRange(0, n+5).Draw(t, "position")
		task, err := q.AddTask(genCommand(t, "command"), "", "", &p)
		if err != nil {
			t.Fatalf("unexpected error inserting at position %d: %v", p, err)
		}

		pendingIDs := make([]string, 0, len(q.Tasks))
		for _, tk := range q.Tasks {
			if tk.Status == TaskPending {
				pendingIDs = append(pendingIDs, tk.ID)
			}
		}

		expectedIdx := p
		if p > n {
			expectedIdx = n
		}
		if pendingIDs[expectedIdx] != task.ID {
			t.Fatalf("expected inserted task at pending index %d, found %q instead of %q", expectedIdx, pendingIDs[expectedIdx], task.ID)
		}
	})
}

// Feature: taskline, Property 6: Name uniqueness invariant
//
// For any sequence of task additions and removals within a single server
// session, no two tasks in the queue shall ever share the same Task_ID or
// Task_Name, and no auto-generated Task_ID or Task_Name shall be reused
// after a task has been removed from the queue.
func TestProperty_NameUniquenessInvariant(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		q := NewQueue(NewNameGenerator())
		seenIDs := map[string]bool{}
		seenNames := map[string]bool{}

		steps := rapid.IntRange(1, 30).Draw(t, "steps")
		for i := 0; i < steps; i++ {
			addOrRemove := rapid.Bool().Draw(t, "addOrRemove")
			if addOrRemove || len(q.Tasks) == 0 {
				task, err := q.AddTask(genCommand(t, "command"), "", "", nil)
				if err != nil {
					t.Fatalf("unexpected error adding task: %v", err)
				}
				if seenIDs[task.ID] {
					t.Fatalf("task ID %q was reused after removal", task.ID)
				}
				if seenNames[task.Name] {
					t.Fatalf("task name %q was reused after removal", task.Name)
				}
				seenIDs[task.ID] = true
				seenNames[task.Name] = true
			} else {
				idx := rapid.IntRange(0, len(q.Tasks)-1).Draw(t, "removeIdx")
				identifier := q.Tasks[idx].ID
				if _, err := q.RemoveTask(identifier); err != nil {
					t.Fatalf("unexpected error removing task %q: %v", identifier, err)
				}
			}

			// Invariant: no two currently active tasks share an ID or name.
			currentIDs := map[string]bool{}
			currentNames := map[string]bool{}
			for _, tk := range q.Tasks {
				if currentIDs[tk.ID] {
					t.Fatalf("duplicate task ID %q found among active tasks", tk.ID)
				}
				currentIDs[tk.ID] = true
				if currentNames[tk.Name] {
					t.Fatalf("duplicate task name %q found among active tasks", tk.Name)
				}
				currentNames[tk.Name] = true
			}
		}
	})
}

// Feature: taskline, Property 14: Identifier resolution order
//
// For any task in the queue, resolving by its Task_ID shall always find it.
// For any identifier string, the system shall first attempt a case-sensitive
// match against Task_ID, and only if no match is found, attempt a
// case-sensitive match against Task_Name. If a Task_ID happens to equal
// another task's Task_Name, the ID match shall take precedence.
func TestProperty_IdentifierResolutionOrder(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		idA := rapid.StringMatching(`[a-z0-9]{8}`).Draw(t, "idA")
		idB := rapid.StringMatching(`[a-z0-9]{8}`).Draw(t, "idB")
		nameA := rapid.StringMatching(`[a-z][a-z0-9-]{1,10}`).Draw(t, "nameA")
		if idA == idB || idA == nameA {
			t.Skip("drew colliding identifiers")
		}

		taskA := &Task{ID: idA, Name: nameA, Command: "echo a", Status: TaskPending, CreatedAt: time.Now().UTC()}
		// Task B's name equals task A's ID, making idA an ambiguous identifier
		// that could resolve to either A (by ID) or B (by name).
		taskB := &Task{ID: idB, Name: idA, Command: "echo b", Status: TaskPending, CreatedAt: time.Now().UTC()}

		q := NewQueue(NewNameGenerator())
		q.Tasks = []*Task{taskA, taskB}

		resolved, err := q.FindTask(idA)
		if err != nil {
			t.Fatalf("unexpected error resolving ambiguous identifier %q: %v", idA, err)
		}
		if resolved != taskA {
			t.Fatalf("expected ID match to take precedence: resolving %q should return task A, got task with ID %q", idA, resolved.ID)
		}

		resolvedByName, err := q.FindTask(nameA)
		if err != nil {
			t.Fatalf("unexpected error resolving name %q: %v", nameA, err)
		}
		if resolvedByName != taskA {
			t.Fatalf("expected name %q to resolve to task A, got task with ID %q", nameA, resolvedByName.ID)
		}
	})
}

// Feature: taskline, Property 8: Failure halts queue
//
// For any task whose command exits with a non-zero exit code, the task's
// status shall transition to "failed" and the queue state shall transition
// to "halted". While the queue is in "halted" state, no pending task shall
// transition to "running".
func TestProperty_FailureHaltsQueue(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		q := NewQueue(NewNameGenerator())
		n := rapid.IntRange(1, 5).Draw(t, "n")
		for i := 0; i < n; i++ {
			if _, err := q.AddTask(genCommand(t, "seedCommand"), "", "", nil); err != nil {
				t.Fatalf("unexpected error seeding pending task: %v", err)
			}
		}

		running := q.StartNext()
		if running == nil {
			t.Fatalf("expected a task to start running")
		}

		exitCode := rapid.IntRange(1, 255).Draw(t, "exitCode")
		failed, err := q.MarkFailed(exitCode)
		if err != nil {
			t.Fatalf("unexpected error marking task failed: %v", err)
		}
		if failed.Status != TaskFailed {
			t.Fatalf("expected task status failed, got %q", failed.Status)
		}
		if failed.ExitCode == nil || *failed.ExitCode != exitCode {
			t.Fatalf("expected exit code %d recorded, got %v", exitCode, failed.ExitCode)
		}
		if q.State != QueueHalted {
			t.Fatalf("expected queue state halted, got %q", q.State)
		}

		if next := q.StartNext(); next != nil {
			t.Fatalf("expected no task to start while queue is halted, got %q", next.ID)
		}
	})
}

// Feature: taskline, Property 9: Retry resets failed task
//
// For any queue in "halted" state with a failed task and zero or more
// pending tasks, invoking retry shall reset the failed task's status to
// "pending", place it at position 0 among pending tasks, and transition the
// queue state to "running".
func TestProperty_RetryResetsFailedTask(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		q := NewQueue(NewNameGenerator())
		n := rapid.IntRange(1, 5).Draw(t, "n")
		for i := 0; i < n; i++ {
			if _, err := q.AddTask(genCommand(t, "seedCommand"), "", "", nil); err != nil {
				t.Fatalf("unexpected error seeding pending task: %v", err)
			}
		}

		running := q.StartNext()
		if running == nil {
			t.Fatalf("expected a task to start running")
		}
		failed, err := q.MarkFailed(rapid.IntRange(1, 255).Draw(t, "exitCode"))
		if err != nil {
			t.Fatalf("unexpected error marking task failed: %v", err)
		}

		retried, err := q.Retry()
		if err != nil {
			t.Fatalf("unexpected error retrying: %v", err)
		}
		if retried != failed {
			t.Fatalf("expected retry to return the previously failed task")
		}
		if retried.Status != TaskPending {
			t.Fatalf("expected retried task status pending, got %q", retried.Status)
		}
		if retried.ExitCode != nil {
			t.Fatalf("expected retried task exit code cleared, got %v", retried.ExitCode)
		}
		if q.State != QueueRunning {
			t.Fatalf("expected queue state running, got %q", q.State)
		}

		pendingIDs := make([]string, 0, len(q.Tasks))
		for _, tk := range q.Tasks {
			if tk.Status == TaskPending {
				pendingIDs = append(pendingIDs, tk.ID)
			}
		}
		if len(pendingIDs) == 0 || pendingIDs[0] != retried.ID {
			t.Fatalf("expected retried task at pending position 0, got %v", pendingIDs)
		}
	})
}

// Feature: taskline, Property 10: Skip removes failed task
//
// For any queue in "halted" state with a failed task, invoking skip shall
// remove the failed task from the queue and transition the queue state to
// "running".
func TestProperty_SkipRemovesFailedTask(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		q := NewQueue(NewNameGenerator())
		n := rapid.IntRange(1, 5).Draw(t, "n")
		for i := 0; i < n; i++ {
			if _, err := q.AddTask(genCommand(t, "seedCommand"), "", "", nil); err != nil {
				t.Fatalf("unexpected error seeding pending task: %v", err)
			}
		}

		running := q.StartNext()
		if running == nil {
			t.Fatalf("expected a task to start running")
		}
		failed, err := q.MarkFailed(rapid.IntRange(1, 255).Draw(t, "exitCode"))
		if err != nil {
			t.Fatalf("unexpected error marking task failed: %v", err)
		}

		skipped, err := q.Skip()
		if err != nil {
			t.Fatalf("unexpected error skipping: %v", err)
		}
		if skipped != failed {
			t.Fatalf("expected skip to return the previously failed task")
		}
		if q.State != QueueRunning {
			t.Fatalf("expected queue state running, got %q", q.State)
		}
		for _, tk := range q.Tasks {
			if tk == failed {
				t.Fatalf("expected failed task %q to be removed from queue", failed.ID)
			}
		}
	})
}

// Feature: taskline, Property 11: Task list ordering
//
// For any queue containing tasks, the list response shall order tasks by
// queue position: the running task (if any) at index 0, followed by pending
// tasks in execution order, followed by the failed task (if any) at the last
// index.
func TestProperty_TaskListOrdering(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		q := NewQueue(NewNameGenerator())
		n := rapid.IntRange(0, 6).Draw(t, "n")
		var pendingIDsInOrder []string
		for i := 0; i < n; i++ {
			task, err := q.AddTask(genCommand(t, "seedCommand"), "", "", nil)
			if err != nil {
				t.Fatalf("unexpected error seeding pending task: %v", err)
			}
			pendingIDsInOrder = append(pendingIDsInOrder, task.ID)
		}

		var runningID, failedID string
		if n > 0 && rapid.Bool().Draw(t, "hasRunning") {
			running := q.StartNext()
			runningID = running.ID
			pendingIDsInOrder = pendingIDsInOrder[1:]

			if rapid.Bool().Draw(t, "hasFailed") {
				failed, err := q.MarkFailed(1)
				if err != nil {
					t.Fatalf("unexpected error marking task failed: %v", err)
				}
				failedID = failed.ID
				runningID = ""
			}
		}

		list := q.List()

		var expected []string
		if runningID != "" {
			expected = append(expected, runningID)
		}
		expected = append(expected, pendingIDsInOrder...)
		if failedID != "" {
			expected = append(expected, failedID)
		}

		if len(list) != len(expected) {
			t.Fatalf("expected %d tasks in list, got %d", len(expected), len(list))
		}
		for i, tk := range list {
			if tk.ID != expected[i] {
				t.Fatalf("expected task at index %d to be %q, got %q", i, expected[i], tk.ID)
			}
		}
	})
}

// Feature: taskline, Property 18: Task update applies fields correctly
//
// For any pending task and a valid update request containing a non-empty
// command and/or non-empty name, after the update, the task's fields shall
// reflect the new values for provided fields and retain original values for
// omitted fields. The task's ID, status, and creation timestamp shall remain
// unchanged.
func TestProperty_TaskUpdateAppliesFieldsCorrectly(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		q := NewQueue(NewNameGenerator())
		task, err := q.AddTask(genCommand(t, "command"), "", "", nil)
		if err != nil {
			t.Fatalf("unexpected error adding task: %v", err)
		}
		originalID := task.ID
		originalStatus := task.Status
		originalCreatedAt := task.CreatedAt
		originalCommand := task.Command
		originalName := task.Name

		updateCommand := rapid.Bool().Draw(t, "updateCommand")
		updateName := rapid.Bool().Draw(t, "updateName")
		if !updateCommand && !updateName {
			updateCommand = true
		}

		var newCommand, newName string
		if updateCommand {
			newCommand = genCommand(t, "newCommand")
		}
		if updateName {
			newName = rapid.StringMatching(`[a-z][a-z0-9-]{0,20}`).Draw(t, "newName")
		}

		updated, err := q.UpdateTask(originalID, newCommand, newName)
		if err != nil {
			t.Fatalf("unexpected error updating task: %v", err)
		}

		if updated.ID != originalID {
			t.Fatalf("expected ID unchanged, got %q instead of %q", updated.ID, originalID)
		}
		if updated.Status != originalStatus {
			t.Fatalf("expected status unchanged, got %q instead of %q", updated.Status, originalStatus)
		}
		if !updated.CreatedAt.Equal(originalCreatedAt) {
			t.Fatalf("expected CreatedAt unchanged, got %v instead of %v", updated.CreatedAt, originalCreatedAt)
		}

		if updateCommand {
			if updated.Command != newCommand {
				t.Fatalf("expected command updated to %q, got %q", newCommand, updated.Command)
			}
		} else if updated.Command != originalCommand {
			t.Fatalf("expected command unchanged, got %q instead of %q", updated.Command, originalCommand)
		}

		if updateName {
			if updated.Name != newName {
				t.Fatalf("expected name updated to %q, got %q", newName, updated.Name)
			}
		} else if updated.Name != originalName {
			t.Fatalf("expected name unchanged, got %q instead of %q", updated.Name, originalName)
		}
	})
}

func TestDeleteTask_NotFoundReturnsError(t *testing.T) {
	q := NewQueue(NewNameGenerator())
	if _, err := q.DeleteTask("nonexistent"); err != ErrTaskNotFound {
		t.Fatalf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestDeleteTask_RunningTaskReturnsError(t *testing.T) {
	q := NewQueue(NewNameGenerator())
	task, err := q.AddTask("echo hi", "", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	q.StartNext()

	if _, err := q.DeleteTask(task.ID); err != ErrTaskRunning {
		t.Fatalf("expected ErrTaskRunning, got %v", err)
	}
}

func TestDeleteTask_FailedTaskTransitionsQueueToIdle(t *testing.T) {
	q := NewQueue(NewNameGenerator())
	task, err := q.AddTask("echo hi", "", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	q.StartNext()
	if _, err := q.MarkFailed(1); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	deleted, err := q.DeleteTask(task.ID)
	if err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	if deleted.ID != task.ID {
		t.Fatalf("expected deleted task %q, got %q", task.ID, deleted.ID)
	}
	if q.State != QueueIdle {
		t.Fatalf("expected queue idle after deleting failed task, got %q", q.State)
	}
}

func TestUpdateTask_InvalidNameReturnsValidationError(t *testing.T) {
	q := NewQueue(NewNameGenerator())
	task, err := q.AddTask("echo hi", "", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	if _, err := q.UpdateTask(task.ID, "", "Invalid Name"); err == nil {
		t.Fatal("expected validation error for invalid name, got nil")
	}
}

func TestUpdateTask_NameCollisionReturnsErrNameTaken(t *testing.T) {
	q := NewQueue(NewNameGenerator())
	_, err := q.AddTask("echo a", "taken-name", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	task2, err := q.AddTask("echo b", "other-name", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	if _, err := q.UpdateTask(task2.ID, "", "taken-name"); err != ErrNameTaken {
		t.Fatalf("expected ErrNameTaken, got %v", err)
	}
}

func TestUpdateTask_RunningTaskReturnsError(t *testing.T) {
	q := NewQueue(NewNameGenerator())
	task, err := q.AddTask("echo hi", "", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	q.StartNext()

	if _, err := q.UpdateTask(task.ID, "echo bye", ""); err != ErrTaskRunning {
		t.Fatalf("expected ErrTaskRunning, got %v", err)
	}
}

func TestUpdateTask_FailedTaskReturnsImmutableError(t *testing.T) {
	q := NewQueue(NewNameGenerator())
	task, err := q.AddTask("echo hi", "", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	q.StartNext()
	if _, err := q.MarkFailed(1); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	if _, err := q.UpdateTask(task.ID, "echo bye", ""); err != ErrTaskFailedImmutable {
		t.Fatalf("expected ErrTaskFailedImmutable, got %v", err)
	}
}

func TestUpdateTask_SameNameIsAllowed(t *testing.T) {
	q := NewQueue(NewNameGenerator())
	task, err := q.AddTask("echo hi", "my-name", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	updated, err := q.UpdateTask(task.ID, "", "my-name")
	if err != nil {
		t.Fatalf("expected updating to the same name to be allowed, got error: %v", err)
	}
	if updated.Name != "my-name" {
		t.Fatalf("expected name %q, got %q", "my-name", updated.Name)
	}
}

func TestResume_HaltedQueueReturnsError(t *testing.T) {
	q := NewQueue(NewNameGenerator())
	if _, err := q.AddTask("echo hi", "", "", nil); err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	q.StartNext()
	if _, err := q.MarkFailed(1); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	if _, err := q.Resume(); err != ErrQueueHalted {
		t.Fatalf("expected ErrQueueHalted, got %v", err)
	}
}

func TestResume_AlreadyRunningReturnsError(t *testing.T) {
	q := NewQueue(NewNameGenerator())
	if _, err := q.AddTask("echo hi", "", "", nil); err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	if _, err := q.Resume(); err != ErrQueueAlreadyRunning {
		t.Fatalf("expected ErrQueueAlreadyRunning, got %v", err)
	}
}

func TestResume_NoPendingTasksTransitionsToIdleAndReportsEmpty(t *testing.T) {
	// A fresh queue is already idle with zero pending tasks and no failed
	// task, which is exactly Resume's no-pending branch.
	q := NewQueue(NewNameGenerator())

	empty, err := q.Resume()
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if !empty {
		t.Fatal("expected empty=true when no pending tasks remain")
	}
	if q.State != QueueIdle {
		t.Fatalf("expected queue idle, got %q", q.State)
	}
}

func TestSnapshot_NotFoundReturnsError(t *testing.T) {
	q := NewQueue(NewNameGenerator())
	if _, err := q.Snapshot("nonexistent"); err != ErrTaskNotFound {
		t.Fatalf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestRemoveTask_NotFoundReturnsError(t *testing.T) {
	q := NewQueue(NewNameGenerator())
	if _, err := q.RemoveTask("nonexistent"); err != ErrTaskNotFound {
		t.Fatalf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestFindTask_NotFoundReturnsError(t *testing.T) {
	q := NewQueue(NewNameGenerator())
	if _, err := q.FindTask("nonexistent"); err != ErrTaskNotFound {
		t.Fatalf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestStartNext_NotRunningQueueReturnsNil(t *testing.T) {
	q := NewQueue(NewNameGenerator())
	// A freshly created queue with no tasks is idle, not running.
	if next := q.StartNext(); next != nil {
		t.Fatalf("expected nil when queue is not running, got %+v", next)
	}
}

func TestStartNext_AlreadyRunningTaskReturnsNil(t *testing.T) {
	q := NewQueue(NewNameGenerator())
	if _, err := q.AddTask("echo a", "", "", nil); err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if _, err := q.AddTask("echo b", "", "", nil); err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	first := q.StartNext()
	if first == nil {
		t.Fatal("expected first task to start")
	}
	if second := q.StartNext(); second != nil {
		t.Fatalf("expected nil while a task is already running, got %+v", second)
	}
}

func TestStartNext_NoPendingTasksReturnsNil(t *testing.T) {
	q := NewQueue(NewNameGenerator())
	task, err := q.AddTask("echo hi", "", "", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	// AddTask transitioned the queue to "running"; removing the only task
	// (without going through the worker) leaves it "running" with zero
	// pending tasks, exercising StartNext's own no-pending branch.
	if _, err := q.RemoveTask(task.ID); err != nil {
		t.Fatalf("RemoveTask: %v", err)
	}

	if next := q.StartNext(); next != nil {
		t.Fatalf("expected nil when no pending tasks remain, got %+v", next)
	}
}

func TestMarkComplete_NoRunningTaskReturnsError(t *testing.T) {
	q := NewQueue(NewNameGenerator())
	if _, err := q.MarkComplete(); err != ErrNoRunningTask {
		t.Fatalf("expected ErrNoRunningTask, got %v", err)
	}
}

func TestMarkFailed_NoRunningTaskReturnsError(t *testing.T) {
	q := NewQueue(NewNameGenerator())
	if _, err := q.MarkFailed(1); err != ErrNoRunningTask {
		t.Fatalf("expected ErrNoRunningTask, got %v", err)
	}
}

func TestCurrentTask_NilWhenNoneRunning(t *testing.T) {
	q := NewQueue(NewNameGenerator())
	if got := q.CurrentTask(); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestFailedTaskInfo_NilWhenNoneFailed(t *testing.T) {
	q := NewQueue(NewNameGenerator())
	if got := q.FailedTaskInfo(); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

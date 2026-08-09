package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ErrCorruptedState is returned by LoadState when the State_File exists but
// contains invalid JSON or JSON that does not match the expected schema. The
// file has already been renamed with a ".corrupted" suffix by the time this
// error is returned.
var ErrCorruptedState = errors.New("state file is corrupted")

// stateFile is the on-disk JSON representation of a Queue.
type stateFile struct {
	State QueueState `json:"state"`
	Tasks []*Task    `json:"tasks"`
}

// valid reports whether sf matches the minimum expected schema: a recognized
// queue state, and, for every task, a non-empty ID, a non-empty command, and
// a recognized status.
func (sf stateFile) valid() bool {
	switch sf.State {
	case QueueIdle, QueueRunning, QueueHalted:
	default:
		return false
	}
	for _, t := range sf.Tasks {
		if t == nil || t.ID == "" || strings.TrimSpace(t.Command) == "" {
			return false
		}
		switch t.Status {
		case TaskPending, TaskRunning, TaskFailed:
		default:
			return false
		}
	}
	return true
}

// SaveState atomically persists queue's current state and tasks to path by
// writing to a temporary file in the same directory and renaming it into
// place, so a crash mid-write never leaves a truncated or partially written
// State_File. Requirement 11.2.
func SaveState(queue *Queue, path string) error {
	queue.mu.Lock()
	data, err := json.MarshalIndent(stateFile{State: queue.State, Tasks: queue.Tasks}, "", "  ")
	queue.mu.Unlock()
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".taskline-state-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp state file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp state file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp state file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp state file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp state file into place: %w", err)
	}
	return nil
}

// LoadState reads the persisted Queue from path and returns a ready-to-use
// Queue backed by names.
//
// If path does not exist, it returns an empty, idle Queue and a nil error
// (Requirement 11.4 — this is the normal first-run case, not a warning
// condition). If path exists but contains invalid JSON or JSON that does not
// match the expected schema, it is renamed with a ".corrupted" suffix and
// LoadState returns an empty, idle Queue along with ErrCorruptedState so the
// caller can log a warning (Requirement 11.7). Otherwise the persisted state
// and tasks are restored, and every restored Task_ID and Task_Name is marked
// as used in names so future generation never collides with them
// (Requirement 11.6).
func LoadState(path string, names *NameGenerator) (*Queue, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewQueue(names), nil
		}
		return NewQueue(names), fmt.Errorf("read state file: %w", err)
	}

	var sf stateFile
	if err := json.Unmarshal(data, &sf); err != nil || !sf.valid() {
		if renameErr := os.Rename(path, path+".corrupted"); renameErr != nil {
			return NewQueue(names), fmt.Errorf("%w (also failed to rename corrupted file: %v)", ErrCorruptedState, renameErr)
		}
		return NewQueue(names), ErrCorruptedState
	}

	tasks := sf.Tasks
	if tasks == nil {
		tasks = []*Task{}
	}
	for _, t := range tasks {
		names.MarkUsed(t.ID, t.Name)
	}

	return &Queue{State: sf.State, Tasks: tasks, names: names}, nil
}

// projectStateFileRE matches the state file names the Registry creates via
// Registry.StatePath: taskline-<project>.json.
var projectStateFileRE = regexp.MustCompile(`^taskline-(.+)\.json$`)

// ScanStateDir finds every taskline-*.json file directly inside dir and
// loads each into a Queue, returning a map of project name to its restored
// Queue (FR-2.4). Used at server startup to repopulate the Registry from a
// previous run's persisted state. A dir that does not exist yet is treated
// as containing no projects, not an error (the normal case on first run). A
// project whose state file fails to load is logged and skipped rather than
// aborting the scan for every other project.
func ScanStateDir(dir string, names *NameGenerator) (map[string]*Queue, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]*Queue{}, nil
		}
		return nil, fmt.Errorf("read state dir: %w", err)
	}

	queues := make(map[string]*Queue)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		match := projectStateFileRE.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		project := match[1]

		queue, err := LoadState(filepath.Join(dir, entry.Name()), names)
		if err != nil {
			if errors.Is(err, ErrCorruptedState) {
				logWarn("project %s: state file %s is corrupted; renamed with .corrupted suffix, starting with an empty queue", project, entry.Name())
			} else {
				logError("project %s: failed to load state file %s: %v", project, entry.Name(), err)
				continue
			}
		}
		queues[project] = queue
	}
	return queues, nil
}

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ProjectInstance bundles everything the server maintains for a single
// project: its Queue, the Worker goroutine that drains it, the log it writes
// task output to, and the path its state is persisted to.
type ProjectInstance struct {
	Name      string
	Queue     *Queue
	Worker    *Worker
	LogWriter *ProjectLog
	StateFile string
}

// Registry is the top-level collection of every project the server knows
// about, keyed by project name (FR-1.1). Projects are created on demand the
// first time they are referenced (FR-1.4) and each runs its own Worker
// goroutine, so tasks in different projects execute in parallel while tasks
// within one project remain sequential (FR-1.2, FR-1.3).
type Registry struct {
	mu       sync.RWMutex
	projects map[string]*ProjectInstance
	stateDir string
	logDir   string
	names    *NameGenerator
	notifier Notifier
}

// NewRegistry returns an empty Registry that creates state files under
// stateDir and log files under logDir. names is shared across every
// project's Queue so generated Task IDs are unique server-wide.
func NewRegistry(stateDir, logDir string, names *NameGenerator, notifier Notifier) *Registry {
	return &Registry{
		projects: make(map[string]*ProjectInstance),
		stateDir: stateDir,
		logDir:   logDir,
		names:    names,
		notifier: notifier,
	}
}

// ValidateProjectName checks a project name against FR-1.5: lowercase
// alphanumeric characters, hyphens, and underscores, 1-64 characters long.
func ValidateProjectName(name string) error {
	if len(name) == 0 {
		return fmt.Errorf("project name must not be empty")
	}
	if len(name) > 64 {
		return fmt.Errorf("project name must not exceed 64 characters")
	}
	if strings.IndexFunc(name, func(r rune) bool {
		isLower := r >= 'a' && r <= 'z'
		isDigit := r >= '0' && r <= '9'
		return !isLower && !isDigit && r != '-' && r != '_'
	}) != -1 {
		return fmt.Errorf("project name may only contain lowercase letters, digits, hyphens, and underscores")
	}
	return nil
}

// StatePath returns the path this Registry persists project's state to
// (FR-2.1): <stateDir>/taskline-<project>.json.
func (r *Registry) StatePath(project string) string {
	return filepath.Join(r.stateDir, "taskline-"+project+".json")
}

// LogPath returns the path this Registry writes project's task output to
// (FR-3.1): <logDir>/<project>.log.
func (r *Registry) LogPath(project string) string {
	return filepath.Join(r.logDir, project+".log")
}

// GetOrCreate returns the ProjectInstance for project, validating the name
// first (FR-1.5). If this is the first time project has been referenced, its
// Queue, Worker, and ProjectLog are created — restoring any existing state
// file — and the Worker goroutine is started (FR-1.4).
func (r *Registry) GetOrCreate(project string) (*ProjectInstance, error) {
	if err := ValidateProjectName(project); err != nil {
		return nil, err
	}

	r.mu.RLock()
	inst, ok := r.projects[project]
	r.mu.RUnlock()
	if ok {
		return inst, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if inst, ok := r.projects[project]; ok {
		return inst, nil
	}

	inst, err := r.createLocked(project)
	if err != nil {
		return nil, err
	}
	return inst, nil
}

// createLocked builds and starts a new ProjectInstance for project. Callers
// must hold r.mu for writing.
func (r *Registry) createLocked(project string) (*ProjectInstance, error) {
	if err := r.ensureDirs(); err != nil {
		return nil, err
	}

	statePath := r.StatePath(project)
	queue, err := LoadState(statePath, r.names)
	if err != nil {
		if errors.Is(err, ErrCorruptedState) {
			logWarn("project %s: state file %s is corrupted; renamed with .corrupted suffix, starting with an empty queue", project, statePath)
		} else {
			return nil, fmt.Errorf("load state for project %q: %w", project, err)
		}
	}

	return r.startInstanceLocked(project, queue)
}

// ensureDirs creates the Registry's state and log directories if they do not
// already exist.
func (r *Registry) ensureDirs() error {
	if err := os.MkdirAll(r.stateDir, 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	if err := os.MkdirAll(r.logDir, 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	return nil
}

// startInstanceLocked recovers any Task left "running" in queue from an
// unclean previous shutdown (Requirement 1.10), then builds a ProjectInstance
// around queue, registers it in r.projects, and starts its Worker goroutine.
// Callers must hold r.mu for writing.
func (r *Registry) startInstanceLocked(project string, queue *Queue) (*ProjectInstance, error) {
	statePath := r.StatePath(project)
	recoverRunningTask(queue, r.notifier, statePath)

	logWriter, err := NewProjectLog(r.LogPath(project))
	if err != nil {
		return nil, fmt.Errorf("open log for project %q: %w", project, err)
	}

	worker := NewWorker(queue, ShellRunner{}, r.notifier, statePath, logWriter)

	inst := &ProjectInstance{
		Name:      project,
		Queue:     queue,
		Worker:    worker,
		LogWriter: logWriter,
		StateFile: statePath,
	}
	r.projects[project] = inst
	go worker.Run()

	return inst, nil
}

// RestoreAll scans stateDir for existing taskline-*.json files and starts a
// running ProjectInstance for each project found, recovering any task left
// "running" from an unclean previous shutdown along the way (FR-2.4,
// Requirement 1.10). It is intended to be called once at startup, before the
// HTTP server begins accepting requests. A project whose state file fails to
// load is logged and skipped rather than aborting the scan for every other
// project.
func (r *Registry) RestoreAll() error {
	if err := r.ensureDirs(); err != nil {
		return err
	}

	queues, err := ScanStateDir(r.stateDir, r.names)
	if err != nil {
		return fmt.Errorf("scan state dir: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for project, queue := range queues {
		if err := ValidateProjectName(project); err != nil {
			logWarn("skipping state file for invalid project name %q: %v", project, err)
			continue
		}
		if _, err := r.startInstanceLocked(project, queue); err != nil {
			logError("project %s: failed to restore: %v", project, err)
		}
	}
	return nil
}

// ProjectSummary is a lightweight snapshot of one project's queue, used to
// answer GET /projects (FR-4.3).
type ProjectSummary struct {
	Name         string
	State        QueueState
	PendingCount int
}

// List returns a summary of every known project, sorted by name.
func (r *Registry) List() []ProjectSummary {
	r.mu.RLock()
	defer r.mu.RUnlock()

	summaries := make([]ProjectSummary, 0, len(r.projects))
	for name, inst := range r.projects {
		summaries = append(summaries, ProjectSummary{
			Name:         name,
			State:        inst.Queue.CurrentState(),
			PendingCount: inst.Queue.PendingCount(),
		})
	}
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].Name < summaries[j].Name })
	return summaries
}

// Shutdown stops every project's Worker, waiting up to gracePeriod for each
// one's running Task to finish on its own before escalating to ForceKill,
// then flushes and closes each project's LogWriter and persists its final
// state (NFR-3.1, NFR-3.2, NFR-3.3). Every project shuts down concurrently,
// so the total time Shutdown blocks is bounded by gracePeriod rather than
// growing with the number of projects (NFR-2.1).
func (r *Registry) Shutdown(gracePeriod time.Duration) {
	r.mu.RLock()
	instances := make([]*ProjectInstance, 0, len(r.projects))
	for _, inst := range r.projects {
		instances = append(instances, inst)
	}
	r.mu.RUnlock()

	var wg sync.WaitGroup
	for _, inst := range instances {
		wg.Add(1)
		go func(inst *ProjectInstance) {
			defer wg.Done()
			r.shutdownInstance(inst, gracePeriod)
		}(inst)
	}
	wg.Wait()
}

func (r *Registry) shutdownInstance(inst *ProjectInstance, gracePeriod time.Duration) {
	inst.Worker.Shutdown()

	if inst.Queue.CurrentTask() != nil {
		if waitForQueueIdle(inst.Queue, gracePeriod) {
			logInfo("project %s: running task finished before shutdown timeout", inst.Name)
		} else {
			logWarn("project %s: running task did not finish within %s; sending SIGKILL", inst.Name, gracePeriod)
			if _, err := inst.Worker.ForceKill(); err != nil && !errors.Is(err, ErrNoRunningTask) {
				logError("project %s: failed to force-kill running task: %v", inst.Name, err)
			}
		}
	}

	if err := inst.LogWriter.Close(); err != nil {
		logError("project %s: failed to close log file: %v", inst.Name, err)
	}

	if err := SaveState(inst.Queue, inst.StateFile); err != nil {
		logError("project %s: failed to persist state: %v", inst.Name, err)
	}
}

// waitForQueueIdle polls queue for up to timeout, returning true as soon as
// no Task is running. It returns false if a Task is still running once
// timeout elapses.
func waitForQueueIdle(queue *Queue, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if queue.CurrentTask() == nil {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return queue.CurrentTask() == nil
}

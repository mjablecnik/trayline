package store

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

// WorkflowStatus represents the lifecycle state of a scheduled pipeline execution.
type WorkflowStatus string

const (
	WorkflowQueued    WorkflowStatus = "queued"
	WorkflowRunning   WorkflowStatus = "running"
	WorkflowCompleted WorkflowStatus = "completed"
	WorkflowFailed    WorkflowStatus = "failed"
	WorkflowCancelled WorkflowStatus = "cancelled"
)

// IsWorkflowTerminal reports whether a workflow status is terminal (non-progressing).
func IsWorkflowTerminal(s WorkflowStatus) bool {
	return s == WorkflowCompleted || s == WorkflowFailed || s == WorkflowCancelled
}

// workflowStatusPriority returns the sort priority for a workflow status.
// Lower value = higher priority (appears first in the list).
func workflowStatusPriority(s WorkflowStatus) int {
	switch s {
	case WorkflowRunning:
		return 0
	case WorkflowQueued:
		return 1
	default:
		return 2
	}
}

// maxWorkflowsPerProject is the number of workflows retained per project;
// oldest terminal workflows are evicted beyond this cap.
const maxWorkflowsPerProject = 20

// Workflow represents a scheduled trayline pipeline execution.
type Workflow struct {
	ID          string            `json:"id"`
	Project     string            `json:"project"`
	Pipeline    string            `json:"pipeline"`
	Variables   map[string]string `json:"variables"`
	Status      WorkflowStatus    `json:"status"`
	CreatedAt   time.Time         `json:"created_at"`
	StartedAt   *time.Time        `json:"started_at,omitempty"`
	CompletedAt *time.Time        `json:"completed_at,omitempty"`
	Error       string            `json:"error,omitempty"`
	ExitCode    *int              `json:"exit_code,omitempty"`
	// NotBefore delays execution — NextQueued skips this workflow until
	// time.Now() is past this timestamp. Used for rate-limit backoff.
	NotBefore   *time.Time         `json:"not_before,omitempty"`
	ContainerID string             `json:"-"`
	CancelFunc  context.CancelFunc `json:"-"`
	LogBuffer   *RingBuffer        `json:"-"`
	LogSubs     []chan string      `json:"-"` // subscribers for live log streaming
	// CancelRequested marks that a user-initiated cancel is in progress for a
	// running workflow, so the queue processor records the eventual terminal
	// status as "cancelled" instead of "failed" once the killed container's
	// output stream closes.
	CancelRequested bool `json:"-"`
}

// WorkflowStore is a thread-safe in-memory store for workflows, keyed by ID
// and indexed per project in creation order.
type WorkflowStore struct {
	mu        sync.RWMutex
	workflows map[string]*Workflow
	byProject map[string][]string // project -> ordered workflow IDs (creation order)
}

// NewWorkflowStore creates an empty WorkflowStore.
func NewWorkflowStore() *WorkflowStore {
	return &WorkflowStore{
		workflows: make(map[string]*Workflow),
		byProject: make(map[string][]string),
	}
}

// Add inserts a workflow into the store.
func (s *WorkflowStore) Add(w *Workflow) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workflows[w.ID] = w
	s.byProject[w.Project] = append(s.byProject[w.Project], w.ID)
}

// Get returns a workflow by ID, or nil if not found.
func (s *WorkflowStore) Get(id string) *Workflow {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.workflows[id]
}

// Update applies fn to the workflow with the given ID while holding a write
// lock. Returns false if the workflow was not found.
func (s *WorkflowStore) Update(id string, fn func(*Workflow)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.workflows[id]
	if !ok {
		return false
	}
	fn(w)
	return true
}

// Remove deletes a workflow from the store and its project index.
func (s *WorkflowStore) Remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.workflows[id]
	if !ok {
		return
	}
	delete(s.workflows, id)
	ids := s.byProject[w.Project]
	for i, wid := range ids {
		if wid == id {
			s.byProject[w.Project] = append(ids[:i], ids[i+1:]...)
			break
		}
	}
}

// Snapshot returns a copy of the workflow with the given ID, safe to read
// without racing the queue processor's concurrent field mutations (unlike
// dereferencing a Get() pointer directly — see .agents/MEMORY.md). Returns
// ok=false if no workflow with that ID exists. Supports prefix matching:
// if no exact match is found, searches for a unique workflow whose ID starts
// with the given prefix.
func (s *WorkflowStore) Snapshot(id string) (Workflow, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Exact match first.
	if w, ok := s.workflows[id]; ok {
		return *w, true
	}
	// Prefix match: find unique workflow whose ID starts with the given prefix.
	var match *Workflow
	for wid, w := range s.workflows {
		if strings.HasPrefix(wid, id) {
			if match != nil {
				// Ambiguous prefix — multiple matches.
				return Workflow{}, false
			}
			match = w
		}
	}
	if match != nil {
		return *match, true
	}
	return Workflow{}, false
}

// ListByProjectSnapshot returns copies of the same set ListByProject would
// return (up to the 20 most recent workflows for a project), safe to read
// without racing concurrent mutation. Sorted by execution order: running first,
// then queued (oldest first = next to execute), then terminal (newest first).
func (s *WorkflowStore) ListByProjectSnapshot(project string) []Workflow {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := s.byProject[project]
	all := make([]Workflow, 0, len(ids))
	for _, id := range ids {
		if w, ok := s.workflows[id]; ok {
			all = append(all, *w)
		}
	}
	sort.Slice(all, func(i, j int) bool {
		pi, pj := workflowStatusPriority(all[i].Status), workflowStatusPriority(all[j].Status)
		if pi != pj {
			return pi < pj
		}
		// Running and queued: oldest first (execution order).
		// Terminal: newest first (most recent completions on top).
		if !IsWorkflowTerminal(all[i].Status) {
			return all[i].CreatedAt.Before(all[j].CreatedAt)
		}
		return all[i].CreatedAt.After(all[j].CreatedAt)
	})
	if len(all) > maxWorkflowsPerProject {
		all = all[:maxWorkflowsPerProject]
	}
	return all
}

// ListByProject returns up to the 20 most recent workflows for a project,
// sorted by execution order: running first, then queued (oldest first), then
// terminal (newest first).
func (s *WorkflowStore) ListByProject(project string) []*Workflow {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := s.byProject[project]
	all := make([]*Workflow, 0, len(ids))
	for _, id := range ids {
		if w, ok := s.workflows[id]; ok {
			all = append(all, w)
		}
	}
	sort.Slice(all, func(i, j int) bool {
		pi, pj := workflowStatusPriority(all[i].Status), workflowStatusPriority(all[j].Status)
		if pi != pj {
			return pi < pj
		}
		if !IsWorkflowTerminal(all[i].Status) {
			return all[i].CreatedAt.Before(all[j].CreatedAt)
		}
		return all[i].CreatedAt.After(all[j].CreatedAt)
	})
	if len(all) > maxWorkflowsPerProject {
		all = all[:maxWorkflowsPerProject]
	}
	return all
}

// NextQueued returns the oldest queued workflow for a project (by creation
// order) that is eligible to run now, or nil if none is queued. Workflows
// with a NotBefore timestamp in the future are skipped (rate-limit backoff).
func (s *WorkflowStore) NextQueued(project string) *Workflow {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	for _, id := range s.byProject[project] {
		if w, ok := s.workflows[id]; ok && w.Status == WorkflowQueued {
			if w.NotBefore != nil && now.Before(*w.NotBefore) {
				continue
			}
			return w
		}
	}
	return nil
}

// HasRunning reports whether a project currently has a running workflow.
func (s *WorkflowStore) HasRunning(project string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, id := range s.byProject[project] {
		if w, ok := s.workflows[id]; ok && w.Status == WorkflowRunning {
			return true
		}
	}
	return false
}

// HasQueuedWaiting reports whether a project has queued workflows that are
// not yet eligible (NotBefore is in the future). Used to keep the processor
// loop alive during rate-limit backoff.
func (s *WorkflowStore) HasQueuedWaiting(project string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	for _, id := range s.byProject[project] {
		if w, ok := s.workflows[id]; ok && w.Status == WorkflowQueued {
			if w.NotBefore != nil && now.Before(*w.NotBefore) {
				return true
			}
		}
	}
	return false
}

// HasFailed reports whether a project has at least one failed workflow.
// When true, the processor loop halts and does not pick up the next queued
// workflow until the failed one is resolved (deleted or retried via a new
// schedule request).
func (s *WorkflowStore) HasFailed(project string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, id := range s.byProject[project] {
		if w, ok := s.workflows[id]; ok && w.Status == WorkflowFailed {
			return true
		}
	}
	return false
}

// All returns all workflows (unordered). Used for state persistence.
func (s *WorkflowStore) All() []*Workflow {
	s.mu.RLock()
	defer s.mu.RUnlock()
	all := make([]*Workflow, 0, len(s.workflows))
	for _, w := range s.workflows {
		all = append(all, w)
	}
	return all
}

// ListActiveSnapshot returns copies of all non-terminal workflows across all
// projects (queued or running), sorted by execution order: running first, then
// queued oldest-first (next to execute at the top).
func (s *WorkflowStore) ListActiveSnapshot() []Workflow {
	s.mu.RLock()
	defer s.mu.RUnlock()

	all := make([]Workflow, 0)
	for _, w := range s.workflows {
		if !IsWorkflowTerminal(w.Status) {
			all = append(all, *w)
		}
	}
	sort.Slice(all, func(i, j int) bool {
		pi, pj := workflowStatusPriority(all[i].Status), workflowStatusPriority(all[j].Status)
		if pi != pj {
			return pi < pj
		}
		// Within same status: oldest first (execution order).
		return all[i].CreatedAt.Before(all[j].CreatedAt)
	})
	return all
}

// Evict removes the oldest terminal workflows for a project beyond the
// maxWorkflowsPerProject cap, keeping only the most recent 20.
func (s *WorkflowStore) Evict(project string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ids := s.byProject[project]
	if len(ids) <= maxWorkflowsPerProject {
		return
	}

	// ids are in creation order (oldest first); walk from the oldest and
	// remove terminal workflows until the project is back at the cap.
	kept := make([]string, 0, len(ids))
	toRemove := len(ids) - maxWorkflowsPerProject
	for _, id := range ids {
		if toRemove > 0 {
			if w, ok := s.workflows[id]; ok && IsWorkflowTerminal(w.Status) {
				delete(s.workflows, id)
				toRemove--
				continue
			}
		}
		kept = append(kept, id)
	}
	s.byProject[project] = kept
}

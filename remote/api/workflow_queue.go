package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/pkg/stdcopy"

	"remote/core"
	"remote/docker"
	"remote/store"
)

// idlePollInterval bounds how long processLoop waits between HasRunning
// re-checks when a project unexpectedly still shows a running workflow
// (e.g. a brief window during status transition). It is not the common
// path — the common path is driven by Enqueue's notify signal.
const idlePollInterval = time.Second

// maxCapturedStderr bounds how much stderr output is retained in memory to
// build a workflow's Error message on non-zero exit; the full combined
// stdout+stderr stream is still written to the workflow's RingBuffer (capped
// separately at store.WorkflowLogBufferSize).
const maxCapturedStderr = 4096

// cancelGraceTimeout bounds how long CancelRunning waits after sending
// SIGTERM before escalating to SIGKILL (Requirement 6.6).
const cancelGraceTimeout = 10 * time.Second

// cancelPollInterval is how often CancelRunning's escalation goroutine polls
// container state while waiting out cancelGraceTimeout.
const cancelPollInterval = 200 * time.Millisecond

// errWorkflowNotRunning is returned by CancelRunning when the workflow is not
// currently in the "running" status (e.g. it already finished, or a
// concurrent cancel/completion raced ahead of this call).
var errWorkflowNotRunning = errors.New("workflow is not running")

// WorkflowQueueManager enforces sequential workflow execution per project:
// at most one workflow runs at a time for a given project, processed in
// creation order. One goroutine ("processor") runs per project with a
// queued or running workflow; it exits once its project's queue is empty
// and is respawned on the next Enqueue call.
type WorkflowQueueManager struct {
	mu     sync.Mutex
	active map[string]bool          // project -> has a running processor goroutine
	notify map[string]chan struct{} // project -> signal channel for that processor

	store    *store.WorkflowStore
	cm       *docker.ContainerManager
	config   *core.Config
	logger   *core.Logger
	stateMgr *store.WorkflowStateManager
}

// NewWorkflowQueueManager creates a WorkflowQueueManager.
func NewWorkflowQueueManager(workflowStore *store.WorkflowStore, cm *docker.ContainerManager, config *core.Config, logger *core.Logger, stateMgr *store.WorkflowStateManager) *WorkflowQueueManager {
	return &WorkflowQueueManager{
		active:   make(map[string]bool),
		notify:   make(map[string]chan struct{}),
		store:    workflowStore,
		cm:       cm,
		config:   config,
		logger:   logger,
		stateMgr: stateMgr,
	}
}

// Enqueue signals that project has a workflow ready to process (newly
// scheduled, or resumed after a restart), starting a processor goroutine for
// it if one isn't already running.
func (q *WorkflowQueueManager) Enqueue(project string) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if ch, ok := q.notify[project]; ok {
		select {
		case ch <- struct{}{}:
		default:
		}
	}

	if q.active[project] {
		return
	}
	q.active[project] = true
	q.notify[project] = make(chan struct{}, 1)
	go q.processLoop(project)
}

// processLoop is the per-project processor goroutine. It repeatedly runs the
// oldest queued workflow to completion until the project's queue is empty,
// then exits. Exit is synchronized with Enqueue via q.mu so a workflow
// scheduled concurrently with the exit check is never lost.
func (q *WorkflowQueueManager) processLoop(project string) {
	for {
		if q.store.HasRunning(project) {
			q.waitForNotify(project)
			continue
		}

		wf := q.store.NextQueued(project)
		if wf != nil {
			q.runWorkflow(wf)
			continue
		}

		// No immediately eligible workflow. Check if any are waiting for
		// their NotBefore backoff to expire (rate-limited re-queued).
		if q.store.HasQueuedWaiting(project) {
			q.waitForNotify(project)
			continue
		}

		q.mu.Lock()
		if q.store.NextQueued(project) != nil {
			// A workflow was enqueued between the check above and
			// acquiring the lock; keep processing instead of exiting.
			q.mu.Unlock()
			continue
		}
		if q.store.HasQueuedWaiting(project) {
			q.mu.Unlock()
			q.waitForNotify(project)
			continue
		}
		delete(q.active, project)
		delete(q.notify, project)
		q.mu.Unlock()
		return
	}
}

// waitForNotify blocks until project's processor is signalled or a short
// poll interval elapses, whichever comes first.
func (q *WorkflowQueueManager) waitForNotify(project string) {
	q.mu.Lock()
	ch := q.notify[project]
	q.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case <-ch:
	case <-time.After(idlePollInterval):
	}
}

// runWorkflow creates a container, runs the workflow's pipeline command to
// completion (or timeout), streams its output, and records the final status.
func (q *WorkflowQueueManager) runWorkflow(wf *store.Workflow) {
	ctx, cancel := context.WithTimeout(context.Background(), q.config.WorkflowTimeout)
	defer cancel()

	startedAt := time.Now()
	logBuf := store.NewRingBuffer(store.WorkflowLogBufferSize)
	q.store.Update(wf.ID, func(w *store.Workflow) {
		w.Status = store.WorkflowRunning
		w.StartedAt = &startedAt
		w.LogBuffer = logBuf
		w.CancelFunc = cancel
	})
	q.persist()

	cmd := buildWorkflowCmd(wf.Pipeline, wf.Variables)

	containerID, err := q.cm.StartWorkflowContainer(ctx, wf.Project, cmd)
	if err != nil {
		q.finishWorkflow(wf, store.WorkflowFailed, fmt.Sprintf("failed to create container: %v", err), nil)
		return
	}
	q.store.Update(wf.ID, func(w *store.Workflow) { w.ContainerID = containerID })

	attached, err := q.cm.AttachWorkflowContainer(ctx, containerID)
	if err != nil {
		_ = q.cm.StopAndRemoveContainer(context.Background(), containerID)
		q.finishWorkflow(wf, store.WorkflowFailed, fmt.Sprintf("failed to attach to container: %v", err), nil)
		return
	}

	if err := q.cm.StartContainer(ctx, containerID); err != nil {
		attached.Close()
		_ = q.cm.StopAndRemoveContainer(context.Background(), containerID)
		q.finishWorkflow(wf, store.WorkflowFailed, fmt.Sprintf("failed to start container: %v", err), nil)
		return
	}

	outW := &workflowOutputWriter{q: q, id: wf.ID}
	var stderrCap bytes.Buffer
	stderrW := io.MultiWriter(outW, &limitedCapture{buf: &stderrCap, limit: maxCapturedStderr})

	copyDone := make(chan struct{})
	go func() {
		defer close(copyDone)
		_, _ = stdcopy.StdCopy(outW, stderrW, attached.Reader)
	}()

	select {
	case <-copyDone:
	case <-ctx.Done():
		_ = q.cm.KillContainer(context.Background(), containerID, "SIGKILL")
		<-copyDone
	}
	timedOut := ctx.Err() == context.DeadlineExceeded
	attached.Close()

	exitCode := 0
	if info, inspectErr := q.cm.InspectContainer(context.Background(), containerID); inspectErr == nil && info.State != nil {
		exitCode = info.State.ExitCode
	}
	_ = q.cm.StopAndRemoveContainer(context.Background(), containerID)

	var cancelRequested bool
	q.store.Update(wf.ID, func(w *store.Workflow) { cancelRequested = w.CancelRequested })

	switch {
	case cancelRequested:
		// A user-initiated DELETE (HandleCancel -> CancelRunning) requested
		// termination — report "cancelled" regardless of the exit code a
		// SIGTERM/SIGKILL produced, not "failed".
		q.finishWorkflow(wf, store.WorkflowCancelled, "", &exitCode)
	case timedOut:
		q.finishWorkflow(wf, store.WorkflowFailed, fmt.Sprintf("workflow timed out after %s", q.config.WorkflowTimeout), &exitCode)
	case exitCode == 2:
		// Exit code 2 from `trayline run` means rate limit — the orchestrator's
		// internal retries were exhausted. Re-queue the workflow with a backoff
		// delay so it will be retried once the rate limit window resets.
		q.requeueRateLimited(wf)
	case exitCode != 0:
		errMsg := strings.TrimSpace(stderrCap.String())
		if errMsg == "" {
			errMsg = fmt.Sprintf("workflow exited with code %d", exitCode)
		}
		q.finishWorkflow(wf, store.WorkflowFailed, errMsg, &exitCode)
	default:
		q.finishWorkflow(wf, store.WorkflowCompleted, "", &exitCode)
	}
}

// finishWorkflow records a workflow's terminal status, evicts old terminal
// workflows beyond the per-project cap, and persists state. It is a no-op if
// the workflow is already in a terminal status, so it is safe to call from
// multiple goroutines racing to finalize the same workflow.
func (q *WorkflowQueueManager) finishWorkflow(wf *store.Workflow, status store.WorkflowStatus, errMsg string, exitCode *int) {
	completedAt := time.Now()
	q.store.Update(wf.ID, func(w *store.Workflow) {
		if store.IsWorkflowTerminal(w.Status) {
			return
		}
		w.Status = status
		w.Error = errMsg
		w.ExitCode = exitCode
		w.CompletedAt = &completedAt
		w.CancelFunc = nil
	})
	q.store.Evict(wf.Project)
	q.persist()
}

// rateLimitBackoff is how long a rate-limited workflow waits before being
// eligible for execution again.
const rateLimitBackoff = 30 * time.Minute

// requeueRateLimited resets a running workflow back to queued with a NotBefore
// delay. This handles the case where the orchestrator's internal rate-limit
// retries were exhausted (exit code 2) — instead of marking the workflow as
// failed, it gets another chance after the backoff period.
func (q *WorkflowQueueManager) requeueRateLimited(wf *store.Workflow) {
	notBefore := time.Now().Add(rateLimitBackoff)
	q.store.Update(wf.ID, func(w *store.Workflow) {
		w.Status = store.WorkflowQueued
		w.StartedAt = nil
		w.NotBefore = &notBefore
		w.Error = fmt.Sprintf("rate limited, retrying after %s", notBefore.Local().Format("15:04"))
		w.ExitCode = nil
		w.ContainerID = ""
		w.CancelFunc = nil
		w.LogBuffer = nil
		w.LogSubs = nil
	})
	q.persist()
}

// CancelRunning begins cancelling a currently-running workflow: it marks the
// workflow so its eventual terminal status is recorded as "cancelled" (see
// the cancelRequested check in runWorkflow above), sends SIGTERM to its
// container, and spawns a goroutine that escalates to SIGKILL after
// cancelGraceTimeout if the container hasn't exited by then. It returns
// immediately without waiting for the container to actually stop — the
// workflow's processor goroutine (already blocked in runWorkflow) observes
// the container's output stream closing and finalizes the status via the
// normal completion path. Returns errWorkflowNotRunning if the workflow is
// not currently in the "running" status.
func (q *WorkflowQueueManager) CancelRunning(id string) error {
	var containerID string
	q.store.Update(id, func(w *store.Workflow) {
		if w.Status != store.WorkflowRunning || w.ContainerID == "" {
			return
		}
		w.CancelRequested = true
		containerID = w.ContainerID
	})
	if containerID == "" {
		return errWorkflowNotRunning
	}

	if err := q.cm.KillContainer(context.Background(), containerID, "SIGTERM"); err != nil && q.logger != nil {
		q.logger.Warn(context.Background(), fmt.Sprintf("workflow %s: SIGTERM failed: %v", id, err))
	}
	go q.escalateToSigkill(containerID)
	return nil
}

// escalateToSigkill polls containerID's state and sends SIGKILL if it is
// still running after cancelGraceTimeout has elapsed since this goroutine was
// started (i.e. since CancelRunning sent SIGTERM).
func (q *WorkflowQueueManager) escalateToSigkill(containerID string) {
	deadline := time.Now().Add(cancelGraceTimeout)
	for time.Now().Before(deadline) {
		time.Sleep(cancelPollInterval)
		info, err := q.cm.InspectContainer(context.Background(), containerID)
		if err == nil && info.State != nil && !info.State.Running {
			return
		}
	}
	_ = q.cm.KillContainer(context.Background(), containerID, "SIGKILL")
}

// persist saves the current workflow state, tolerating a nil stateMgr (e.g. in tests).
func (q *WorkflowQueueManager) persist() {
	if q.stateMgr == nil {
		return
	}
	_ = q.stateMgr.Save()
}

// workflowOutputWriter appends container output to a workflow's ring buffer
// and broadcasts it to any live log-stream subscribers.
type workflowOutputWriter struct {
	q  *WorkflowQueueManager
	id string
}

func (w *workflowOutputWriter) Write(p []byte) (int, error) {
	if wf := w.q.store.Get(w.id); wf != nil && wf.LogBuffer != nil {
		_, _ = wf.LogBuffer.Write(p)
	}

	chunk := string(p)

	// Collect subscribers under lock, send outside to avoid holding the
	// store lock during potentially blocking channel sends.
	var subs []chan string
	w.q.store.Update(w.id, func(wf *store.Workflow) {
		if len(wf.LogSubs) > 0 {
			subs = make([]chan string, len(wf.LogSubs))
			copy(subs, wf.LogSubs)
		}
	})
	for _, sub := range subs {
		select {
		case sub <- chunk:
		case <-time.After(100 * time.Millisecond):
			// Subscriber cannot keep up — drop this chunk rather than
			// blocking the container's output pipe indefinitely.
		}
	}
	return len(p), nil
}

// limitedCapture writes into buf until limit bytes have been written, then
// silently discards further writes (used to bound in-memory stderr capture).
type limitedCapture struct {
	buf   *bytes.Buffer
	limit int
}

func (c *limitedCapture) Write(p []byte) (int, error) {
	remaining := c.limit - c.buf.Len()
	if remaining <= 0 {
		return len(p), nil
	}
	if len(p) > remaining {
		p = p[:remaining]
	}
	c.buf.Write(p)
	return len(p), nil
}

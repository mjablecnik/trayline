package api

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/pkg/stdcopy"

	"remote/core"
	"remote/docker"
	"remote/store"
)

// fakeQueueResult scripts a fake container's behavior for a given pipeline reference.
type fakeQueueResult struct {
	exitCode int
	stdout   string
	stderr   string
}

// fakeQueueContainerClient implements docker.ContainerClient, scripting container
// behavior by pipeline reference (parsed from the container's Cmd), so tests can
// assert on creation/start order and control when each container's output completes.
type fakeQueueContainerClient struct {
	noopContainerClient

	mu         sync.Mutex
	nextID     int
	containers map[string]string // containerID -> pipeline ref
	results    map[string]fakeQueueResult
	gates      map[string]chan struct{} // pipeline ref -> optional release gate
	created    []string                 // pipeline refs in creation order
	started    []string                 // pipeline refs in start order
	stopped    []string
	killed     []string
}

func newFakeQueueContainerClient() *fakeQueueContainerClient {
	return &fakeQueueContainerClient{
		containers: make(map[string]string),
		results:    make(map[string]fakeQueueResult),
		gates:      make(map[string]chan struct{}),
	}
}

func (f *fakeQueueContainerClient) ContainerCreate(_ context.Context, cfg *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ string) (container.CreateResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	id := fmt.Sprintf("fake-container-%d", f.nextID)
	pipeline := ""
	if len(cfg.Cmd) >= 3 {
		pipeline = cfg.Cmd[2]
	}
	f.containers[id] = pipeline
	f.created = append(f.created, pipeline)
	return container.CreateResponse{ID: id}, nil
}

func (f *fakeQueueContainerClient) ContainerStart(_ context.Context, containerID string, _ dockertypes.ContainerStartOptions) error {
	f.mu.Lock()
	f.started = append(f.started, f.containers[containerID])
	f.mu.Unlock()
	return nil
}

func (f *fakeQueueContainerClient) ContainerAttach(_ context.Context, containerID string, _ dockertypes.ContainerAttachOptions) (dockertypes.HijackedResponse, error) {
	f.mu.Lock()
	pipeline := f.containers[containerID]
	result := f.results[pipeline]
	gate := f.gates[pipeline]
	f.mu.Unlock()

	serverConn, clientConn := net.Pipe()
	go func() {
		if gate != nil {
			<-gate
		}
		_, _ = stdcopy.NewStdWriter(serverConn, stdcopy.Stdout).Write([]byte(result.stdout))
		_, _ = stdcopy.NewStdWriter(serverConn, stdcopy.Stderr).Write([]byte(result.stderr))
		serverConn.Close()
	}()
	return dockertypes.NewHijackedResponse(clientConn, ""), nil
}

func (f *fakeQueueContainerClient) ContainerInspect(_ context.Context, containerID string) (dockertypes.ContainerJSON, error) {
	f.mu.Lock()
	pipeline := f.containers[containerID]
	exitCode := f.results[pipeline].exitCode
	f.mu.Unlock()
	return dockertypes.ContainerJSON{
		ContainerJSONBase: &dockertypes.ContainerJSONBase{
			State: &dockertypes.ContainerState{ExitCode: exitCode},
		},
	}, nil
}

func (f *fakeQueueContainerClient) ContainerStop(_ context.Context, containerID string, _ container.StopOptions) error {
	f.mu.Lock()
	f.stopped = append(f.stopped, f.containers[containerID])
	f.mu.Unlock()
	return nil
}

func (f *fakeQueueContainerClient) ContainerKill(_ context.Context, containerID string, _ string) error {
	f.mu.Lock()
	f.killed = append(f.killed, f.containers[containerID])
	f.mu.Unlock()
	return nil
}

func newTestQueueManager(t *testing.T, client docker.ContainerClient) (*WorkflowQueueManager, *store.WorkflowStore) {
	t.Helper()
	cfg := &core.Config{
		ProjectsDir:     t.TempDir(),
		WorkflowTimeout: 10 * time.Second,
	}
	cm := docker.NewContainerManager(client, cfg, nil)
	s := store.NewWorkflowStore()
	q := NewWorkflowQueueManager(s, cm, cfg, nil, nil)
	return q, s
}

func addQueuedWorkflow(s *store.WorkflowStore, id, project, pipeline string, createdAt time.Time) *store.Workflow {
	w := &store.Workflow{
		ID:        id,
		Project:   project,
		Pipeline:  pipeline,
		Variables: map[string]string{},
		Status:    store.WorkflowQueued,
		CreatedAt: createdAt,
	}
	s.Add(w)
	return w
}

// workflowStatus reads a workflow's Status through WorkflowStore.Update's lock,
// since the queue manager mutates the same *Workflow concurrently via Update;
// reading the field directly off a Get() pointer while that's happening is a
// data race (Get only protects the store's map, not the struct's fields).
func workflowStatus(s *store.WorkflowStore, id string) store.WorkflowStatus {
	var status store.WorkflowStatus
	s.Update(id, func(w *store.Workflow) { status = w.Status })
	return status
}

// waitForTerminal polls until id reaches a terminal status (lock-protected via
// workflowStatus), then returns its final *Workflow. The final Get is safe to
// read directly since the queue manager never mutates a workflow again once
// it's terminal.
func waitForTerminal(t *testing.T, s *store.WorkflowStore, id string, timeout time.Duration) *store.Workflow {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if store.IsWorkflowTerminal(workflowStatus(s, id)) {
			return s.Get(id)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("workflow %q did not reach a terminal status within %s", id, timeout)
	return nil
}

func TestWorkflowQueueManager_CompletesSuccessfully(t *testing.T) {
	client := newFakeQueueContainerClient()
	client.results["processes/success"] = fakeQueueResult{exitCode: 0, stdout: "hello\n"}
	q, s := newTestQueueManager(t, client)

	addQueuedWorkflow(s, "w1", "proj", "processes/success", time.Now())
	q.Enqueue("proj")

	w := waitForTerminal(t, s, "w1", 2*time.Second)
	if w.Status != store.WorkflowCompleted {
		t.Fatalf("expected completed, got %q (error: %s)", w.Status, w.Error)
	}
	if w.ExitCode == nil || *w.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %v", w.ExitCode)
	}
	if w.LogBuffer == nil || w.LogBuffer.String() != "hello\n" {
		t.Fatalf("expected log buffer to contain %q, got %v", "hello\n", w.LogBuffer)
	}
}

func TestWorkflowQueueManager_FailsOnNonZeroExit(t *testing.T) {
	client := newFakeQueueContainerClient()
	client.results["processes/fail"] = fakeQueueResult{exitCode: 1, stderr: "boom\n"}
	q, s := newTestQueueManager(t, client)

	addQueuedWorkflow(s, "w1", "proj", "processes/fail", time.Now())
	q.Enqueue("proj")

	w := waitForTerminal(t, s, "w1", 2*time.Second)
	if w.Status != store.WorkflowFailed {
		t.Fatalf("expected failed, got %q", w.Status)
	}
	if w.ExitCode == nil || *w.ExitCode != 1 {
		t.Fatalf("expected exit code 1, got %v", w.ExitCode)
	}
	if w.Error != "boom" {
		t.Fatalf("expected error %q, got %q", "boom", w.Error)
	}
}

func TestWorkflowQueueManager_SequentialExecutionPerProject(t *testing.T) {
	client := newFakeQueueContainerClient()
	client.results["processes/first"] = fakeQueueResult{exitCode: 0}
	client.results["processes/second"] = fakeQueueResult{exitCode: 0}
	client.gates["processes/first"] = make(chan struct{})

	q, s := newTestQueueManager(t, client)
	base := time.Now()
	addQueuedWorkflow(s, "w1", "proj", "processes/first", base)
	addQueuedWorkflow(s, "w2", "proj", "processes/second", base.Add(time.Millisecond))
	q.Enqueue("proj")

	// Give the processor a moment to create/start the first container; the
	// second must not have started yet since "first" is gated open.
	time.Sleep(100 * time.Millisecond)
	client.mu.Lock()
	startedSoFar := append([]string(nil), client.started...)
	client.mu.Unlock()
	if len(startedSoFar) != 1 || startedSoFar[0] != "processes/first" {
		t.Fatalf("expected only 'first' started before it finishes, got %v", startedSoFar)
	}
	if status := workflowStatus(s, "w2"); status != store.WorkflowQueued {
		t.Fatalf("expected w2 to still be queued, got %q", status)
	}

	close(client.gates["processes/first"])

	waitForTerminal(t, s, "w1", 2*time.Second)
	waitForTerminal(t, s, "w2", 2*time.Second)

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.started) != 2 || client.started[0] != "processes/first" || client.started[1] != "processes/second" {
		t.Fatalf("expected sequential start order [first second], got %v", client.started)
	}
}

func TestWorkflowQueueManager_CrossProjectParallelism(t *testing.T) {
	client := newFakeQueueContainerClient()
	client.results["processes/a"] = fakeQueueResult{exitCode: 0}
	client.results["processes/b"] = fakeQueueResult{exitCode: 0}
	client.gates["processes/a"] = make(chan struct{})
	client.gates["processes/b"] = make(chan struct{})

	q, s := newTestQueueManager(t, client)
	addQueuedWorkflow(s, "wa", "proj-a", "processes/a", time.Now())
	addQueuedWorkflow(s, "wb", "proj-b", "processes/b", time.Now())
	q.Enqueue("proj-a")
	q.Enqueue("proj-b")

	// Both projects' containers should start concurrently, independent of
	// each other, since they belong to different projects.
	deadline := time.Now().Add(2 * time.Second)
	for {
		client.mu.Lock()
		started := len(client.started)
		client.mu.Unlock()
		if started == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected both projects' containers to start, got %d started", started)
		}
		time.Sleep(5 * time.Millisecond)
	}

	close(client.gates["processes/a"])
	close(client.gates["processes/b"])
	waitForTerminal(t, s, "wa", 2*time.Second)
	waitForTerminal(t, s, "wb", 2*time.Second)
}

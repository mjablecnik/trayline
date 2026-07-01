package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"pgregory.net/rapid"
)

// --- mock ContainerClient ---

type mockContainerClient struct {
	mu          sync.Mutex
	nextID      int
	containers  map[string]chan struct{} // containerID -> done channel (close to signal exit)
	startOrder  []string                 // ordered list of containerIDs in the order ContainerStart was called
	createErr   error                    // if set, ContainerCreate returns this error
	startErr    error                    // if set, ContainerStart returns this error
	// Extended fields for RunOneShot/waitAndCapture/limitWriter tests.
	autoComplete bool   // if true, close done channel immediately on ContainerCreate
	autoExitCode int    // exit code sent by ContainerWait when autoComplete is true
	logContent   []byte // content returned by ContainerLogs (nil → empty buffer)
	waitErr      error  // if set, ContainerWait goroutine sends this error instead of waiting
	stopCount    int32  // incremented on each ContainerStop call
	removeCount  int32  // incremented on each ContainerRemove call
	// Fields for InspectContainer and ContainerAttach control.
	inspectResult dockertypes.ContainerJSON // returned by ContainerInspect when inspectErr is nil
	inspectErr    error                     // if set, ContainerInspect returns this error
	attachErr     error                     // if set, ContainerAttach returns this error
}

func newMockContainerClient() *mockContainerClient {
	return &mockContainerClient{
		containers: make(map[string]chan struct{}),
	}
}

func (m *mockContainerClient) ContainerCreate(_ context.Context, _ *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ string) (container.CreateResponse, error) {
	if m.createErr != nil {
		return container.CreateResponse{}, m.createErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	id := fmt.Sprintf("mock-container-%d", m.nextID)
	doneCh := make(chan struct{})
	m.containers[id] = doneCh
	if m.autoComplete {
		close(doneCh)
	}
	return container.CreateResponse{ID: id}, nil
}

func (m *mockContainerClient) ContainerStart(_ context.Context, containerID string, _ dockertypes.ContainerStartOptions) error {
	if m.startErr != nil {
		return m.startErr
	}
	m.mu.Lock()
	m.startOrder = append(m.startOrder, containerID)
	m.mu.Unlock()
	return nil
}

func (m *mockContainerClient) ContainerWait(ctx context.Context, containerID string, _ container.WaitCondition) (<-chan container.WaitResponse, <-chan error) {
	respCh := make(chan container.WaitResponse, 1)
	errCh := make(chan error, 1)

	m.mu.Lock()
	done := m.containers[containerID]
	wErr := m.waitErr
	exitCode := m.autoExitCode
	m.mu.Unlock()

	go func() {
		if wErr != nil {
			errCh <- wErr
			return
		}
		if done == nil {
			errCh <- fmt.Errorf("unknown container %s", containerID)
			return
		}
		select {
		case <-done:
			respCh <- container.WaitResponse{StatusCode: int64(exitCode)}
		case <-ctx.Done():
			errCh <- ctx.Err()
		}
	}()
	return respCh, errCh
}

func (m *mockContainerClient) ContainerLogs(_ context.Context, _ string, _ dockertypes.ContainerLogsOptions) (io.ReadCloser, error) {
	m.mu.Lock()
	content := m.logContent
	m.mu.Unlock()
	if content != nil {
		return io.NopCloser(bytes.NewBuffer(content)), nil
	}
	return io.NopCloser(&bytes.Buffer{}), nil
}

func (m *mockContainerClient) ContainerAttach(_ context.Context, _ string, _ dockertypes.ContainerAttachOptions) (dockertypes.HijackedResponse, error) {
	if m.attachErr != nil {
		return dockertypes.HijackedResponse{}, m.attachErr
	}
	c1, c2 := net.Pipe()
	_ = c2
	return dockertypes.NewHijackedResponse(c1, ""), nil
}

func (m *mockContainerClient) ContainerStop(_ context.Context, _ string, _ container.StopOptions) error {
	atomic.AddInt32(&m.stopCount, 1)
	return nil
}

func (m *mockContainerClient) ContainerRemove(_ context.Context, _ string, _ dockertypes.ContainerRemoveOptions) error {
	atomic.AddInt32(&m.removeCount, 1)
	return nil
}

func (m *mockContainerClient) ContainerInspect(_ context.Context, _ string) (dockertypes.ContainerJSON, error) {
	if m.inspectErr != nil {
		return dockertypes.ContainerJSON{}, m.inspectErr
	}
	return m.inspectResult, nil
}

func (m *mockContainerClient) ContainerKill(_ context.Context, _ string, _ string) error {
	return nil
}

// completeContainer signals that a mock container has finished executing.
func (m *mockContainerClient) completeContainer(containerID string) {
	m.mu.Lock()
	done := m.containers[containerID]
	m.mu.Unlock()
	if done != nil {
		close(done)
	}
}

// StartOrderSnapshot returns a copy of the start order recorded so far.
func (m *mockContainerClient) StartOrderSnapshot() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]string, len(m.startOrder))
	copy(cp, m.startOrder)
	return cp
}

// --- helper ---

func indexStr(s []string, target string) int {
	for i, v := range s {
		if v == target {
			return i
		}
	}
	return -1
}

func containsStr(s []string, target string) bool {
	return indexStr(s, target) >= 0
}

// --- Property 5: Agent command construction ---
// Feature: agent-api-server, Property 5: Agent command construction

func TestPropertyAgentCommandConstruction(t *testing.T) {
	t.Run("one-shot kiro", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			prompt := rapid.StringN(1, 200, -1).Draw(t, "prompt")
			model := rapid.String().Draw(t, "model")
			system := rapid.String().Draw(t, "system")

			cmd := buildOneShotCmd("kiro", prompt, model, system)

			if len(cmd) == 0 {
				t.Fatal("expected non-empty command")
			}
			if cmd[0] != "kiro-cli" {
				t.Fatalf("expected kiro-cli binary, got %q", cmd[0])
			}
			if !containsStr(cmd, "--trust-all-tools") {
				t.Fatal("expected --trust-all-tools flag")
			}
			if !containsStr(cmd, "--no-interactive") {
				t.Fatal("expected --no-interactive flag")
			}
			if !containsStr(cmd, prompt) {
				t.Fatalf("prompt %q not found in command %v", prompt, cmd)
			}
			if model != "" {
				idx := indexStr(cmd, "--model")
				if idx == -1 || idx+1 >= len(cmd) || cmd[idx+1] != model {
					t.Fatalf("expected --model %q in command %v", model, cmd)
				}
			} else {
				if containsStr(cmd, "--model") {
					t.Fatal("--model flag present when model is empty")
				}
			}
			if system != "" {
				idx := indexStr(cmd, "--system-prompt")
				if idx == -1 || idx+1 >= len(cmd) || cmd[idx+1] != system {
					t.Fatalf("expected --system-prompt %q in command %v", system, cmd)
				}
			} else {
				if containsStr(cmd, "--system-prompt") {
					t.Fatal("--system-prompt flag present when system is empty")
				}
			}
		})
	})

	t.Run("one-shot claude", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			prompt := rapid.StringN(1, 200, -1).Draw(t, "prompt")
			model := rapid.String().Draw(t, "model")
			system := rapid.String().Draw(t, "system")

			cmd := buildOneShotCmd("claude", prompt, model, system)

			if len(cmd) == 0 {
				t.Fatal("expected non-empty command")
			}
			if cmd[0] != "claude" {
				t.Fatalf("expected claude binary, got %q", cmd[0])
			}
			if !containsStr(cmd, "--dangerously-skip-permissions") {
				t.Fatal("expected --dangerously-skip-permissions flag")
			}
			// prompt should immediately follow -p
			pIdx := indexStr(cmd, "-p")
			if pIdx == -1 || pIdx+1 >= len(cmd) || cmd[pIdx+1] != prompt {
				t.Fatalf("expected -p %q in command %v", prompt, cmd)
			}
			if model != "" {
				idx := indexStr(cmd, "--model")
				if idx == -1 || idx+1 >= len(cmd) || cmd[idx+1] != model {
					t.Fatalf("expected --model %q in command %v", model, cmd)
				}
			} else {
				if containsStr(cmd, "--model") {
					t.Fatal("--model flag present when model is empty")
				}
			}
			if system != "" {
				idx := indexStr(cmd, "--system-prompt")
				if idx == -1 || idx+1 >= len(cmd) || cmd[idx+1] != system {
					t.Fatalf("expected --system-prompt %q in command %v", system, cmd)
				}
			} else {
				if containsStr(cmd, "--system-prompt") {
					t.Fatal("--system-prompt flag present when system is empty")
				}
			}
		})
	})

	t.Run("chat kiro", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			model := rapid.String().Draw(t, "model")
			system := rapid.String().Draw(t, "system")

			cmd := buildChatCmd("kiro", model, system)

			if len(cmd) == 0 {
				t.Fatal("expected non-empty command")
			}
			if cmd[0] != "kiro-cli" {
				t.Fatalf("expected kiro-cli binary, got %q", cmd[0])
			}
			if !containsStr(cmd, "--trust-all-tools") {
				t.Fatal("expected --trust-all-tools flag")
			}
			// no --no-interactive for chat
			if containsStr(cmd, "--no-interactive") {
				t.Fatal("--no-interactive should not be in chat command")
			}
			if model != "" {
				idx := indexStr(cmd, "--model")
				if idx == -1 || idx+1 >= len(cmd) || cmd[idx+1] != model {
					t.Fatalf("expected --model %q in command %v", model, cmd)
				}
			}
			if system != "" {
				idx := indexStr(cmd, "--system-prompt")
				if idx == -1 || idx+1 >= len(cmd) || cmd[idx+1] != system {
					t.Fatalf("expected --system-prompt %q in command %v", system, cmd)
				}
			}
		})
	})

	t.Run("chat claude", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			model := rapid.String().Draw(t, "model")
			system := rapid.String().Draw(t, "system")

			cmd := buildChatCmd("claude", model, system)

			if len(cmd) == 0 {
				t.Fatal("expected non-empty command")
			}
			if cmd[0] != "claude" {
				t.Fatalf("expected claude binary, got %q", cmd[0])
			}
			if !containsStr(cmd, "--dangerously-skip-permissions") {
				t.Fatal("expected --dangerously-skip-permissions flag")
			}
			// chat mode has no -p flag
			if containsStr(cmd, "-p") {
				t.Fatal("-p should not be in chat command")
			}
			if model != "" {
				idx := indexStr(cmd, "--model")
				if idx == -1 || idx+1 >= len(cmd) || cmd[idx+1] != model {
					t.Fatalf("expected --model %q in command %v", model, cmd)
				}
			}
			if system != "" {
				idx := indexStr(cmd, "--system-prompt")
				if idx == -1 || idx+1 >= len(cmd) || cmd[idx+1] != system {
					t.Fatalf("expected --system-prompt %q in command %v", system, cmd)
				}
			}
		})
	})

	t.Run("unknown agent returns nil", func(t *testing.T) {
		if buildOneShotCmd("unknown", "prompt", "", "") != nil {
			t.Fatal("expected nil for unknown agent in one-shot")
		}
		if buildChatCmd("unknown", "", "") != nil {
			t.Fatal("expected nil for unknown agent in chat")
		}
	})
}

// --- Property 10: Concurrency semaphore enforcement ---
// Feature: agent-api-server, Property 10: Concurrency semaphore enforcement

func TestPropertyConcurrencySemaphore(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		maxSlots := rapid.IntRange(1, 4).Draw(t, "maxSlots")
		// Submit between maxSlots and 3×maxSlots tasks.
		n := rapid.IntRange(maxSlots, maxSlots*3).Draw(t, "n")

		cfg := &Config{MaxConcurrentTasks: maxSlots, TaskTimeout: 30 * time.Second}
		mgr := NewContainerManager(nil, cfg, nil)

		var (
			running     int32
			maxObserved int32
			obsmu       sync.Mutex
		)

		base := time.Now()
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			createdAt := base.Add(time.Duration(i) * time.Millisecond)
			go func(createdAt time.Time) {
				defer wg.Done()
				if err := mgr.acquireSlot(context.Background(), createdAt); err != nil {
					return
				}
				curr := atomic.AddInt32(&running, 1)
				obsmu.Lock()
				if curr > maxObserved {
					maxObserved = curr
				}
				obsmu.Unlock()

				// Hold the slot briefly to allow other goroutines to queue up.
				time.Sleep(time.Millisecond)

				atomic.AddInt32(&running, -1)
				mgr.releaseSlot()
			}(createdAt)
		}

		wg.Wait()

		if int(maxObserved) > maxSlots {
			t.Fatalf("observed %d concurrent tasks but limit is %d", maxObserved, maxSlots)
		}
		if int(maxObserved) == 0 && n > 0 {
			t.Fatal("no tasks ran")
		}
	})
}

// --- Property 11: FIFO dequeuing order ---
// Feature: agent-api-server, Property 11: FIFO dequeuing order

func TestPropertyFIFODequeuing(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 20).Draw(t, "n")

		// Generate n timestamps (offsets in milliseconds from a base time).
		// Offsets may repeat, producing equal timestamps.
		offsets := make([]int64, n)
		for i := 0; i < n; i++ {
			offsets[i] = rapid.Int64Range(0, 100).Draw(t, fmt.Sprintf("offset%d", i))
		}

		base := time.Unix(1_000_000, 0)
		timestamps := make([]time.Time, n)
		for i, off := range offsets {
			timestamps[i] = base.Add(time.Duration(off) * time.Millisecond)
		}

		// Build a ContainerManager with 0 initial slots so every task must queue.
		cfg := &Config{MaxConcurrentTasks: 1} // overridden below
		mgr := &ContainerManager{
			config:  cfg,
			pending: nil,
			slots:   0, // all tasks will queue immediately
		}

		// Insert all tasks into the pending queue (simulating concurrent submission
		// when no slot is available).
		items := make([]*pendingItem, n)
		for i, ts := range timestamps {
			items[i] = &pendingItem{
				createdAt: ts,
				ready:     make(chan struct{}),
			}
			mgr.mu.Lock()
			mgr.insertPendingSorted(items[i])
			mgr.mu.Unlock()
		}

		// Dispatch one item at a time and verify FIFO (oldest createdAt first).
		prevTS := time.Time{}
		for round := 0; round < n; round++ {
			// Grant one slot.
			mgr.mu.Lock()
			mgr.slots = 1
			mgr.tryDispatch()
			mgr.mu.Unlock()

			// Find which item was dispatched (its ready channel is now closed).
			dispatchedIdx := -1
			for j, item := range items {
				select {
				case <-item.ready:
					if item.dispatched {
						dispatchedIdx = j
					}
				default:
				}
			}

			if dispatchedIdx == -1 {
				t.Fatalf("round %d: no item was dispatched", round)
			}

			dispatchedTS := items[dispatchedIdx].createdAt

			// The dispatched item's timestamp must be ≥ the previous dispatched timestamp.
			if !prevTS.IsZero() && dispatchedTS.Before(prevTS) {
				t.Fatalf("FIFO violation: dispatched ts %v before previous ts %v",
					dispatchedTS, prevTS)
			}

			// The dispatched item's timestamp must be ≤ all remaining (not yet dispatched) items.
			for j, item := range items {
				if j == dispatchedIdx || item.dispatched || item.cancelled {
					continue
				}
				if item.createdAt.Before(dispatchedTS) {
					t.Fatalf("FIFO violation: dispatched item ts %v but remaining item %d has earlier ts %v",
						dispatchedTS, j, item.createdAt)
				}
			}

			prevTS = dispatchedTS

			// Return the slot so the next round can dispatch.
			mgr.mu.Lock()
			mgr.slots = 0
			mgr.mu.Unlock()
		}

		// All items should now be dispatched.
		for i, item := range items {
			if !item.dispatched {
				t.Fatalf("item %d was never dispatched", i)
			}
		}
	})
}

// --- limitWriter tests ---

func TestLimitWriter_BelowLimit(t *testing.T) {
	var buf bytes.Buffer
	lw := &limitWriter{w: &buf, limit: 10}
	n, err := lw.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 5 {
		t.Errorf("expected n=5, got %d", n)
	}
	if buf.String() != "hello" {
		t.Errorf("expected 'hello', got %q", buf.String())
	}
}

func TestLimitWriter_AtLimit(t *testing.T) {
	var buf bytes.Buffer
	lw := &limitWriter{w: &buf, limit: 5}
	n, err := lw.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 5 {
		t.Errorf("expected n=5, got %d", n)
	}
	if buf.String() != "hello" {
		t.Errorf("expected all 5 bytes written at limit, got %q", buf.String())
	}
}

func TestLimitWriter_PastLimit(t *testing.T) {
	var buf bytes.Buffer
	lw := &limitWriter{w: &buf, limit: 5}
	// Write 10 bytes; only the first 5 should pass through.
	n, err := lw.Write([]byte("0123456789"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Current behavior: returns len(truncated_p) == 5 (not the original 10).
	// The caller (stdcopy) ignores the return value (_, _), so this doesn't break anything.
	if n != 5 {
		t.Errorf("expected n=5 (bytes actually written after truncation), got %d", n)
	}
	if buf.String() != "01234" {
		t.Errorf("expected first 5 bytes %q, got %q", "01234", buf.String())
	}
}

func TestLimitWriter_SubsequentWriteIgnoredAfterLimit(t *testing.T) {
	var buf bytes.Buffer
	lw := &limitWriter{w: &buf, limit: 5}
	lw.Write([]byte("hello"))    // fills the limit
	n, err := lw.Write([]byte("world")) // should be silently discarded
	if err != nil {
		t.Fatalf("unexpected error on post-limit write: %v", err)
	}
	if n != 5 {
		t.Errorf("expected n=5 (len of discarded input), got %d", n)
	}
	if buf.String() != "hello" {
		t.Errorf("buffer should still be 'hello', got %q", buf.String())
	}
}

// --- RunOneShot tests ---

func newOneShotManager(t *testing.T, mock *mockContainerClient, maxSlots int) *ContainerManager {
	t.Helper()
	cfg := &Config{
		MaxConcurrentTasks: maxSlots,
		TaskTimeout:        5 * time.Second,
		WorkspaceHostDir:   t.TempDir(),
	}
	return NewContainerManager(mock, cfg, NewLogger(""))
}

func TestRunOneShot_HappyPathExitZero(t *testing.T) {
	mock := newMockContainerClient()
	mock.autoComplete = true
	mgr := newOneShotManager(t, mock, 1)

	result, err := mgr.RunOneShot(context.Background(), "claude", "hello", "", "", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
}

func TestRunOneShot_NonZeroExitPropagated(t *testing.T) {
	mock := newMockContainerClient()
	mock.autoComplete = true
	mock.autoExitCode = 2
	mgr := newOneShotManager(t, mock, 1)

	result, err := mgr.RunOneShot(context.Background(), "claude", "hello", "", "", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 2 {
		t.Errorf("expected exit code 2, got %d", result.ExitCode)
	}
}

func TestRunOneShot_ContainerCreateFails_SlotReleased(t *testing.T) {
	mock := newMockContainerClient()
	mock.createErr = fmt.Errorf("disk full")
	mgr := newOneShotManager(t, mock, 1)

	_, err := mgr.RunOneShot(context.Background(), "claude", "hello", "", "", time.Now())
	if err == nil {
		t.Fatal("expected error when ContainerCreate fails")
	}
	if mgr.AvailableSlots() != 1 {
		t.Errorf("expected slot released after create failure, got %d available", mgr.AvailableSlots())
	}
}

func TestRunOneShot_ContainerStartFails_SlotReleased(t *testing.T) {
	mock := newMockContainerClient()
	mock.startErr = fmt.Errorf("start failed")
	mgr := newOneShotManager(t, mock, 1)

	_, err := mgr.RunOneShot(context.Background(), "claude", "hello", "", "", time.Now())
	if err == nil {
		t.Fatal("expected error when ContainerStart fails")
	}
	if mgr.AvailableSlots() != 1 {
		t.Errorf("expected slot released after start failure, got %d available", mgr.AvailableSlots())
	}
}

func TestRunOneShot_SlotReleasedAfterSuccess(t *testing.T) {
	mock := newMockContainerClient()
	mock.autoComplete = true
	mgr := newOneShotManager(t, mock, 2)

	if mgr.AvailableSlots() != 2 {
		t.Fatalf("expected 2 slots initially")
	}
	_, err := mgr.RunOneShot(context.Background(), "claude", "hello", "", "", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mgr.AvailableSlots() != 2 {
		t.Errorf("expected both slots returned after success, got %d", mgr.AvailableSlots())
	}
}

func TestRunOneShot_TaskTimeout(t *testing.T) {
	mock := newMockContainerClient()
	// Container never completes — task timeout should fire.
	cfg := &Config{
		MaxConcurrentTasks: 1,
		TaskTimeout:        50 * time.Millisecond,
		WorkspaceHostDir:   t.TempDir(),
	}
	mgr := NewContainerManager(mock, cfg, NewLogger(""))

	_, err := mgr.RunOneShot(context.Background(), "claude", "hello", "", "", time.Now())
	if err == nil {
		t.Fatal("expected error when task times out")
	}
	if mgr.AvailableSlots() != 1 {
		t.Errorf("expected slot released after timeout, got %d", mgr.AvailableSlots())
	}
}

func TestRunOneShot_StopAndRemoveCalledOnSuccess(t *testing.T) {
	mock := newMockContainerClient()
	mock.autoComplete = true
	mgr := newOneShotManager(t, mock, 1)

	_, err := mgr.RunOneShot(context.Background(), "claude", "hello", "", "", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if atomic.LoadInt32(&mock.stopCount) == 0 {
		t.Error("expected ContainerStop to be called")
	}
	if atomic.LoadInt32(&mock.removeCount) == 0 {
		t.Error("expected ContainerRemove to be called")
	}
}

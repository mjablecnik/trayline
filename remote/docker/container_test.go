package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pgregory.net/rapid"

	"remote/core"
)

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

func newOneShotManager(t *testing.T, mock *MockContainerClient, maxSlots int) *ContainerManager {
	t.Helper()
	cfg := &core.Config{
		MaxConcurrentTasks: maxSlots,
		TaskTimeout:        5 * time.Second,
		WorkspaceHostDir:   t.TempDir(),
	}
	return NewContainerManager(mock, cfg, core.NewLogger(""))
}

// --- Property 5: Agent command construction ---

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
			// claude commands are wrapped in a shell seed step that copies
			// credentials from the read-only host mount into a private writable
			// location before exec'ing the real binary (avoids concurrent
			// containers racing on the same host ~/.claude.json).
			if cmd[0] != "sh" || cmd[1] != "-c" {
				t.Fatalf("expected shell-wrapped command, got %v", cmd)
			}
			if !containsStr(cmd, "claude") {
				t.Fatal("expected claude binary in wrapped command")
			}
			if !containsStr(cmd, "--dangerously-skip-permissions") {
				t.Fatal("expected --dangerously-skip-permissions flag")
			}
			pIdx := indexStr(cmd, "-p")
			if pIdx == -1 || pIdx+1 >= len(cmd) || cmd[pIdx+1] != prompt {
				t.Fatalf("expected -p %q in command %v", prompt, cmd)
			}
			if model != "" {
				idx := indexStr(cmd, "--model")
				if idx == -1 || idx+1 >= len(cmd) || cmd[idx+1] != model {
					t.Fatalf("expected --model %q in command %v", model, cmd)
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
			if cmd[0] != "sh" || cmd[1] != "-c" {
				t.Fatalf("expected shell-wrapped command, got %v", cmd)
			}
			if !containsStr(cmd, "claude") {
				t.Fatal("expected claude binary in wrapped command")
			}
			if !containsStr(cmd, "--output-format") {
				t.Fatal("expected --output-format flag")
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
			if containsStr(cmd, "--no-interactive") {
				t.Fatal("--no-interactive should not be in chat command")
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

// --- Feature: project-ai-agent, Property 1: Project bind mount is correctly scoped ---

func TestPropertyProjectContainerBindsScoping(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		projectsDir := "/projects"
		agent := rapid.SampledFrom([]string{"kiro", "claude", "unknown", ""}).Draw(t, "agent")
		projectName := rapid.StringMatching(`[a-zA-Z0-9_][a-zA-Z0-9_.-]{0,39}`).Draw(t, "projectName")

		cfg := &core.Config{
			ProjectsDir:          projectsDir,
			KiroHostDir:          "/host/.kiro",
			KiroCredsHostDir:     "/host/.local/share/kiro-cli",
			ClaudeHostDir:        "/host/.claude",
			ClaudeConfigHostFile: "/host/.claude.json",
		}
		m := NewContainerManager(&MockContainerClient{}, cfg, core.NewLogger(""))

		binds := m.BuildProjectContainerBinds(agent, projectName)

		if len(binds) == 0 {
			t.Fatal("expected at least one bind")
		}
		expected := filepath.Join(projectsDir, projectName) + ":" + workspaceMount
		if binds[0] != expected {
			t.Fatalf("expected first bind %q, got %q", expected, binds[0])
		}
	})
}

// --- Claude credential mounts are read-only, seeded into a private writable copy ---

func TestClaudeCredentialBindsAreReadOnly(t *testing.T) {
	cfg := &core.Config{
		ProjectsDir:          "/projects",
		WorkspaceHostDir:     "/workspace-host",
		ClaudeHostDir:        "/host/.claude",
		ClaudeConfigHostFile: "/host/.claude.json",
	}
	m := NewContainerManager(&MockContainerClient{}, cfg, core.NewLogger(""))

	for _, binds := range [][]string{
		m.buildContainerBinds("claude"),
		m.BuildProjectContainerBinds("claude", "myproject"),
	} {
		if !containsStr(binds, "/host/.claude:/home/agent/.claude-src:ro") {
			t.Fatalf("expected read-only .claude-src bind, got %v", binds)
		}
		if !containsStr(binds, "/host/.claude.json:/home/agent/.claude.json-src:ro") {
			t.Fatalf("expected read-only .claude.json-src bind, got %v", binds)
		}
		if containsStr(binds, "/host/.claude:/home/agent/.claude") {
			t.Fatalf("did not expect a directly writable .claude bind, got %v", binds)
		}
		if containsStr(binds, "/host/.claude.json:/home/agent/.claude.json") {
			t.Fatalf("did not expect a directly writable .claude.json bind, got %v", binds)
		}
	}
}

// --- Feature: 011-personal-assistant-agent, Property 1: Assistant container binds are correctly constructed ---

func TestPropertyAssistantContainerBindsScoping(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		assistantDataDir := rapid.StringMatching(`/[a-zA-Z0-9_/.-]{1,40}`).Draw(t, "assistantDataDir")
		projectsDir := rapid.StringMatching(`/[a-zA-Z0-9_/.-]{1,40}`).Draw(t, "projectsDir")
		agent := rapid.SampledFrom([]string{"kiro", "claude", "unknown", ""}).Draw(t, "agent")

		cfg := &core.Config{
			AssistantDataDir:     assistantDataDir,
			ProjectsDir:          projectsDir,
			KiroHostDir:          "/host/.kiro",
			KiroCredsHostDir:     "/host/.local/share/kiro-cli",
			ClaudeHostDir:        "/host/.claude",
			ClaudeConfigHostFile: "/host/.claude.json",
		}
		m := NewContainerManager(&MockContainerClient{}, cfg, core.NewLogger(""))

		binds := m.BuildAssistantContainerBinds(agent)

		if len(binds) < 2 {
			t.Fatalf("expected at least 2 binds, got %v", binds)
		}
		if binds[0] != assistantDataDir+":/workspace" {
			t.Fatalf("expected first bind %q, got %q", assistantDataDir+":/workspace", binds[0])
		}
		if binds[1] != projectsDir+":/projects" {
			t.Fatalf("expected second bind %q, got %q", projectsDir+":/projects", binds[1])
		}

		switch agent {
		case "kiro":
			if !containsStr(binds, "/host/.kiro:/home/agent/.kiro") {
				t.Fatalf("expected .kiro credential bind, got %v", binds)
			}
			if !containsStr(binds, "/host/.local/share/kiro-cli:/home/agent/.local/share/kiro-cli") {
				t.Fatalf("expected kiro-cli credential bind, got %v", binds)
			}
			if len(binds) != 4 {
				t.Fatalf("expected exactly 4 binds for kiro, got %v", binds)
			}
		case "claude":
			if !containsStr(binds, "/host/.claude:/home/agent/.claude-src:ro") {
				t.Fatalf("expected read-only .claude-src bind, got %v", binds)
			}
			if !containsStr(binds, "/host/.claude.json:/home/agent/.claude.json-src:ro") {
				t.Fatalf("expected read-only .claude.json-src bind, got %v", binds)
			}
			if len(binds) != 4 {
				t.Fatalf("expected exactly 4 binds for claude, got %v", binds)
			}
		default:
			if len(binds) != 2 {
				t.Fatalf("expected exactly 2 binds for unrecognized agent, got %v", binds)
			}
		}
	})
}

// --- Feature: 011-personal-assistant-agent, Property 2: Assistant container name follows prefix format ---

func TestPropertyAssistantContainerNamePrefixFormat(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		sessionID := rapid.StringMatching(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`).Draw(t, "sessionID")

		cfg := &core.Config{}
		// No container named "trayline-assistant-{first8}" exists.
		mock := &MockContainerClient{InspectErr: fmt.Errorf("no such container")}
		m := NewContainerManager(mock, cfg, core.NewLogger(""))

		name := m.resolveAssistantContainerName(context.Background(), sessionID)

		expected := "trayline-assistant-" + sessionID[:8]
		if name != expected {
			t.Fatalf("expected %q, got %q", expected, name)
		}
	})
}

// --- Feature: 011-personal-assistant-agent, Property 3: Container name conflict resolution appends numeric suffix ---

func TestPropertyAssistantContainerNameConflictResolution(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		sessionID := rapid.StringMatching(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`).Draw(t, "sessionID")
		// N containers with suffixes -2 through -(N+1) already exist, in addition
		// to the unsuffixed base name. N ranges 0..4 so at least one free suffix
		// remains within the 5-attempt (-2 through -6) budget.
		n := rapid.IntRange(0, 4).Draw(t, "n")

		base := "trayline-assistant-" + sessionID[:8]
		existing := map[string]bool{base: true}
		for i := 2; i <= n+1; i++ {
			existing[fmt.Sprintf("%s-%d", base, i)] = true
		}

		cfg := &core.Config{}
		mock := &MockContainerClient{ExistingContainerNames: existing}
		m := NewContainerManager(mock, cfg, core.NewLogger(""))

		name := m.resolveAssistantContainerName(context.Background(), sessionID)

		expected := fmt.Sprintf("%s-%d", base, n+2)
		if name != expected {
			t.Fatalf("expected %q, got %q (existing: %v)", expected, name, existing)
		}
	})
}

// --- Property 10: Concurrency semaphore enforcement ---

func TestPropertyConcurrencySemaphore(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		maxSlots := rapid.IntRange(1, 4).Draw(t, "maxSlots")
		n := rapid.IntRange(maxSlots, maxSlots*3).Draw(t, "n")

		cfg := &core.Config{MaxConcurrentTasks: maxSlots, TaskTimeout: 30 * time.Second}
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

func TestPropertyFIFODequeuing(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 20).Draw(t, "n")

		offsets := make([]int64, n)
		for i := 0; i < n; i++ {
			offsets[i] = rapid.Int64Range(0, 100).Draw(t, fmt.Sprintf("offset%d", i))
		}

		base := time.Unix(1_000_000, 0)
		timestamps := make([]time.Time, n)
		for i, off := range offsets {
			timestamps[i] = base.Add(time.Duration(off) * time.Millisecond)
		}

		mgr := &ContainerManager{
			config:    &core.Config{MaxConcurrentTasks: 1},
			pending:   nil,
			taskSlots: 0,
		}

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

		prevTS := time.Time{}
		for round := 0; round < n; round++ {
			mgr.mu.Lock()
			mgr.taskSlots = 1
			mgr.tryDispatch()
			mgr.mu.Unlock()

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

			if !prevTS.IsZero() && dispatchedTS.Before(prevTS) {
				t.Fatalf("FIFO violation: dispatched ts %v before previous ts %v", dispatchedTS, prevTS)
			}

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
			mgr.mu.Lock()
			mgr.taskSlots = 0
			mgr.mu.Unlock()
		}

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

func TestLimitWriter_PastLimit(t *testing.T) {
	var buf bytes.Buffer
	lw := &limitWriter{w: &buf, limit: 5}
	n, err := lw.Write([]byte("0123456789"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 5 {
		t.Errorf("expected n=5, got %d", n)
	}
	if buf.String() != "01234" {
		t.Errorf("expected first 5 bytes, got %q", buf.String())
	}
}

// --- RunOneShot tests ---

func TestRunOneShot_HappyPathExitZero(t *testing.T) {
	mock := NewMockContainerClient()
	mock.AutoComplete = true
	mgr := newOneShotManager(t, mock, 1)

	result, err := mgr.RunOneShot(context.Background(), "claude", "hello", "", "", time.Now(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
}

func TestRunOneShot_ContainerCreateFails_SlotReleased(t *testing.T) {
	mock := NewMockContainerClient()
	mock.CreateErr = fmt.Errorf("disk full")
	mgr := newOneShotManager(t, mock, 1)

	_, err := mgr.RunOneShot(context.Background(), "claude", "hello", "", "", time.Now(), nil)
	if err == nil {
		t.Fatal("expected error when ContainerCreate fails")
	}
	if mgr.AvailableSlots() != 1 {
		t.Errorf("expected slot released after create failure, got %d available", mgr.AvailableSlots())
	}
}

func TestRunOneShot_SlotReleasedAfterSuccess(t *testing.T) {
	mock := NewMockContainerClient()
	mock.AutoComplete = true
	mgr := newOneShotManager(t, mock, 2)

	_, err := mgr.RunOneShot(context.Background(), "claude", "hello", "", "", time.Now(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mgr.AvailableSlots() != 2 {
		t.Errorf("expected both slots returned after success, got %d", mgr.AvailableSlots())
	}
}

func TestRunOneShot_TaskTimeout(t *testing.T) {
	mock := NewMockContainerClient()
	cfg := &core.Config{
		MaxConcurrentTasks: 1,
		TaskTimeout:        50 * time.Millisecond,
		WorkspaceHostDir:   t.TempDir(),
	}
	mgr := NewContainerManager(mock, cfg, core.NewLogger(""))

	_, err := mgr.RunOneShot(context.Background(), "claude", "hello", "", "", time.Now(), nil)
	if err == nil {
		t.Fatal("expected error when task times out")
	}
	if mgr.AvailableSlots() != 1 {
		t.Errorf("expected slot released after timeout, got %d", mgr.AvailableSlots())
	}
}

func TestRunOneShot_StopAndRemoveCalledOnSuccess(t *testing.T) {
	mock := NewMockContainerClient()
	mock.AutoComplete = true
	mgr := newOneShotManager(t, mock, 1)

	_, err := mgr.RunOneShot(context.Background(), "claude", "hello", "", "", time.Now(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if atomic.LoadInt32(&mock.StopCount) == 0 {
		t.Error("expected ContainerStop to be called")
	}
	if atomic.LoadInt32(&mock.RemoveCount) == 0 {
		t.Error("expected ContainerRemove to be called")
	}
}

func TestRunOneShot_OnStartCalledWithContainerIDBeforeCleanup(t *testing.T) {
	mock := NewMockContainerClient()
	mock.AutoComplete = true
	mgr := newOneShotManager(t, mock, 1)

	var startedID string
	_, err := mgr.RunOneShot(context.Background(), "claude", "hello", "", "", time.Now(), func(containerID string) {
		startedID = containerID
		if atomic.LoadInt32(&mock.StopCount) != 0 || atomic.LoadInt32(&mock.RemoveCount) != 0 {
			t.Error("expected onStart to fire before the container is stopped/removed")
		}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if startedID == "" {
		t.Error("expected onStart to be called with a non-empty container ID")
	}
}

func TestCopyFileToContainer_WritesTarWithFileContent(t *testing.T) {
	mock := NewMockContainerClient()
	mgr := newOneShotManager(t, mock, 1)

	data := []byte("hello uploaded file")
	if err := mgr.CopyFileToContainer(context.Background(), "container-1", "/tmp/uploads", "notes.txt", data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	written, ok := mock.CopiedFiles["container-1:/tmp/uploads"]
	if !ok {
		t.Fatal("expected CopyToContainer to be called with the given containerID and dstPath")
	}

	tr := tar.NewReader(bytes.NewReader(written))
	hdr, err := tr.Next()
	if err != nil {
		t.Fatalf("failed to read tar entry: %v", err)
	}
	if hdr.Name != "notes.txt" {
		t.Errorf("expected tar entry name %q, got %q", "notes.txt", hdr.Name)
	}
	content, err := io.ReadAll(tr)
	if err != nil {
		t.Fatalf("failed to read tar content: %v", err)
	}
	if string(content) != string(data) {
		t.Errorf("expected tar content %q, got %q", data, content)
	}
}

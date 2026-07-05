package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"sort"
	"sync"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"

	"server/core"
)

const (
	SandboxImage   = "trayline-sandbox"
	sandboxNetwork = "trayline-net"
	dockerHostEnv  = "DOCKER_HOST=tcp://trayline-proxy:2375"
	workspaceMount = "/workspace"
	maxOutputBytes = 1 * 1024 * 1024
)

// ContainerClient abstracts Docker SDK operations for testability.
type ContainerClient interface {
	ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, containerName string) (container.CreateResponse, error)
	ContainerStart(ctx context.Context, containerID string, options dockertypes.ContainerStartOptions) error
	ContainerLogs(ctx context.Context, containerID string, options dockertypes.ContainerLogsOptions) (io.ReadCloser, error)
	ContainerAttach(ctx context.Context, containerID string, options dockertypes.ContainerAttachOptions) (dockertypes.HijackedResponse, error)
	ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error
	ContainerRemove(ctx context.Context, containerID string, options dockertypes.ContainerRemoveOptions) error
	ContainerInspect(ctx context.Context, containerID string) (dockertypes.ContainerJSON, error)
	ContainerWait(ctx context.Context, containerID string, condition container.WaitCondition) (<-chan container.WaitResponse, <-chan error)
	ContainerKill(ctx context.Context, containerID string, signal string) error
}

// dockerClientAdapter wraps the real Docker client to implement ContainerClient,
// hiding the ocispec.Platform parameter from ContainerCreate (always nil for our use case).
type dockerClientAdapter struct {
	cli *client.Client
}

func (a *dockerClientAdapter) ContainerCreate(ctx context.Context, cfg *container.Config, hostCfg *container.HostConfig, netCfg *network.NetworkingConfig, containerName string) (container.CreateResponse, error) {
	return a.cli.ContainerCreate(ctx, cfg, hostCfg, netCfg, nil, containerName)
}

func (a *dockerClientAdapter) ContainerStart(ctx context.Context, containerID string, options dockertypes.ContainerStartOptions) error {
	return a.cli.ContainerStart(ctx, containerID, options)
}

func (a *dockerClientAdapter) ContainerLogs(ctx context.Context, containerID string, options dockertypes.ContainerLogsOptions) (io.ReadCloser, error) {
	return a.cli.ContainerLogs(ctx, containerID, options)
}

func (a *dockerClientAdapter) ContainerAttach(ctx context.Context, containerID string, options dockertypes.ContainerAttachOptions) (dockertypes.HijackedResponse, error) {
	return a.cli.ContainerAttach(ctx, containerID, options)
}

func (a *dockerClientAdapter) ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error {
	return a.cli.ContainerStop(ctx, containerID, options)
}

func (a *dockerClientAdapter) ContainerRemove(ctx context.Context, containerID string, options dockertypes.ContainerRemoveOptions) error {
	return a.cli.ContainerRemove(ctx, containerID, options)
}

func (a *dockerClientAdapter) ContainerInspect(ctx context.Context, containerID string) (dockertypes.ContainerJSON, error) {
	return a.cli.ContainerInspect(ctx, containerID)
}

func (a *dockerClientAdapter) ContainerWait(ctx context.Context, containerID string, condition container.WaitCondition) (<-chan container.WaitResponse, <-chan error) {
	return a.cli.ContainerWait(ctx, containerID, condition)
}

func (a *dockerClientAdapter) ContainerKill(ctx context.Context, containerID string, signal string) error {
	return a.cli.ContainerKill(ctx, containerID, signal)
}

// NewDockerClient creates a Docker client adapter connected to the daemon via the environment.
func NewDockerClient() (ContainerClient, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}
	return &dockerClientAdapter{cli: cli}, nil
}

// pendingItem is a task waiting in the concurrency queue.
type pendingItem struct {
	createdAt  time.Time
	ready      chan struct{}
	cancelled  bool
	dispatched bool
}

// ContainerManager manages agent container lifecycle and enforces concurrency limits.
type ContainerManager struct {
	client ContainerClient
	config *core.Config
	logger *core.Logger

	mu      sync.Mutex
	slots   int            // available concurrency slots
	pending []*pendingItem // sorted by createdAt ascending (FIFO)
}

// NewContainerManager creates a ContainerManager with the given concurrency limit.
func NewContainerManager(client ContainerClient, config *core.Config, logger *core.Logger) *ContainerManager {
	return &ContainerManager{
		client: client,
		config: config,
		logger: logger,
		slots:  config.MaxConcurrentTasks,
	}
}

// acquireSlot blocks until a concurrency slot is available, respecting FIFO order by createdAt.
// Returns an error if ctx is cancelled before a slot is acquired.
func (m *ContainerManager) acquireSlot(ctx context.Context, createdAt time.Time) error {
	item := &pendingItem{
		createdAt: createdAt,
		ready:     make(chan struct{}),
	}

	m.mu.Lock()
	m.insertPendingSorted(item)
	m.tryDispatch()
	m.mu.Unlock()

	select {
	case <-item.ready:
		return nil
	case <-ctx.Done():
		m.mu.Lock()
		if item.dispatched {
			// Slot was granted while we were cancelling — return it.
			m.slots++
			m.tryDispatch()
			m.mu.Unlock()
		} else {
			item.cancelled = true
			m.mu.Unlock()
		}
		return ctx.Err()
	}
}

// releaseSlot returns a concurrency slot, possibly granting it to a waiting task.
func (m *ContainerManager) releaseSlot() {
	m.mu.Lock()
	m.slots++
	m.tryDispatch()
	m.mu.Unlock()
}

// tryDispatch grants available slots to the oldest non-cancelled waiting tasks.
// Must be called with m.mu held.
func (m *ContainerManager) tryDispatch() {
	for m.slots > 0 && len(m.pending) > 0 {
		item := m.pending[0]
		m.pending = m.pending[1:]
		if item.cancelled {
			continue
		}
		m.slots--
		item.dispatched = true
		close(item.ready)
	}
}

// insertPendingSorted inserts item into the pending list, maintaining ascending createdAt order.
// Must be called with m.mu held.
func (m *ContainerManager) insertPendingSorted(item *pendingItem) {
	idx := sort.Search(len(m.pending), func(i int) bool {
		return m.pending[i].createdAt.After(item.createdAt)
	})
	m.pending = append(m.pending, nil)
	copy(m.pending[idx+1:], m.pending[idx:])
	m.pending[idx] = item
}

// AvailableSlots returns the number of available concurrency slots.
func (m *ContainerManager) AvailableSlots() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.slots
}

// TryAcquireSlot non-blocking acquires one concurrency slot.
// Returns true if the slot was acquired; caller must call ReleaseChatSlot when done.
func (m *ContainerManager) TryAcquireSlot() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.slots > 0 {
		m.slots--
		return true
	}
	return false
}

// PendingCount returns the number of tasks waiting for a slot.
func (m *ContainerManager) PendingCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.pending)
}

// buildOneShotCmd constructs the CLI command for a one-shot task invocation.
func buildOneShotCmd(agent, prompt, model, system string) []string {
	switch agent {
	case "kiro":
		cmd := []string{"kiro-cli", "chat", "--trust-all-tools", "--no-interactive", "--wrap", "never", prompt}
		if model != "" {
			cmd = append(cmd, "--model", model)
		}
		if system != "" {
			cmd = append(cmd, "--system-prompt", system)
		}
		return cmd
	case "claude":
		cmd := []string{"claude", "--dangerously-skip-permissions", "-p", prompt}
		if model != "" {
			cmd = append(cmd, "--model", model)
		}
		if system != "" {
			cmd = append(cmd, "--system-prompt", system)
		}
		return cmd
	default:
		return nil
	}
}

// buildChatCmd constructs the CLI command for an interactive chat session.
func buildChatCmd(agent, model, system string) []string {
	switch agent {
	case "kiro":
		cmd := []string{"kiro-cli", "chat", "--trust-all-tools"}
		if model != "" {
			cmd = append(cmd, "--model", model)
		}
		if system != "" {
			cmd = append(cmd, "--system-prompt", system)
		}
		return cmd
	case "claude":
		cmd := []string{"claude", "--dangerously-skip-permissions"}
		if model != "" {
			cmd = append(cmd, "--model", model)
		}
		if system != "" {
			cmd = append(cmd, "--system-prompt", system)
		}
		return cmd
	default:
		return nil
	}
}

// ContainerResult holds the captured output of a completed container run.
type ContainerResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// RunOneShot acquires a slot, runs a one-shot container to completion, and returns the output.
// Enforces FIFO ordering, the configured task timeout, and the concurrency limit.
func (m *ContainerManager) RunOneShot(ctx context.Context, agent, prompt, model, system string, createdAt time.Time) (*ContainerResult, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, m.config.TaskTimeout)
	defer cancel()

	if err := m.acquireSlot(timeoutCtx, createdAt); err != nil {
		return nil, fmt.Errorf("task timed out waiting for a free slot: %w", err)
	}
	defer m.releaseSlot()

	cmd := buildOneShotCmd(agent, prompt, model, system)
	containerID, err := m.createAndStartContainer(timeoutCtx, agent, cmd, false)
	if err != nil {
		return nil, err
	}

	result, waitErr := m.waitAndCapture(timeoutCtx, containerID)

	// Always clean up the container, even on error.
	stopCtx := context.Background()
	stopTimeout := 10
	_ = m.client.ContainerStop(stopCtx, containerID, container.StopOptions{Timeout: &stopTimeout})
	_ = m.client.ContainerRemove(stopCtx, containerID, dockertypes.ContainerRemoveOptions{Force: true})

	if waitErr != nil {
		return nil, waitErr
	}
	return result, nil
}

// StartChatContainer starts a persistent interactive container.
// The caller must have pre-acquired a slot via TryAcquireSlot before calling this.
// The caller must also call ReleaseChatSlot and StopAndRemoveContainer when the session ends.
func (m *ContainerManager) StartChatContainer(ctx context.Context, agent, model, system string) (string, error) {
	cmd := buildChatCmd(agent, model, system)
	containerID, err := m.createAndStartContainer(ctx, agent, cmd, true)
	if err != nil {
		return "", err
	}
	return containerID, nil
}

// ReleaseChatSlot returns the concurrency slot held by a chat session.
func (m *ContainerManager) ReleaseChatSlot() {
	m.releaseSlot()
}

// AttachChatContainer attaches stdin/stdout/stderr to a running interactive container.
// The returned HijackedResponse must be closed by the caller.
func (m *ContainerManager) AttachChatContainer(ctx context.Context, containerID string) (dockertypes.HijackedResponse, error) {
	return m.client.ContainerAttach(ctx, containerID, dockertypes.ContainerAttachOptions{
		Stream: true,
		Stdin:  true,
		Stdout: true,
		Stderr: true,
	})
}

// StopAndRemoveContainer stops (with 10s grace) and removes a container.
func (m *ContainerManager) StopAndRemoveContainer(ctx context.Context, containerID string) error {
	timeout := 10
	_ = m.client.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &timeout})
	return m.client.ContainerRemove(ctx, containerID, dockertypes.ContainerRemoveOptions{Force: true})
}

// InspectContainer returns information about a container (used for state recovery).
func (m *ContainerManager) InspectContainer(ctx context.Context, containerID string) (dockertypes.ContainerJSON, error) {
	return m.client.ContainerInspect(ctx, containerID)
}

// KillContainer sends a signal to a running container.
func (m *ContainerManager) KillContainer(ctx context.Context, containerID, signal string) error {
	return m.client.ContainerKill(ctx, containerID, signal)
}

// CaptureContainerOutput reads up to 1MB of stdout/stderr from a container that
// may already have exited. Used during startup recovery.
func (m *ContainerManager) CaptureContainerOutput(ctx context.Context, containerID string) (*ContainerResult, error) {
	info, err := m.client.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("cannot inspect container: %w", err)
	}

	if info.State != nil && info.State.Running {
		result, err := m.waitAndCapture(ctx, containerID)
		if err != nil {
			return nil, err
		}
		return result, nil
	}

	logReader, err := m.client.ContainerLogs(ctx, containerID, dockertypes.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get container logs: %w", err)
	}
	defer logReader.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	stdoutLW := &limitWriter{w: &stdoutBuf, limit: maxOutputBytes}
	stderrLW := &limitWriter{w: &stderrBuf, limit: maxOutputBytes}
	_, _ = stdcopy.StdCopy(stdoutLW, stderrLW, logReader)

	exitCode := 0
	if info.State != nil {
		exitCode = info.State.ExitCode
	}

	return &ContainerResult{
		Stdout:   stdoutBuf.String(),
		Stderr:   stderrBuf.String(),
		ExitCode: exitCode,
	}, nil
}

// buildContainerEnv constructs the environment variable list for agent containers.
func (m *ContainerManager) buildContainerEnv() []string {
	return []string{dockerHostEnv, "NO_COLOR=1"}
}

// buildContainerBinds constructs the volume bind list for agent containers.
func (m *ContainerManager) buildContainerBinds(agent string) []string {
	const agentHome = "/home/agent"
	binds := []string{m.config.WorkspaceHostDir + ":" + workspaceMount}

	switch agent {
	case "kiro":
		if m.config.KiroHostDir != "" {
			binds = append(binds, m.config.KiroHostDir+":"+agentHome+"/.kiro")
		}
		if m.config.KiroCredsHostDir != "" {
			binds = append(binds, m.config.KiroCredsHostDir+":"+agentHome+"/.local/share/kiro-cli")
		}
	case "claude":
		if m.config.ClaudeHostDir != "" {
			binds = append(binds, m.config.ClaudeHostDir+":"+agentHome+"/.claude:ro")
		}
		if m.config.ClaudeConfigHostFile != "" {
			binds = append(binds, m.config.ClaudeConfigHostFile+":"+agentHome+"/.claude.json:ro")
		}
	}

	return binds
}

// createAndStartContainer creates and starts a container with the given command.
func (m *ContainerManager) createAndStartContainer(ctx context.Context, agent string, cmd []string, interactive bool) (string, error) {
	cfg := &container.Config{
		Image:       SandboxImage,
		Cmd:         cmd,
		Env:         m.buildContainerEnv(),
		Tty:         interactive,
		AttachStdin: interactive,
		OpenStdin:   interactive,
		StdinOnce:   interactive,
	}

	hostCfg := &container.HostConfig{
		Binds:      m.buildContainerBinds(agent),
		AutoRemove: false,
	}

	netCfg := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			sandboxNetwork: {},
		},
	}

	resp, err := m.client.ContainerCreate(ctx, cfg, hostCfg, netCfg, "")
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	if err := m.client.ContainerStart(ctx, resp.ID, dockertypes.ContainerStartOptions{}); err != nil {
		_ = m.client.ContainerRemove(context.Background(), resp.ID, dockertypes.ContainerRemoveOptions{Force: true})
		return "", fmt.Errorf("failed to start container: %w", err)
	}

	return resp.ID, nil
}

// waitAndCapture waits for a container to exit and captures up to 1MB of stdout/stderr.
func (m *ContainerManager) waitAndCapture(ctx context.Context, containerID string) (*ContainerResult, error) {
	waitCh, errCh := m.client.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)

	var waitResp container.WaitResponse
	select {
	case waitResp = <-waitCh:
	case err := <-errCh:
		return nil, fmt.Errorf("error waiting for container: %w", err)
	case <-ctx.Done():
		return nil, fmt.Errorf("task timed out: %w", ctx.Err())
	}

	logReader, err := m.client.ContainerLogs(context.Background(), containerID, dockertypes.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get container logs: %w", err)
	}
	defer logReader.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	stdoutLW := &limitWriter{w: &stdoutBuf, limit: maxOutputBytes}
	stderrLW := &limitWriter{w: &stderrBuf, limit: maxOutputBytes}

	_, _ = stdcopy.StdCopy(stdoutLW, stderrLW, logReader)

	return &ContainerResult{
		Stdout:   stdoutBuf.String(),
		Stderr:   stderrBuf.String(),
		ExitCode: int(waitResp.StatusCode),
	}, nil
}

// limitWriter wraps an io.Writer and silently truncates writes beyond the byte limit.
type limitWriter struct {
	w     io.Writer
	limit int
	n     int
}

func (lw *limitWriter) Write(p []byte) (int, error) {
	remaining := lw.limit - lw.n
	if remaining <= 0 {
		return len(p), nil
	}
	if len(p) > remaining {
		p = p[:remaining]
	}
	n, err := lw.w.Write(p)
	lw.n += n
	return len(p), err
}

// ansiRe matches ANSI/VT100 escape sequences (colours, cursor movement, etc.)
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b[^[]`)

// StripANSI removes all ANSI escape sequences from s.
func StripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

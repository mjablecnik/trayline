package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"

	"remote/core"
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
	CopyToContainer(ctx context.Context, containerID string, dstPath string, content io.Reader, options dockertypes.CopyToContainerOptions) error
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

func (a *dockerClientAdapter) CopyToContainer(ctx context.Context, containerID string, dstPath string, content io.Reader, options dockertypes.CopyToContainerOptions) error {
	return a.cli.CopyToContainer(ctx, containerID, dstPath, content, options)
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
//
// Task slots and chat slots are separate pools so that a backlog of one-shot
// tasks (e.g. an automated pipeline submitting many RunOneShot calls) can
// never starve out interactive chat sessions, and vice versa.
type ContainerManager struct {
	client ContainerClient
	config *core.Config
	logger *core.Logger

	mu        sync.Mutex
	taskSlots int            // available one-shot task concurrency slots
	pending   []*pendingItem // sorted by createdAt ascending (FIFO), task slots only
	chatSlots int            // available interactive chat session slots
}

// NewContainerManager creates a ContainerManager with the given concurrency limits.
func NewContainerManager(client ContainerClient, config *core.Config, logger *core.Logger) *ContainerManager {
	return &ContainerManager{
		client:    client,
		config:    config,
		logger:    logger,
		taskSlots: config.MaxConcurrentTasks,
		chatSlots: config.MaxChatSessions,
	}
}

// acquireSlot blocks until a task slot is available, respecting FIFO order by createdAt.
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
			m.taskSlots++
			m.tryDispatch()
			m.mu.Unlock()
		} else {
			item.cancelled = true
			m.mu.Unlock()
		}
		return ctx.Err()
	}
}

// releaseSlot returns a task slot, possibly granting it to a waiting task.
func (m *ContainerManager) releaseSlot() {
	m.mu.Lock()
	m.taskSlots++
	m.tryDispatch()
	m.mu.Unlock()
}

// tryDispatch grants available task slots to the oldest non-cancelled waiting tasks.
// Must be called with m.mu held.
func (m *ContainerManager) tryDispatch() {
	for m.taskSlots > 0 && len(m.pending) > 0 {
		item := m.pending[0]
		m.pending = m.pending[1:]
		if item.cancelled {
			continue
		}
		m.taskSlots--
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

// AvailableSlots returns the number of available one-shot task concurrency slots.
func (m *ContainerManager) AvailableSlots() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.taskSlots
}

// AvailableChatSlots returns the number of available interactive chat session slots.
func (m *ContainerManager) AvailableChatSlots() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.chatSlots
}

// TryAcquireSlot non-blocking acquires one chat session slot, from a pool
// reserved separately from one-shot task slots so a busy task pipeline can't
// starve interactive chat sessions of capacity.
// Returns true if the slot was acquired; caller must call ReleaseChatSlot when done.
func (m *ContainerManager) TryAcquireSlot() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.chatSlots > 0 {
		m.chatSlots--
		return true
	}
	return false
}

// PendingCount returns the number of one-shot tasks waiting for a slot.
func (m *ContainerManager) PendingCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.pending)
}

// claudeCredentialSeedScript copies the read-only-mounted Claude credential
// snapshot (~/.claude-src, ~/.claude.json-src) into HOME before exec'ing the
// real command. Every agent container mounts the same host
// ~/.claude and ~/.claude.json; the claude CLI rewrites those files on
// nearly every invocation, so mounting them read-write directly would let
// concurrent containers race on the same file and corrupt it. Seeding a
// private writable copy per container avoids that while still letting the
// CLI persist its own state for the lifetime of the container.
const claudeCredentialSeedScript = `
if [ -d "$HOME/.claude-src" ]; then mkdir -p "$HOME/.claude" && cp -a "$HOME/.claude-src/." "$HOME/.claude/"; fi
if [ -f "$HOME/.claude.json-src" ]; then cp "$HOME/.claude.json-src" "$HOME/.claude.json"; fi
exec "$@"
`

// wrapWithClaudeCredentialSeed prefixes cmd with the credential seed script.
// Arguments are passed through "$@" so prompt text is never interpolated
// into the shell script itself.
func wrapWithClaudeCredentialSeed(cmd []string) []string {
	return append([]string{"sh", "-c", claudeCredentialSeedScript, "sh"}, cmd...)
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
		return wrapWithClaudeCredentialSeed(cmd)
	default:
		return nil
	}
}

// buildChatCmd constructs the CLI command for an interactive chat session.
// Uses SDK streaming JSON mode for programmatic stdin/stdout communication.
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
		cmd := []string{"claude", "-p", "--dangerously-skip-permissions",
			"--output-format", "stream-json", "--input-format", "stream-json",
			"--verbose"}
		if model != "" {
			cmd = append(cmd, "--model", model)
		}
		if system != "" {
			cmd = append(cmd, "--append-system-prompt", system)
		}
		return wrapWithClaudeCredentialSeed(cmd)
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
// If onStart is non-nil, it is called with the container ID as soon as the container has
// started, so the caller can persist it (e.g. to recover/clean it up after a server restart).
func (m *ContainerManager) RunOneShot(ctx context.Context, agent, prompt, model, system string, createdAt time.Time, onStart func(containerID string)) (*ContainerResult, error) {
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
	if onStart != nil {
		onStart(containerID)
	}

	result, waitErr := m.waitAndCapture(timeoutCtx, containerID)

	// Always clean up the container, even on error.
	stopCtx := context.Background()
	stopTimeout := 10
	if err := m.client.ContainerStop(stopCtx, containerID, container.StopOptions{Timeout: &stopTimeout}); err != nil && m.logger != nil {
		m.logger.Warn(stopCtx, fmt.Sprintf("failed to stop one-shot container %s: %v", containerID, err))
	}
	if err := m.client.ContainerRemove(stopCtx, containerID, dockertypes.ContainerRemoveOptions{Force: true}); err != nil && m.logger != nil {
		m.logger.Warn(stopCtx, fmt.Sprintf("failed to remove one-shot container %s: %v", containerID, err))
	}

	if waitErr != nil {
		return nil, waitErr
	}
	return result, nil
}

// StartChatContainer creates a persistent interactive container but does NOT start it.
// The caller must attach (via AttachChatContainer) before starting (via StartContainer).
// The caller must have pre-acquired a slot via TryAcquireSlot before calling this.
// The caller must also call ReleaseChatSlot and StopAndRemoveContainer when the session ends.
func (m *ContainerManager) StartChatContainer(ctx context.Context, agent, model, system string) (string, error) {
	cmd := buildChatCmd(agent, model, system)
	// Claude uses NDJSON stream-json mode (no TTY needed).
	// Kiro uses interactive plain-text mode (needs TTY for output flushing).
	useTTY := agent != "claude"
	containerID, err := m.createContainer(ctx, agent, cmd, true, useTTY)
	if err != nil {
		return "", err
	}
	return containerID, nil
}

// StartContainer starts an already-created container.
func (m *ContainerManager) StartContainer(ctx context.Context, containerID string) error {
	return m.client.ContainerStart(ctx, containerID, dockertypes.ContainerStartOptions{})
}

// ReleaseChatSlot returns the chat session slot held by a chat session.
func (m *ContainerManager) ReleaseChatSlot() {
	m.mu.Lock()
	m.chatSlots++
	m.mu.Unlock()
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
		// Mounted read-only at a "-src" path; the container's entrypoint copies
		// these into a private writable location (see wrapWithClaudeCredentialSeed)
		// instead of every container writing back to the same host file.
		if m.config.ClaudeHostDir != "" {
			binds = append(binds, m.config.ClaudeHostDir+":"+agentHome+"/.claude-src:ro")
		}
		if m.config.ClaudeConfigHostFile != "" {
			binds = append(binds, m.config.ClaudeConfigHostFile+":"+agentHome+"/.claude.json-src:ro")
		}
	}

	return binds
}

// BuildProjectContainerBinds constructs volume binds for a project-scoped agent container.
// Mounts PROJECTS_DIR/{projectName} as /workspace instead of the full workspace.
func (m *ContainerManager) BuildProjectContainerBinds(agent, projectName string) []string {
	const agentHome = "/home/agent"
	projectHostPath := filepath.Join(m.config.ProjectsDir, projectName)
	binds := []string{projectHostPath + ":" + workspaceMount}

	switch agent {
	case "kiro":
		if m.config.KiroHostDir != "" {
			binds = append(binds, m.config.KiroHostDir+":"+agentHome+"/.kiro")
		}
		if m.config.KiroCredsHostDir != "" {
			binds = append(binds, m.config.KiroCredsHostDir+":"+agentHome+"/.local/share/kiro-cli")
		}
	case "claude":
		// Mounted read-only at a "-src" path; the container's entrypoint copies
		// these into a private writable location (see wrapWithClaudeCredentialSeed)
		// instead of every container writing back to the same host file.
		if m.config.ClaudeHostDir != "" {
			binds = append(binds, m.config.ClaudeHostDir+":"+agentHome+"/.claude-src:ro")
		}
		if m.config.ClaudeConfigHostFile != "" {
			binds = append(binds, m.config.ClaudeConfigHostFile+":"+agentHome+"/.claude.json-src:ro")
		}
	}

	return binds
}

// StartProjectChatContainer creates a persistent interactive container scoped to a single
// project directory (mounted at /workspace) but does NOT start it. The caller must attach
// (via AttachChatContainer) before starting (via StartContainer), and must have pre-acquired
// a slot via TryAcquireSlot before calling this. The caller must also call ReleaseChatSlot
// and StopAndRemoveContainer when the session ends.
func (m *ContainerManager) StartProjectChatContainer(ctx context.Context, agent, model, system, projectName string) (string, error) {
	cmd := buildChatCmd(agent, model, system)
	// Claude uses NDJSON stream-json mode (no TTY needed).
	// Kiro uses interactive plain-text mode (needs TTY for output flushing).
	useTTY := agent != "claude"

	cfg := &container.Config{
		Image:       SandboxImage,
		Cmd:         cmd,
		Env:         m.buildContainerEnv(),
		Tty:         useTTY,
		AttachStdin: true,
		OpenStdin:   true,
		StdinOnce:   false,
		WorkingDir:  workspaceMount,
	}

	hostCfg := &container.HostConfig{
		Binds:      m.BuildProjectContainerBinds(agent, projectName),
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
	return resp.ID, nil
}

// CopyFileToContainer writes a single file into a running container's filesystem at dstDir.
func (m *ContainerManager) CopyFileToContainer(ctx context.Context, containerID, dstDir, filename string, data []byte) error {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{
		Name: filename,
		Mode: 0644,
		Size: int64(len(data)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if _, err := tw.Write(data); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}

	return m.client.CopyToContainer(ctx, containerID, dstDir, &buf, dockertypes.CopyToContainerOptions{})
}

// createAndStartContainer creates and starts a container with the given command.
func (m *ContainerManager) createAndStartContainer(ctx context.Context, agent string, cmd []string, interactive bool) (string, error) {
	containerID, err := m.createContainer(ctx, agent, cmd, interactive, interactive)
	if err != nil {
		return "", err
	}

	if err := m.client.ContainerStart(ctx, containerID, dockertypes.ContainerStartOptions{}); err != nil {
		_ = m.client.ContainerRemove(context.Background(), containerID, dockertypes.ContainerRemoveOptions{Force: true})
		return "", fmt.Errorf("failed to start container: %w", err)
	}

	return containerID, nil
}

// createContainer creates a container without starting it.
// interactive: whether to attach stdin. tty: whether to allocate a pseudo-TTY.
func (m *ContainerManager) createContainer(ctx context.Context, agent string, cmd []string, interactive bool, tty bool) (string, error) {
	cfg := &container.Config{
		Image:       SandboxImage,
		Cmd:         cmd,
		Env:         m.buildContainerEnv(),
		Tty:         tty,
		AttachStdin: interactive,
		OpenStdin:   interactive,
		StdinOnce:   false,
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

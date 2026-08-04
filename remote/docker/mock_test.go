package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
)

type MockContainerClient struct {
	mu            sync.Mutex
	nextID        int
	containers    map[string]chan struct{}
	StartOrder    []string
	CreateErr     error
	StartErr      error
	AutoComplete  bool
	AutoExitCode  int
	LogContent    []byte
	WaitErr       error
	StopCount     int32
	RemoveCount   int32
	InspectResult dockertypes.ContainerJSON
	InspectErr    error
	AttachErr     error
	CopyErr       error
	CopiedFiles   map[string][]byte // "containerID:dstPath" -> raw tar bytes written

	// ExistingContainerNames, when non-nil, makes ContainerInspect succeed only
	// for names present in the set and fail (not found) for all others —
	// letting tests simulate specific name conflicts. When nil, ContainerInspect
	// falls back to the name-agnostic InspectResult/InspectErr behavior above.
	ExistingContainerNames map[string]bool
}

func NewMockContainerClient() *MockContainerClient {
	return &MockContainerClient{
		containers: make(map[string]chan struct{}),
	}
}

func (m *MockContainerClient) ContainerCreate(_ context.Context, _ *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ string) (container.CreateResponse, error) {
	if m.CreateErr != nil {
		return container.CreateResponse{}, m.CreateErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	id := fmt.Sprintf("mock-container-%d", m.nextID)
	doneCh := make(chan struct{})
	m.containers[id] = doneCh
	if m.AutoComplete {
		close(doneCh)
	}
	return container.CreateResponse{ID: id}, nil
}

func (m *MockContainerClient) ContainerStart(_ context.Context, containerID string, _ dockertypes.ContainerStartOptions) error {
	if m.StartErr != nil {
		return m.StartErr
	}
	m.mu.Lock()
	m.StartOrder = append(m.StartOrder, containerID)
	m.mu.Unlock()
	return nil
}

func (m *MockContainerClient) ContainerWait(ctx context.Context, containerID string, _ container.WaitCondition) (<-chan container.WaitResponse, <-chan error) {
	respCh := make(chan container.WaitResponse, 1)
	errCh := make(chan error, 1)

	m.mu.Lock()
	done := m.containers[containerID]
	wErr := m.WaitErr
	exitCode := m.AutoExitCode
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

func (m *MockContainerClient) ContainerLogs(_ context.Context, _ string, _ dockertypes.ContainerLogsOptions) (io.ReadCloser, error) {
	m.mu.Lock()
	content := m.LogContent
	m.mu.Unlock()
	if content != nil {
		return io.NopCloser(bytes.NewBuffer(content)), nil
	}
	return io.NopCloser(&bytes.Buffer{}), nil
}

func (m *MockContainerClient) ContainerAttach(_ context.Context, _ string, _ dockertypes.ContainerAttachOptions) (dockertypes.HijackedResponse, error) {
	if m.AttachErr != nil {
		return dockertypes.HijackedResponse{}, m.AttachErr
	}
	c1, c2 := net.Pipe()
	_ = c2
	return dockertypes.NewHijackedResponse(c1, ""), nil
}

func (m *MockContainerClient) ContainerStop(_ context.Context, _ string, _ container.StopOptions) error {
	atomic.AddInt32(&m.StopCount, 1)
	return nil
}

func (m *MockContainerClient) ContainerRemove(_ context.Context, _ string, _ dockertypes.ContainerRemoveOptions) error {
	atomic.AddInt32(&m.RemoveCount, 1)
	return nil
}

func (m *MockContainerClient) ContainerInspect(_ context.Context, containerID string) (dockertypes.ContainerJSON, error) {
	if m.ExistingContainerNames != nil {
		if m.ExistingContainerNames[containerID] {
			return m.InspectResult, nil
		}
		return dockertypes.ContainerJSON{}, fmt.Errorf("no such container: %s", containerID)
	}
	if m.InspectErr != nil {
		return dockertypes.ContainerJSON{}, m.InspectErr
	}
	return m.InspectResult, nil
}

func (m *MockContainerClient) ContainerKill(_ context.Context, _ string, _ string) error {
	return nil
}

func (m *MockContainerClient) CopyToContainer(_ context.Context, containerID string, dstPath string, content io.Reader, _ dockertypes.CopyToContainerOptions) error {
	if m.CopyErr != nil {
		return m.CopyErr
	}
	data, err := io.ReadAll(content)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if m.CopiedFiles == nil {
		m.CopiedFiles = make(map[string][]byte)
	}
	m.CopiedFiles[containerID+":"+dstPath] = data
	m.mu.Unlock()
	return nil
}

func (m *MockContainerClient) CompleteContainer(containerID string) {
	m.mu.Lock()
	done := m.containers[containerID]
	m.mu.Unlock()
	if done != nil {
		close(done)
	}
}

func (m *MockContainerClient) StartOrderSnapshot() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]string, len(m.StartOrder))
	copy(cp, m.StartOrder)
	return cp
}

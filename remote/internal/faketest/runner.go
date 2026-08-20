// Package faketest provides scripted test doubles for the agent execution
// layer. It exists so the OpenAI-compatible API can be exercised end to end —
// over real HTTP, through the real router and middleware — without Docker,
// agent credentials, or the cost and non-determinism of a real model call.
//
// It is deliberately under internal/ and is only imported by
// cmd/fake-openai-server. Nothing in the production server path depends on it.
package faketest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"

	"remote/docker"
)

// Behaviour markers. A request whose composed prompt contains one of these
// drives the corresponding scripted behaviour, which lets a client-side test
// suite provoke server states (timeouts, crashes, empty output) that a real
// agent cannot be asked for reliably.
const (
	MarkerEcho  = "__echo__"  // return the composed (system, prompt) as JSON
	MarkerEmpty = "__empty__" // produce no output
	MarkerFail  = "__fail__"  // exit non-zero
	MarkerError = "__error__" // fail before the container starts
	MarkerHang  = "__hang__"  // block until the context is cancelled
	MarkerCrash = "__crash__" // stream two chunks, then die mid-stream
	MarkerANSI  = "__ansi__"  // emit ANSI escape sequences
	MarkerSlow  = "__slow__"  // stream five chunks with a delay between them
	MarkerBig   = "__big__"   // emit a large payload
	MarkerUTF8  = "__utf8__"  // emit multi-byte text
)

// DefaultOutput is what the runner returns for any prompt without a marker.
const DefaultOutput = "Hello from the fake agent"

// EchoPayload is the JSON returned for MarkerEcho. A conformance test can use
// it to assert how the messages array was composed into (system, prompt)
// without needing access to server internals.
type EchoPayload struct {
	Agent  string `json:"agent"`
	Model  string `json:"model"`
	System string `json:"system"`
	Prompt string `json:"prompt"`
}

// Runner is a scripted docker-free implementation of the container runner
// interface used by the API handlers.
//
// It models the real ContainerManager's task-slot pool, including its blocking
// FIFO acquisition. Without that, capacity behaviour (Req 7) could not be
// exercised through the fake server at all — the handler's fast-path 429 check
// asks the runner how many slots are free.
type Runner struct {
	// ChunkDelay is the pause between streamed chunks. Non-zero values let a
	// client observe incremental delivery rather than one coalesced burst.
	ChunkDelay time.Duration

	slots chan struct{}
}

// NewRunner creates a Runner with maxConcurrent task slots.
func NewRunner(maxConcurrent int, chunkDelay time.Duration) *Runner {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	return &Runner{ChunkDelay: chunkDelay, slots: make(chan struct{}, maxConcurrent)}
}

// AvailableSlots reports free task capacity. The OpenAI handler uses this to
// reject at capacity instead of queuing (Req 7.2).
func (r *Runner) AvailableSlots() int {
	if r.slots == nil {
		return 1
	}
	return cap(r.slots) - len(r.slots)
}

// acquire takes a task slot, blocking (like the real manager) until one frees
// up or the context is cancelled.
func (r *Runner) acquire(ctx context.Context) error {
	if r.slots == nil {
		return nil
	}
	select {
	case r.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("task timed out waiting for a free slot: %w", ctx.Err())
	}
}

func (r *Runner) release() {
	if r.slots == nil {
		return
	}
	select {
	case <-r.slots:
	default:
	}
}

// RunOneShot implements the blocking, buffered execution path.
func (r *Runner) RunOneShot(ctx context.Context, agent, prompt, model, system string, _ time.Time, onStart func(string)) (*docker.ContainerResult, error) {
	if err := r.acquire(ctx); err != nil {
		return nil, err
	}
	defer r.release()

	if onStart != nil {
		onStart("fake-container")
	}

	switch {
	case strings.Contains(prompt, MarkerError):
		return nil, fmt.Errorf("failed to create container: scripted failure")
	case strings.Contains(prompt, MarkerHang):
		<-ctx.Done()
		return nil, ctx.Err()
	case strings.Contains(prompt, MarkerFail):
		return &docker.ContainerResult{Stderr: "scripted agent failure", ExitCode: 1}, nil
	case strings.Contains(prompt, MarkerEmpty):
		return &docker.ContainerResult{Stdout: "", ExitCode: 0}, nil
	case strings.Contains(prompt, MarkerANSI):
		return &docker.ContainerResult{Stdout: "\x1b[32mgreen text\x1b[0m", ExitCode: 0}, nil
	case strings.Contains(prompt, MarkerBig):
		return &docker.ContainerResult{Stdout: strings.Repeat("lorem ipsum dolor sit amet ", 40000), ExitCode: 0}, nil
	case strings.Contains(prompt, MarkerUTF8):
		return &docker.ContainerResult{Stdout: "Příliš žluťoučký kůň úpěl ďábelské ódy 🐴", ExitCode: 0}, nil
	case strings.Contains(prompt, MarkerEcho):
		payload, _ := json.Marshal(EchoPayload{Agent: agent, Model: model, System: system, Prompt: prompt})
		return &docker.ContainerResult{Stdout: string(payload), ExitCode: 0}, nil
	default:
		return &docker.ContainerResult{Stdout: DefaultOutput, ExitCode: 0}, nil
	}
}

// RunOneShotStreaming implements the incremental execution path, framing output
// the way the real agent would: NDJSON stream-json for claude, plain lines for
// kiro's TTY.
func (r *Runner) RunOneShotStreaming(ctx context.Context, agent, prompt, model, system string, _ time.Time) (*docker.OneShotStream, error) {
	if err := r.acquire(ctx); err != nil {
		return nil, err
	}
	if strings.Contains(prompt, MarkerError) {
		r.release()
		return nil, fmt.Errorf("failed to create container: scripted failure")
	}

	pr, pw := io.Pipe()

	frame := func(text string) string {
		if agent != "claude" {
			return text
		}
		b, _ := json.Marshal(map[string]any{
			"type": "assistant",
			"message": map[string]any{
				"content": []map[string]any{{"type": "text", "text": text}},
			},
		})
		return string(b)
	}

	go func() {
		defer pw.Close()

		write := func(line string) bool {
			select {
			case <-ctx.Done():
				return false
			default:
			}
			if _, err := io.WriteString(pw, line+"\n"); err != nil {
				return false
			}
			if r.ChunkDelay > 0 {
				select {
				case <-ctx.Done():
					return false
				case <-time.After(r.ChunkDelay):
				}
			}
			return true
		}

		switch {
		case strings.Contains(prompt, MarkerHang):
			<-ctx.Done()
		case strings.Contains(prompt, MarkerCrash):
			write(frame("partial one"))
			write(frame("partial two"))
			pw.CloseWithError(fmt.Errorf("container exited unexpectedly"))
		case strings.Contains(prompt, MarkerEmpty):
			// Nothing to emit.
		case strings.Contains(prompt, MarkerANSI):
			write("\x1b[32mgreen\x1b[0m")
			write("\x1b[1mbold\x1b[0m")
		case strings.Contains(prompt, MarkerUTF8):
			write(frame("Příliš žluťoučký kůň "))
			write(frame("úpěl ďábelské ódy 🐴"))
		case strings.Contains(prompt, MarkerSlow):
			for i := 1; i <= 5; i++ {
				if !write(frame(fmt.Sprintf("chunk %d", i))) {
					return
				}
			}
		case strings.Contains(prompt, MarkerEcho):
			payload, _ := json.Marshal(EchoPayload{Agent: agent, Model: model, System: system, Prompt: prompt})
			write(frame(string(payload)))
		default:
			// The streamed answer must reassemble to exactly what the
			// non-streaming path returns, so clients can rely on the two modes
			// agreeing. kiro's reader is line-based (the handler appends a
			// newline per line), so its default answer is emitted as one line.
			if agent != "claude" {
				write(DefaultOutput)
				return
			}
			for _, part := range []string{"Hello ", "from the ", "fake agent"} {
				if !write(frame(part)) {
					return
				}
			}
		}
	}()

	return &docker.OneShotStream{
		ContainerID: "fake-container",
		Reader:      pr,
		Closer:      &slotReleasingCloser{runner: r, reader: pr},
	}, nil
}

// slotReleasingCloser returns the task slot when the handler closes the stream,
// mirroring OneShotStream.Close releasing the real manager's slot. Closing more
// than once must not release more than once.
type slotReleasingCloser struct {
	runner *Runner
	reader *io.PipeReader
	once   sync.Once
}

func (c *slotReleasingCloser) Close() error {
	err := c.reader.Close()
	c.once.Do(c.runner.release)
	return err
}

// StopAndRemoveContainer is a no-op: there is no container to clean up.
func (r *Runner) StopAndRemoveContainer(context.Context, string) error { return nil }

// NoopContainerClient satisfies docker.ContainerClient with zero-value
// responses. The fake server constructs a ContainerManager purely to hand to
// handlers it does not exercise (sessions, workflows), so none of these methods
// are expected to be called.
type NoopContainerClient struct{}

func (NoopContainerClient) ContainerCreate(context.Context, *container.Config, *container.HostConfig, *network.NetworkingConfig, string) (container.CreateResponse, error) {
	return container.CreateResponse{}, nil
}
func (NoopContainerClient) ContainerStart(context.Context, string, dockertypes.ContainerStartOptions) error {
	return nil
}
func (NoopContainerClient) ContainerLogs(context.Context, string, dockertypes.ContainerLogsOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}
func (NoopContainerClient) ContainerAttach(context.Context, string, dockertypes.ContainerAttachOptions) (dockertypes.HijackedResponse, error) {
	return dockertypes.HijackedResponse{}, nil
}
func (NoopContainerClient) ContainerStop(context.Context, string, container.StopOptions) error {
	return nil
}
func (NoopContainerClient) ContainerRemove(context.Context, string, dockertypes.ContainerRemoveOptions) error {
	return nil
}
func (NoopContainerClient) ContainerInspect(context.Context, string) (dockertypes.ContainerJSON, error) {
	return dockertypes.ContainerJSON{}, nil
}
func (NoopContainerClient) ContainerWait(context.Context, string, container.WaitCondition) (<-chan container.WaitResponse, <-chan error) {
	return make(chan container.WaitResponse), make(chan error)
}
func (NoopContainerClient) ContainerKill(context.Context, string, string) error { return nil }
func (NoopContainerClient) CopyToContainer(context.Context, string, string, io.Reader, dockertypes.CopyToContainerOptions) error {
	return nil
}

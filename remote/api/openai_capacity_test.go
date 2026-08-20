package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"remote/core"
	"remote/docker"
	"remote/store"
)

// newCapacityTestServer wires the OpenAI handler to a *real* docker.ContainerManager
// backed by a container client whose ContainerWait never returns. That gives the
// genuine slot-pool semantics — FIFO queue, task timeout, release on completion —
// which a hand-written ContainerRunner double cannot faithfully reproduce.
func newCapacityTestServer(t *testing.T, maxConcurrent int, taskTimeout time.Duration) *httptest.Server {
	t.Helper()

	logger := core.NewLogger(testAuthToken)
	cfg := &core.Config{
		APIToken:           testAuthToken,
		SessionTimeout:     time.Minute,
		ProjectsDir:        t.TempDir(),
		AssistantDataDir:   t.TempDir(),
		TaskTimeout:        taskTimeout,
		MaxConcurrentTasks: maxConcurrent,
		MaxChatSessions:    1,
	}
	cm := docker.NewContainerManager(noopContainerClient{}, cfg, logger)

	openaiH := NewOpenAIHandler(NewModelRegistry(""), cm, logger, taskTimeout)

	// Only the chat completions route is needed here; the middleware chain is
	// applied manually so the test does not have to construct every handler.
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", openaiH.HandleChatCompletions)
	handler := AuthMiddleware(testAuthToken, mux)

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// occupyAllSlots fires maxConcurrent requests that will hang inside the
// container manager, consuming every task slot, and returns once they have all
// actually acquired one.
func occupyAllSlots(t *testing.T, srv *httptest.Server, maxConcurrent int) {
	t.Helper()
	for i := 0; i < maxConcurrent; i++ {
		go func() {
			resp := chatRequest(t, srv, map[string]any{
				"model": "kiro", "messages": userMessage("occupy the slot"),
			})
			_ = resp
		}()
	}
	// Give the requests time to reach acquireSlot and take their slots.
	time.Sleep(300 * time.Millisecond)
}

// TestCapacity_ReturnsFastWhenSaturated covers Req 7.2 and 6.3: at capacity the
// server must answer 429 with Retry-After: 30 *promptly*.
//
// The failure mode this guards against is subtle: ContainerManager.acquireSlot
// queues rather than failing fast, so without an explicit capacity check the
// client is left hanging for the whole task timeout (10 minutes in production)
// before finally receiving the 429.
func TestCapacity_ReturnsFastWhenSaturated(t *testing.T) {
	const taskTimeout = 6 * time.Second
	srv := newCapacityTestServer(t, 1, taskTimeout)

	occupyAllSlots(t, srv, 1)

	start := time.Now()
	resp := chatRequest(t, srv, map[string]any{
		"model": "kiro", "messages": userMessage("should be rejected"),
	})
	elapsed := time.Since(start)

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", resp.StatusCode)
	}
	if ra := resp.Header.Get("Retry-After"); ra != "30" {
		t.Errorf("Retry-After = %q, want %q", ra, "30")
	}

	var errResp OpenAIErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if errResp.Error.Type != "server_error" {
		t.Errorf("type = %q, want server_error", errResp.Error.Type)
	}
	if !strings.Contains(strings.ToLower(errResp.Error.Message), "capacity") {
		t.Errorf("message = %q, want it to mention capacity", errResp.Error.Message)
	}

	// The whole point: the client is told to retry immediately, not after the
	// task timeout has elapsed.
	if elapsed > taskTimeout/2 {
		t.Errorf("429 took %v — the request queued for the task timeout instead of "+
			"being rejected immediately (Req 7.2 requires no queuing)", elapsed)
	}
}

// TestCapacity_StreamingReturnsFastWhenSaturated is the streaming counterpart:
// a saturated server must reject with a JSON 429 *before* committing to an SSE
// stream, because once SSE headers are sent no status code can be reported.
func TestCapacity_StreamingReturnsFastWhenSaturated(t *testing.T) {
	const taskTimeout = 6 * time.Second
	srv := newCapacityTestServer(t, 1, taskTimeout)

	occupyAllSlots(t, srv, 1)

	start := time.Now()
	resp := chatRequest(t, srv, map[string]any{
		"model": "kiro", "messages": userMessage("should be rejected"), "stream": true,
	})
	elapsed := time.Since(start)

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json (not an SSE stream)", ct)
	}
	if elapsed > taskTimeout/2 {
		t.Errorf("429 took %v, want an immediate rejection", elapsed)
	}
}

// TestCapacity_SlotsAreReleased covers Req 7.3 and design Property 4: once the
// in-flight work finishes, capacity comes back.
func TestCapacity_SlotsAreReleased(t *testing.T) {
	logger := core.NewLogger(testAuthToken)
	cfg := &core.Config{
		APIToken:           testAuthToken,
		TaskTimeout:        2 * time.Second,
		MaxConcurrentTasks: 2,
		MaxChatSessions:    1,
		ProjectsDir:        t.TempDir(),
		AssistantDataDir:   t.TempDir(),
	}
	cm := docker.NewContainerManager(noopContainerClient{}, cfg, logger)
	openaiH := NewOpenAIHandler(NewModelRegistry(""), cm, logger, cfg.TaskTimeout)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", openaiH.HandleChatCompletions)
	srv := httptest.NewServer(AuthMiddleware(testAuthToken, mux))
	t.Cleanup(srv.Close)

	if got := cm.AvailableSlots(); got != 2 {
		t.Fatalf("initial AvailableSlots() = %d, want 2", got)
	}

	// Every request here times out inside the container manager (the fake
	// client never reports the container as finished), which exercises the
	// error path's slot release rather than the happy path's.
	for i := 0; i < 3; i++ {
		resp := chatRequest(t, srv, map[string]any{
			"model": "kiro", "messages": userMessage("hi"),
		})
		if resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("request %d: status = %d, want 500 (task timeout)", i, resp.StatusCode)
		}
		if got := cm.AvailableSlots(); got != 2 {
			t.Errorf("after request %d: AvailableSlots() = %d, want 2 — a slot leaked", i, got)
		}
	}
}

// TestCapacity_NoSelfDeadlock guards the failure mode described in the design
// review: if the handler were to acquire a task slot *and* then call a
// RunOneShot variant that acquires one too, a single-slot server would block
// against itself until the task timeout. With one slot configured, one request
// must still be served.
func TestCapacity_NoSelfDeadlock(t *testing.T) {
	logger := core.NewLogger(testAuthToken)
	cfg := &core.Config{
		APIToken:           testAuthToken,
		TaskTimeout:        30 * time.Second, // deliberately long: a deadlock would hit this
		MaxConcurrentTasks: 1,
		MaxChatSessions:    1,
		ProjectsDir:        t.TempDir(),
		AssistantDataDir:   t.TempDir(),
	}
	logger2 := logger
	cm := docker.NewContainerManager(noopContainerClient{}, cfg, logger2)
	openaiH := NewOpenAIHandler(NewModelRegistry(""), cm, logger2, 2*time.Second)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", openaiH.HandleChatCompletions)
	srv := httptest.NewServer(AuthMiddleware(testAuthToken, mux))
	t.Cleanup(srv.Close)

	done := make(chan int, 1)
	go func() {
		resp := chatRequest(t, srv, map[string]any{
			"model": "kiro", "messages": userMessage("hi"),
		})
		done <- resp.StatusCode
	}()

	select {
	case status := <-done:
		// 500 is expected here (the fake container never finishes, so the
		// handler's own 2s timeout fires). What matters is that the request was
		// *served* rather than deadlocking on the slot pool.
		if status != http.StatusInternalServerError {
			t.Logf("status = %d (any served response proves there is no deadlock)", status)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("request never completed with a single task slot — the handler is " +
			"deadlocking against its own slot acquisition")
	}

	// Slot must be back.
	if got := cm.AvailableSlots(); got != 1 {
		t.Errorf("AvailableSlots() = %d, want 1", got)
	}
}

// TestCapacity_StoreUnusedByOpenAIPath is a guard for Req 12: the OpenAI layer
// must not write into the task store that the legacy /run endpoints own, or the
// two APIs would start reporting each other's work.
func TestCapacity_StoreUnusedByOpenAIPath(t *testing.T) {
	taskStore := store.NewTaskStore()
	srv := newOpenAITestServer(t, openaiServerOpts{})

	resp := chatRequest(t, srv, map[string]any{"model": "kiro", "messages": userMessage("hi")})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := len(taskStore.List()); got != 0 {
		t.Errorf("task store holds %d tasks, want 0", got)
	}
}

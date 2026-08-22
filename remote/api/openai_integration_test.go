package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"remote/core"
	"remote/docker"
	"remote/git"
	"remote/store"
)

// ---------------------------------------------------------------------------
// Scripted container runner
// ---------------------------------------------------------------------------

// Prompt markers understood by scriptedRunner. Tests drive agent behaviour by
// putting one of these in the user message, which keeps the fake usable both
// from Go tests and (via the fake server binary) from the SDK conformance suite.
const (
	markerEcho  = "__echo__"  // return the composed (system, prompt) as JSON
	markerEmpty = "__empty__" // produce no output at all
	markerFail  = "__fail__"  // exit non-zero with a stderr message
	markerError = "__error__" // fail to start the container at all
	markerHang  = "__hang__"  // block until the context is cancelled
	markerCrash = "__crash__" // stream two chunks, then die mid-stream
	markerANSI  = "__ansi__"  // emit output wrapped in ANSI escape sequences
	markerSlow  = "__slow__"  // stream five chunks with a delay between them
)

// echoPayload is what markerEcho returns, letting black-box tests assert on the
// prompt composition that Req 11 specifies.
type echoPayload struct {
	Agent  string `json:"agent"`
	Model  string `json:"model"`
	System string `json:"system"`
	Prompt string `json:"prompt"`
}

// scriptedRunner is a ContainerRunner double whose behaviour is selected by
// markers in the prompt. It records enough state for tests to assert that the
// handler cleaned up after itself.
type scriptedRunner struct {
	chunkDelay time.Duration

	mu         sync.Mutex
	lastAgent  string
	lastModel  string
	lastSystem string
	lastPrompt string

	streamsOpened int32
	streamsClosed int32
}

func (s *scriptedRunner) record(agent, prompt, model, system string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastAgent, s.lastPrompt, s.lastModel, s.lastSystem = agent, prompt, model, system
}

func (s *scriptedRunner) RunOneShot(ctx context.Context, agent, prompt, model, system string, _ time.Time, onStart func(string)) (*docker.ContainerResult, error) {
	s.record(agent, prompt, model, system)
	if onStart != nil {
		onStart("scripted-container")
	}

	switch {
	case strings.Contains(prompt, markerError):
		return nil, fmt.Errorf("failed to create container: scripted failure")
	case strings.Contains(prompt, markerHang):
		<-ctx.Done()
		return nil, ctx.Err()
	case strings.Contains(prompt, markerFail):
		return &docker.ContainerResult{Stderr: "scripted agent failure", ExitCode: 1}, nil
	case strings.Contains(prompt, markerEmpty):
		return &docker.ContainerResult{Stdout: "", ExitCode: 0}, nil
	case strings.Contains(prompt, markerANSI):
		return &docker.ContainerResult{Stdout: "\x1b[32mgreen text\x1b[0m", ExitCode: 0}, nil
	case strings.Contains(prompt, markerEcho):
		payload, _ := json.Marshal(echoPayload{Agent: agent, Model: model, System: system, Prompt: prompt})
		return &docker.ContainerResult{Stdout: string(payload), ExitCode: 0}, nil
	default:
		return &docker.ContainerResult{Stdout: "Hello from the scripted agent", ExitCode: 0}, nil
	}
}

// recordingCloser counts Close calls so tests can prove the handler released the
// stream on every exit path, including client disconnects.
type recordingCloser struct {
	runner *scriptedRunner
	pipe   *io.PipeReader
}

func (rc *recordingCloser) Close() error {
	atomic.AddInt32(&rc.runner.streamsClosed, 1)
	return rc.pipe.Close()
}

func (s *scriptedRunner) RunOneShotStreaming(ctx context.Context, agent, prompt, model, system string, _ time.Time) (*docker.OneShotStream, error) {
	s.record(agent, prompt, model, system)

	if strings.Contains(prompt, markerError) {
		return nil, fmt.Errorf("failed to create container: scripted failure")
	}

	atomic.AddInt32(&s.streamsOpened, 1)
	pr, pw := io.Pipe()

	go func() {
		write := func(line string) bool {
			select {
			case <-ctx.Done():
				return false
			default:
			}
			if _, err := io.WriteString(pw, line+"\n"); err != nil {
				return false
			}
			if s.chunkDelay > 0 {
				select {
				case <-ctx.Done():
					return false
				case <-time.After(s.chunkDelay):
				}
			}
			return true
		}

		// Agent-appropriate framing: claude speaks NDJSON stream-json, kiro
		// writes plain lines to a TTY.
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

		defer pw.Close()

		switch {
		case strings.Contains(prompt, markerHang):
			<-ctx.Done()
		case strings.Contains(prompt, markerCrash):
			write(frame("partial one"))
			write(frame("partial two"))
			// Die mid-stream: close the read side with an error, mimicking a
			// container that vanished while the handler was still reading.
			pw.CloseWithError(fmt.Errorf("container exited unexpectedly"))
		case strings.Contains(prompt, markerEmpty):
			// Produce nothing and end the stream immediately.
		case strings.Contains(prompt, markerANSI):
			write("\x1b[32mgreen\x1b[0m")
			write("\x1b[1mbold\x1b[0m")
		case strings.Contains(prompt, markerSlow):
			for i := 1; i <= 5; i++ {
				if !write(frame(fmt.Sprintf("chunk %d", i))) {
					return
				}
			}
		default:
			for _, part := range []string{"Hello", " streamed", " world"} {
				if !write(frame(part)) {
					return
				}
			}
		}
	}()

	return &docker.OneShotStream{
		ContainerID: "scripted-container",
		Reader:      pr,
		Closer:      &recordingCloser{runner: s, pipe: pr},
	}, nil
}

func (s *scriptedRunner) StopAndRemoveContainer(context.Context, string) error { return nil }

// ---------------------------------------------------------------------------
// Test server wiring
// ---------------------------------------------------------------------------

type openaiServerOpts struct {
	runner      ContainerRunner
	modelConfig string
	taskTimeout time.Duration
	ratePerMin  int
}

// newOpenAITestServer starts an httptest server running the production router,
// so requests traverse the real middleware chain (recovery → CORS → rate limit
// → auth → requestID → mux) exactly as they would in the deployed server.
func newOpenAITestServer(t *testing.T, opts openaiServerOpts) *httptest.Server {
	t.Helper()

	if opts.runner == nil {
		opts.runner = &scriptedRunner{}
	}
	if opts.taskTimeout == 0 {
		opts.taskTimeout = 5 * time.Second
	}
	if opts.ratePerMin == 0 {
		opts.ratePerMin = 10000
	}

	projectsDir := t.TempDir()
	assistantDataDir := t.TempDir()
	logger := core.NewLogger(testAuthToken)
	cfg := &core.Config{
		APIToken:         testAuthToken,
		SessionTimeout:   time.Minute,
		ProjectsDir:      projectsDir,
		AssistantDataDir: assistantDataDir,
		TaskTimeout:      opts.taskTimeout,
	}

	cm := docker.NewContainerManager(noopContainerClient{}, cfg, logger)
	sessionStore := store.NewSessionStore()

	taskH := NewTaskHandler(store.NewTaskStore(), opts.runner, logger, nil, t.TempDir(), MaxUploadFileSize, MaxUploadFileCount, 32000)
	sessionH := NewSessionHandler(sessionStore, cm, logger, cfg, nil)
	gitH := NewGitHandler(projectsDir, git.NewRunner(), logger)
	envH := NewEnvHandler(projectsDir, logger)
	projectH := NewProjectHandler(projectsDir, git.NewRunner(), logger)
	projectAgentH := NewProjectAgentHandler(sessionStore, cm, logger, cfg, nil)
	pipelineH := NewPipelineHandler(cfg, logger)
	specH := NewSpecHandler(cfg, logger)
	workflowStore := store.NewWorkflowStore()
	queues := NewWorkflowQueueManager(workflowStore, cm, cfg, logger, nil)
	workflowH := NewWorkflowHandler(workflowStore, cfg, logger, nil, queues)

	assistantFolderMgr := NewAssistantFolderManager(assistantDataDir, logger)
	if err := assistantFolderMgr.Init(); err != nil {
		t.Fatalf("assistant folder init: %v", err)
	}
	assistantH := NewAssistantHandler(sessionStore, cm, logger, cfg, nil, assistantFolderMgr)

	openaiH := NewOpenAIHandler(NewModelRegistry(opts.modelConfig), opts.runner, logger, opts.taskTimeout)

	csrf := NewCSRFStore()
	authH := NewAuthHandler(testAuthToken, true, csrf)
	router := NewRouter(&HealthHandler{}, taskH, sessionH, gitH, envH, projectH, projectAgentH,
		pipelineH, specH, workflowH, assistantH, openaiH, authH, testAuthToken, csrf, NewRateLimiter(opts.ratePerMin), logger, "")

	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return srv
}

// chatRequest posts a chat completion request with valid credentials.
func chatRequest(t *testing.T, srv *httptest.Server, body any) *http.Response {
	t.Helper()
	return chatRequestWithToken(t, srv, body, testAuthToken)
}

func chatRequestWithToken(t *testing.T, srv *httptest.Server, body any, token string) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	switch v := body.(type) {
	case string:
		buf.WriteString(v)
	default:
		if err := json.NewEncoder(&buf).Encode(v); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/chat/completions", &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func getRequest(t *testing.T, srv *httptest.Server, path, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func decodeChatResponse(t *testing.T, resp *http.Response) OpenAIChatResponse {
	t.Helper()
	var out OpenAIChatResponse
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode chat response: %v\nbody: %s", err, body)
	}
	return out
}

func decodeErrorResponse(t *testing.T, resp *http.Response) OpenAIErrorResponse {
	t.Helper()
	var out OpenAIErrorResponse
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode error response: %v\nbody: %s", err, body)
	}
	if out.Error.Type == "" {
		t.Fatalf("response is not an OpenAI error object: %s", body)
	}
	return out
}

func userMessage(content string) []map[string]string {
	return []map[string]string{{"role": "user", "content": content}}
}

// ---------------------------------------------------------------------------
// Non-streaming
// ---------------------------------------------------------------------------

// TestIntegration_NonStreaming_HappyPath covers Req 1.1, 1.3, 1.5, 8.1 and 8.3
// end to end through the real router.
func TestIntegration_NonStreaming_HappyPath(t *testing.T) {
	srv := newOpenAITestServer(t, openaiServerOpts{})

	before := time.Now().Unix()
	resp := chatRequest(t, srv, map[string]any{
		"model":    "kiro",
		"messages": userMessage("Hello"),
	})
	after := time.Now().Unix()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	out := decodeChatResponse(t, resp)
	if !strings.HasPrefix(out.ID, "chatcmpl-") || len(out.ID) < 24 {
		t.Errorf("id = %q, want chatcmpl- prefix and >= 24 chars", out.ID)
	}
	if out.Object != "chat.completion" {
		t.Errorf("object = %q, want chat.completion", out.Object)
	}
	if out.Created < before || out.Created > after {
		t.Errorf("created = %d, want within [%d, %d]", out.Created, before, after)
	}
	if out.Model != "kiro" {
		t.Errorf("model = %q, want kiro (request echo)", out.Model)
	}
	if len(out.Choices) != 1 {
		t.Fatalf("%d choices, want exactly 1", len(out.Choices))
	}
	c := out.Choices[0]
	if c.Index != 0 || c.Message.Role != "assistant" || c.FinishReason != "stop" {
		t.Errorf("choice = %+v, want index 0 / role assistant / finish_reason stop", c)
	}
	if c.Message.Content != "Hello from the scripted agent" {
		t.Errorf("content = %q, want the agent output", c.Message.Content)
	}
	if out.Usage.TotalTokens != out.Usage.PromptTokens+out.Usage.CompletionTokens {
		t.Errorf("usage %+v: total != prompt + completion", out.Usage)
	}
	if out.Usage.PromptTokens < 0 || out.Usage.CompletionTokens < 0 {
		t.Errorf("usage %+v: token counts must be non-negative", out.Usage)
	}
}

// TestIntegration_NonStreaming_ComposedPrompt covers Req 1.6 and 11.1–11.3 as
// observed from outside: the agent must receive the full conversation with role
// attribution, and the system message separately.
func TestIntegration_NonStreaming_ComposedPrompt(t *testing.T) {
	srv := newOpenAITestServer(t, openaiServerOpts{})

	resp := chatRequest(t, srv, map[string]any{
		"model": "claude-sonnet",
		"messages": []map[string]string{
			{"role": "system", "content": "You are terse"},
			{"role": "user", "content": "First question"},
			{"role": "assistant", "content": "First answer"},
			{"role": "user", "content": markerEcho},
		},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var echo echoPayload
	content := decodeChatResponse(t, resp).Choices[0].Message.Content
	if err := json.Unmarshal([]byte(content), &echo); err != nil {
		t.Fatalf("decode echo payload %q: %v", content, err)
	}

	if echo.Agent != "claude" || echo.Model != "sonnet" {
		t.Errorf("agent/model = %q/%q, want claude/sonnet (Req 4.1)", echo.Agent, echo.Model)
	}
	if echo.System != "You are terse" {
		t.Errorf("system = %q, want the system message content", echo.System)
	}
	want := "User:\nFirst question\n\nAssistant:\nFirst answer\n\nUser:\n" + markerEcho
	if echo.Prompt != want {
		t.Errorf("prompt:\n got %q\nwant %q", echo.Prompt, want)
	}
}

// TestIntegration_NonStreaming_EmptyOutput covers Req 8.3: no agent output must
// still produce a well-formed choice with an empty string content.
func TestIntegration_NonStreaming_EmptyOutput(t *testing.T) {
	srv := newOpenAITestServer(t, openaiServerOpts{})

	resp := chatRequest(t, srv, map[string]any{"model": "kiro", "messages": userMessage(markerEmpty)})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	out := decodeChatResponse(t, resp)
	if len(out.Choices) != 1 {
		t.Fatalf("%d choices, want 1", len(out.Choices))
	}
	if out.Choices[0].Message.Content != "" {
		t.Errorf("content = %q, want empty string", out.Choices[0].Message.Content)
	}
	if out.Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason = %q, want stop", out.Choices[0].FinishReason)
	}
	if out.Usage.CompletionTokens != 0 {
		t.Errorf("completion_tokens = %d, want 0", out.Usage.CompletionTokens)
	}
}

// TestIntegration_NonStreaming_ANSIStripped: kiro writes to a TTY, so its raw
// output carries escape sequences that must never reach an API client.
func TestIntegration_NonStreaming_ANSIStripped(t *testing.T) {
	srv := newOpenAITestServer(t, openaiServerOpts{})

	resp := chatRequest(t, srv, map[string]any{"model": "kiro", "messages": userMessage(markerANSI)})
	content := decodeChatResponse(t, resp).Choices[0].Message.Content
	if strings.Contains(content, "\x1b[") {
		t.Errorf("content still contains ANSI escapes: %q", content)
	}
	if content != "green text" {
		t.Errorf("content = %q, want %q", content, "green text")
	}
}

// TestIntegration_NonStreaming_AgentFailure covers Req 6.4.
func TestIntegration_NonStreaming_AgentFailure(t *testing.T) {
	tests := map[string]string{
		"non-zero exit":     markerFail,
		"container failure": markerError,
	}
	for name, marker := range tests {
		t.Run(name, func(t *testing.T) {
			srv := newOpenAITestServer(t, openaiServerOpts{})
			resp := chatRequest(t, srv, map[string]any{"model": "kiro", "messages": userMessage(marker)})

			if resp.StatusCode != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500", resp.StatusCode)
			}
			errResp := decodeErrorResponse(t, resp)
			if errResp.Error.Type != "server_error" {
				t.Errorf("type = %q, want server_error", errResp.Error.Type)
			}
			if errResp.Error.Param != nil {
				t.Errorf("param = %v, want null", *errResp.Error.Param)
			}
			if errResp.Error.Code != nil {
				t.Errorf("code = %v, want null", *errResp.Error.Code)
			}
		})
	}
}

// TestIntegration_NonStreaming_Timeout covers the design's task-timeout rule: a
// hung agent must produce a 500 rather than blocking forever.
func TestIntegration_NonStreaming_Timeout(t *testing.T) {
	srv := newOpenAITestServer(t, openaiServerOpts{taskTimeout: 300 * time.Millisecond})

	start := time.Now()
	resp := chatRequest(t, srv, map[string]any{"model": "kiro", "messages": userMessage(markerHang)})
	elapsed := time.Since(start)

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	if elapsed > 5*time.Second {
		t.Errorf("took %v, want the 300ms task timeout to apply", elapsed)
	}
	errResp := decodeErrorResponse(t, resp)
	if errResp.Error.Type != "server_error" {
		t.Errorf("type = %q, want server_error", errResp.Error.Type)
	}
	if !strings.Contains(strings.ToLower(errResp.Error.Message), "timed out") {
		t.Errorf("message = %q, want it to mention a timeout", errResp.Error.Message)
	}
}

// ---------------------------------------------------------------------------
// Validation (Req 9) and model resolution (Req 1.8, 4.3)
// ---------------------------------------------------------------------------

func TestIntegration_ValidationMatrix(t *testing.T) {
	srv := newOpenAITestServer(t, openaiServerOpts{})

	tests := []struct {
		name      string
		body      any
		wantParam string // "" means: no param assertion
	}{
		{
			name:      "missing model", // Req 9.1
			body:      map[string]any{"messages": userMessage("hi")},
			wantParam: "model",
		},
		{
			name:      "empty model", // Req 9.1
			body:      map[string]any{"model": "", "messages": userMessage("hi")},
			wantParam: "model",
		},
		{
			name:      "missing messages", // Req 9.2
			body:      map[string]any{"model": "kiro"},
			wantParam: "messages",
		},
		{
			name:      "empty messages", // Req 9.2
			body:      map[string]any{"model": "kiro", "messages": []any{}},
			wantParam: "messages",
		},
		{
			name:      "message missing role", // Req 9.3
			body:      map[string]any{"model": "kiro", "messages": []map[string]string{{"content": "hi"}}},
			wantParam: "messages[0]",
		},
		{
			name:      "message missing content", // Req 9.3
			body:      map[string]any{"model": "kiro", "messages": []map[string]string{{"role": "user"}}},
			wantParam: "messages[0]",
		},
		{
			name: "invalid role reports its index", // Req 9.4
			body: map[string]any{"model": "kiro", "messages": []map[string]string{
				{"role": "user", "content": "ok"},
				{"role": "wizard", "content": "nope"},
			}},
			wantParam: "messages[1].role",
		},
		{
			name:      "malformed JSON body",
			body:      `{"model": "kiro", "messages": [`,
			wantParam: "",
		},
		{
			name:      "empty body",
			body:      ``,
			wantParam: "",
		},
		{
			name: "too many messages", // Req 1.2
			body: func() any {
				messages := make([]map[string]string, 200)
				for i := range messages {
					messages[i] = map[string]string{"role": "user", "content": "m"}
				}
				return map[string]any{"model": "kiro", "messages": messages}
			}(),
			wantParam: "messages",
		},
		{
			name:      "model name too long", // Req 1.2
			body:      map[string]any{"model": strings.Repeat("x", 300), "messages": userMessage("hi")},
			wantParam: "model",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := chatRequest(t, srv, tt.body)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
			errResp := decodeErrorResponse(t, resp)
			if errResp.Error.Type != "invalid_request_error" {
				t.Errorf("type = %q, want invalid_request_error", errResp.Error.Type)
			}
			if errResp.Error.Message == "" {
				t.Error("message is empty, want a human-readable description")
			}
			if tt.wantParam == "" {
				return
			}
			if errResp.Error.Param == nil {
				t.Fatalf("param = null, want %q", tt.wantParam)
			}
			if *errResp.Error.Param != tt.wantParam {
				t.Errorf("param = %q, want %q", *errResp.Error.Param, tt.wantParam)
			}
		})
	}
}

// TestIntegration_ValidRolesAccepted covers Req 9.5.
func TestIntegration_ValidRolesAccepted(t *testing.T) {
	srv := newOpenAITestServer(t, openaiServerOpts{})

	resp := chatRequest(t, srv, map[string]any{
		"model": "kiro",
		"messages": []map[string]string{
			{"role": "system", "content": "sys"},
			{"role": "user", "content": "u"},
			{"role": "assistant", "content": "a"},
			{"role": "user", "content": "u2"},
		},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

// TestIntegration_UnknownModel covers Req 1.8 and 4.3.
func TestIntegration_UnknownModel(t *testing.T) {
	srv := newOpenAITestServer(t, openaiServerOpts{})

	resp := chatRequest(t, srv, map[string]any{"model": "gpt-4o", "messages": userMessage("hi")})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	errResp := decodeErrorResponse(t, resp)
	if errResp.Error.Type != "invalid_request_error" {
		t.Errorf("type = %q, want invalid_request_error", errResp.Error.Type)
	}
	if errResp.Error.Code == nil || *errResp.Error.Code != "model_not_found" {
		t.Errorf("code = %v, want model_not_found", errResp.Error.Code)
	}
	if !strings.Contains(errResp.Error.Message, "gpt-4o") {
		t.Errorf("message = %q, want it to name the unknown model", errResp.Error.Message)
	}
}

// TestIntegration_ModelResolutionIsCaseInsensitive covers Req 4.2 over HTTP.
func TestIntegration_ModelResolutionIsCaseInsensitive(t *testing.T) {
	srv := newOpenAITestServer(t, openaiServerOpts{})

	for _, name := range []string{"KIRO", "Kiro", "kiro"} {
		resp := chatRequest(t, srv, map[string]any{"model": name, "messages": userMessage("hi")})
		if resp.StatusCode != http.StatusOK {
			t.Errorf("model %q: status = %d, want 200", name, resp.StatusCode)
			continue
		}
		// Req 1.5: the response echoes what the client asked for, verbatim.
		if got := decodeChatResponse(t, resp).Model; got != name {
			t.Errorf("model %q: response model = %q, want the request value echoed", name, got)
		}
	}
}

// ---------------------------------------------------------------------------
// Ignored parameters (Req 10)
// ---------------------------------------------------------------------------

func TestIntegration_IgnoredParams(t *testing.T) {
	srv := newOpenAITestServer(t, openaiServerOpts{})

	tests := []struct {
		name  string
		extra map[string]any
	}{
		{ // Req 10.1
			name: "all documented ignored params",
			extra: map[string]any{
				"temperature": 0.7, "top_p": 0.9, "max_tokens": 100,
				"stop": []string{"a", "b"}, "n": 1, "presence_penalty": 0.5,
				"frequency_penalty": 0.5, "logit_bias": map[string]int{"1": -100},
				"user": "user-123",
			},
		},
		{ // Req 10.1: stop may be a bare string as well as an array
			name:  "stop as string",
			extra: map[string]any{"stop": "END"},
		},
		{ // Req 10.3
			name:  "unknown parameters ignored",
			extra: map[string]any{"totally_unknown": map[string]any{"nested": true}, "seed": 42},
		},
		{ // Req 10.4
			name: "wrong types on ignored params",
			extra: map[string]any{
				"temperature": "hot", "top_p": "high", "max_tokens": "many",
				"n": "three", "presence_penalty": []int{1}, "frequency_penalty": true,
				"user": 42,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := map[string]any{"model": "kiro", "messages": userMessage("hi")}
			for k, v := range tt.extra {
				body[k] = v
			}
			resp := chatRequest(t, srv, body)
			if resp.StatusCode != http.StatusOK {
				errBody, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, want 200 (params must be accepted and ignored)\nbody: %s", resp.StatusCode, errBody)
			}
		})
	}
}

// TestIntegration_NGreaterThanOne covers Req 10.2 and design Property 2.
func TestIntegration_NGreaterThanOne(t *testing.T) {
	srv := newOpenAITestServer(t, openaiServerOpts{})

	resp := chatRequest(t, srv, map[string]any{"model": "kiro", "messages": userMessage("hi"), "n": 5})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	out := decodeChatResponse(t, resp)
	if len(out.Choices) != 1 {
		t.Errorf("%d choices, want exactly 1 regardless of n", len(out.Choices))
	}
	if out.Choices[0].Index != 0 {
		t.Errorf("choice index = %d, want 0", out.Choices[0].Index)
	}
}

// ---------------------------------------------------------------------------
// Authentication (Req 5) and rate limiting (Req 6.3)
// ---------------------------------------------------------------------------

func TestIntegration_Auth(t *testing.T) {
	srv := newOpenAITestServer(t, openaiServerOpts{})

	body := map[string]any{"model": "kiro", "messages": userMessage("hi")}

	t.Run("missing header returns OpenAI-format 401", func(t *testing.T) { // Req 5.2
		resp := chatRequestWithToken(t, srv, body, "")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", resp.StatusCode)
		}
		errResp := decodeErrorResponse(t, resp)
		if errResp.Error.Type != "invalid_request_error" {
			t.Errorf("type = %q, want invalid_request_error", errResp.Error.Type)
		}
		if errResp.Error.Param != nil {
			t.Errorf("param = %v, want null", *errResp.Error.Param)
		}
		if errResp.Error.Code != nil {
			t.Errorf("code = %v, want null", *errResp.Error.Code)
		}
	})

	t.Run("invalid token reports invalid_api_key", func(t *testing.T) { // Req 5.3
		resp := chatRequestWithToken(t, srv, body, "wrong-token")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", resp.StatusCode)
		}
		errResp := decodeErrorResponse(t, resp)
		if errResp.Error.Code == nil || *errResp.Error.Code != "invalid_api_key" {
			t.Errorf("code = %v, want invalid_api_key", errResp.Error.Code)
		}
	})

	t.Run("models endpoints require the same auth", func(t *testing.T) { // Req 5.4
		for _, path := range []string{"/v1/models", "/v1/models/kiro"} {
			resp := getRequest(t, srv, path, "")
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("%s: status = %d, want 401", path, resp.StatusCode)
				continue
			}
			if errResp := decodeErrorResponse(t, resp); errResp.Error.Type != "invalid_request_error" {
				t.Errorf("%s: type = %q, want invalid_request_error", path, errResp.Error.Type)
			}
		}
	})

	t.Run("existing endpoints keep the Trayline error format", func(t *testing.T) { // Req 12.1
		resp := getRequest(t, srv, "/runs", "")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		var legacy core.ErrorResponse
		if err := json.Unmarshal(body, &legacy); err != nil || legacy.Error != "UNAUTHORIZED" {
			t.Errorf("legacy endpoint error changed shape: %s", body)
		}
	})
}

// TestIntegration_RateLimitErrorFormat covers Req 6.3 for the rate limiter's
// 429: /v1/ clients must get the OpenAI error shape, legacy clients must not.
func TestIntegration_RateLimitErrorFormat(t *testing.T) {
	srv := newOpenAITestServer(t, openaiServerOpts{ratePerMin: 1})

	// Burst until the limiter trips.
	var resp *http.Response
	for i := 0; i < 5; i++ {
		resp = getRequest(t, srv, "/v1/models", testAuthToken)
		if resp.StatusCode == http.StatusTooManyRequests {
			break
		}
	}
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("never rate limited: last status = %d", resp.StatusCode)
	}
	if ra := resp.Header.Get("Retry-After"); ra == "" {
		t.Error("Retry-After header missing on rate limit response")
	}
	errResp := decodeErrorResponse(t, resp)
	if errResp.Error.Type != "server_error" {
		t.Errorf("type = %q, want server_error", errResp.Error.Type)
	}
}

// ---------------------------------------------------------------------------
// Models endpoints (Req 3)
// ---------------------------------------------------------------------------

func TestIntegration_ModelsEndpoints(t *testing.T) {
	srv := newOpenAITestServer(t, openaiServerOpts{})

	t.Run("list", func(t *testing.T) { // Req 3.1, 3.2
		resp := getRequest(t, srv, "/v1/models", testAuthToken)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		var list OpenAIModelList
		if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if list.Object != "list" {
			t.Errorf("object = %q, want list", list.Object)
		}
		if len(list.Data) != 3 {
			t.Fatalf("%d models, want the 3 defaults", len(list.Data))
		}
		for _, m := range list.Data {
			if m.Object != "model" {
				t.Errorf("model %q: object = %q, want model", m.ID, m.Object)
			}
			if m.OwnedBy == "" {
				t.Errorf("model %q: owned_by is empty", m.ID)
			}
			if m.Created <= 0 {
				t.Errorf("model %q: created = %d, want a Unix timestamp", m.ID, m.Created)
			}
		}
	})

	t.Run("get single", func(t *testing.T) { // Req 3.3
		resp := getRequest(t, srv, "/v1/models/claude-sonnet", testAuthToken)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		var m OpenAIModel
		if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if m.ID != "claude-sonnet" || m.Object != "model" {
			t.Errorf("got %+v, want id claude-sonnet / object model", m)
		}
	})

	t.Run("get unknown", func(t *testing.T) { // Req 3.4
		resp := getRequest(t, srv, "/v1/models/nope", testAuthToken)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
		errResp := decodeErrorResponse(t, resp)
		if errResp.Error.Type != "invalid_request_error" {
			t.Errorf("type = %q, want invalid_request_error", errResp.Error.Type)
		}
		if !strings.Contains(errResp.Error.Message, "nope") {
			t.Errorf("message = %q, want it to include the requested model id", errResp.Error.Message)
		}
	})

	t.Run("empty registry lists nothing", func(t *testing.T) { // Req 3.5, 4.5
		emptySrv := newOpenAITestServer(t, openaiServerOpts{modelConfig: "not-a-valid-entry"})
		resp := getRequest(t, emptySrv, "/v1/models", testAuthToken)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		var list OpenAIModelList
		if err := json.Unmarshal(body, &list); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if list.Object != "list" || len(list.Data) != 0 {
			t.Errorf("got %+v, want object list with empty data", list)
		}
		// Req 3.5 requires an empty array, not JSON null — SDKs iterate this.
		if !strings.Contains(string(body), `"data":[]`) {
			t.Errorf("data did not serialise as []: %s", body)
		}
	})
}

// ---------------------------------------------------------------------------
// Streaming (Req 2)
// ---------------------------------------------------------------------------

// sseFrame is one parsed `data:` payload from a live SSE response.
func readSSEFrames(t *testing.T, r io.Reader) []string {
	t.Helper()
	var frames []string
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			t.Fatalf("unexpected SSE line %q", line)
		}
		frames = append(frames, strings.TrimPrefix(line, "data: "))
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read SSE stream: %v", err)
	}
	return frames
}

// assertWellFormedStream checks the invariants every stream must satisfy
// (Req 2.2, 2.4, 2.5, 2.6 and design Properties 2 and 5) and returns the
// reassembled assistant text.
func assertWellFormedStream(t *testing.T, frames []string) string {
	t.Helper()

	if len(frames) == 0 {
		t.Fatal("stream produced no frames")
	}
	if frames[len(frames)-1] != "[DONE]" {
		t.Errorf("last frame = %q, want [DONE]", frames[len(frames)-1])
	}
	doneCount := 0
	for _, f := range frames {
		if f == "[DONE]" {
			doneCount++
		}
	}
	if doneCount != 1 {
		t.Errorf("[DONE] appears %d times, want exactly 1", doneCount)
	}

	var text strings.Builder
	var id string
	stopSeen := 0
	for i, f := range frames {
		if f == "[DONE]" {
			continue
		}
		var chunk OpenAIStreamChunk
		if err := json.Unmarshal([]byte(f), &chunk); err != nil {
			t.Fatalf("frame %d is not valid JSON: %v\n%s", i, err, f)
		}
		if chunk.Object != "chat.completion.chunk" {
			t.Errorf("frame %d: object = %q, want chat.completion.chunk", i, chunk.Object)
		}
		if id == "" {
			id = chunk.ID
		} else if chunk.ID != id {
			t.Errorf("frame %d: id = %q, want %q (stable across chunks)", i, chunk.ID, id)
		}
		if len(chunk.Choices) != 1 {
			t.Fatalf("frame %d: %d choices, want 1", i, len(chunk.Choices))
		}
		choice := chunk.Choices[0]
		if choice.Index != 0 {
			t.Errorf("frame %d: index = %d, want 0", i, choice.Index)
		}
		// Req 2.6: role appears in the first chunk only. When the agent produced
		// no output at all the only chunk is the terminating one, whose delta
		// must stay empty per Req 2.4 — so the role assertion applies to the
		// first *content* chunk.
		if i == 0 && choice.FinishReason == nil && choice.Delta.Role != "assistant" {
			t.Errorf("first frame: delta.role = %q, want assistant", choice.Delta.Role)
		}
		if i > 0 && choice.Delta.Role != "" {
			t.Errorf("frame %d: delta.role = %q, want empty (role only in first chunk)", i, choice.Delta.Role)
		}
		if choice.FinishReason != nil {
			if *choice.FinishReason != "stop" {
				t.Errorf("frame %d: finish_reason = %q, want stop", i, *choice.FinishReason)
			}
			stopSeen++
		}
		text.WriteString(choice.Delta.Content)
	}
	if stopSeen != 1 {
		t.Errorf("%d chunks carry finish_reason, want exactly 1", stopSeen)
	}
	if !strings.HasPrefix(id, "chatcmpl-") {
		t.Errorf("stream id = %q, want chatcmpl- prefix", id)
	}
	return text.String()
}

func TestIntegration_Streaming_HappyPath(t *testing.T) {
	for _, model := range []string{"kiro", "claude-sonnet"} {
		t.Run(model, func(t *testing.T) {
			srv := newOpenAITestServer(t, openaiServerOpts{})
			resp := chatRequest(t, srv, map[string]any{
				"model": model, "messages": userMessage("hi"), "stream": true,
			})

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			// Req 2.1
			if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
				t.Errorf("Content-Type = %q, want text/event-stream", ct)
			}
			if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
				t.Errorf("Cache-Control = %q, want no-cache", cc)
			}

			frames := readSSEFrames(t, resp.Body)
			text := assertWellFormedStream(t, frames)

			// Req 2.3: three agent chunks plus the terminating stop chunk and [DONE].
			if len(frames) < 4 {
				t.Errorf("%d frames, want at least 4 (3 content + stop + [DONE])", len(frames))
			}
			if !strings.Contains(text, "Hello") || !strings.Contains(text, "world") {
				t.Errorf("reassembled text = %q, want the agent output", text)
			}
		})
	}
}

// TestIntegration_Streaming_Ordering covers design Property 3: chunks arrive in
// production order, with no reordering or coalescing.
func TestIntegration_Streaming_Ordering(t *testing.T) {
	srv := newOpenAITestServer(t, openaiServerOpts{runner: &scriptedRunner{chunkDelay: 10 * time.Millisecond}})

	resp := chatRequest(t, srv, map[string]any{
		"model": "claude-sonnet", "messages": userMessage(markerSlow), "stream": true,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	frames := readSSEFrames(t, resp.Body)
	text := assertWellFormedStream(t, frames)

	want := "chunk 1chunk 2chunk 3chunk 4chunk 5"
	if text != want {
		t.Errorf("reassembled text = %q, want %q", text, want)
	}
}

// TestIntegration_Streaming_ANSIStripped: kiro's TTY escapes must not leak into
// SSE deltas either.
func TestIntegration_Streaming_ANSIStripped(t *testing.T) {
	srv := newOpenAITestServer(t, openaiServerOpts{})

	resp := chatRequest(t, srv, map[string]any{
		"model": "kiro", "messages": userMessage(markerANSI), "stream": true,
	})
	text := assertWellFormedStream(t, readSSEFrames(t, resp.Body))
	if strings.Contains(text, "\x1b[") {
		t.Errorf("stream contains ANSI escapes: %q", text)
	}
	if !strings.Contains(text, "green") || !strings.Contains(text, "bold") {
		t.Errorf("text = %q, want the stripped agent output", text)
	}
}

// TestIntegration_Streaming_ContainerCrash covers Req 2.7: a container that dies
// mid-stream must still terminate the SSE stream cleanly.
func TestIntegration_Streaming_ContainerCrash(t *testing.T) {
	srv := newOpenAITestServer(t, openaiServerOpts{})

	resp := chatRequest(t, srv, map[string]any{
		"model": "claude-sonnet", "messages": userMessage(markerCrash), "stream": true,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	frames := readSSEFrames(t, resp.Body)
	text := assertWellFormedStream(t, frames)
	if !strings.Contains(text, "partial one") {
		t.Errorf("text = %q, want the chunks produced before the crash", text)
	}
}

// TestIntegration_Streaming_EmptyOutput: an agent that produces nothing must
// still yield a valid, terminated stream rather than a dangling connection.
func TestIntegration_Streaming_EmptyOutput(t *testing.T) {
	srv := newOpenAITestServer(t, openaiServerOpts{})

	resp := chatRequest(t, srv, map[string]any{
		"model": "kiro", "messages": userMessage(markerEmpty), "stream": true,
	})
	frames := readSSEFrames(t, resp.Body)
	if len(frames) != 2 {
		t.Fatalf("%d frames, want 2 (stop + [DONE]); frames: %v", len(frames), frames)
	}
	assertWellFormedStream(t, frames)
}

// TestIntegration_Streaming_StartFailureIsJSON: when the container never
// starts, the client has not yet been promised a stream, so the error must be a
// regular JSON error response rather than a half-open SSE stream.
func TestIntegration_Streaming_StartFailure(t *testing.T) {
	srv := newOpenAITestServer(t, openaiServerOpts{})

	resp := chatRequest(t, srv, map[string]any{
		"model": "kiro", "messages": userMessage(markerError), "stream": true,
	})
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	if errResp := decodeErrorResponse(t, resp); errResp.Error.Type != "server_error" {
		t.Errorf("type = %q, want server_error", errResp.Error.Type)
	}
}

// TestIntegration_Streaming_ClientDisconnect covers Req 7.3/7.4: abandoning the
// stream must not leak the container or panic the handler.
func TestIntegration_Streaming_ClientDisconnect(t *testing.T) {
	runner := &scriptedRunner{chunkDelay: 50 * time.Millisecond}
	srv := newOpenAITestServer(t, openaiServerOpts{runner: runner})

	ctx, cancel := context.WithCancel(context.Background())
	body, _ := json.Marshal(map[string]any{
		"model": "claude-sonnet", "messages": userMessage(markerSlow), "stream": true,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+testAuthToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}

	// Consume one frame, then hang up mid-stream.
	buf := make([]byte, 256)
	if _, err := resp.Body.Read(buf); err != nil {
		t.Fatalf("read first chunk: %v", err)
	}
	cancel()
	resp.Body.Close()

	// The handler must notice and release the stream.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&runner.streamsClosed) > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("stream was not closed after client disconnect (opened=%d closed=%d)",
		atomic.LoadInt32(&runner.streamsOpened), atomic.LoadInt32(&runner.streamsClosed))
}

// ---------------------------------------------------------------------------
// Backward compatibility (Req 12)
// ---------------------------------------------------------------------------

// TestIntegration_ExistingEndpointsUnchanged covers Req 12.1/12.2: adding the
// /v1/ layer must not disturb the endpoints that were already there.
func TestIntegration_ExistingEndpointsUnchanged(t *testing.T) {
	srv := newOpenAITestServer(t, openaiServerOpts{})

	t.Run("health needs no auth", func(t *testing.T) {
		resp := getRequest(t, srv, "/health", "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("runs listing still works", func(t *testing.T) {
		resp := getRequest(t, srv, "/runs", testAuthToken)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("legacy run endpoint still executes tasks", func(t *testing.T) {
		var buf bytes.Buffer
		json.NewEncoder(&buf).Encode(map[string]any{"prompt": "hello", "agent": "kiro"})
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/run", &buf)
		req.Header.Set("Authorization", "Bearer "+testAuthToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("do request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want 200 or 202\nbody: %s", resp.StatusCode, body)
		}
	})
}

// ---------------------------------------------------------------------------
// Client compatibility: structured content parts and the developer role
// ---------------------------------------------------------------------------

// TestIntegration_ContentParts covers the content-parts array form that the
// OpenAI SDKs and most third-party clients emit.
func TestIntegration_ContentParts(t *testing.T) {
	srv := newOpenAITestServer(t, openaiServerOpts{})

	t.Run("single text part behaves like a plain string", func(t *testing.T) {
		resp := chatRequest(t, srv, map[string]any{
			"model": "kiro",
			"messages": []map[string]any{
				{"role": "user", "content": []map[string]string{{"type": "text", "text": "Hello"}}},
			},
		})
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want 200\nbody: %s", resp.StatusCode, body)
		}
	})

	t.Run("parts are flattened into the agent prompt", func(t *testing.T) {
		resp := chatRequest(t, srv, map[string]any{
			"model": "kiro",
			"messages": []map[string]any{
				{"role": "user", "content": []map[string]string{
					{"type": "text", "text": "first line"},
					{"type": "text", "text": markerEcho},
				}},
			},
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}

		var echo echoPayload
		content := decodeChatResponse(t, resp).Choices[0].Message.Content
		if err := json.Unmarshal([]byte(content), &echo); err != nil {
			t.Fatalf("decode echo payload %q: %v", content, err)
		}
		if echo.Prompt != "first line\n"+markerEcho {
			t.Errorf("prompt = %q, want the parts newline-joined", echo.Prompt)
		}
	})

	t.Run("image parts are rejected with a clear error", func(t *testing.T) {
		resp := chatRequest(t, srv, map[string]any{
			"model": "kiro",
			"messages": []map[string]any{
				{"role": "user", "content": []map[string]any{
					{"type": "image_url", "image_url": map[string]string{"url": "http://example.com/x.png"}},
				}},
			},
		})
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
		errResp := decodeErrorResponse(t, resp)
		if errResp.Error.Type != "invalid_request_error" {
			t.Errorf("type = %q, want invalid_request_error", errResp.Error.Type)
		}
		if !strings.Contains(errResp.Error.Message, "image_url") {
			t.Errorf("message = %q, want it to name the unsupported part type", errResp.Error.Message)
		}
	})

	t.Run("empty parts array fails validation like empty content", func(t *testing.T) {
		resp := chatRequest(t, srv, map[string]any{
			"model": "kiro",
			"messages": []map[string]any{
				{"role": "user", "content": []map[string]string{}},
			},
		})
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
		errResp := decodeErrorResponse(t, resp)
		if errResp.Error.Param == nil || *errResp.Error.Param != "messages[0]" {
			t.Errorf("param = %v, want messages[0]", errResp.Error.Param)
		}
	})
}

// TestIntegration_DeveloperRole: OpenAI's newer name for the system role must
// reach the agent as a system prompt, not be rejected as unknown.
func TestIntegration_DeveloperRole(t *testing.T) {
	srv := newOpenAITestServer(t, openaiServerOpts{})

	resp := chatRequest(t, srv, map[string]any{
		"model": "claude-sonnet",
		"messages": []map[string]string{
			{"role": "developer", "content": "You are terse"},
			{"role": "user", "content": markerEcho},
		},
	})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200\nbody: %s", resp.StatusCode, body)
	}

	var echo echoPayload
	content := decodeChatResponse(t, resp).Choices[0].Message.Content
	if err := json.Unmarshal([]byte(content), &echo); err != nil {
		t.Fatalf("decode echo payload %q: %v", content, err)
	}
	if echo.System != "You are terse" {
		t.Errorf("system = %q, want the developer message to become the system prompt", echo.System)
	}
	if echo.Prompt != markerEcho {
		t.Errorf("prompt = %q, want only the user message", echo.Prompt)
	}
}

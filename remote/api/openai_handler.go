package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"remote/core"
	"remote/docker"
)

// validOpenAIRoles are the message roles accepted in the messages array.
var validOpenAIRoles = map[string]bool{"system": true, "user": true, "assistant": true}

// Request size limits from Req 1.2. They also bound the work a single
// unauthenticated-by-mistake request can create.
const (
	maxModelNameLen       = 256
	maxMessagesPerRequest = 128
)

// OpenAIHandler handles all /v1/ endpoints.
type OpenAIHandler struct {
	registry    *ModelRegistry
	cm          ContainerRunner
	logger      *core.Logger
	taskTimeout time.Duration
}

// NewOpenAIHandler creates an OpenAIHandler.
func NewOpenAIHandler(registry *ModelRegistry, cm ContainerRunner, logger *core.Logger, taskTimeout time.Duration) *OpenAIHandler {
	return &OpenAIHandler{registry: registry, cm: cm, logger: logger, taskTimeout: taskTimeout}
}

// HandleChatCompletions handles POST /v1/chat/completions, dispatching to the
// non-streaming (JSON) or streaming (SSE) path based on req.Stream.
func (h *OpenAIHandler) HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	var req OpenAIChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Covers both malformed JSON and semantically rejected content (an
		// unsupported content part type, say), so the wording stays accurate
		// for either.
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "invalid request body: "+err.Error(), nil, nil)
		return
	}

	if msg, param := validateOpenAIChatRequest(req); msg != "" {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", msg, param, nil)
		return
	}

	entry, ok := h.registry.Resolve(req.Model)
	if !ok {
		code := "model_not_found"
		writeOpenAIError(w, http.StatusNotFound, "invalid_request_error", fmt.Sprintf("model %q not found", req.Model), nil, &code)
		return
	}

	if !h.hasFreeSlot() {
		w.Header().Set("Retry-After", "30")
		writeOpenAIError(w, http.StatusTooManyRequests, "server_error", "server is at capacity, please retry shortly", nil, nil)
		return
	}

	system, prompt := ComposeMessages(req.Messages)
	id := generateCompletionID()
	created := time.Now().Unix()

	if req.Stream {
		h.handleStreamingChatCompletion(w, r, req, entry, system, prompt, id, created)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.taskTimeout)
	defer cancel()

	result, err := h.cm.RunOneShot(ctx, entry.Agent, prompt, entry.Model, system, time.Now(), nil)
	if err != nil {
		h.handleRunError(w, r, err, ctx)
		return
	}

	if result == nil {
		// A runner returning neither a result nor an error violates its
		// contract, but dereferencing nil here would panic mid-request.
		h.logger.Error(r.Context(), "openai: agent returned no result and no error")
		writeOpenAIError(w, http.StatusInternalServerError, "server_error", "agent execution failed", nil, nil)
		return
	}

	if result.ExitCode != 0 {
		msg := result.Stderr
		if msg == "" {
			msg = result.Stdout
		}
		if msg == "" {
			msg = fmt.Sprintf("agent exited with code %d", result.ExitCode)
		}
		h.logger.Error(r.Context(), "openai: agent exited non-zero: "+msg)
		writeOpenAIError(w, http.StatusInternalServerError, "server_error", msg, nil, nil)
		return
	}

	output := result.Stdout
	if entry.Agent == "kiro" {
		output = docker.StripANSI(output)
	}

	promptTokens := estimateTokens(prompt)
	completionTokens := estimateTokens(output)

	resp := OpenAIChatResponse{
		ID:      id,
		Object:  "chat.completion",
		Created: created,
		Model:   req.Model,
		Choices: []OpenAIChoice{
			{
				Index:        0,
				Message:      OpenAIMessage{Role: "assistant", Content: output},
				FinishReason: "stop",
			},
		},
		Usage: OpenAIUsage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
	}

	writeJSON(w, http.StatusOK, resp)
}

// slotReporter is implemented by container runners that can report free
// one-shot task capacity without blocking. *docker.ContainerManager satisfies
// it; test doubles need not.
type slotReporter interface {
	AvailableSlots() int
}

// hasFreeSlot reports whether a task slot is available right now.
//
// Req 7.2 requires an immediate 429 at capacity, but the container manager's
// slot acquisition queues instead of failing fast — without this check a
// saturated server leaves the client hanging for the whole task timeout (ten
// minutes by default) before answering. The check is inherently racy: two
// requests can both observe the last free slot, in which case the loser simply
// falls back to the queuing behaviour and waits. That is the pre-existing
// worst case, not a regression.
func (h *OpenAIHandler) hasFreeSlot() bool {
	reporter, ok := h.cm.(slotReporter)
	if !ok {
		return true
	}
	return reporter.AvailableSlots() > 0
}

// HandleListModels handles GET /v1/models, returning every entry in the
// Model_Registry as an OpenAI model object.
func (h *OpenAIHandler) HandleListModels(w http.ResponseWriter, r *http.Request) {
	entries := h.registry.List()
	data := make([]OpenAIModel, 0, len(entries))
	for _, e := range entries {
		data = append(data, OpenAIModel{
			ID:      e.ID,
			Object:  "model",
			Created: e.Created,
			OwnedBy: "trayline",
		})
	}
	writeJSON(w, http.StatusOK, OpenAIModelList{Object: "list", Data: data})
}

// HandleGetModel handles GET /v1/models/{model_id}, returning the single
// matching model object or a 404 if it is not in the Model_Registry.
func (h *OpenAIHandler) HandleGetModel(w http.ResponseWriter, r *http.Request) {
	modelID := r.PathValue("model_id")
	entry, ok := h.registry.Resolve(modelID)
	if !ok {
		writeOpenAIError(w, http.StatusNotFound, "invalid_request_error", fmt.Sprintf("model %q not found", modelID), nil, nil)
		return
	}
	writeJSON(w, http.StatusOK, OpenAIModel{
		ID:      entry.ID,
		Object:  "model",
		Created: entry.Created,
		OwnedBy: "trayline",
	})
}

// handleRunError translates a RunOneShot error into the appropriate OpenAI
// error response, or no response at all if the client already disconnected.
func (h *OpenAIHandler) handleRunError(w http.ResponseWriter, r *http.Request, err error, ctx context.Context) {
	if r.Context().Err() != nil {
		// Client disconnected; abort without writing a response.
		return
	}
	if strings.Contains(err.Error(), "waiting for a free slot") {
		w.Header().Set("Retry-After", "30")
		writeOpenAIError(w, http.StatusTooManyRequests, "server_error", "server is at capacity, please retry shortly", nil, nil)
		return
	}
	if ctx.Err() != nil {
		writeOpenAIError(w, http.StatusInternalServerError, "server_error", "request timed out", nil, nil)
		return
	}
	h.logger.Error(r.Context(), "openai: agent execution failed: "+err.Error())
	writeOpenAIError(w, http.StatusInternalServerError, "server_error", "agent execution failed", nil, nil)
}

// handleStreamingChatCompletion implements the stream: true path of
// HandleChatCompletions: it starts an incrementally-readable agent container
// via RunOneShotStreaming and forwards its output as SSE chunks until the
// container's output stream ends, the request is cancelled, or the client
// disconnects.
func (h *OpenAIHandler) handleStreamingChatCompletion(w http.ResponseWriter, r *http.Request, req OpenAIChatRequest, entry ModelEntry, system, prompt, id string, created int64) {
	ctx, cancel := context.WithTimeout(r.Context(), h.taskTimeout)
	defer cancel()

	stream, err := h.cm.RunOneShotStreaming(ctx, entry.Agent, prompt, entry.Model, system, time.Now())
	if err != nil {
		h.handleRunError(w, r, err, ctx)
		return
	}
	defer stream.Close(ctx)

	sseWriter, err := NewSSEWriter(w, id, req.Model, created)
	if err != nil {
		h.logger.Error(r.Context(), "openai: response writer does not support streaming: "+err.Error())
		writeOpenAIError(w, http.StatusInternalServerError, "server_error", "streaming is not supported", nil, nil)
		return
	}

	var streamErr error
	if entry.Agent == "kiro" {
		streamErr = streamKiroChunks(ctx, stream.Reader, sseWriter)
	} else {
		streamErr = streamClaudeChunks(ctx, stream.Reader, sseWriter)
	}

	if _, waitErr := stream.Wait(ctx); waitErr != nil {
		h.logger.Warn(r.Context(), "openai: streamed container wait error: "+waitErr.Error())
	}

	if r.Context().Err() != nil {
		// Client already disconnected; nothing more to write.
		return
	}
	if streamErr != nil {
		h.logger.Warn(r.Context(), "openai: streaming output ended with error: "+streamErr.Error())
		_ = sseWriter.WriteError()
		return
	}
	_ = sseWriter.WriteDone()
}

// scanLines reads newline-delimited text from r on a background goroutine.
// The returned channel is closed once r is exhausted or errors; the caller
// must then receive from errCh for the terminal error (nil on clean EOF).
func scanLines(r io.Reader) (lines <-chan string, errCh <-chan error) {
	lineCh := make(chan string, 32)
	errc := make(chan error, 1)
	go func() {
		defer close(lineCh)
		// This runs outside the HTTP handler's stack, so a panic here would
		// take down the whole server process rather than a single request.
		// Turn it into a stream error the caller can terminate cleanly on.
		defer func() {
			if rec := recover(); rec != nil {
				errc <- fmt.Errorf("panic while reading agent output: %v", rec)
			}
		}()
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			lineCh <- scanner.Text()
		}
		errc <- scanner.Err()
	}()
	return lineCh, errc
}

// streamKiroChunks reads kiro's raw TTY output line by line, strips ANSI
// escape sequences, and forwards each line as an SSE chunk.
func streamKiroChunks(ctx context.Context, r io.Reader, sseWriter *SSEWriter) error {
	lineCh, errCh := scanLines(r)
	for {
		select {
		case line, ok := <-lineCh:
			if !ok {
				return <-errCh
			}
			if err := sseWriter.WriteChunk(docker.StripANSI(line) + "\n"); err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// streamClaudeChunks reads claude's NDJSON stream-json output line by line,
// extracting assistant text content and forwarding it as SSE chunks. Other
// NDJSON message types (system, result, rate_limit_event, ...) are ignored.
func streamClaudeChunks(ctx context.Context, r io.Reader, sseWriter *SSEWriter) error {
	lineCh, errCh := scanLines(r)
	for {
		select {
		case line, ok := <-lineCh:
			if !ok {
				return <-errCh
			}
			if text := extractClaudeAssistantText(line); text != "" {
				if err := sseWriter.WriteChunk(text); err != nil {
					return err
				}
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// extractClaudeAssistantText extracts assistant-visible text from one line of
// claude's --output-format stream-json output. Only "assistant" messages'
// text content blocks are surfaced; other message types (system init,
// result, rate_limit_event, ...) carry no user-visible delta.
func extractClaudeAssistantText(line string) string {
	var msg map[string]interface{}
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return ""
	}
	if msg["type"] != "assistant" {
		return ""
	}
	message, _ := msg["message"].(map[string]interface{})
	content, _ := message["content"].([]interface{})

	var sb strings.Builder
	for _, block := range content {
		b, _ := block.(map[string]interface{})
		if b == nil || b["type"] != "text" {
			continue
		}
		if text, _ := b["text"].(string); text != "" {
			sb.WriteString(text)
		}
	}
	return sb.String()
}

// validateOpenAIChatRequest checks required fields and message shape.
// Returns a non-empty msg and the associated param name on failure.
func validateOpenAIChatRequest(req OpenAIChatRequest) (msg string, param *string) {
	if strings.TrimSpace(req.Model) == "" {
		p := "model"
		return "model is required", &p
	}
	if len(req.Model) > maxModelNameLen {
		p := "model"
		return fmt.Sprintf("model name must be at most %d characters", maxModelNameLen), &p
	}
	if len(req.Messages) == 0 {
		p := "messages"
		return "at least one message is required", &p
	}
	if len(req.Messages) > maxMessagesPerRequest {
		p := "messages"
		return fmt.Sprintf("at most %d messages are supported per request", maxMessagesPerRequest), &p
	}
	for i, m := range req.Messages {
		if m.Role == "" {
			p := fmt.Sprintf("messages[%d]", i)
			return fmt.Sprintf("message at index %d is missing \"role\"", i), &p
		}
		if m.Content == "" {
			p := fmt.Sprintf("messages[%d]", i)
			return fmt.Sprintf("message at index %d is missing \"content\"", i), &p
		}
		if !validOpenAIRoles[m.Role] {
			p := fmt.Sprintf("messages[%d].role", i)
			return fmt.Sprintf("role %q is not supported", m.Role), &p
		}
	}
	return "", nil
}

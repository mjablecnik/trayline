# Technical Design: OpenAI-Compatible API Layer

## Overview

This design adds an OpenAI-compatible API layer to the Trayline Remote Server under the `/v1/` prefix. The layer translates standard OpenAI Chat Completions requests into internal agent execution calls, reusing the existing container management, slot allocation, and authentication infrastructure. The existing API remains unchanged.

## Architecture

### Component Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│                      HTTP Request (POST /v1/chat/completions)    │
└───────────────────────────────────┬─────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────┐
│  Existing Middleware Stack (recovery → CORS → ratelimit → auth) │
└───────────────────────────────────┬─────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────┐
│                       OpenAI Handler (NEW)                       │
│  ┌──────────────┐  ┌──────────────┐  ┌───────────────────────┐ │
│  │ ModelRegistry │  │MessageComposer│  │     SSEWriter         │ │
│  └──────┬───────┘  └──────┬───────┘  └───────────┬───────────┘ │
│         │                  │                      │             │
└─────────┼──────────────────┼──────────────────────┼─────────────┘
          │                  │                      │
          ▼                  ▼                      ▼
┌─────────────────────────────────────────────────────────────────┐
│              Existing Infrastructure (unchanged)                  │
│  ┌──────────────────┐  ┌──────────────────────────────────────┐ │
│  │ ContainerManager  │  │ SlotManager (acquireSlot/releaseSlot)│ │
│  │  - RunOneShot()   │  └──────────────────────────────────────┘ │
│  └──────────────────┘                                           │
└─────────────────────────────────────────────────────────────────┘
```

### Request Flow

**Non-streaming:**
1. Client sends `POST /v1/chat/completions` with `stream: false` (or omitted)
2. Middleware validates auth, rate limit
3. `OpenAIHandler.HandleChatCompletions` validates request, resolves model via `ModelRegistry`
4. `MessageComposer.Compose()` transforms messages array → (system, prompt)
5. Acquire slot via `ContainerManager.acquireSlot()`
6. Execute `ContainerManager.RunOneShot(agent, prompt, model, system, ...)`
7. Build OpenAI response object with estimated usage
8. Return JSON response, release slot

**Streaming:**
1. Steps 1–5 same as non-streaming
2. Set SSE headers, flush
3. Start container, attach to stdout
4. For each output chunk: wrap in OpenAI chunk format, write `data: {...}\n\n`, flush
5. On completion: write final chunk with `finish_reason: "stop"`, write `data: [DONE]\n\n`
6. Release slot

## Components and Interfaces

### OpenAIHandler

The main HTTP handler struct for all `/v1/` endpoints. Depends on:
- `ModelRegistry` — resolves model names to agent+model pairs
- `ContainerRunner` interface — executes agent containers (existing interface from `task_handler.go`)
- `*core.Logger` — structured logging

```go
type OpenAIHandler struct {
    registry *ModelRegistry
    cm       ContainerRunner
    logger   *core.Logger
}
```

**Interface Methods:**
- `HandleChatCompletions(w http.ResponseWriter, r *http.Request)` — POST /v1/chat/completions
- `HandleListModels(w http.ResponseWriter, r *http.Request)` — GET /v1/models
- `HandleGetModel(w http.ResponseWriter, r *http.Request)` — GET /v1/models/{model_id}

### ModelRegistry

Read-only after initialization. Maps public model names to internal agent+model pairs.

```go
type ModelRegistry struct {
    entries []ModelEntry
    lookup  map[string]ModelEntry // lowercase(id) → entry
}
```

**Interface Methods:**
- `Resolve(name string) (ModelEntry, bool)` — case-insensitive lookup
- `List() []ModelEntry` — all registered models

### SSEWriter

Wraps `http.ResponseWriter` for streaming OpenAI-format chunks. Requires the writer to implement `http.Flusher`.

```go
type SSEWriter struct {
    w       http.ResponseWriter
    flusher http.Flusher
    id      string
    model   string
    created int64
    first   bool
}
```

**Interface Methods:**
- `WriteChunk(content string) error` — write one content delta
- `WriteDone() error` — write final chunk + `[DONE]`
- `WriteError() error` — graceful termination on error

### MessageComposer

Stateless function (no struct needed):

```go
func ComposeMessages(messages []OpenAIMessage) (system string, prompt string)
```

### ContainerManager Extension

New method added to existing `ContainerManager`:

```go
func (m *ContainerManager) RunOneShotStreaming(ctx context.Context, agent, prompt, model, system string, createdAt time.Time) (*OneShotStream, error)
```

```go
type OneShotStream struct {
    ContainerID string
    Reader      io.Reader
    Closer      io.Closer
    manager     *ContainerManager
}

func (s *OneShotStream) Wait(ctx context.Context) (exitCode int, err error)
func (s *OneShotStream) Close(ctx context.Context)
```

## Data Models

### Request Model

| Field | Type | Required | Validation |
|-------|------|----------|------------|
| `model` | string | Yes | Non-empty, max 256 chars, must exist in ModelRegistry |
| `messages` | []OpenAIMessage | Yes | 1–128 items, each with valid role + content |
| `stream` | bool | No | Default: false |
| `temperature` | float64 | No | Accepted, ignored |
| `top_p` | float64 | No | Accepted, ignored |
| `max_tokens` | int | No | Accepted, ignored |
| `stop` | string/[]string | No | Accepted, ignored |
| `n` | int | No | Accepted, ignored (always returns 1 choice) |
| `presence_penalty` | float64 | No | Accepted, ignored |
| `frequency_penalty` | float64 | No | Accepted, ignored |
| `logit_bias` | object | No | Accepted, ignored |
| `user` | string | No | Accepted, ignored |

### Message Model

| Field | Type | Required | Validation |
|-------|------|----------|------------|
| `role` | string | Yes | One of: "system", "user", "assistant" |
| `content` | string | Yes | Non-empty string |

### Response Model (non-streaming)

```json
{
  "id": "chatcmpl-abc123def456ghij7890klmn",
  "object": "chat.completion",
  "created": 1722345678,
  "model": "kiro",
  "choices": [{
    "index": 0,
    "message": {"role": "assistant", "content": "..."},
    "finish_reason": "stop"
  }],
  "usage": {
    "prompt_tokens": 42,
    "completion_tokens": 128,
    "total_tokens": 170
  }
}
```

### Response Model (streaming chunk)

```json
{
  "id": "chatcmpl-abc123def456ghij7890klmn",
  "object": "chat.completion.chunk",
  "created": 1722345678,
  "model": "kiro",
  "choices": [{
    "index": 0,
    "delta": {"content": "..."},
    "finish_reason": null
  }]
}
```

### Model Entry

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Public model name (e.g., "kiro") |
| `agent` | string | Internal agent identifier ("kiro" or "claude") |
| `model` | string | Model variant (e.g., "sonnet", empty for default) |
| `created` | int64 | Unix timestamp (server start time) |

### Error Model

```json
{
  "error": {
    "message": "Human-readable error description",
    "type": "invalid_request_error",
    "param": "messages",
    "code": null
  }
}
```

## Error Handling

| Condition | HTTP Status | Error Type | Error Code |
|-----------|-------------|------------|------------|
| Missing/invalid auth | 401 | `invalid_request_error` | `invalid_api_key` or null |
| Missing `model` field | 400 | `invalid_request_error` | null |
| Empty `messages` array | 400 | `invalid_request_error` | null |
| Invalid message role | 400 | `invalid_request_error` | null |
| Unknown model name | 404 | `invalid_request_error` | `model_not_found` |
| No available slots | 429 | `server_error` | null |
| Agent execution failure | 500 | `server_error` | null |
| Container crash during stream | Graceful close | N/A (sends stop + [DONE]) | N/A |
| Client disconnects during stream | N/A | No response | N/A |
| Malformed JSON body | 400 | `invalid_request_error` | null |

**Auth error format for /v1/ paths:** The existing `AuthMiddleware` is modified to detect `/v1/` prefix and emit OpenAI-format errors instead of the Trayline format. This ensures SDKs parse auth errors correctly.

**Rate limiter error format for /v1/ paths:** The existing `RateLimiter` middleware is similarly modified — when the request path starts with `/v1/`, it returns the OpenAI error format (`type: "server_error"`) instead of the Trayline format, while keeping the existing `Retry-After` header behavior.

**Request timeout for non-streaming:** Non-streaming requests use the existing `Config.TaskTimeout` (default 10 minutes) as a context deadline. If the agent does not complete within this window, the request returns HTTP 500 with `type: "server_error"` and a message indicating timeout. This prevents indefinite blocking on slow agent responses.

**Streaming error recovery:** If an error occurs mid-stream (after headers are sent), the SSE writer gracefully terminates with a stop chunk + `[DONE]`. The SDK will see the stream end cleanly but with potentially incomplete content.

## Correctness Properties

### Property 1: Model Resolution Idempotency
Given the same model name (case-insensitive), `ModelRegistry.Resolve()` always returns the same `ModelEntry`. The registry is immutable after initialization.
**Validates: Requirements 4.1, 4.2**

### Property 2: Single Choice Invariant
Every response (streaming and non-streaming) contains exactly one choice with `index: 0`. The `n` parameter is accepted but never produces multiple choices.
**Validates: Requirements 8.3, 10.2**

### Property 3: SSE Ordering
Chunks are emitted in the exact order they are received from the container's stdout. No reordering, buffering, or batching occurs.
**Validates: Requirements 2.3**

### Property 4: Slot Balance
For every acquired slot, exactly one release occurs — guaranteed by `defer` in the handler, regardless of success/failure/panic.
**Validates: Requirements 7.1, 7.3**

### Property 5: Stream Termination
Every started SSE stream ends with exactly one `data: [DONE]\n\n` message, regardless of how the stream ends (success, error, or container crash).
**Validates: Requirements 2.5, 2.7**

### Property 6: Backward Compatibility
No existing endpoint behavior changes. The `/v1/` prefix is strictly additive — verified by existing test suite passing unchanged.
**Validates: Requirements 12.1, 12.2, 12.3**

### Property 7: Token Estimation Consistency
`estimateTokens(text)` is a pure function with no side effects. `total_tokens` always equals `prompt_tokens + completion_tokens`.
**Validates: Requirements 8.1, 8.2**

## New Files

| File | Purpose |
|------|---------|
| `api/openai_handler.go` | Main handler: `HandleChatCompletions`, `HandleListModels`, `HandleGetModel` |
| `api/openai_types.go` | Request/response structs matching OpenAI API schema |
| `api/openai_registry.go` | `ModelRegistry` — model name → agent+model mapping |
| `api/openai_composer.go` | `MessageComposer` — messages array → (system, prompt) |
| `api/openai_sse.go` | `SSEWriter` — streaming output in OpenAI chunk format |
| `api/openai_handler_test.go` | Tests for handler, validation, composition, streaming |

## Modified Files

| File | Change |
|------|--------|
| `api/router.go` | Add `openaiH *OpenAIHandler` parameter, register 3 new routes |
| `api/auth.go` | Add `/v1/` prefix check for OpenAI-format auth errors |
| `api/ratelimit.go` | Add `/v1/` prefix check for OpenAI-format rate limit errors |
| `cmd/server/main.go` | Instantiate `OpenAIHandler` with dependencies, pass to `NewRouter` |
| `.env.example` | Add `OPENAI_MODELS` env var documentation |

## Detailed Design

### 1. Data Types (`api/openai_types.go`)

```go
package api

// --- Request types ---

// OpenAIChatRequest is the request body for POST /v1/chat/completions.
type OpenAIChatRequest struct {
    Model            string            `json:"model"`
    Messages         []OpenAIMessage   `json:"messages"`
    Stream           bool              `json:"stream,omitempty"`
    Temperature      *float64          `json:"temperature,omitempty"`
    TopP             *float64          `json:"top_p,omitempty"`
    MaxTokens        *int              `json:"max_tokens,omitempty"`
    Stop             json.RawMessage   `json:"stop,omitempty"`
    N                *int              `json:"n,omitempty"`
    PresencePenalty  *float64          `json:"presence_penalty,omitempty"`
    FrequencyPenalty *float64          `json:"frequency_penalty,omitempty"`
    LogitBias        json.RawMessage   `json:"logit_bias,omitempty"`
    User             string            `json:"user,omitempty"`
}

// OpenAIMessage is a single message in the messages array.
type OpenAIMessage struct {
    Role    string `json:"role"`
    Content string `json:"content"`
}

// --- Response types (non-streaming) ---

// OpenAIChatResponse is the response for POST /v1/chat/completions (non-streaming).
type OpenAIChatResponse struct {
    ID      string              `json:"id"`
    Object  string              `json:"object"`
    Created int64               `json:"created"`
    Model   string              `json:"model"`
    Choices []OpenAIChoice      `json:"choices"`
    Usage   OpenAIUsage         `json:"usage"`
}

// OpenAIChoice is one element in the choices array.
type OpenAIChoice struct {
    Index        int           `json:"index"`
    Message      OpenAIMessage `json:"message"`
    FinishReason string        `json:"finish_reason"`
}

// OpenAIUsage holds estimated token usage.
type OpenAIUsage struct {
    PromptTokens     int `json:"prompt_tokens"`
    CompletionTokens int `json:"completion_tokens"`
    TotalTokens      int `json:"total_tokens"`
}

// --- Response types (streaming) ---

// OpenAIStreamChunk is one SSE chunk for streaming responses.
type OpenAIStreamChunk struct {
    ID      string                `json:"id"`
    Object  string                `json:"object"`
    Created int64                 `json:"created"`
    Model   string                `json:"model"`
    Choices []OpenAIStreamChoice  `json:"choices"`
}

// OpenAIStreamChoice is one element in a streaming chunk's choices array.
type OpenAIStreamChoice struct {
    Index        int              `json:"index"`
    Delta        OpenAIStreamDelta `json:"delta"`
    FinishReason *string          `json:"finish_reason"`
}

// OpenAIStreamDelta holds incremental content in a stream chunk.
type OpenAIStreamDelta struct {
    Role    string `json:"role,omitempty"`
    Content string `json:"content,omitempty"`
}

// --- Error types ---

// OpenAIErrorResponse wraps an OpenAI-format error.
type OpenAIErrorResponse struct {
    Error OpenAIError `json:"error"`
}

// OpenAIError is the error object matching the OpenAI error schema.
type OpenAIError struct {
    Message string  `json:"message"`
    Type    string  `json:"type"`
    Param   *string `json:"param"`
    Code    *string `json:"code"`
}

// --- Models types ---

// OpenAIModelList is the response for GET /v1/models.
type OpenAIModelList struct {
    Object string        `json:"object"`
    Data   []OpenAIModel `json:"data"`
}

// OpenAIModel is a single model object.
type OpenAIModel struct {
    ID      string `json:"id"`
    Object  string `json:"object"`
    Created int64  `json:"created"`
    OwnedBy string `json:"owned_by"`
}
```

### 2. Model Registry (`api/openai_registry.go`)

```go
package api

// ModelEntry maps a public model name to internal agent + model combination.
type ModelEntry struct {
    ID      string // public name (e.g., "kiro", "claude-sonnet")
    Agent   string // internal agent ("kiro" or "claude")
    Model   string // model variant passed to agent (e.g., "sonnet", "" for default)
    Created int64  // Unix timestamp (fixed at server start)
}

// ModelRegistry holds the model mappings. Thread-safe (read-only after init).
type ModelRegistry struct {
    entries []ModelEntry
    lookup  map[string]ModelEntry // lowercase(id) → entry
}

// NewModelRegistry creates a registry from config.
// Config format (env var OPENAI_MODELS):
//   "kiro:kiro:,claude:claude:,claude-sonnet:claude:sonnet"
// Each entry: "public_name:agent:model_variant" (comma-separated)
func NewModelRegistry(config string) *ModelRegistry { ... }

// Resolve finds a model entry by name (case-insensitive).
// Returns (entry, true) if found, (zero, false) if not.
func (r *ModelRegistry) Resolve(name string) (ModelEntry, bool) { ... }

// List returns all registered models.
func (r *ModelRegistry) List() []ModelEntry { ... }
```

**Configuration via environment variable:**

```env
# Format: name:agent:model_variant (comma-separated)
# Empty model_variant means use agent's default model
OPENAI_MODELS=kiro:kiro:,claude:claude:,claude-sonnet:claude:sonnet
```

Default (if `OPENAI_MODELS` is empty or unset):
```
kiro:kiro:,claude:claude:,claude-sonnet:claude:sonnet
```

### 3. Message Composer (`api/openai_composer.go`)

```go
package api

// ComposeMessages transforms an OpenAI messages array into (system, prompt)
// suitable for agent execution via RunOneShot.
//
// Rules:
// - Messages with role "system" are concatenated (newline-separated) → system param
// - If only one "user" message remains (no assistant messages) → content used directly as prompt
// - Multiple user/assistant messages → formatted with role labels:
//     "User:\n{content}\n\nAssistant:\n{content}\n\n..."
// - Adjacent same-role messages are preserved with individual labels
func ComposeMessages(messages []OpenAIMessage) (system string, prompt string) { ... }
```

**Examples:**

Single user message:
```json
[{"role": "user", "content": "Hello"}]
```
→ system: `""`, prompt: `"Hello"`

With system + multi-turn:
```json
[
  {"role": "system", "content": "You are a Go expert"},
  {"role": "user", "content": "What is a goroutine?"},
  {"role": "assistant", "content": "A goroutine is a lightweight thread..."},
  {"role": "user", "content": "How do I use channels?"}
]
```
→ system: `"You are a Go expert"`, prompt:
```
User:
What is a goroutine?

Assistant:
A goroutine is a lightweight thread...

User:
How do I use channels?
```

### 4. SSE Writer (`api/openai_sse.go`)

```go
package api

// SSEWriter handles streaming output in OpenAI-compatible format.
// It wraps an http.ResponseWriter with Flusher support.
type SSEWriter struct {
    w       http.ResponseWriter
    flusher http.Flusher
    id      string // "chatcmpl-..." ID shared across all chunks
    model   string
    created int64
    first   bool // true = next chunk is the first (include role)
}

// NewSSEWriter initializes streaming headers and returns the writer.
// Sets Content-Type: text/event-stream, Cache-Control: no-cache, Connection: keep-alive.
func NewSSEWriter(w http.ResponseWriter, id, model string, created int64) (*SSEWriter, error) { ... }

// WriteChunk writes a single content delta as an SSE event.
// On the first call, includes role: "assistant" in the delta.
func (s *SSEWriter) WriteChunk(content string) error { ... }

// WriteDone writes the final chunk (finish_reason: "stop", empty delta)
// followed by "data: [DONE]\n\n".
func (s *SSEWriter) WriteDone() error { ... }

// WriteError writes a graceful termination (stop + [DONE]) on error.
func (s *SSEWriter) WriteError() error { ... }
```

**Wire format example:**

```
data: {"id":"chatcmpl-abc123def456","object":"chat.completion.chunk","created":1722345678,"model":"kiro","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"},"finish_reason":null}]}

data: {"id":"chatcmpl-abc123def456","object":"chat.completion.chunk","created":1722345678,"model":"kiro","choices":[{"index":0,"delta":{"content":" there!"},"finish_reason":null}]}

data: {"id":"chatcmpl-abc123def456","object":"chat.completion.chunk","created":1722345678,"model":"kiro","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

```

### 5. Handler (`api/openai_handler.go`)

```go
package api

// OpenAIHandler handles all /v1/ endpoints.
type OpenAIHandler struct {
    registry *ModelRegistry
    cm       ContainerRunner  // existing interface (RunOneShot, StopAndRemoveContainer)
    logger   *core.Logger
}

// NewOpenAIHandler creates the handler.
func NewOpenAIHandler(registry *ModelRegistry, cm ContainerRunner, logger *core.Logger) *OpenAIHandler { ... }

// HandleChatCompletions handles POST /v1/chat/completions.
// Supports both streaming (SSE) and non-streaming (JSON) responses.
func (h *OpenAIHandler) HandleChatCompletions(w http.ResponseWriter, r *http.Request) { ... }

// HandleListModels handles GET /v1/models.
func (h *OpenAIHandler) HandleListModels(w http.ResponseWriter, r *http.Request) { ... }

// HandleGetModel handles GET /v1/models/{model_id}.
func (h *OpenAIHandler) HandleGetModel(w http.ResponseWriter, r *http.Request) { ... }
```

#### HandleChatCompletions Flow (non-streaming):

```
1. Parse JSON body → OpenAIChatRequest
2. Validate: model required, messages non-empty, roles valid
3. Resolve model → ModelEntry (or 404)
4. ComposeMessages(req.Messages) → (system, prompt)
5. Generate completion ID: "chatcmpl-" + uuid (no hyphens, truncated to 24+ chars)
6. Acquire slot: cm.acquireSlot() — if fails → 429 with Retry-After: 30
7. Create context with TaskTimeout deadline
8. Run: cm.RunOneShot(ctx, entry.Agent, prompt, entry.Model, system, time.Now(), onStart)
9. Release slot (deferred)
10. Strip ANSI from output if agent is "kiro" (same as existing task_handler)
11. Build OpenAIChatResponse with estimated usage
12. Return 200 JSON (or 500 on timeout/error)
```

#### HandleChatCompletions Flow (streaming):

```
1. Steps 1–5 same as non-streaming
6. Acquire slot
7. Create SSEWriter(w, id, model, created)
8. Start container (RunOneShot variant that streams output)
   - For streaming, we use a modified approach:
     a. Start container with cmd that produces output
     b. Attach to container stdout
     c. Read chunks and write via SSEWriter
   - Implementation note: RunOneShot blocks until completion, so for streaming
     we use a goroutine with pipe-based reading (similar to session_handler pattern)
9. For each stdout line/chunk → sseWriter.WriteChunk(text)
10. On EOF → sseWriter.WriteDone()
11. Release slot (deferred)
```

**Streaming implementation detail:**

For streaming, we cannot use `RunOneShot` directly (it blocks and returns the full output). Instead, we create a container, attach, stream output line-by-line via SSE, then wait for exit. This reuses the existing `createAndStartContainer` + `AttachChatContainer` pattern from `session_handler.go` but in a one-shot context:

```go
// RunOneShotStreaming starts a one-shot container and returns an attached reader
// for incremental output consumption. Caller is responsible for waiting/cleanup.
func (m *ContainerManager) RunOneShotStreaming(ctx context.Context, agent, prompt, model, system string, createdAt time.Time, onStart func(containerID string)) (containerID string, reader io.Reader, err error)
```

This requires a small addition to `docker/container.go` (a new exported method that splits `RunOneShot` into start + attach, without waiting).

### 6. Router Changes (`api/router.go`)

```go
// Add to NewRouter parameter list:
//   openaiH *OpenAIHandler,

// Add after existing route registrations:

// OpenAI-compatible endpoints.
mux.HandleFunc("POST /v1/chat/completions", openaiH.HandleChatCompletions)
mux.HandleFunc("GET /v1/models", openaiH.HandleListModels)
mux.HandleFunc("GET /v1/models/{model_id}", openaiH.HandleGetModel)
```

No changes to existing middleware chain — the new routes go through the same `recovery → CORS → ratelimit → auth → requestID → mux` stack.

### 7. Error Response Helper

```go
// writeOpenAIError writes an OpenAI-format error response.
func writeOpenAIError(w http.ResponseWriter, status int, errType, message string, param, code *string) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(OpenAIErrorResponse{
        Error: OpenAIError{
            Message: message,
            Type:    errType,
            Param:   param,
            Code:    code,
        },
    })
}
```

### 8. ID Generation

```go
// generateCompletionID returns "chatcmpl-" + 24 random alphanumeric characters.
func generateCompletionID() string {
    return "chatcmpl-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24]
}
```

### 9. Token Estimation

```go
// estimateTokens approximates token count as character_count / 4, rounded.
func estimateTokens(text string) int {
    n := len(text)
    return (n + 2) / 4 // round to nearest
}
```

## Configuration

### New Environment Variable

| Variable | Default | Description |
|----------|---------|-------------|
| `OPENAI_MODELS` | `kiro:kiro:,claude:claude:,claude-sonnet:claude:sonnet` | Comma-separated model mappings in format `public_name:agent:model_variant` |

### .env.example Addition

```env
# OpenAI-compatible API model mappings
# Format: public_name:agent:model_variant (comma-separated)
# Empty model_variant uses the agent's default model
OPENAI_MODELS=kiro:kiro:,claude:claude:,claude-sonnet:claude:sonnet
```

## SDK Usage Examples

### Python (openai SDK)

```python
from openai import OpenAI

client = OpenAI(
    base_url="https://trayline-relay.fly.dev/v1",
    api_key="your-api-token"
)

# Non-streaming
response = client.chat.completions.create(
    model="kiro",
    messages=[
        {"role": "system", "content": "You are a Go expert"},
        {"role": "user", "content": "Explain goroutines"}
    ]
)
print(response.choices[0].message.content)

# Streaming
stream = client.chat.completions.create(
    model="claude-sonnet",
    messages=[{"role": "user", "content": "Write a haiku"}],
    stream=True
)
for chunk in stream:
    if chunk.choices[0].delta.content:
        print(chunk.choices[0].delta.content, end="")
```

### JavaScript (openai SDK)

```javascript
import OpenAI from "openai";

const client = new OpenAI({
    baseURL: "https://trayline-relay.fly.dev/v1",
    apiKey: "your-api-token"
});

const completion = await client.chat.completions.create({
    model: "kiro",
    messages: [{ role: "user", content: "Hello!" }]
});
console.log(completion.choices[0].message.content);
```

## Streaming Implementation Strategy

The current `ContainerManager.RunOneShot` blocks until the container exits and returns the full output. For SSE streaming, we need incremental output. Two approaches:

**Chosen approach: New `RunOneShotStreaming` method**

Add a new method to `ContainerManager` that:
1. Creates and starts the container (same as `RunOneShot`)
2. Attaches to stdout (like `AttachChatContainer`)
3. Returns the attached reader immediately (does NOT wait for exit)
4. Caller reads chunks from the reader and streams them
5. Caller waits for container exit after reader EOF

This minimizes changes to existing code — `RunOneShot` stays unchanged, and the new method shares the same container creation logic.

```go
// In docker/container.go:

// OneShotStream holds the resources for streaming a one-shot container's output.
type OneShotStream struct {
    ContainerID string
    Reader      io.Reader      // demuxed stdout
    Closer      io.Closer      // attached connection
    manager     *ContainerManager
}

// Wait blocks until the container exits and cleans up. Must be called after reading.
func (s *OneShotStream) Wait(ctx context.Context) (exitCode int, err error) { ... }

// Close releases all resources (stop + remove container).
func (s *OneShotStream) Close(ctx context.Context) { ... }

// RunOneShotStreaming starts a one-shot agent container and returns a stream
// for incremental output reading. The caller MUST call Wait() after the reader
// returns EOF, and Close() for cleanup.
func (m *ContainerManager) RunOneShotStreaming(
    ctx context.Context,
    agent, prompt, model, system string,
    createdAt time.Time,
) (*OneShotStream, error) { ... }
```

## Non-Streaming vs. Streaming Decision

The handler checks `req.Stream`:
- `false` (default): Uses existing `RunOneShot` (blocks, returns full output)
- `true`: Uses new `RunOneShotStreaming` (attaches reader, streams via SSE)

This means non-streaming requests have zero code changes to the existing execution path.

## Concurrency

The OpenAI handler uses the same slot management as existing endpoints:
- Non-streaming: `cm.acquireSlot()` before `RunOneShot`, release on completion (same pattern as `TaskHandler.executeTask`)
- Streaming: `cm.acquireSlot()` before `RunOneShotStreaming`, release after `Wait()` completes

If no slots are available, returns immediately with HTTP 429 (no queuing). This matches existing behavior where `POST /run` returns 503 at capacity. The OpenAI layer returns 429 per OpenAI convention.

## Auth Integration

The existing `AuthMiddleware` already handles `/v1/` paths (it excludes only `/health`). No changes needed. The middleware returns the existing Trayline error format (`{"error": "UNAUTHORIZED", "message": "..."}`), but this is acceptable because:
- OpenAI SDKs check HTTP status code 401 first
- The middleware fires before reaching the handler

However, for full OpenAI SDK compatibility on auth errors, we add a special-case in the auth middleware to detect `/v1/` paths and return OpenAI-format errors:

```go
// In AuthMiddleware, before writing the error:
if strings.HasPrefix(r.URL.Path, "/v1/") {
    writeOpenAIError(w, 401, "invalid_request_error", "...", nil, nil)
    return
}
// else: existing format for backward compat
```

The same pattern is applied to the `RateLimiter` middleware:

```go
// In RateLimiter.Middleware, before writing the error:
if strings.HasPrefix(r.URL.Path, "/v1/") {
    writeOpenAIError(w, 429, "server_error", "Rate limit exceeded, retry after ...", nil, nil)
    return
}
// else: existing format
```

These are minimal, targeted changes in `auth.go` and `ratelimit.go`.

## Testing Strategy

| Test Category | What's Tested |
|---------------|---------------|
| Unit: `openai_registry.go` | Model resolution (case-insensitive), unknown model, empty config |
| Unit: `openai_composer.go` | Single message, multi-turn, system extraction, adjacent same-role |
| Unit: `openai_handler.go` | Validation errors (missing model, empty messages, invalid role) |
| Integration: non-streaming | Full request → RunOneShot → response format |
| Integration: streaming | SSE format, chunk structure, [DONE] terminator |
| Integration: auth | 401 in OpenAI format for /v1/ paths |
| Integration: 429 | Slot exhaustion → Retry-After header |

## Requirements Traceability

| Requirement | Design Component |
|-------------|-----------------|
| Req 1: Chat Completions Endpoint | `OpenAIHandler.HandleChatCompletions`, `OpenAIChatRequest/Response` types |
| Req 2: Streaming via SSE | `SSEWriter`, `RunOneShotStreaming`, `OpenAIStreamChunk` types |
| Req 3: Model Listing | `OpenAIHandler.HandleListModels`, `HandleGetModel` |
| Req 4: Model Name Resolution | `ModelRegistry`, `OPENAI_MODELS` env var |
| Req 5: Authentication | Existing `AuthMiddleware` with `/v1/` OpenAI-format branch |
| Req 6: Error Format | `writeOpenAIError`, `OpenAIErrorResponse` type |
| Req 7: Slot Management | `cm.acquireSlot()` / `releaseSlot()` in handler |
| Req 8: Non-streaming Structure | `OpenAIChatResponse`, `estimateTokens()` |
| Req 9: Request Validation | Validation logic in `HandleChatCompletions` |
| Req 10: Ignored Parameters | `json.RawMessage` fields + struct tags in `OpenAIChatRequest` |
| Req 11: Multi-turn Handling | `ComposeMessages()` |
| Req 12: Existing API Preservation | Additive routes only, no middleware changes for existing paths |

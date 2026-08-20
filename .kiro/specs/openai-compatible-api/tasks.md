# Implementation Plan: OpenAI-Compatible API Layer

## Overview

This plan implements the OpenAI-compatible API layer for the Trayline Remote Server. Tasks are ordered by dependency — types and utilities first, then core components, followed by integration and testing.

## Tasks

- [ ] 1. Create OpenAI request/response types and helper functions in `remote/api/openai_types.go`: OpenAIChatRequest (with Model, Messages, Stream, Temperature, TopP, MaxTokens, Stop as json.RawMessage, N, PresencePenalty, FrequencyPenalty, LogitBias as json.RawMessage, User), OpenAIMessage (Role, Content), OpenAIChatResponse (ID, Object, Created, Model, Choices, Usage), OpenAIChoice (Index, Message, FinishReason), OpenAIUsage (PromptTokens, CompletionTokens, TotalTokens), OpenAIStreamChunk, OpenAIStreamChoice (with FinishReason as *string), OpenAIStreamDelta (Role omitempty, Content omitempty), OpenAIErrorResponse, OpenAIError (Message, Type, Param *string, Code *string), OpenAIModelList, OpenAIModel. Add writeOpenAIError helper, generateCompletionID ("chatcmpl-" + 24 alphanumeric), and estimateTokens ((len+2)/4). Verify: `go build ./api/...`
  - **Requirements addressed:** 1.4, 1.5, 2.2, 6.1, 8.1, 8.2

- [ ] 2. Implement Model Registry in `remote/api/openai_registry.go`: ModelEntry struct (ID, Agent, Model, Created int64), ModelRegistry struct (entries, lookup map), NewModelRegistry(config string) parsing comma-separated "name:agent:model_variant" entries with default "kiro:kiro:,claude:claude:,claude-sonnet:claude:sonnet", Resolve(name) with case-insensitive lookup, List() returning all entries. Log warning if zero entries. Add OPENAI_MODELS to remote/.env.example. Verify: `go build ./api/...`
  - **Requirements addressed:** 4.1, 4.2, 4.4, 4.5

- [ ] 3. Implement Message Composer in `remote/api/openai_composer.go`: ComposeMessages(messages []OpenAIMessage) (system, prompt string) — extract system messages concatenated with newline, single user message passes content directly as prompt, multiple user/assistant messages formatted with "User:\n{content}\n\nAssistant:\n{content}\n\n..." role labels preserving order, adjacent same-role messages preserved with individual labels. Verify: `go build ./api/...`
  - **Requirements addressed:** 11.1, 11.2, 11.3, 11.4, 11.5, 11.6

- [ ] 4. Implement SSE Writer in `remote/api/openai_sse.go`: SSEWriter struct (w, flusher, id, model, created, first bool), NewSSEWriter setting Content-Type text/event-stream + Cache-Control no-cache + Connection keep-alive headers and asserting http.Flusher, WriteChunk(content) marshaling OpenAIStreamChunk with delta.content (first call includes delta.role="assistant") and writing "data: {json}\n\n" then flushing, WriteDone() writing final chunk with finish_reason="stop" + empty delta + "data: [DONE]\n\n", WriteError() same as WriteDone for graceful termination. Verify: `go build ./api/...`
  - **Requirements addressed:** 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7

- [ ] 5. Add RunOneShotStreaming to ContainerManager in `remote/docker/container.go`: OneShotStream struct (ContainerID, Reader io.Reader, Closer io.Closer, manager), Wait(ctx) (exitCode int, err error) calling ContainerWait + cleanup, Close(ctx) calling StopAndRemoveContainer. RunOneShotStreaming builds cmd via buildOneShotCmd, acquires slot, creates container, attaches stdout, starts container, returns OneShotStream. For kiro (TTY): raw attached reader. For claude (non-TTY): demuxed via stdcopy pipe. Verify: `go build ./docker/...`
  - **Requirements addressed:** 2.3, 7.3

- [ ] 6. Implement OpenAI Handler (non-streaming) in `remote/api/openai_handler.go`: OpenAIHandler struct (registry, cm ContainerRunner, logger, taskTimeout time.Duration), NewOpenAIHandler constructor. HandleChatCompletions parses JSON body, validates model non-empty (400 param "model"), messages non-empty (400 param "messages"), each message role/content (400 param "messages[i]"), resolves model (404 code "model_not_found"), composes messages, generates ID, acquires slot (429 with Retry-After 30), creates context with taskTimeout deadline, runs RunOneShot, strips ANSI from output if agent is "kiro" (using docker.StripANSI), builds OpenAIChatResponse with estimateTokens usage, returns 200. Handles agent failure → 500 server_error. Handles context deadline exceeded (timeout) → 500 server_error with timeout message. Verify: `go build ./api/...`
  - **Requirements addressed:** 1.1, 1.2, 1.3, 1.6, 1.7, 1.8, 7.1, 7.2, 8.3, 9.1, 9.2, 9.3, 9.4, 10.1, 10.2, 10.3, 10.4

- [ ] 7. Implement streaming path in HandleChatCompletions: when req.Stream is true, after validation and model resolution, acquire slot, call RunOneShotStreaming, create SSEWriter, read from stream.Reader (kiro: read lines + strip ANSI + WriteChunk; claude: parse NDJSON + extract text deltas + WriteChunk), on EOF call WriteDone, on error call WriteError, handle client disconnect via r.Context().Done(), defer stream.Close and slot release. Verify: `go build ./api/...`
  - **Requirements addressed:** 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7, 7.3, 7.4

- [ ] 8. Implement Models endpoints in `openai_handler.go`: HandleListModels returns OpenAIModelList with object "list" and data from registry.List() mapped to OpenAIModel with owned_by "trayline". HandleGetModel reads model_id from path, resolves via registry, returns single OpenAIModel or 404 with type "invalid_request_error". Verify: `go build ./api/...`
  - **Requirements addressed:** 3.1, 3.2, 3.3, 3.4, 3.5

- [ ] 9. Update AuthMiddleware and RateLimiter in `remote/api/auth.go` and `remote/api/ratelimit.go` for /v1/ OpenAI error format: in AuthMiddleware, before writing existing error response, check strings.HasPrefix(r.URL.Path, "/v1/") — if true call writeOpenAIError with type "invalid_request_error" (missing auth: message about missing header, code nil; invalid token: message "Invalid API key provided", code "invalid_api_key") — if false use existing core.ErrorResponse format unchanged. In RateLimiter middleware, same /v1/ prefix check — if true return OpenAI error format with type "server_error" and message about rate limiting (keeping existing Retry-After header) — if false use existing format. Verify existing tests pass: `go test ./api/...`
  - **Requirements addressed:** 5.1, 5.2, 5.3, 5.4, 6.3, 6.5

- [ ] 10. Register routes and wire dependencies: add openaiH *OpenAIHandler parameter to NewRouter in router.go, register POST /v1/chat/completions, GET /v1/models, GET /v1/models/{model_id}. In cmd/server/main.go read OPENAI_MODELS env var, create ModelRegistry, create OpenAIHandler, pass to NewRouter. Verify full build: `go build ./...` and existing tests: `go test ./...`
  - **Requirements addressed:** 12.1, 12.2, 12.3

- [ ] 11. Write unit tests in `remote/api/openai_handler_test.go`: TestComposeMessages_SingleUser, TestComposeMessages_SystemExtraction, TestComposeMessages_MultiTurn, TestComposeMessages_AdjacentSameRole, TestModelRegistry_Resolve (case-insensitive), TestModelRegistry_ResolveNotFound, TestModelRegistry_EmptyConfig (uses defaults), TestHandleChatCompletions_MissingModel (400 param "model"), TestHandleChatCompletions_EmptyMessages (400 param "messages"), TestHandleChatCompletions_InvalidRole (400 param "messages[0].role"), TestHandleChatCompletions_UnknownModel (404 code "model_not_found"), TestHandleChatCompletions_NonStreaming (mock RunOneShot, verify response format), TestHandleChatCompletions_IgnoredParams, TestHandleListModels, TestHandleGetModel, TestEstimateTokens, TestGenerateCompletionID. Run: `go test ./api/... -v`
  - **Requirements addressed:** All requirements (verification)

- [ ] 12. Update API documentation: add OpenAI-compatible API section to `remote/API.md` covering POST /v1/chat/completions (request/response, streaming SSE format), GET /v1/models, GET /v1/models/{model_id}, error format differences, model name mapping table, SDK usage examples (Python, JavaScript). Update remote/README.md to mention OpenAI API availability.
  - **Requirements addressed:** Documentation of all requirements

## Task Dependency Graph

```json
{
  "waves": [
    {"tasks": [1], "description": "Foundation types and helpers"},
    {"tasks": [2, 3, 4], "description": "Core components (registry, composer, SSE writer)"},
    {"tasks": [5], "description": "Container streaming extension"},
    {"tasks": [6, 8, 9], "description": "Handler implementation (non-streaming, models, auth)"},
    {"tasks": [7], "description": "Streaming path implementation"},
    {"tasks": [10], "description": "Route registration and dependency wiring"},
    {"tasks": [11], "description": "Unit tests"},
    {"tasks": [12], "description": "Documentation updates"}
  ]
}
```

## Notes

- Tasks 1–4 are independent of each other and can be implemented in parallel
- Task 5 requires understanding of existing container.go patterns (buildOneShotCmd, createAndStartContainer)
- Task 6 depends on tasks 1, 2, 3 (types, registry, composer)
- Task 7 depends on tasks 4, 5, 6 (sse writer, streaming container, base handler)
- Task 9 (auth) is independent of handler tasks but must be done before task 10
- Task 10 (wiring) requires all handler code to be in place (tasks 6–9)
- Task 11 (tests) should be done after task 10 to test the full integration
- Task 12 (docs) is last as it documents the final implemented behavior

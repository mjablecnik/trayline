# Requirements Document

## Introduction

This feature adds an OpenAI-compatible API layer to the Trayline Remote Server, enabling any standard OpenAI SDK (Python, JavaScript, Go) to connect and interact with the hosted AI agents (Kiro and Claude Code). The new endpoints live under the `/v1/` prefix and implement the OpenAI Chat Completions protocol, including both streaming (SSE) and non-streaming response modes. The existing API remains unchanged — this is purely additive.

## Glossary

- **Completions_Handler**: The HTTP handler responsible for processing POST /v1/chat/completions requests
- **Models_Handler**: The HTTP handler responsible for listing available models via GET /v1/models
- **SSE_Writer**: The component that formats and writes Server-Sent Events for streaming responses
- **Model_Registry**: The mapping layer that translates OpenAI model names to internal agent and model combinations
- **Slot_Manager**: The existing concurrency controller that limits concurrent agent containers (default 2 slots)
- **Container_Manager**: The existing Docker container orchestration layer (RunOneShot, StartChatContainer)
- **Message_Composer**: The component that transforms an OpenAI messages array into a prompt suitable for agent execution

## Requirements

### Requirement 1: Chat Completions Endpoint

**User Story:** As an external developer, I want to send chat completion requests to POST /v1/chat/completions using a standard OpenAI SDK, so that I can interact with Trayline agents without custom client code.

#### Acceptance Criteria

1. WHEN a POST request is received at /v1/chat/completions with a JSON body containing a `model` field (non-empty string) and a `messages` array (containing at least 1 message, each with a `role` string of "system", "user", or "assistant" and a `content` string), THE Completions_Handler SHALL return an HTTP 200 response conforming to the OpenAI Chat Completions response schema
2. THE Completions_Handler SHALL accept the following request body fields: `model` (string, required, max 256 characters), `messages` (array, required, 1 to 128 messages), `stream` (boolean, optional), `temperature` (number, optional, 0.0 to 2.0), `top_p` (number, optional, 0.0 to 1.0), `max_tokens` (integer, optional, 1 to 4194304), `stop` (string or array of up to 4 strings, optional), `n` (integer, optional, 1 to 5)
3. WHEN the `stream` field is absent or set to false, THE Completions_Handler SHALL return a single JSON response with a `choices` array where each element contains an `index` (integer), a `message` object with `role` set to "assistant" and `content` containing the agent output text, and a `finish_reason` string
4. THE Completions_Handler SHALL generate a unique `id` field prefixed with "chatcmpl-" followed by a random alphanumeric string (at least 24 characters total length) for each response
5. THE Completions_Handler SHALL include the `created` field as a Unix timestamp (seconds since epoch), the `model` field echoing the requested model name, the `object` field set to "chat.completion", and a `usage` object containing `prompt_tokens`, `completion_tokens`, and `total_tokens` as integers
6. WHEN the `messages` array contains multiple messages, THE Message_Composer SHALL compose them into a single prompt in the order provided, prefixing each message's content with its role label so that the agent receives the full conversation history with role attribution intact
7. IF the request body is missing the `model` field, the `messages` array, or contains a message without a valid `role` or `content` field, THEN THE Completions_Handler SHALL return HTTP 400 with a JSON error response containing an `error` object with `message` (human-readable description), `type` set to "invalid_request_error", and `code` indicating the specific validation failure
8. IF the `model` field value does not match any configured agent, THEN THE Completions_Handler SHALL return HTTP 404 with a JSON error response containing an `error` object with `type` set to "invalid_request_error" and `message` indicating the model was not found

### Requirement 2: Streaming Responses via SSE

**User Story:** As an external developer, I want to receive streaming responses in Server-Sent Events format, so that I can display incremental agent output in real time using standard OpenAI SDK streaming.

#### Acceptance Criteria

1. WHEN the `stream` field is set to true, THE Completions_Handler SHALL respond with Content-Type "text/event-stream", Cache-Control "no-cache", and Connection "keep-alive" headers, and send incremental chunks as SSE messages, flushing each chunk to the network immediately after writing
2. THE SSE_Writer SHALL format each chunk as `data: {json}\n\n` where the JSON contains the fields `id` (same "chatcmpl-" prefixed ID for all chunks in a response), `object` set to "chat.completion.chunk", `created` (Unix timestamp), `model` (echoing the requested model name), and a `choices` array with one entry containing `index` set to 0 and a `delta` object
3. THE SSE_Writer SHALL include the incremental `content` field within the `delta` object of each choice, where each chunk contains the next segment of agent output as received from the container's stdout
4. WHEN the agent finishes producing output, THE SSE_Writer SHALL send a final chunk with `finish_reason` set to "stop" and a `delta` containing an empty object
5. WHEN the agent finishes producing output, THE SSE_Writer SHALL send a terminating `data: [DONE]\n\n` message after the final chunk
6. THE SSE_Writer SHALL set the `role` field to "assistant" in the `delta` of the first chunk only
7. IF the agent container exits unexpectedly or an internal error occurs during active streaming, THEN THE SSE_Writer SHALL send a chunk with `finish_reason` set to "stop" and a `delta` containing an empty object, followed by the `data: [DONE]\n\n` terminator, and then close the connection

### Requirement 3: Model Listing Endpoint

**User Story:** As an external developer, I want to query GET /v1/models to discover available models, so that my OpenAI SDK client can validate model availability before sending requests.

#### Acceptance Criteria

1. WHEN a GET request is received at /v1/models, THE Models_Handler SHALL return an HTTP 200 response with Content-Type `application/json`, containing a JSON body with `object` set to "list" and a `data` array of model objects
2. THE Models_Handler SHALL include one model object for each entry in the Model_Registry, with fields `id` (string), `object` set to "model", `created` (Unix timestamp integer), and `owned_by` (string)
3. WHEN a GET request is received at /v1/models/{model_id} and the model_id exists in the Model_Registry, THE Models_Handler SHALL return an HTTP 200 response with Content-Type `application/json`, containing the single model object with fields `id`, `object`, `created`, and `owned_by` matching the registry entry
4. IF the requested model_id does not exist in the Model_Registry, THEN THE Models_Handler SHALL return HTTP 404 with a JSON error object containing `type` set to "invalid_request_error" and a `message` field that includes the unrecognized model_id value
5. IF the Model_Registry contains zero entries, THEN THE Models_Handler SHALL return an HTTP 200 response with `object` set to "list" and `data` set to an empty array

### Requirement 4: Model Name Resolution

**User Story:** As an external developer, I want to use intuitive model names like "kiro" or "claude-sonnet" in my requests, so that I can select the right agent and model combination without knowing internal details.

#### Acceptance Criteria

1. THE Model_Registry SHALL map model name strings to internal agent and model combinations, where each entry associates a model name (string) with an agent identifier and an optional model variant identifier (e.g., "kiro" maps to agent "kiro" with default model, "claude-sonnet" maps to agent "claude" with model "sonnet")
2. WHEN resolving a model name from a request, THE Model_Registry SHALL perform case-insensitive matching against registered entries (e.g., "Kiro", "KIRO", and "kiro" all resolve to the same mapping)
3. IF the `model` field in a chat completions request does not match any entry in the Model_Registry, THEN THE Completions_Handler SHALL return HTTP 404 with an error object containing `type` set to "invalid_request_error" and `code` set to "model_not_found"
4. THE Model_Registry SHALL be configurable via an external configuration file or environment-based mechanism, allowing new model mappings to be added without code changes and without restarting unrelated services
5. IF the Model_Registry configuration is empty or contains no valid entries at startup, THEN THE system SHALL log an error indicating no models are available, and THE Models_Handler SHALL return an empty `data` array for GET /v1/models requests

### Requirement 5: Authentication Compatibility

**User Story:** As an external developer, I want to authenticate using the same Bearer token format that OpenAI SDKs send by default, so that I can configure my SDK with just a base URL and API key.

#### Acceptance Criteria

1. THE Completions_Handler SHALL require an Authorization header with the format "Bearer {token}" where {token} is validated against the configured API_TOKEN using constant-time comparison
2. IF the Authorization header is missing or does not start with "Bearer ", THEN THE Completions_Handler SHALL return HTTP 401 with a JSON body containing an `error` object with `type` set to "invalid_request_error", `message` indicating that the authorization header is missing or malformed, `param` set to null, and `code` set to null
3. IF the Authorization header contains a Bearer token that does not match the configured API_TOKEN, THEN THE Completions_Handler SHALL return HTTP 401 with a JSON body containing an `error` object with `type` set to "invalid_request_error", `message` indicating an invalid API key, `param` set to null, and `code` set to "invalid_api_key"
4. THE Models_Handler SHALL require the same Bearer token authentication as the Completions_Handler, returning identical HTTP 401 error responses for missing or invalid tokens

### Requirement 6: Error Response Format

**User Story:** As an external developer, I want errors returned in the standard OpenAI error format, so that my SDK handles errors automatically without custom error parsing.

#### Acceptance Criteria

1. THE Completions_Handler SHALL return all errors as a JSON body with Content-Type `application/json`, wrapped in an `error` object with fields: `message` (string, human-readable), `type` (string), `param` (string or null), and `code` (string or null)
2. WHEN the request body is malformed or missing required fields, THE Completions_Handler SHALL return HTTP 400 with `type` set to "invalid_request_error" and `param` identifying the problematic field
3. WHEN the server has no available container slots, THE Completions_Handler SHALL return HTTP 429 with `type` set to "server_error", a `message` indicating capacity is full, and a Retry-After header set to 30 seconds
4. IF an internal error occurs during agent execution, THEN THE Completions_Handler SHALL return HTTP 500 with `type` set to "server_error", `param` set to null, and `code` set to null
5. IF the Authorization header is missing or invalid, THEN THE Completions_Handler SHALL return HTTP 401 with `type` set to "invalid_request_error" (consistent with Requirement 5)

### Requirement 7: Concurrency and Slot Management

**User Story:** As a server operator, I want OpenAI API requests to share the same slot-based concurrency pool as existing endpoints, so that the server never exceeds its container limit.

#### Acceptance Criteria

1. THE Completions_Handler SHALL acquire a task slot from the Slot_Manager before starting agent execution, using the same one-shot task slot pool used by existing endpoints
2. WHEN no task slots are available, THE Completions_Handler SHALL return HTTP 429 with a Retry-After header set to 30 seconds and an error body conforming to the OpenAI error format with `type` set to "server_error"
3. THE Completions_Handler SHALL release the acquired task slot after agent execution completes, regardless of whether execution succeeded, failed, or the client disconnected
4. IF the request context is cancelled while waiting for a task slot, THEN THE Completions_Handler SHALL abort slot acquisition without starting agent execution and return no response to the disconnected client

### Requirement 8: Non-streaming Response Structure

**User Story:** As an external developer, I want non-streaming responses to include usage information, so that my SDK can track token consumption metrics.

#### Acceptance Criteria

1. THE Completions_Handler SHALL include a `usage` object in non-streaming responses with fields: `prompt_tokens` (non-negative integer), `completion_tokens` (non-negative integer), and `total_tokens` (non-negative integer equal to `prompt_tokens` + `completion_tokens`)
2. IF exact token counts are unavailable, THEN THE Completions_Handler SHALL estimate `prompt_tokens` by dividing the input prompt character length by 4 (rounded to the nearest integer) and `completion_tokens` by dividing the generated output character length by 4 (rounded to the nearest integer)
3. THE `choices` array in non-streaming responses SHALL contain exactly one choice object with `index` set to 0, `message` containing `role` set to "assistant" and `content` set to the complete agent output (empty string if the agent produced no output), and `finish_reason` set to "stop"

### Requirement 9: Request Validation

**User Story:** As an external developer, I want clear validation errors when my request is malformed, so that I can correct issues during integration development.

#### Acceptance Criteria

1. IF the `model` field is missing or empty, THEN THE Completions_Handler SHALL return HTTP 400 with `param` set to "model" and a message indicating the field is required
2. IF the `messages` array is missing or empty, THEN THE Completions_Handler SHALL return HTTP 400 with `param` set to "messages" and a message indicating at least one message is required
3. IF any message in the `messages` array is missing the `role` or `content` field, THEN THE Completions_Handler SHALL return HTTP 400 with `param` set to "messages[{index}]" (where `{index}` is the zero-based position of the invalid message) and a message identifying which field is missing
4. IF a message in the `messages` array has a `role` value other than "system", "user", or "assistant", THEN THE Completions_Handler SHALL return HTTP 400 with `param` set to "messages[{index}].role" (where `{index}` is the zero-based position) and a message indicating the role value is not supported
5. WHEN the request body passes all validation checks, THE Completions_Handler SHALL accept messages with role values "system", "user", and "assistant" and proceed with processing

### Requirement 10: Ignored Parameters Handling

**User Story:** As an external developer, I want to pass standard OpenAI parameters without errors even if the server does not use them, so that existing OpenAI SDK configurations work without modification.

#### Acceptance Criteria

1. THE Completions_Handler SHALL accept without returning errors and have no effect on response behavior for the following parameters when present in the request body: `temperature`, `top_p`, `max_tokens`, `stop`, `n`, `presence_penalty`, `frequency_penalty`, `logit_bias`, `user`
2. WHEN the `n` parameter is provided with a value greater than 1, THE Completions_Handler SHALL return exactly one choice in the response (the server does not support multiple completions)
3. IF the request body contains parameters not listed in criterion 1 and not recognized by the Completions_Handler, THEN THE Completions_Handler SHALL ignore them without returning an error
4. IF any ignored parameter is provided with an invalid type (e.g., a string where a number is expected), THEN THE Completions_Handler SHALL still accept the request without returning a validation error for that parameter

### Requirement 11: Multi-turn Conversation Handling

**User Story:** As an external developer, I want to send full conversation history in the messages array, so that the agent has context from previous turns when generating its response.

#### Acceptance Criteria

1. WHEN the `messages` array contains one or more messages with role "system", THE Message_Composer SHALL concatenate their content in array order (separated by a single newline) and pass the result as the system prompt parameter to the agent, separate from the user/assistant prompt
2. WHEN the `messages` array contains multiple user and assistant messages, THE Message_Composer SHALL format them into a single prompt string preserving array order, with each message prefixed by a role label (`User:` or `Assistant:`) followed by a newline, the message content, and a blank line separating consecutive messages
3. THE Message_Composer SHALL place the system message content before user/assistant messages in the composed prompt by passing the system content as the agent's system prompt parameter and the formatted user/assistant conversation as the prompt parameter
4. WHEN no system message is present, THE Message_Composer SHALL compose the prompt using only the user and assistant messages and pass an empty system prompt to the agent
5. WHEN the `messages` array contains exactly one user message and no assistant messages, THE Message_Composer SHALL pass that message's content directly as the prompt without role-label formatting
6. IF the `messages` array contains adjacent messages with the same role (two consecutive user or two consecutive assistant messages), THEN THE Message_Composer SHALL preserve them in array order with each message individually prefixed by its role label

### Requirement 12: Existing API Preservation

**User Story:** As an existing API user, I want all current endpoints to continue working exactly as before, so that the new OpenAI layer does not break my existing integrations.

#### Acceptance Criteria

1. THE Server SHALL continue to serve all existing endpoints (POST /run, GET /run/{id}, GET /runs, POST /run/{id}/cancel, GET /chat, GET /chat/{id}, GET /sessions, POST /sessions/{id}/terminate) with unchanged request/response schemas and behavior
2. THE Server SHALL register the new /v1/ endpoints additively in the router without modifying the middleware chain or handler logic for existing routes
3. THE Server SHALL apply the same authentication, rate limiting, and CORS middleware to /v1/ endpoints as to existing endpoints, sharing the same middleware stack

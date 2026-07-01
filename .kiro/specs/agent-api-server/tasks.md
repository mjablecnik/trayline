# Implementation Plan: Agent API Server

## Overview

Build a Go HTTP server (`trayline/server/`) exposing REST and WebSocket APIs for programmatic interaction with AI agents. The server manages agent containers via the Docker API, supports one-shot task execution with long polling, and WebSocket-based chat sessions with streaming output. Implementation follows the same Go patterns as the existing `orchestrator/` module.

## Tasks

- [x] 1. Set up project structure, Go module, and core types
  - [x] 1.1 Initialize Go module and project skeleton
    - Create `server/` directory with `go.mod` (module name `server`, Go 1.23)
    - Add external dependencies: `github.com/docker/docker/client`, `github.com/gorilla/websocket`, `golang.org/x/time/rate`, `github.com/google/uuid`, `github.com/joho/godotenv`, `pgregory.net/rapid`
    - Create `.env.example` with all environment variables from Requirement 18
    - Create `main.go` with placeholder `main()` function
    - _Requirements: 18.1_

  - [x] 1.2 Implement configuration loading and validation (`config.go`)
    - Define `Config` struct with all fields: Port, APIToken, MaxConcurrentTasks, WorkspaceDir, WorkspaceHostDir, SessionTimeout, TaskTimeout, RateLimit, StateDir
    - Implement `LoadConfig()` that reads environment variables with defaults per Requirement 18
    - Validate: APP_PORT (1–65535), MAX_CONCURRENT_TASKS (1–32), API_TOKEN (required, non-empty), duration parsing for SESSION_TIMEOUT and TASK_TIMEOUT, integer parsing for RATE_LIMIT
    - Return descriptive error on invalid values, causing the server to exit with non-zero code
    - _Requirements: 1.1, 1.2, 1.3, 12.5, 13.1, 13.2, 14.1, 14.2, 14.3, 18.1, 18.2, 18.3, 19.2_

  - [x]* 1.3 Write property test for config validation (Property 1)
    - **Property 1: Config validation rejects invalid values**
    - Generate random invalid environment values (non-numeric ports, out-of-range MAX_CONCURRENT_TASKS, unparseable durations) and assert `LoadConfig()` returns an error
    - Generate valid values and assert `LoadConfig()` succeeds
    - **Validates: Requirements 1.3, 12.5, 14.3, 18.3**

  - [x] 1.4 Implement structured JSON logger (`logger.go`)
    - Write newline-delimited JSON to stdout with fields: timestamp (ISO 8601), level (debug/info/warn/error), message, requestId
    - Implement context propagation for requestId via `context.Context`
    - Ensure API_TOKEN and auth header values are never logged
    - _Requirements: 17.1, 17.2, 17.6_

  - [x]* 1.5 Write property test for log format (Property 14)
    - **Property 14: Log entries are valid JSON with required fields**
    - Generate random log events with varying levels and messages, verify each output line is valid JSON with required fields (timestamp, level, message, requestId) and does not contain the API_TOKEN value
    - **Validates: Requirements 17.1, 17.2, 17.6**

  - [x] 1.6 Define data models (`task.go`, `session.go`)
    - Define `TaskStatus` constants: queued, running, completed, failed, cancelled
    - Define `Task` struct with all fields per design (ID, Status, Agent, Prompt, Model, System, OutputFormat, Result, Error, Valid, CreatedAt, CompletedAt, ContainerID, CancelFunc)
    - Define `TaskStore` with thread-safe map (`sync.RWMutex`), FIFO ordering, 100-task cap
    - Define `Session` struct with all fields per design (ID, Agent, Model, System, CreatedAt, LastMessageAt, ContainerID, Conn, ConnMu, Active, CancelFunc)
    - Define `SessionStore` with thread-safe map
    - Define all API request/response types: RunRequest, RunResponse, RunAcceptedResponse, TaskSummary, ErrorResponse, WSClientMessage, WSServerMessage
    - _Requirements: 2.1, 2.5, 5.1, 6.1, 8.5, 9.1_

- [x] 2. Implement middleware and health endpoint
  - [x] 2.1 Implement authentication middleware (`auth.go`)
    - Extract `Authorization: Bearer <token>` header
    - Constant-time comparison against configured API_TOKEN using `crypto/subtle`
    - Return HTTP 401 with JSON error response on failure
    - Skip authentication for `/health` endpoint
    - Apply to WebSocket upgrade requests before connection establishment
    - _Requirements: 15.1, 15.2, 15.3, 15.4, 15.5, 15.6_

  - [x]* 2.2 Write property test for authentication enforcement (Property 12)
    - **Property 12: Authentication enforcement**
    - Generate random tokens (valid and invalid), random endpoints, verify that non-matching tokens always get 401 and `/health` never requires auth
    - **Validates: Requirements 15.1, 15.3, 15.5, 15.6**

  - [x] 2.3 Implement rate limiting middleware (`ratelimit.go`)
    - Token bucket per client IP using `golang.org/x/time/rate`
    - Configurable requests/minute from RATE_LIMIT config
    - Return HTTP 429 with `Retry-After` header when limit exceeded
    - Skip rate limiting for `/health` endpoint
    - Periodic cleanup of stale IP entries
    - _Requirements: 16.1, 16.2, 16.3, 16.4, 16.5_

  - [x]* 2.4 Write property test for rate limiting (Property 13)
    - **Property 13: Rate limiting enforcement**
    - Generate sequences of requests from random IPs, verify that exceeding the limit returns 429 with Retry-After header, and `/health` is always exempt
    - **Validates: Requirements 16.1, 16.3, 16.4, 16.5**

  - [x] 2.5 Implement health endpoint (`health.go`)
    - GET `/health` returns HTTP 200 with `{"status": "ok"}` while accepting traffic
    - Returns HTTP 503 with `{"status": "shutting_down"}` during shutdown
    - No auth required, no rate limiting applied
    - _Requirements: 1.6, 1.7_

- [x] 3. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 4. Implement container manager
  - [x] 4.1 Implement container lifecycle management (`container.go`)
    - Define `ContainerClient` interface abstracting Docker SDK operations (for testability)
    - Implement container creation from `trayline-sandbox` image: connect to `trayline-net`, set `DOCKER_HOST=tcp://trayline-proxy:2375`, mount workspace (using host path), no TTY, `--rm` flag equivalent
    - Implement one-shot command construction: Kiro (`kiro-cli chat --trust-all-tools --no-interactive "<prompt>"`) and Claude (`claude --dangerously-skip-permissions -p "<prompt>"`) with optional `--model` flag
    - Implement chat session command construction: Kiro (`kiro-cli chat --trust-all-tools`) and Claude (`claude --dangerously-skip-permissions`) with optional `--model` and `--system-prompt` handling
    - Implement stdout/stderr capture (up to 1MB) via `ContainerLogs` API
    - Implement container stop (10s grace), remove, and timeout enforcement (TASK_TIMEOUT)
    - Implement concurrency semaphore using buffered channel sized to MAX_CONCURRENT_TASKS
    - Implement FIFO queue for tasks waiting on semaphore
    - Handle container start failures (missing image, daemon unreachable, network failure) → task status "failed" with error message
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7, 4.8, 4.9, 14.1, 14.4, 14.5, 14.6_

  - [x]* 4.2 Write property test for agent command construction (Property 5)
    - **Property 5: Agent command construction**
    - Generate random valid prompts (with special characters, quotes, newlines), agent types, and optional model/system params. Verify the constructed command is well-formed with correct binary, flags, and properly escaped prompt.
    - **Validates: Requirements 4.3**

  - [x]* 4.3 Write property test for concurrency semaphore (Property 10)
    - **Property 10: Concurrency semaphore enforcement**
    - Generate random sequences of task submissions with varying MAX_CONCURRENT_TASKS values. Verify that running container count never exceeds the limit and excess tasks stay queued.
    - **Validates: Requirements 14.1, 14.4**

  - [x]* 4.4 Write property test for FIFO dequeuing (Property 11)
    - **Property 11: FIFO dequeuing order**
    - Generate multiple queued tasks with different creation timestamps. When a slot frees, verify the task with the earliest creation timestamp is dequeued first.
    - **Validates: Requirements 14.6**

- [x] 5. Implement one-shot task handlers
  - [x] 5.1 Implement POST /run handler (`task_handler.go`)
    - Validate request body: require `prompt` (non-empty, max 32000 chars) and `agent` ("kiro" or "claude")
    - Return HTTP 400 with structured error for invalid JSON, missing fields, invalid agent, or prompt too long
    - On valid request: create Task with UUID, status "queued", store in TaskStore
    - Start execution goroutine: acquire semaphore slot, transition to "running", start container, capture output
    - Implement long-poll: hold connection up to 30s, return HTTP 200 with result if complete, HTTP 202 with id+status if timeout
    - Handle output_format validation: json (unmarshal test), text/markdown (always valid), omit `valid` if not specified
    - Pass optional `model` and `system` fields to container command
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7, 2.8, 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8_

  - [x]* 5.2 Write property test for request validation (Property 2)
    - **Property 2: Request validation accepts valid requests and rejects invalid ones**
    - Generate random request bodies (valid and invalid prompts, agents, lengths). Verify valid requests are accepted and invalid ones return 400 with appropriate error message.
    - **Validates: Requirements 2.2, 2.4**

  - [x]* 5.3 Write property test for task creation (Property 3)
    - **Property 3: Valid task submission creates a queued task**
    - Generate valid RunRequests, submit them, verify a Task is created in TaskStore with valid UUID, status "queued", correct agent, and creation timestamp >= request time.
    - **Validates: Requirements 2.1, 2.5**

  - [x]* 5.4 Write property test for output format validation (Property 4)
    - **Property 4: Output format validation**
    - Generate random agent outputs and output_format values. Verify: json format → valid iff json.Unmarshal succeeds; text/markdown → always valid; unspecified → valid field omitted.
    - **Validates: Requirements 3.5, 3.6, 3.7, 3.8**

  - [x] 5.5 Implement GET /run/{id} handler
    - Return task status with appropriate fields based on status per Requirement 5
    - Include `result` only for "completed", `error` only for "failed", `valid` only when output_format was specified and status is "completed"
    - Include `completed_at` only for terminal statuses
    - Return HTTP 404 for non-existent task ID
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5, 5.6_

  - [x]* 5.6 Write property test for task retrieval fields (Property 6)
    - **Property 6: Task retrieval returns correct fields based on status**
    - Generate Tasks with different statuses and output_format settings. Verify GET response includes/omits fields correctly based on status.
    - **Validates: Requirements 5.1, 5.2, 5.3, 5.4, 5.5**

  - [x] 5.7 Implement GET /runs handler
    - Return JSON array of TaskSummary (id, status, agent, created_at)
    - Order by created_at descending (most recent first)
    - Return at most 100 tasks (most recent 100 when total exceeds limit)
    - Return empty array when no tasks exist
    - _Requirements: 6.1, 6.2, 6.3, 6.4_

  - [x]* 5.8 Write property test for task listing (Property 7)
    - **Property 7: Task listing is ordered and bounded**
    - Generate random sets of tasks, verify GET /runs returns them ordered by created_at descending and capped at 100.
    - **Validates: Requirements 6.1, 6.2, 6.4**

  - [x] 5.9 Implement POST /run/{id}/cancel handler
    - If task is "running": stop container within 10s, remove it, transition to "cancelled"
    - If task is "queued": transition to "cancelled" without starting container
    - If task is in terminal status: return HTTP 409
    - If task ID doesn't exist: return HTTP 404
    - Return HTTP 200 with task id and status "cancelled" on success
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5_

  - [x]* 5.10 Write property test for cancel terminal tasks (Property 8)
    - **Property 8: Cancellation of terminal tasks is rejected**
    - Generate tasks in terminal statuses (completed, failed, cancelled). Verify POST /run/{id}/cancel returns 409 and does not modify the task status.
    - **Validates: Requirements 7.4**

- [x] 6. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 7. Implement WebSocket chat session handlers
  - [x] 7.1 Implement WS /chat handler (`session_handler.go`)
    - Validate `agent` query parameter ("kiro" or "claude"), reject with HTTP 400 if missing/invalid
    - Accept optional `model` and `system` query parameters
    - Upgrade connection using gorilla/websocket
    - Start agent container (same image, workspace mount, trayline-net)
    - Create Session in SessionStore with UUID, send `{"type": "session_started", "sessionId": "..."}` to client
    - Implement read loop: handle "message" (forward to container stdin), "interrupt" (signal agent), "terminate" (stop container, remove session, send terminated, close)
    - Implement write loop: stream container stdout as `{"type": "output", "data": "..."}` messages, send `{"type": "done"}` when agent finishes responding
    - Send `{"type": "error", "message": "..."}` on processing errors
    - Handle unexpected client disconnect: disconnect WebSocket but keep session and container active for reconnection (idle timer continues)
    - Handle container start failure: send error and close WebSocket
    - Reject new chat sessions with HTTP 503 when at capacity
    - _Requirements: 8.1, 8.2, 8.3, 8.4, 8.5, 8.6, 8.7, 8.8, 8.9, 8.10, 8.11, 8.12, 8.13, 8.14, 14.5_

  - [x] 7.2 Implement WS /chat/{id} reconnect handler
    - Validate session exists and is active, reject with HTTP 404 if not found/terminated
    - Reject with HTTP 409 if session already has an active WebSocket connection
    - Establish connection and send `{"type": "session_resumed", "sessionId": "..."}`
    - Do NOT replay past messages on reconnect
    - Allow only one client connection per session at a time
    - _Requirements: 10.1, 10.2, 10.3, 10.4, 10.5, 10.6_

  - [x] 7.3 Implement GET /sessions handler
    - Return JSON array of active sessions with session_id, agent, model, created_at, last_message_at
    - Order by last_message_at descending (most recently active first)
    - Return empty array when no active sessions exist
    - _Requirements: 9.1, 9.2, 9.3_

  - [x] 7.4 Implement POST /sessions/{id}/terminate handler
    - Stop and remove the associated Agent_Container, remove session from SessionStore
    - If session has an active WebSocket connection: send `{"type": "terminated"}` before closing it
    - If session exists but has no active WebSocket: stop container and clean up
    - Return HTTP 404 if session doesn't exist or is already terminated
    - Return HTTP 200 with session_id and status "terminated" on success
    - _Requirements: 20.1, 20.2, 20.3, 20.4, 20.5_

  - [x]* 7.5 Write property test for session listing (Property 9)
    - **Property 9: Session listing is ordered by last activity**
    - Generate random sets of active sessions with different last_message_at timestamps. Verify GET /sessions returns them ordered by last_message_at descending.
    - **Validates: Requirements 9.1, 9.2**

  - [x] 7.6 Implement context compaction detection
    - Monitor agent stdout for context compaction indicators during chat sessions
    - Send `{"type": "context_compacted"}` to WebSocket client when detected
    - Omit event silently if detection is unreliable
    - _Requirements: 11.1, 11.2, 11.3_

  - [x] 7.7 Implement session idle timeout
    - Background goroutine checks sessions against SESSION_TIMEOUT
    - Track last received client message timestamp in SessionStore
    - On timeout: stop container, remove session, send `{"type": "terminated"}`, close connection
    - Reset timer on any client message
    - _Requirements: 12.1, 12.2, 12.3, 12.4_

- [x] 8. Implement state persistence and recovery
  - [x] 8.1 Implement state persistence (`state.go`)
    - Define state file format: JSON object with `tasks` array and `sessions` array (per design)
    - Implement atomic write: write to `state.json.tmp`, then `os.Rename` to `state.json` in STATE_DIR
    - Trigger state write on every state change (new task, status change, new session, session termination)
    - Validate STATE_DIR exists and is writable on startup, create if missing
    - _Requirements: 19.1, 19.2, 19.3, 19.12, 19.13_

  - [x] 8.2 Implement startup recovery logic
    - On startup: read state file if it exists, start with empty state if not
    - For each Chat_Session: query Docker API for container existence
    - If container running: re-attach stdin/stdout, mark session active, make available for reconnect
    - If container not running: mark session terminated, remove from store
    - For each One_Shot_Task in "running" status: query Docker for container
    - If container exists: capture output, transition to appropriate terminal status
    - If container missing: transition to "failed" with restart error message
    - _Requirements: 19.4, 19.5, 19.6, 19.7, 19.8, 19.9, 19.10, 19.11_

  - [x]* 8.3 Write property test for state persistence round-trip (Property 15)
    - **Property 15: State persistence round-trip**
    - Generate random valid server states (tasks with valid statuses, sessions with valid container IDs). Write to disk via atomic mechanism, read back, verify identical state structure.
    - **Validates: Requirements 19.1, 19.3, 19.4**

- [x] 9. Implement server lifecycle and router wiring
  - [x] 9.1 Implement HTTP router and middleware chain (`router.go`)
    - Register all routes using `net/http` ServeMux
    - Apply middleware chain: rate limiter → auth → handler
    - Health endpoint bypasses both auth and rate limiting
    - Wire task handlers: POST /run, GET /run/{id}, GET /runs, POST /run/{id}/cancel
    - Wire session handlers: WS /chat, WS /chat/{id}, GET /sessions, POST /sessions/{id}/terminate
    - Add recovery middleware for panic handling (return 500 with generic error)
    - _Requirements: 1.6, 15.6, 16.4_

  - [x] 9.2 Implement server startup and graceful shutdown (`main.go`)
    - Load config, validate workspace directory (create if missing, verify writable)
    - Initialize logger, task store, session store, container manager, state persistence
    - Run startup recovery from state file
    - Start HTTP server on configured port
    - Handle SIGTERM/SIGINT: stop accepting connections, send terminate to all sessions, wait 30s grace period, force-stop remaining containers, exit 0
    - Log startup and shutdown events
    - _Requirements: 1.1, 1.2, 1.4, 1.5, 13.3, 13.4, 17.3, 17.4, 17.5_

  - [x] 9.3 Implement workspace directory validation
    - Resolve from WORKSPACE_DIR environment variable (default: `./workspace`)
    - Verify directory exists and is writable, create if missing
    - Refuse to start with error if directory cannot be created or is not writable
    - Mount into every agent container at consistent internal path
    - Do NOT delete or modify workspace files when containers stop
    - _Requirements: 13.1, 13.2, 13.3, 13.4, 13.5, 13.6_

- [x] 10. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 11. Create Dockerfile and deployment files
  - [x] 11.1 Create multi-stage Dockerfile (`server/Dockerfile`)
    - Build stage: `golang:1.23.6` base, copy go.mod/go.sum first, `go mod download`, copy source, build with `CGO_ENABLED=0`
    - Production stage: `gcr.io/distroless/static`, copy binary from build stage
    - Expose configured port
    - _Requirements: 18.1_

  - [x] 11.2 Create scripts and documentation
    - Create `server/scripts/build.sh` (builds Docker image)
    - Create `server/scripts/start-docker.sh` (builds and runs container locally with .env)
    - Create `server/scripts/stop-docker.sh` (stops and removes container)
    - Create `server/README.md` with setup instructions, environment variables, and usage examples
    - _Requirements: 18.1_

- [x] 12. Final checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties using `pgregory.net/rapid`
- Unit tests validate specific examples and edge cases
- The `ContainerClient` interface enables unit testing without a real Docker daemon
- All code goes in `trayline/server/` as a new Go module following the same patterns as `orchestrator/`

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1"] },
    { "id": 1, "tasks": ["1.2", "1.4", "1.6"] },
    { "id": 2, "tasks": ["1.3", "1.5", "2.1", "2.3", "2.5"] },
    { "id": 3, "tasks": ["2.2", "2.4"] },
    { "id": 4, "tasks": ["4.1"] },
    { "id": 5, "tasks": ["4.2", "4.3", "4.4", "5.1"] },
    { "id": 6, "tasks": ["5.2", "5.3", "5.4", "5.5", "5.7", "5.9"] },
    { "id": 7, "tasks": ["5.6", "5.8", "5.10"] },
    { "id": 8, "tasks": ["7.1", "7.3"] },
    { "id": 9, "tasks": ["7.2", "7.4", "7.5", "7.6"] },
    { "id": 10, "tasks": ["8.1"] },
    { "id": 11, "tasks": ["8.2", "8.3"] },
    { "id": 12, "tasks": ["9.1", "9.3"] },
    { "id": 13, "tasks": ["9.2"] },
    { "id": 14, "tasks": ["11.1", "11.2"] }
  ]
}
```

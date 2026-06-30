# Requirements Document

## Introduction

The Agent API Server adds an HTTP API layer to the existing Trayline infrastructure. Instead of triggering AI agents exclusively through the CLI (`trayline agent`), users can interact with agents programmatically via two distinct operational modes:

1. **One-Shot Mode (REST)** — Submit a prompt via `POST /run`, receive the result synchronously (long polling with 30-second timeout) or via polling with `GET /run/{id}`. The container shuts down after completion. Stateless, no conversation context.
2. **Chat Mode (WebSocket)** — Open a WebSocket session at `WS /chat`, exchange messages with a running agent in real-time with streaming output. The agent maintains conversation context within the session until termination. Sessions can be listed and reconnected.

Both modes share a global workspace directory mounted into every agent container, providing persistent file-based storage across invocations. The server reuses the existing Docker sandbox setup (the `trayline-sandbox` image and docker-socket-proxy).

## Glossary

- **API_Server**: The Go HTTP server exposing the REST and WebSocket API for agent interaction
- **One_Shot_Task**: A single stateless unit of work submitted via REST, including a prompt and agent type, executed in an ephemeral container
- **Chat_Session**: A stateful WebSocket connection to a running agent container, supporting multiple message exchanges with streaming output
- **Agent_Container**: A Docker container running the `trayline-sandbox` image that executes agent work
- **Container_Manager**: The component responsible for creating, starting, stopping, and removing Docker containers
- **Task_Store**: The in-memory data structure that tracks submitted one-shot tasks and their status
- **Session_Store**: The in-memory data structure that tracks active chat sessions and their state
- **Health_Endpoint**: The HTTP endpoint that reports server readiness
- **Request_Validator**: The component that validates incoming API requests before processing
- **Workspace**: The global shared directory mounted into all agent containers, persisting files across container lifecycles
- **Session_Timeout**: The maximum idle duration for a chat session before automatic termination
- **Rate_Limiter**: The component that enforces per-IP request rate limits across all endpoints except the Health_Endpoint
- **Output_Format**: A simple string hint ("json", "text", "markdown") that instructs the agent about the desired response format
- **System_Prompt**: An optional string field providing system-level instructions or context to the agent about how to behave
- **State_File**: The JSON file persisted to disk containing all session and task state, enabling recovery after server restarts
- **State_Dir**: The directory where the State_File is stored, configurable via the `STATE_DIR` environment variable

## Requirements

### Requirement 1: Server Lifecycle

**User Story:** As a developer, I want the API server to start reliably, listen on a configurable port, and shut down gracefully, so that I can deploy it as a long-running service.

#### Acceptance Criteria

1. THE API_Server SHALL listen for HTTP and WebSocket connections on a port specified by the `APP_PORT` environment variable, where `APP_PORT` is a numeric value between 1 and 65535
2. IF the `APP_PORT` environment variable is not set, THEN THE API_Server SHALL listen on port 8080
3. IF the `APP_PORT` environment variable is set to a non-numeric value or a number outside the range 1–65535, THEN THE API_Server SHALL exit immediately with a non-zero exit code and log an error message indicating the invalid port value
4. WHEN a SIGTERM or SIGINT signal is received, THE API_Server SHALL stop accepting new connections, send terminate signals to all active Chat_Sessions, wait up to 30 seconds for in-flight requests and sessions to close, and then exit with code 0
5. IF in-flight requests or sessions have not completed within the 30-second grace period after a SIGTERM or SIGINT signal, THEN THE API_Server SHALL forcibly terminate remaining containers, close remaining connections, and exit with code 0
6. WHILE the API_Server is accepting traffic, THE Health_Endpoint SHALL respond to GET requests at `/health` with HTTP 200 and a JSON body `{"status": "ok"}`
7. WHILE the API_Server is shutting down, THE Health_Endpoint SHALL respond to GET requests at `/health` with HTTP 503 and a JSON body `{"status": "shutting_down"}`

### Requirement 2: One-Shot Task Submission

**User Story:** As a developer, I want to submit a prompt via a POST request and receive the agent's result synchronously or via polling, so that I can integrate agent work into automated pipelines without managing sessions.

#### Acceptance Criteria

1. WHEN a POST request is received at `/run` with a valid JSON body, THE API_Server SHALL create a new One_Shot_Task with a UUID identifier and begin execution
2. THE Request_Validator SHALL require the `prompt` field (non-empty string, maximum 32,000 characters) and `agent` field (one of "kiro" or "claude") in the request body
3. IF a request body is not valid JSON, THEN THE Request_Validator SHALL return HTTP 400 with a JSON error response containing the error code and a message indicating the body is malformed
4. IF a request is missing required fields, contains an invalid `agent` value, or the `prompt` exceeds 32,000 characters, THEN THE Request_Validator SHALL return HTTP 400 with a JSON error response containing the error code and a message indicating which validation failed
5. WHEN a valid task is submitted, THE Task_Store SHALL persist the One_Shot_Task with status "queued", creation timestamp, and all submitted parameters
6. WHERE the `output_format` field is provided (one of "json", "text", "markdown"), THE API_Server SHALL append a format instruction to the prompt context sent to the agent
7. WHERE the `model` field is provided, THE API_Server SHALL pass the model parameter to the agent command
8. WHERE the `system` field is provided (a non-empty string), THE API_Server SHALL pass the system field to the agent as a system-level instruction

### Requirement 3: One-Shot Long Polling Response

**User Story:** As a developer, I want the POST request to return the result immediately when the agent finishes quickly, or return a polling reference when it takes longer, so that I can avoid unnecessary polling for fast tasks.

#### Acceptance Criteria

1. WHEN a POST request is received at `/run`, THE API_Server SHALL hold the HTTP connection open for up to 30 seconds while the agent executes
2. IF the agent completes within 30 seconds, THEN THE API_Server SHALL return HTTP 200 with the full task result in the JSON response body
3. IF the agent does not complete within 30 seconds, THEN THE API_Server SHALL return HTTP 202 with a JSON body containing the `id` field and `status` field set to "running"
4. WHILE the API_Server is holding the connection open during the 30-second window, THE One_Shot_Task SHALL continue executing in the background regardless of the response timing
5. IF the `output_format` field was specified in the request and the task completes within 30 seconds, THEN THE API_Server SHALL include a `valid` boolean field in the HTTP 200 response
6. WHEN the `output_format` is "json", THE API_Server SHALL attempt `json.Unmarshal` on the agent output and set `valid` to true if the output is valid JSON, or false otherwise
7. WHEN the `output_format` is "text" or "markdown", THE API_Server SHALL set `valid` to true
8. IF the `output_format` field was not specified in the request, THEN THE API_Server SHALL omit the `valid` field from the response

### Requirement 4: One-Shot Task Execution

**User Story:** As a developer, I want submitted tasks to be executed inside ephemeral Docker containers that mount the shared workspace, so that agents run in a consistent sandboxed environment and can read/write persistent files.

#### Acceptance Criteria

1. WHEN a One_Shot_Task transitions to status "running", THE Container_Manager SHALL start a new Agent_Container from the `trayline-sandbox` image connected to the `trayline-net` Docker network with the environment variable `DOCKER_HOST` set to `tcp://trayline-proxy:2375`
2. THE Container_Manager SHALL mount the Workspace directory into the Agent_Container at a fixed path accessible to the agent process
3. THE Container_Manager SHALL pass the task prompt to the agent command inside the container as a non-interactive execution with no TTY attached and the `--rm` flag set on the container
4. WHEN the agent process inside the container exits with code 0, THE Container_Manager SHALL capture up to 1 MB of the container stdout as the task result and transition the One_Shot_Task status to "completed"
5. IF the agent process exits with a non-zero code, THEN THE Container_Manager SHALL capture up to 1 MB of the container stderr, transition the One_Shot_Task status to "failed", and store the error output
6. WHEN a One_Shot_Task completes or fails, THE Container_Manager SHALL remove the Agent_Container to free resources
7. THE Container_Manager SHALL enforce a maximum execution time per one-shot task, configurable via the `TASK_TIMEOUT` environment variable with a default of 10 minutes
8. IF the execution timeout is exceeded, THEN THE Container_Manager SHALL stop the container, transition the One_Shot_Task status to "failed" with a timeout error indication, and remove the container
9. IF the Agent_Container fails to start due to a missing image, unavailable Docker daemon, or network creation failure, THEN THE Container_Manager SHALL transition the One_Shot_Task status to "failed" and store an error output indicating the cause of the container start failure

### Requirement 5: One-Shot Task Status and Result Retrieval

**User Story:** As a developer, I want to check the status of a submitted task and retrieve its result when complete, so that I can poll for results in automated workflows.

#### Acceptance Criteria

1. WHEN a GET request is received at `/run/{id}`, THE API_Server SHALL return HTTP 200 with a JSON response containing the One_Shot_Task identifier, current status, agent type, creation timestamp, and completion timestamp (if the task has reached a terminal status)
2. IF the One_Shot_Task status is "completed", THEN THE API_Server SHALL include the agent output in the `result` field of the response
3. IF the One_Shot_Task status is "completed" and `output_format` was specified in the original request, THEN THE API_Server SHALL include the `valid` boolean field in the response using the same validation logic as the long polling response
4. IF the One_Shot_Task status is "failed", THEN THE API_Server SHALL include the error details in the `error` field of the response
5. IF the One_Shot_Task status is "queued" or "running", THEN THE API_Server SHALL omit the `result` and `error` fields from the response
6. IF a GET request references a non-existent task identifier, THEN THE API_Server SHALL return HTTP 404 with a JSON error response containing the error code and a descriptive message

### Requirement 6: One-Shot Task Listing

**User Story:** As a developer, I want to list all submitted one-shot tasks, so that I can monitor the overall state of the system.

#### Acceptance Criteria

1. WHEN a GET request is received at `/runs`, THE API_Server SHALL return HTTP 200 with a JSON array containing each One_Shot_Task's identifier, status, agent type, and creation timestamp
2. THE API_Server SHALL order the task list by creation timestamp, most recent first
3. IF no tasks exist in the Task_Store, THEN THE API_Server SHALL return HTTP 200 with an empty JSON array
4. THE API_Server SHALL return at most 100 tasks in a single response, returning the 100 most recent tasks when the total exceeds this limit

### Requirement 7: One-Shot Task Cancellation

**User Story:** As a developer, I want to cancel a running one-shot task, so that I can stop unnecessary work and free resources.

#### Acceptance Criteria

1. WHEN a POST request is received at `/run/{id}/cancel` and the One_Shot_Task status transitions to "cancelled", THE API_Server SHALL return HTTP 200 with a JSON body containing the task identifier and status "cancelled"
2. IF the One_Shot_Task is in status "running", THEN THE Container_Manager SHALL stop the Agent_Container within 10 seconds, remove it, and transition the One_Shot_Task status to "cancelled"
3. IF the One_Shot_Task is in status "queued", THEN THE Task_Store SHALL transition the One_Shot_Task status to "cancelled" without starting a container
4. IF the One_Shot_Task is already in a terminal status ("completed", "failed", "cancelled"), THEN THE API_Server SHALL return HTTP 409 with a JSON error response indicating the task cannot be cancelled
5. IF the cancel request references a non-existent task identifier, THEN THE API_Server SHALL return HTTP 404 with a JSON error response indicating the task was not found

### Requirement 8: WebSocket Chat Session

**User Story:** As a developer, I want to open a WebSocket connection to an agent, send multiple messages, and receive streaming responses, so that I can have interactive conversations with agents programmatically.

#### Acceptance Criteria

1. WHEN a WebSocket upgrade request is received at `/chat`, THE API_Server SHALL validate the `agent` query parameter (one of "kiro" or "claude") and establish a WebSocket connection
2. IF the `agent` query parameter is missing or invalid, THEN THE API_Server SHALL reject the WebSocket upgrade with HTTP 400
3. WHERE the `model` query parameter is provided in the WebSocket upgrade request, THE API_Server SHALL pass the model parameter to the agent command
4. WHERE the `system` query parameter is provided in the WebSocket upgrade request, THE API_Server SHALL pass the system parameter to the agent as a system-level instruction
5. WHEN a WebSocket connection is established, THE Container_Manager SHALL start a new Agent_Container from the `trayline-sandbox` image with the Workspace directory mounted, connected to the `trayline-net` Docker network, and the Session_Store SHALL create a new Chat_Session with a UUID identifier
6. THE API_Server SHALL send a WebSocket message `{"type": "session_started", "sessionId": "<uuid>"}` to the client upon successful session initialization
7. WHEN a client sends `{"type": "message", "prompt": "..."}`, THE API_Server SHALL forward the prompt to the running agent in the Chat_Session container
8. WHILE the agent is generating a response, THE API_Server SHALL stream output to the client as `{"type": "output", "data": "..."}` messages containing incremental output chunks
9. WHEN the agent finishes responding to a message, THE API_Server SHALL send `{"type": "done"}` to the client
10. WHEN a client sends `{"type": "interrupt"}`, THE API_Server SHALL signal the agent process to stop the current response generation, and the Chat_Session SHALL remain active for further messages
11. WHEN a client sends `{"type": "terminate"}`, THE Container_Manager SHALL stop and remove the Agent_Container, the Session_Store SHALL remove the Chat_Session, and THE API_Server SHALL send `{"type": "terminated"}` to the client before closing the WebSocket connection
12. IF an error occurs during message processing, THEN THE API_Server SHALL send `{"type": "error", "message": "..."}` to the client with a description of the failure
13. IF the WebSocket connection is closed unexpectedly by the client, THEN THE API_Server SHALL disconnect the WebSocket but the Chat_Session SHALL remain active with the Agent_Container still running, available for client reconnection via `/chat/{session_id}`. The Session_Timeout idle timer SHALL continue from the timestamp of the last received client message.
14. IF the Agent_Container fails to start during session initialization, THEN THE API_Server SHALL send `{"type": "error", "message": "..."}` and close the WebSocket connection

### Requirement 9: Chat Session Listing

**User Story:** As a developer, I want to list all active chat sessions, so that I can monitor and manage ongoing conversations.

#### Acceptance Criteria

1. WHEN a GET request is received at `/sessions`, THE API_Server SHALL return HTTP 200 with a JSON array containing each active Chat_Session's session_id, agent type, model, created_at, and last_message_at
2. THE API_Server SHALL order the session list by last_message_at, most recent first
3. IF no active sessions exist in the Session_Store, THEN THE API_Server SHALL return HTTP 200 with an empty JSON array

### Requirement 10: Chat Session Reconnect

**User Story:** As a developer, I want to reconnect to an existing chat session after a disconnection, so that I can resume a conversation without losing context.

#### Acceptance Criteria

1. WHEN a WebSocket upgrade request is received at `/chat/{session_id}`, THE API_Server SHALL attempt to reconnect the client to the existing Chat_Session
2. IF the Chat_Session exists and is active, THEN THE API_Server SHALL establish the WebSocket connection and send `{"type": "session_resumed", "sessionId": "<session_id>"}` to the client
3. IF the Chat_Session does not exist or has been terminated, THEN THE API_Server SHALL reject the WebSocket upgrade with HTTP 404 and a JSON error response indicating the session was not found or is no longer active
4. THE API_Server SHALL allow only one client connection per Chat_Session at a time
5. IF a client attempts to connect to a Chat_Session that already has an active WebSocket connection, THEN THE API_Server SHALL reject the WebSocket upgrade with HTTP 409 and a JSON error response indicating the session is already in use
6. WHEN a client reconnects to a Chat_Session, THE API_Server SHALL NOT replay past messages to the client

### Requirement 11: Context Compaction Detection

**User Story:** As a developer, I want to be notified when the agent's context is compacted during a chat session, so that I can be aware of potential context loss.

#### Acceptance Criteria

1. WHILE a Chat_Session is active, THE API_Server SHALL monitor the agent stdout for context compaction indicators
2. WHEN a context compaction indicator is detected, THE API_Server SHALL send `{"type": "context_compacted"}` to the WebSocket client
3. IF context compaction cannot be reliably detected from the agent output, THEN THE API_Server SHALL omit the `context_compacted` event without generating an error

### Requirement 12: Session Timeout

**User Story:** As a developer, I want idle chat sessions to be automatically terminated after a configurable duration, so that resources are not wasted on abandoned sessions.

#### Acceptance Criteria

1. THE API_Server SHALL enforce a maximum idle duration for Chat_Sessions, configurable via the `SESSION_TIMEOUT` environment variable with a default of 24 hours
2. WHILE a Chat_Session is active, THE Session_Store SHALL track the timestamp of the last received client message
3. IF no client message is received within the configured Session_Timeout duration, THEN THE Container_Manager SHALL stop and remove the Agent_Container, the Session_Store SHALL remove the Chat_Session, and THE API_Server SHALL send `{"type": "terminated"}` to the client before closing the WebSocket connection
4. WHEN a client sends any message to the Chat_Session, THE Session_Store SHALL reset the idle timer to the full Session_Timeout duration
5. IF the `SESSION_TIMEOUT` environment variable is set to an unparseable value, THEN THE API_Server SHALL refuse to start and log an error message indicating the invalid timeout value

### Requirement 13: Workspace Configuration

**User Story:** As a developer, I want all agent containers to share a persistent workspace directory, so that agents can read and write files that survive container shutdowns.

#### Acceptance Criteria

1. THE API_Server SHALL resolve the Workspace path from the `WORKSPACE_DIR` environment variable
2. IF the `WORKSPACE_DIR` environment variable is not set, THEN THE API_Server SHALL default to `./workspace` relative to the server's working directory
3. WHEN the API_Server starts, THE API_Server SHALL verify that the Workspace directory exists and is writable, creating it if it does not exist
4. IF the Workspace directory cannot be created or is not writable, THEN THE API_Server SHALL refuse to start and log an error message indicating the workspace path issue
5. THE Container_Manager SHALL mount the Workspace directory into every Agent_Container (both One_Shot_Task containers and Chat_Session containers) at the same internal path
6. THE API_Server SHALL NOT delete or modify files in the Workspace directory when containers are stopped or removed

### Requirement 14: Concurrency Control

**User Story:** As a developer, I want the server to limit the number of concurrently running agent containers across both modes, so that the host machine is not overwhelmed.

#### Acceptance Criteria

1. THE API_Server SHALL enforce a maximum number of concurrently running agent containers (combining both One_Shot_Tasks and Chat_Sessions), configurable via the `MAX_CONCURRENT_TASKS` environment variable, accepting integer values between 1 and 32
2. IF the `MAX_CONCURRENT_TASKS` variable is not set, THEN THE API_Server SHALL default to 2 concurrent agents
3. IF the `MAX_CONCURRENT_TASKS` variable is set to a non-integer, zero, or negative value, THEN THE API_Server SHALL reject the configuration and fail to start with an error message indicating the invalid value
4. WHILE the number of running containers equals the maximum, THE API_Server SHALL keep newly submitted One_Shot_Tasks in "queued" status until a slot becomes available
5. WHILE the number of running containers equals the maximum, THE API_Server SHALL reject new WebSocket Chat_Session requests with HTTP 503 and a JSON error response indicating no capacity is available
6. WHEN a running container stops (task completes, fails, is cancelled, or session terminates), THE API_Server SHALL start the next queued One_Shot_Task in FIFO order

### Requirement 15: Authentication

**User Story:** As a developer, I want the API to require a bearer token for all requests, so that unauthorized users cannot trigger agent tasks or open chat sessions.

#### Acceptance Criteria

1. THE API_Server SHALL require a bearer token in the `Authorization` header using the format `Bearer <token>` for all endpoints except `/health`
2. THE API_Server SHALL validate the token by comparing it against the value configured in the `API_TOKEN` environment variable using a constant-time string comparison
3. IF the `Authorization` header is missing, does not use the `Bearer` scheme, or the token does not match the configured value, THEN THE API_Server SHALL return HTTP 401 with a JSON error response containing an `error` code and a `message` field indicating authentication failure
4. IF the `API_TOKEN` environment variable is not set or is empty, THEN THE API_Server SHALL refuse to start and log an error message indicating the token must be configured
5. WHEN a WebSocket upgrade request is received, THE API_Server SHALL validate the bearer token before establishing the connection
6. WHEN a request is received for the `/health` endpoint, THE API_Server SHALL process the request without requiring an `Authorization` header

### Requirement 16: Rate Limiting

**User Story:** As a developer, I want the API to enforce rate limits per client IP, so that no single client can overwhelm the server with excessive requests.

#### Acceptance Criteria

1. THE Rate_Limiter SHALL enforce a maximum number of requests per minute per client IP address, configurable via the `RATE_LIMIT` environment variable
2. IF the `RATE_LIMIT` environment variable is not set, THEN THE Rate_Limiter SHALL default to 60 requests per minute per IP
3. WHEN a client exceeds the configured rate limit, THE API_Server SHALL return HTTP 429 with a JSON error response containing an `error` code and a `message` field, and include a `Retry-After` header indicating the number of seconds until the client can make another request
4. THE Rate_Limiter SHALL NOT apply rate limiting to the `/health` endpoint
5. THE Rate_Limiter SHALL apply rate limiting to all other endpoints including REST and WebSocket upgrade requests

### Requirement 17: Structured Logging

**User Story:** As a developer, I want the server to produce structured JSON logs, so that I can monitor operations and troubleshoot issues.

#### Acceptance Criteria

1. THE API_Server SHALL output log entries as one JSON object per line (newline-delimited JSON) to stdout
2. THE API_Server SHALL include a `timestamp` (ISO 8601 with timezone offset), `level` (one of: debug, info, warn, error), `message` (non-empty string), and `requestId` (unique identifier generated per incoming request or session) field in every log entry
3. WHEN a One_Shot_Task changes status, THE API_Server SHALL log the transition at the "info" level including the task identifier and new status value
4. WHEN a Chat_Session is created or terminated, THE API_Server SHALL log the event at the "info" level including the session identifier and reason for termination (user-initiated, timeout, or error)
5. IF an unhandled error occurs during task execution or session handling, THEN THE API_Server SHALL log the error at the "error" level including the task or session identifier, an error message describing the failure, and a stack trace or error source location
6. THE API_Server SHALL NOT include sensitive data (passwords, authentication tokens, or personal identifiable information) in log entries

### Requirement 18: Environment Configuration

**User Story:** As a developer, I want all server behavior to be configurable via environment variables with sensible defaults, so that I can deploy the server in different environments without code changes.

#### Acceptance Criteria

1. THE API_Server SHALL read configuration from the following environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `APP_PORT` | 8080 | Server port |
| `API_TOKEN` | (required) | Bearer token for auth |
| `MAX_CONCURRENT_TASKS` | 2 | Max parallel agents (both modes) |
| `WORKSPACE_DIR` | ./workspace | Global workspace directory |
| `SESSION_TIMEOUT` | 24h | Auto-terminate idle chat sessions |
| `TASK_TIMEOUT` | 10m | Max execution time for one-shot tasks |
| `RATE_LIMIT` | 60 | Requests per minute per IP |
| `WORKSPACE_HOST_DIR` | (required) | Host filesystem path to workspace directory (used for Docker volume mounts) |
| `STATE_DIR` | /tmp/trayline-server | Directory for persisting server state |

2. IF a required environment variable (`API_TOKEN` or `WORKSPACE_HOST_DIR`) is not set or is empty, THEN THE API_Server SHALL refuse to start and log an error indicating the missing configuration
3. IF any environment variable with a numeric or duration constraint is set to an invalid value, THEN THE API_Server SHALL refuse to start and log an error indicating which variable has an invalid value

### Requirement 19: State Persistence and Session Recovery

**User Story:** As a developer, I want the server to persist its state to disk and recover active sessions after a restart, so that running agent containers are not orphaned and clients can reconnect to previously active sessions.

#### Acceptance Criteria

1. THE API_Server SHALL continuously persist all session and task state to a JSON file located in the directory specified by the `STATE_DIR` environment variable
2. IF the `STATE_DIR` environment variable is not set, THEN THE API_Server SHALL default to `/tmp/trayline-server`
3. WHEN any state change occurs (new task submission, task status change, new session creation, or session termination), THE API_Server SHALL write the full state atomically by writing to a temporary file and renaming it to the target path
4. WHEN the API_Server starts and a persisted state file exists, THE API_Server SHALL read the state file and attempt recovery for each session and task recorded in the file
5. WHEN recovering a Chat_Session, THE API_Server SHALL query the Docker API to determine whether the associated Agent_Container is still running
6. IF the Agent_Container for a recovering Chat_Session is still running, THEN THE API_Server SHALL re-attach to the container stdin and stdout, transition the Chat_Session to active status, and make the session available for client reconnection
7. IF the Agent_Container for a recovering Chat_Session is not running or does not exist, THEN THE API_Server SHALL mark the Chat_Session as terminated in the state file and remove the session from the Session_Store
8. WHEN recovering a One_Shot_Task that was in "running" status, THE API_Server SHALL query the Docker API to determine whether the associated Agent_Container still exists
9. IF the Agent_Container for a recovering One_Shot_Task still exists, THEN THE API_Server SHALL capture the container output and transition the task to the appropriate terminal status based on the container exit code
10. IF the Agent_Container for a recovering One_Shot_Task does not exist, THEN THE API_Server SHALL transition the task status to "failed" with an error message indicating the server restarted and the container was lost
11. IF the persisted state file does not exist when the API_Server starts, THEN THE API_Server SHALL start with an empty state (no sessions, no tasks)
12. WHEN the API_Server starts, THE API_Server SHALL verify that the `STATE_DIR` directory exists and is writable, creating it if it does not exist
13. IF the `STATE_DIR` directory cannot be created or is not writable, THEN THE API_Server SHALL refuse to start and log an error message indicating the state directory path issue


### Requirement 20: REST Session Termination

**User Story:** As a developer, I want to terminate a chat session via a REST endpoint, so that I can stop sessions even when I don't have an active WebSocket connection.

#### Acceptance Criteria

1. WHEN a POST request is received at `/sessions/{session_id}/terminate`, THE Container_Manager SHALL stop and remove the associated Agent_Container and the Session_Store SHALL remove the Chat_Session
2. IF the Chat_Session has an active WebSocket connection, THEN THE API_Server SHALL send `{"type": "terminated"}` to the connected client before closing the WebSocket connection
3. IF the Chat_Session exists but has no active WebSocket connection (disconnected client), THEN THE Container_Manager SHALL stop and remove the Agent_Container and the Session_Store SHALL remove the Chat_Session
4. IF the session_id does not exist or the session is already terminated, THEN THE API_Server SHALL return HTTP 404 with a JSON error response indicating the session was not found
5. WHEN a session is successfully terminated, THE API_Server SHALL return HTTP 200 with a JSON body containing the session_id and status "terminated"

# Requirements Document

## Introduction

An interactive terminal client (CLI tool) for testing and debugging the Trayline agent API server. The client connects to the server's REST and WebSocket endpoints, enabling developers to start chat sessions with AI agents, send messages in real-time, run one-shot tasks, and manage active sessions and tasks — all from the terminal.

## Glossary

- **Client**: The terminal client CLI program that connects to the Trayline API server
- **Server**: The Trayline agent API server exposing REST and WebSocket endpoints
- **Session**: An interactive WebSocket chat session with an AI agent (kiro or claude)
- **Task**: A one-shot prompt execution submitted via REST that returns a result
- **Agent**: An AI model backend, either "kiro" or "claude"
- **Bearer_Token**: The API authentication token passed in the Authorization header or as a query parameter for WebSocket connections
- **Output_Stream**: The real-time flow of server messages delivered over WebSocket during a chat session
- **Turn**: A single exchange where the user sends a message and the agent responds until a "done" message is received

## Requirements

### Requirement 1: Server Connection Configuration

**User Story:** As a developer, I want to configure the server URL and authentication token, so that the client can connect to any Trayline server instance.

#### Acceptance Criteria

1. THE Client SHALL accept server URL via `--server` flag, `TRAYLINE_SERVER_URL` environment variable, or `.env` file (priority: flag > env var > .env file > default `http://localhost:8080`)
2. THE Client SHALL accept the Bearer_Token via `--token` flag, `TRAYLINE_API_TOKEN` environment variable, or `.env` file (priority: flag > env var > .env file)
3. IF no Bearer_Token is provided from any source, THEN THE Client SHALL exit with an error message indicating that a token is required and exit code 2
4. IF the resolved server URL (from any source: flag, environment variable, or .env file) does not start with "http://" or "https://", THEN THE Client SHALL exit with an error message indicating the URL scheme is invalid and exit code 2
5. IF the `.env` file does not exist in the current working directory, THEN THE Client SHALL silently continue resolution using the remaining sources (flags, environment variables, and defaults)
6. THE Client SHALL strip any trailing slash from the resolved server URL before using it for requests

### Requirement 2: Health Check

**User Story:** As a developer, I want to verify server connectivity before starting a session, so that I get fast feedback if the server is unreachable.

#### Acceptance Criteria

1. WHEN the `health` subcommand is invoked, THE Client SHALL send a GET request to the `/health` endpoint of the server URL configured via the `TRAYLINE_SERVER_URL` environment variable, with a connection timeout of 5 seconds, and upon receiving HTTP 200 with `{"status": "ok"}`, print "Server is healthy" to stdout and exit with code 0
2. IF the server is unreachable (connection refused, DNS resolution failure, or the 5-second timeout elapses without a response), THEN THE Client SHALL print an error message to stderr indicating the connection failure reason and exit with code 1
3. IF the server returns a non-200 HTTP status (including HTTP 503 with `{"status": "shutting_down"}`), THEN THE Client SHALL print an error message to stderr indicating the received status and exit with code 1

### Requirement 3: Interactive Chat Session

**User Story:** As a developer, I want to start an interactive chat session with an agent, so that I can test real-time conversation flow over WebSocket.

#### Acceptance Criteria

1. WHEN the `chat` subcommand is invoked with a required `--agent` flag (value "kiro" or "claude"), THE Client SHALL open a WebSocket connection to `/chat` with the specified agent, using the server address from the `--server` flag or the `TRAYLINE_SERVER_URL` environment variable
2. WHERE the `--model` option is provided, THE Client SHALL include the model parameter in the WebSocket connection query string
3. WHERE the `--system` option is provided, THE Client SHALL include the system parameter in the WebSocket connection query string
4. WHEN a "session_started" message is received from the Server, THE Client SHALL print the session ID to stderr and display an input prompt indicator (e.g., `> `) on stderr to signal that interactive input mode is active
5. WHILE in interactive input mode, THE Client SHALL read user input line-by-line, ignore empty lines (whitespace-only input), and send each non-empty line as a WebSocket message with type "message"
6. WHEN the Server sends an "output" message, THE Client SHALL write the data field content to stdout immediately without buffering
7. WHEN the Server sends a "done" message, THE Client SHALL print a newline separator to stdout and redisplay the input prompt indicator on stderr to signal the agent turn is complete
8. WHEN the Server sends an "error" message, THE Client SHALL write the error message field to stderr
9. WHEN the Server sends a "context_compacted" message, THE Client SHALL write an informational notice about context compaction to stderr
10. WHEN the user sends an interrupt signal (Ctrl+C) while the Client is waiting for server output (between sending a message and receiving a "done" message), THE Client SHALL send a WebSocket message with type "interrupt" to the Server
11. WHEN the user types the `/quit` command, THE Client SHALL send a WebSocket message with type "terminate" and close the connection gracefully with exit code 0
12. IF the WebSocket connection is closed unexpectedly, THEN THE Client SHALL display a disconnection message to stderr and exit with code 1
13. THE Client SHALL include the authentication token from the `--token` flag or the `TRAYLINE_API_TOKEN` environment variable as a `Bearer` token in the WebSocket upgrade request
14. IF the WebSocket connection cannot be established within 10 seconds, THEN THE Client SHALL display a connection timeout error to stderr and exit with code 1

### Requirement 4: Session Reconnection

**User Story:** As a developer, I want to reconnect to an existing chat session, so that I can resume a conversation after disconnecting.

#### Acceptance Criteria

1. WHEN the `chat` subcommand is invoked with a `--session` flag containing a session ID, THE Client SHALL open a WebSocket connection to `/chat/{id}` to reconnect
2. WHEN a "session_resumed" message is received from the Server, THE Client SHALL display the session ID and enter interactive input mode
3. IF the Server returns HTTP 404 or the session is no longer active, THEN THE Client SHALL display an error message indicating the session was not found or is inactive, and exit with code 1
4. IF the Server returns HTTP 409 indicating the session already has an active connection, THEN THE Client SHALL display an error message indicating the session is in use by another client, and exit with code 1

### Requirement 5: One-Shot Task Execution

**User Story:** As a developer, I want to submit one-shot tasks and see results, so that I can test the non-interactive task execution pipeline.

#### Acceptance Criteria

1. WHEN the `run` subcommand is invoked with a required `--agent` flag and a required `--prompt` flag, THE Client SHALL send a POST request to `/run` with the specified agent and prompt
2. WHERE the `--model` option is provided, THE Client SHALL include the model field in the request body
3. WHERE the `--system` option is provided, THE Client SHALL include the system field in the request body
4. WHERE the `--format` option is provided (values: "json", "text", "markdown"), THE Client SHALL include the output_format field in the request body
5. WHEN the Server responds with HTTP 200 (task completed within long-poll), THE Client SHALL display the task result to stdout, and print the task status and elapsed time (duration between created_at and completed_at) to stderr
6. WHEN the Server responds with HTTP 202 (task still running), THE Client SHALL print the task ID and status to stderr, then poll `GET /run/{id}` every 2 seconds until the task reaches a terminal status ("completed", "failed", or "cancelled"), up to a maximum polling duration of 10 minutes
7. WHEN the task reaches "completed" status, THE Client SHALL display the result field content to stdout
8. WHEN the task reaches "failed" status, THE Client SHALL display the error field content to stderr and exit with code 1
9. IF the `--format` is "json" and the response includes a `valid` field set to false, THEN THE Client SHALL print a warning to stderr indicating that the output did not pass format validation
10. IF the Server is unreachable or returns an HTTP status code other than 200, 202, or 404 during the initial request or any polling request, THEN THE Client SHALL print an error message to stderr indicating the connection or server failure and exit with code 1
11. IF the maximum polling duration of 10 minutes is exceeded without reaching a terminal status, THEN THE Client SHALL print an error message to stderr indicating the polling timeout and exit with code 1
12. WHEN the task reaches "cancelled" status during polling, THE Client SHALL print a message to stderr indicating the task was cancelled and exit with code 1

### Requirement 6: Task Management

**User Story:** As a developer, I want to list and cancel tasks, so that I can monitor and control running task executions.

#### Acceptance Criteria

1. WHEN the `tasks` subcommand is invoked, THE Client SHALL send a GET request to `/runs` and display each returned task as a row in a columnar table with columns: ID, status, agent, and creation time, with one header row followed by one data row per task
2. WHEN the `task` subcommand is invoked with a task ID argument, THE Client SHALL send a GET request to `/run/{id}` and display the task's identifier, status, agent, creation timestamp, completion timestamp (if present), result (if status is "completed"), and error (if status is "failed")
3. WHEN the `cancel` subcommand is invoked with a task ID argument, THE Client SHALL send a POST request to `/run/{id}/cancel` and display the returned task identifier and its updated status
4. IF the task to cancel is already in a terminal status, THEN THE Client SHALL display the conflict error message from the Server and exit with a non-zero exit code
5. IF the `task` or `cancel` subcommand is invoked without a task ID argument, THEN THE Client SHALL display an error message indicating the task ID is required and exit with exit code 2
6. IF the Server returns HTTP 404 for a `task` or `cancel` request, THEN THE Client SHALL display an error message indicating the task was not found and exit with a non-zero exit code
7. IF the Server is unreachable or returns an unexpected HTTP error for any task management subcommand, THEN THE Client SHALL display an error message indicating the connection or server failure and exit with a non-zero exit code

### Requirement 7: Session Management

**User Story:** As a developer, I want to list and terminate sessions, so that I can monitor active sessions and clean up stale ones.

#### Acceptance Criteria

1. WHEN the `sessions` subcommand is invoked, THE Client SHALL send a GET request to `/sessions` and display all active sessions in an aligned columnar table with headers for session ID, agent, model, creation time (formatted as `YYYY-MM-DD HH:MM`), and last message time (formatted as `YYYY-MM-DD HH:MM`)
2. IF the GET `/sessions` response contains an empty JSON array, THEN THE Client SHALL display an informational message indicating no active sessions exist
3. WHEN the `terminate` subcommand is invoked with a session ID argument, THE Client SHALL send a POST request to `/sessions/{id}/terminate` and display the returned session ID and its "terminated" status
4. IF the `terminate` subcommand is invoked without a session ID argument, THEN THE Client SHALL display an error message indicating the session ID is required and exit with a non-zero exit code
5. IF the session to terminate is not found, THEN THE Client SHALL display the not-found error message from the Server and exit with a non-zero exit code
6. IF the Client cannot reach the Server when executing the `sessions` or `terminate` subcommand, THEN THE Client SHALL display an error message indicating the server is unreachable and exit with a non-zero exit code

### Requirement 8: Output Formatting

**User Story:** As a developer, I want readable and colored terminal output, so that I can quickly distinguish between different types of information.

#### Acceptance Criteria

1. THE Client SHALL use colored output to differentiate message types: green for success, red for errors, yellow for warnings, cyan for informational messages
2. WHILE output is piped to a non-TTY destination, THE Client SHALL disable colored output and emit plain unformatted text
3. WHEN the `NO_COLOR` environment variable is set (to any value, including an empty string), THE Client SHALL disable colored output regardless of whether the output destination is a TTY
4. THE Client SHALL format table output for list commands (sessions, tasks) with columns aligned using consistent spacing, where each row displays the item identifier, status, agent type, and creation timestamp
5. WHILE in interactive chat mode, THE Client SHALL display a visible prompt prefix (such as `>` followed by a space) on the input line to distinguish user input from agent output
6. IF a table column value exceeds 36 characters, THEN THE Client SHALL truncate the value and append an ellipsis character to indicate truncation

### Requirement 9: CLI Structure and Help

**User Story:** As a developer, I want standard CLI help and version information, so that I can discover available commands and options.

#### Acceptance Criteria

1. WHEN invoked with `--help` or `-h`, THE Client SHALL display usage information including the program name and one-line description, usage syntax, a list of all subcommands with descriptions, all global flags with descriptions, and at least one usage example
2. WHEN a subcommand is followed by `--help` or `-h`, THE Client SHALL display usage information specific to that subcommand including its syntax, available flags, and at least one usage example
3. WHEN invoked with `--version` or `-v`, THE Client SHALL display the program name followed by the version number in semver format (MAJOR.MINOR.PATCH)
4. IF an unknown subcommand or invalid flag combination is provided, THEN THE Client SHALL print to stderr an error message identifying the unrecognized input, followed by a line suggesting the `--help` flag for usage information, and exit with code 2
5. IF both `--quiet` and `--verbose` flags are provided simultaneously, THEN THE Client SHALL exit with code 2 and print an error message to stderr indicating that these flags are mutually exclusive
6. WHILE the `--quiet` flag is active, THE Client SHALL suppress all informational and progress messages on stderr, outputting only command result data on stdout and error messages on stderr
7. WHILE the `--verbose` flag is active, THE Client SHALL output to stderr the HTTP method and URL for each request, the response status code, response timing in milliseconds, and for WebSocket connections the frame type and payload size of each message sent and received

### Requirement 10: Signal Handling and Graceful Shutdown

**User Story:** As a developer, I want the client to handle interrupts gracefully, so that WebSocket connections and resources are cleaned up properly.

#### Acceptance Criteria

1. WHEN a SIGINT signal is received while not in an active chat session, THE Client SHALL close any open HTTP connections, cancel pending requests, and exit with code 130
2. WHEN a SIGTERM signal is received while not in an active chat session, THE Client SHALL close any open HTTP connections, cancel pending requests, and exit with code 0
3. WHILE in an active chat session, WHEN the first SIGINT signal is received, THE Client SHALL send a `{"type": "interrupt"}` message to the agent via the WebSocket connection and remain connected for further interaction
4. WHILE in an active chat session, WHEN a second SIGINT signal is received after the first was sent as an interrupt to the agent, THE Client SHALL close the WebSocket connection and exit with code 130
5. WHILE in an active chat session, WHEN a SIGTERM signal is received, THE Client SHALL send a `{"type": "terminate"}` message via the WebSocket connection, wait up to 5 seconds for the `{"type": "terminated"}` response, close the WebSocket connection, and exit with code 0

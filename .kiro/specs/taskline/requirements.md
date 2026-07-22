# Requirements Document

## Introduction

Taskline is a sequential command queue server that runs as a daemon within the trayline monorepo. It accepts shell commands via an HTTP API, queues them, and executes them one-by-one in order. Each command must complete successfully before the next one starts. If a command fails (non-zero exit code), the queue halts and a notification is sent. The system consists of two Go binaries:

1. **Server** (`taskline/server/`) — An HTTP server with a worker goroutine that processes the command queue sequentially.
2. **CLI** (`taskline/cli/`) — A command-line client that communicates with the server API.

Queue state is persisted to a JSON file. After a server restart, the queue is loaded but execution does not auto-resume — it waits for a manual "resume" command (however, adding a new task to an idle queue auto-starts execution). Command stdout/stderr goes directly to the server's stdout; the API only exposes task metadata.

## Glossary

- **Taskline_Server**: The Go HTTP daemon that accepts commands via API, queues them, and executes them sequentially via a worker goroutine
- **Taskline_CLI**: The Go command-line binary that communicates with the Taskline_Server HTTP API
- **Task**: A single shell command submitted to the queue, identified by a unique short ID and a human-readable name
- **Queue**: The ordered list of Tasks in pending or running state, processed sequentially by the Worker
- **Worker**: The goroutine inside the Taskline_Server that executes Tasks one at a time in FIFO order
- **Task_ID**: An auto-generated short unique identifier assigned to each Task upon creation
- **Task_Name**: A human-readable identifier for a Task, either user-provided or auto-generated using Docker-style random names (e.g., "brave-tiger")
- **State_File**: The JSON file where the Taskline_Server persists the current Queue state
- **Notification_Service**: The component responsible for sending email notifications via SMTP when a Task fails, configured through SMTP environment variables (SMTP_HOST, SMTP_PORT, SMTP_USER, SMTP_PASSWORD, SMTP_FROM)

## Requirements

### Requirement 1: Server Lifecycle

**User Story:** As a developer, I want the taskline server to start as a daemon, listen on a configurable port, and shut down gracefully, so that I can run it as a long-lived background process.

#### Acceptance Criteria

1. THE Taskline_Server SHALL listen for HTTP connections on a port specified by the `APP_PORT` environment variable, where `APP_PORT` is a numeric value between 1 and 65535
2. IF the `APP_PORT` environment variable is not set, THEN THE Taskline_Server SHALL listen on port 9090
3. IF the `APP_PORT` environment variable is set to a non-numeric value or a number outside the range 1–65535, THEN THE Taskline_Server SHALL exit immediately with a non-zero exit code and log an error message indicating the invalid port value
4. WHEN a SIGTERM or SIGINT signal is received, THE Taskline_Server SHALL stop accepting new connections, wait for the currently running Task to complete (up to 30 seconds), persist the Queue state to the State_File, and exit with code 0
5. IF the currently running Task does not complete within 30 seconds after a SIGTERM or SIGINT signal, THEN THE Taskline_Server SHALL send SIGKILL to terminate the running command process, mark the Task as failed, persist the Queue state, and exit with code 0
6. IF no Task is currently running when a SIGTERM or SIGINT signal is received, THEN THE Taskline_Server SHALL persist the Queue state to the State_File and exit with code 0 within 5 seconds
7. WHEN the Taskline_Server starts, THE Taskline_Server SHALL load the Queue state from the State_File if it exists
8. WHEN the Taskline_Server starts and a State_File exists, THE Taskline_Server SHALL NOT auto-resume execution of the loaded Queue
9. WHEN the Taskline_Server starts and no State_File exists, THE Taskline_Server SHALL initialize with an empty Queue in "idle" state
10. WHEN the Taskline_Server starts and loads a State_File containing a Task with status "running", THE Taskline_Server SHALL transition that Task's status to "failed" with exit code -2, transition the Queue state to "halted", and send a failure notification via the Notification_Service

### Requirement 2: Task Submission

**User Story:** As a developer, I want to add shell commands to the queue via an HTTP API, so that I can schedule work to be executed sequentially.

#### Acceptance Criteria

1. WHEN a POST request is received at `/tasks` with a valid JSON body containing a `command` field (non-empty, non-whitespace-only string), THE Taskline_Server SHALL create a new Task with status "pending", a generated Task_ID, a creation timestamp, and append it to the end of the Queue
2. WHERE the `position` field is provided in the request body as a non-negative integer, THE Taskline_Server SHALL insert the Task at the specified index among pending Tasks (0-based, where 0 means first pending position after any running task)
3. IF the `position` value exceeds the number of pending Tasks, THEN THE Taskline_Server SHALL append the Task to the end of the Queue
4. IF the `position` field is provided but is not a non-negative integer (e.g., negative number, float, or string), THEN THE Taskline_Server SHALL return HTTP 400 with a JSON error response indicating the position value is invalid
5. WHERE the `name` field is provided in the request body (non-empty string), THE Taskline_Server SHALL use the provided value as the Task_Name
6. IF the `name` field is not provided in the request body, THEN THE Taskline_Server SHALL generate a Docker-style random name (adjective-noun format, e.g., "brave-tiger") as the Task_Name
7. IF a request body is missing the `command` field or the `command` field is an empty or whitespace-only string, THEN THE Taskline_Server SHALL return HTTP 400 with a JSON error response containing an error code and a descriptive message
8. IF a request body is not valid JSON, THEN THE Taskline_Server SHALL return HTTP 400 with a JSON error response indicating the body is malformed
9. WHEN a Task is successfully created, THE Taskline_Server SHALL return HTTP 201 with a JSON response containing the Task_ID, Task_Name, command, status, position index, and creation timestamp
10. IF a Task with the same Task_Name already exists in the Queue (among pending, running, or failed tasks), THEN THE Taskline_Server SHALL return HTTP 409 with a JSON error response indicating the name is already in use
11. WHEN a Task is successfully added and the Queue is in "idle" state, THE Taskline_Server SHALL transition the Queue to "running" state

### Requirement 3: Sequential Execution

**User Story:** As a developer, I want commands to execute one at a time in the order they were submitted, so that dependent tasks run in the correct sequence.

#### Acceptance Criteria

1. WHILE the Queue contains pending Tasks and no Task is currently running and the Queue is in "running" state, THE Worker SHALL take the first pending Task from the Queue and begin executing its command
2. WHEN the Worker begins executing a Task, THE Worker SHALL transition the Task status from "pending" to "running"
3. WHEN a running Task's command exits with code 0, THE Worker SHALL remove the Task from the Queue and, if no pending Tasks remain, transition the Queue state to "idle"
4. THE Worker SHALL execute shell commands using `sh -c` to support shell features such as pipes, redirects, and chaining
5. WHILE a Task is running, THE Taskline_Server SHALL pipe the command's stdout and stderr directly to the Taskline_Server process stdout
6. IF the Worker fails to spawn the command process (e.g., `sh` not found, permission denied, resource exhaustion), THEN THE Worker SHALL treat the Task as failed with a non-zero exit code and halt the Queue

### Requirement 4: Failure Handling

**User Story:** As a developer, I want the queue to halt when a command fails and receive an email notification, so that I can investigate and decide how to proceed.

#### Acceptance Criteria

1. WHEN a running Task's command exits with a non-zero exit code, THE Worker SHALL transition the Task status to "failed" and transition the Queue state to "halted"
2. WHILE the Queue is in "halted" state, THE Worker SHALL NOT execute any subsequent pending Tasks
3. WHEN a Task transitions to "failed" status, THE Notification_Service SHALL send an email notification to the address specified by the `NOTIFY_EMAIL` environment variable, containing the Task_Name, Task_ID, the command that failed, and the exit code
4. WHILE the Queue is halted, THE Taskline_Server SHALL continue accepting API requests for queue management (list, delete, retry, skip, update, stop)
5. IF the Notification_Service fails to deliver the email notification, THEN THE Taskline_Server SHALL log the delivery failure at "error" level and continue operating without retrying (the Queue remains in "halted" state regardless of notification outcome)
6. IF the `NOTIFY_EMAIL` environment variable is not set, THEN THE Notification_Service SHALL skip sending email notifications and log a warning at startup indicating that failure notifications are disabled
7. THE Notification_Service SHALL use the following environment variables for SMTP configuration: `SMTP_HOST` (SMTP server hostname), `SMTP_PORT` (SMTP server port), `SMTP_USER` (SMTP authentication username), `SMTP_PASSWORD` (SMTP authentication password), and `SMTP_FROM` (sender email address)
8. IF the `SMTP_FROM` environment variable is not set, THEN THE Notification_Service SHALL use the value of `SMTP_USER` as the sender email address
9. IF any of the environment variables `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, or `SMTP_PASSWORD` is not set, THEN THE Notification_Service SHALL treat notifications as disabled (same behavior as missing `NOTIFY_EMAIL`) and log a warning at startup

### Requirement 5: Task Stop

**User Story:** As a developer, I want to kill the currently running task, so that I can abort a long-running or stuck command without shutting down the server.

#### Acceptance Criteria

1. WHEN a POST request is received at `/tasks/stop`, THE Taskline_Server SHALL send SIGTERM to the running command process, wait up to 5 seconds for the process to exit, and upon termination (either by SIGTERM or SIGKILL) return HTTP 200 with the stopped Task details (Task_ID, Task_Name, command, status, and creation timestamp). THE Taskline_Server SHALL NOT send the HTTP response until the process is confirmed terminated.
2. IF the running command process does not exit within 5 seconds after SIGTERM, THEN THE Taskline_Server SHALL send SIGKILL to force-terminate the process
3. WHEN a task is stopped via the stop endpoint, THE Worker SHALL transition the Task status to "failed" with an exit code of -1, and halt the Queue
4. WHEN a task is stopped via the stop endpoint, THE Notification_Service SHALL send an email notification containing the Task_Name, Task_ID, the command, and the exit code
5. IF no Task is currently in "running" status, THEN THE Taskline_Server SHALL return HTTP 409 with a JSON error response indicating no task is currently running

### Requirement 6: Queue Recovery Operations

**User Story:** As a developer, I want to retry a failed task, skip it, or resume the queue after a restart, so that I can recover from failures and continue processing.

#### Acceptance Criteria

1. WHEN a POST request is received at `/tasks/retry`, THE Taskline_Server SHALL reset the failed Task status to "pending", place it at position 0 among pending Tasks (first to execute next), transition the Queue to "running" state, and return HTTP 200 with the Task details
2. IF a retry request is received and no Task is in "failed" status, THEN THE Taskline_Server SHALL return HTTP 409 with a JSON error response indicating no failed task exists to retry
3. WHEN a POST request is received at `/tasks/skip`, THE Taskline_Server SHALL remove the failed Task from the Queue, transition the Queue to "running" state, and return HTTP 200 with a JSON response containing the skipped Task's Task_ID and Task_Name
4. IF a skip request is received and no Task is in "failed" status, THEN THE Taskline_Server SHALL return HTTP 409 with a JSON error response indicating no failed task exists to skip
5. WHEN a POST request is received at `/queue/resume`, THE Taskline_Server SHALL transition the Queue to "running" state and begin executing the next pending Task
6. IF a resume request is received and the Queue is already in "running" state, THEN THE Taskline_Server SHALL return HTTP 409 with a JSON error response indicating the queue is already running
7. IF a resume request is received and no pending Tasks exist in the Queue, THEN THE Taskline_Server SHALL transition the Queue to "idle" state and return HTTP 200 with a JSON response indicating the queue is empty
8. IF a resume request is received while the Queue is "halted" (failed task exists), THEN THE Taskline_Server SHALL return HTTP 409 with a JSON error response indicating the queue is halted due to a failed task and suggesting retry or skip

### Requirement 7: Task Listing

**User Story:** As a developer, I want to list all tasks in the queue with their status, so that I can see what is pending, running, or failed.

#### Acceptance Criteria

1. WHEN a GET request is received at `/tasks`, THE Taskline_Server SHALL return HTTP 200 with a JSON array containing each Task's position index (0-based), Task_ID, Task_Name, command, status, and creation timestamp in RFC 3339 format
2. THE Taskline_Server SHALL include Tasks with status "pending", "running", and "failed" in the list response
3. THE Taskline_Server SHALL NOT include completed Tasks in the list response (completed Tasks are automatically removed from the Queue)
4. THE Taskline_Server SHALL order the task list by Queue position (running task first at index 0, then pending tasks in execution order, then failed task)
5. IF the Queue is empty, THEN THE Taskline_Server SHALL return HTTP 200 with an empty JSON array

### Requirement 8: Task Deletion

**User Story:** As a developer, I want to remove a pending task from the queue by its name or ID, so that I can cancel scheduled work that is no longer needed.

#### Acceptance Criteria

1. WHEN a DELETE request is received at `/tasks/{identifier}` where the identifier matches a pending Task's Task_ID or Task_Name, THE Taskline_Server SHALL remove the Task from the Queue and return HTTP 200 with a JSON response containing the removed Task's Task_ID, Task_Name, command, status, and creation timestamp
2. IF the identifier matches a Task that is in "running" status, THEN THE Taskline_Server SHALL return HTTP 409 with a JSON error response indicating that running tasks cannot be deleted
3. IF the identifier matches a Task that is in "failed" status, THEN THE Taskline_Server SHALL remove the Task from the Queue, transition the Queue state to "idle", and return HTTP 200 with a JSON response containing the removed Task's Task_ID, Task_Name, command, status, and creation timestamp
4. IF the identifier does not match any Task in the Queue, THEN THE Taskline_Server SHALL return HTTP 404 with a JSON error response indicating the task was not found

### Requirement 9: Task Update

**User Story:** As a developer, I want to modify a pending task's command or name, so that I can correct mistakes without removing and re-adding the task.

#### Acceptance Criteria

1. WHEN a PATCH request is received at `/tasks/{identifier}` with a JSON body containing at least one updatable field (`command` or `name`), THE Taskline_Server SHALL update the matching pending Task's fields and return HTTP 200 with the updated Task details including Task_ID, Task_Name, command, status, and creation timestamp
2. WHERE the `command` field is provided in the request body (non-empty string), THE Taskline_Server SHALL update the Task's command to the new value
3. WHERE the `name` field is provided in the request body (non-empty string), THE Taskline_Server SHALL update the Task's Task_Name to the new value
4. IF the identifier matches a Task that is in "running" status, THEN THE Taskline_Server SHALL return HTTP 409 with a JSON error response indicating that running tasks cannot be updated
5. IF the identifier matches a Task that is in "failed" status, THEN THE Taskline_Server SHALL return HTTP 409 with a JSON error response indicating that failed tasks cannot be updated
6. IF the identifier does not match any Task in the Queue, THEN THE Taskline_Server SHALL return HTTP 404 with a JSON error response indicating the task was not found
7. IF the new Task_Name is already in use by another Task in the Queue, THEN THE Taskline_Server SHALL return HTTP 409 with a JSON error response indicating the name is already in use
8. IF the request body is not valid JSON, THEN THE Taskline_Server SHALL return HTTP 400 with a JSON error response indicating the body is malformed
9. IF the request body is valid JSON but contains no recognized updatable fields (`command` or `name`) or all provided fields are empty strings, THEN THE Taskline_Server SHALL return HTTP 400 with a JSON error response indicating that at least one non-empty field must be provided

### Requirement 10: Queue Status

**User Story:** As a developer, I want to query the current state of the queue, so that I can determine whether it is actively processing, halted, or idle.

#### Acceptance Criteria

1. WHEN a GET request is received at `/queue/status`, THE Taskline_Server SHALL return HTTP 200 with a JSON response containing the fields: `state` (string), `pendingCount` (integer), and optionally `currentTask` or `failedTask` (object)
2. THE Taskline_Server SHALL report the queue state as one of: "running" (actively executing tasks), "halted" (stopped due to a failed task), or "idle" (no tasks to process and not actively executing)
3. IF the Queue state is "halted", THEN THE Taskline_Server SHALL include a `failedTask` object containing the Task_ID, Task_Name, command, and exit code
4. IF the Queue state is "running", THEN THE Taskline_Server SHALL include a `currentTask` object containing the Task_ID, Task_Name, and command
5. IF the Queue state is "idle", THEN THE Taskline_Server SHALL return only the `state` and `pendingCount` fields (pendingCount will be 0)

### Requirement 11: State Persistence

**User Story:** As a developer, I want the queue state to be saved to a file, so that the queue survives server restarts without losing pending tasks.

#### Acceptance Criteria

1. THE Taskline_Server SHALL persist the Queue state (all Tasks and their statuses, plus the queue state) to the State_File as JSON
2. WHEN any state change occurs (task added, task status change, task removed, queue state change), THE Taskline_Server SHALL write the full state atomically by writing to a temporary file and renaming it to the State_File path
3. THE Taskline_Server SHALL resolve the State_File path from the `STATE_FILE` environment variable
4. IF the `STATE_FILE` environment variable is not set, THEN THE Taskline_Server SHALL default to `./taskline-state.json` relative to the server's working directory
5. IF the State_File cannot be written due to permissions or disk errors, THEN THE Taskline_Server SHALL log an error at the "error" level, continue operating with in-memory state, and re-attempt writing on the next state change
6. WHEN the Taskline_Server starts and a State_File exists containing valid JSON that includes the expected fields (queue state and a tasks array where each entry has at minimum a Task_ID, command, and status), THE Taskline_Server SHALL restore the Queue to the persisted state, including all Tasks and the queue state
7. IF the State_File exists but contains invalid JSON or valid JSON that does not match the expected schema, THEN THE Taskline_Server SHALL log a warning, start with an empty Queue, and rename the corrupted file by appending a `.corrupted` suffix

### Requirement 12: CLI Interface

**User Story:** As a developer, I want a command-line tool that mirrors the server API, so that I can manage the task queue from my terminal.

#### Acceptance Criteria

1. THE Taskline_CLI SHALL provide the following subcommands: `add`, `list`, `delete`, `update`, `retry`, `skip`, `stop`, `resume`, `status`
2. WHEN the `add` subcommand is invoked with a command string argument, THE Taskline_CLI SHALL send a POST request to the Taskline_Server `/tasks` endpoint and display the created Task's Task_ID, Task_Name, command, status, and position index
3. WHERE the `--name` flag is provided with the `add` subcommand, THE Taskline_CLI SHALL include the name in the request body
4. WHERE the `--position` flag is provided with the `add` subcommand (non-negative integer), THE Taskline_CLI SHALL include the position in the request body to insert the task at the specified queue index
5. WHEN the `list` subcommand is invoked, THE Taskline_CLI SHALL send a GET request to `/tasks` and display the tasks in a tabular format similar to `docker ps` output, showing position index, Task_ID, Task_Name, command (truncated), status, and creation timestamp
6. WHEN the `delete` subcommand is invoked with a Task identifier argument, THE Taskline_CLI SHALL send a DELETE request to `/tasks/{identifier}` and display the removed Task's Task_ID and Task_Name
7. WHEN the `update` subcommand is invoked with a Task identifier and one or more update flags (`--command`, `--name`), THE Taskline_CLI SHALL send a PATCH request to `/tasks/{identifier}` with the updated fields and display the updated Task's Task_ID, Task_Name, and command
8. WHEN the `retry` subcommand is invoked, THE Taskline_CLI SHALL send a POST request to `/tasks/retry` and display the retried Task's Task_ID, Task_Name, and new status
9. WHEN the `skip` subcommand is invoked, THE Taskline_CLI SHALL send a POST request to `/tasks/skip` and display the skipped Task's Task_ID and Task_Name
10. WHEN the `stop` subcommand is invoked, THE Taskline_CLI SHALL send a POST request to `/tasks/stop` and display the stopped Task's Task_ID, Task_Name, and command
11. WHEN the `resume` subcommand is invoked, THE Taskline_CLI SHALL send a POST request to `/queue/resume` and display the new queue state
12. WHEN the `status` subcommand is invoked, THE Taskline_CLI SHALL send a GET request to `/queue/status` and display the queue state, pending task count, and current task details (Task_ID, Task_Name, command) if a task is running
13. IF the `add` subcommand is invoked without a command string argument, THEN THE Taskline_CLI SHALL print an error message to stderr indicating that a command argument is required and exit with code 2
14. IF the `update` subcommand is invoked without at least one update flag (`--command` or `--name`), THEN THE Taskline_CLI SHALL print an error message to stderr indicating that at least one update flag is required and exit with code 2
15. IF the Taskline_Server returns an error response (HTTP 4xx or 5xx), THEN THE Taskline_CLI SHALL print the error message from the response body to stderr and exit with code 1

### Requirement 13: CLI Configuration

**User Story:** As a developer, I want the CLI to connect to the server on a configurable address, so that I can use it with servers running on different ports or hosts.

#### Acceptance Criteria

1. THE Taskline_CLI SHALL resolve the server address from the `TASKLINE_URL` environment variable
2. IF the `TASKLINE_URL` environment variable is not set, THEN THE Taskline_CLI SHALL default to `http://localhost:9090`
3. IF the `TASKLINE_URL` value does not start with `http://` or `https://`, THEN THE Taskline_CLI SHALL print an error message to stderr indicating the URL scheme is invalid and exit with code 2
4. WHEN the `--help` or `-h` flag is provided, THE Taskline_CLI SHALL print a usage message to stdout listing all subcommands and their options, and exit with code 0
5. WHEN the `--version` or `-v` flag is provided, THE Taskline_CLI SHALL print the program name and version number in semver format (e.g., "taskline 1.0.0") to stdout and exit with code 0
6. IF the Taskline_Server is unreachable (connection refused or no response within 10 seconds), THEN THE Taskline_CLI SHALL print an error message to stderr indicating the connection failed and the server address that was attempted, and exit with code 1

### Requirement 14: CLI Output Format

**User Story:** As a developer, I want the CLI output to be human-readable in a terminal and follow standard conventions, so that I can quickly scan task status.

#### Acceptance Criteria

1. WHEN the `list` subcommand is invoked, THE Taskline_CLI SHALL display tasks in a column-aligned tabular format with a header row and columns in this order: #, ID, NAME, COMMAND, STATUS, CREATED, where each column is padded with trailing spaces to the width of the longest value in that column
2. THE Taskline_CLI SHALL truncate the COMMAND column to 40 display characters, replacing the last character with an ellipsis ("…") when the value exceeds 40 characters
3. THE Taskline_CLI SHALL use colored output for the STATUS column value: green for "running", yellow for "pending", red for "failed"
4. IF the `NO_COLOR` environment variable is set (to any value, including empty) or stdout is not a TTY, THEN THE Taskline_CLI SHALL disable colored output and emit plain undecorated text
5. THE Taskline_CLI SHALL print error messages to stderr and data output to stdout
6. WHEN the `list` subcommand is invoked and the Queue is empty, THE Taskline_CLI SHALL display a message indicating no tasks are in the queue (no header row, no table)
7. THE Taskline_CLI SHALL display the CREATED column as a formatted timestamp in "YYYY-MM-DD HH:MM" format using the local timezone

### Requirement 15: Task Identification

**User Story:** As a developer, I want tasks to have both a short unique ID and a human-readable name, so that I can reference them conveniently in different contexts.

#### Acceptance Criteria

1. WHEN a new Task is created, THE Taskline_Server SHALL generate a Task_ID as an 8-character string using lowercase alphanumeric characters (a-z, 0-9)
2. WHEN a new Task is created without a user-provided name, THE Taskline_Server SHALL generate a Task_Name using Docker-style random names consisting of a lowercase adjective and a lowercase noun separated by a hyphen (e.g., "brave-tiger", "calm-river")
3. THE Taskline_Server SHALL ensure that both Task_ID and Task_Name are unique within the current Queue
4. WHEN resolving a task identifier in API requests, THE Taskline_Server SHALL perform case-sensitive matching, first attempting to match by Task_ID, then by Task_Name
5. THE Taskline_Server SHALL NOT reuse Task_IDs or auto-generated Task_Names of tasks that have been removed from the Queue within the current server session
6. IF a user-provided Task_Name exceeds 64 characters, contains characters other than lowercase letters, digits, or hyphens, or does not start with a letter, THEN THE Taskline_Server SHALL return HTTP 400 with a JSON error response indicating the name is invalid

### Requirement 16: Build and Installation Scripts

**User Story:** As a developer, I want build and install scripts for both the server and CLI, so that I can compile and install the binaries with standard project conventions.

#### Acceptance Criteria

1. THE Taskline_Server project SHALL include a `taskline/server/scripts/build.sh` script that resolves its own project root (portable from any working directory), compiles the Go server binary named `taskline-server`, and outputs it to `taskline/server/bin/taskline-server`
2. THE Taskline_Server project SHALL include a `taskline/server/scripts/install.sh` script that executes the build script, creates `~/.local/bin/` if it does not exist, and copies `taskline/server/bin/taskline-server` to `~/.local/bin/taskline-server`
3. THE Taskline_CLI project SHALL include a `taskline/cli/scripts/build.sh` script that resolves its own project root (portable from any working directory), compiles the Go CLI binary named `taskline`, and outputs it to `taskline/cli/bin/taskline`
4. THE Taskline_CLI project SHALL include a `taskline/cli/scripts/install.sh` script that executes the build script, creates `~/.local/bin/` if it does not exist, copies `taskline/cli/bin/taskline` to `~/.local/bin/taskline`, and copies the zsh completion file to `~/.zsh/completions/_taskline`
5. THE Taskline_CLI project SHALL include a zsh completion script at `taskline/cli/completions/_taskline` that provides autocompletion for all CLI subcommands and their applicable flags
6. WHEN the user types the first argument after `taskline`, THE zsh completion script SHALL offer all available subcommand names as completions
7. WHEN the user has entered a subcommand, THE zsh completion script SHALL offer the flags applicable to that specific subcommand as completions

### Requirement 17: Health Check

**User Story:** As a developer, I want a health check endpoint so that monitoring tools can verify the server is running.

#### Acceptance Criteria

1. WHEN a GET request is received at `/health`, THE Taskline_Server SHALL return HTTP 200 with a JSON body containing `{"status": "ok"}`
2. THE Taskline_Server SHALL respond to the `/health` endpoint within 100ms regardless of Queue state
3. THE `/health` endpoint SHALL NOT require authentication

### Requirement 18: Server Logging

**User Story:** As a developer, I want to see all server activity and command output on stdout so I can monitor everything in one stream.

#### Acceptance Criteria

1. THE Taskline_Server SHALL write all server log messages to stdout in human-readable format: "YYYY-MM-DD HH:MM:SS [LEVEL] message"
2. THE Taskline_Server SHALL use log levels INFO, WARN, and ERROR for server log messages
3. WHILE a Task command is running, THE Taskline_Server SHALL pipe the command's stdout and stderr directly to the Taskline_Server stdout WITHOUT any prefix or modification, preserving ANSI color codes and raw terminal output
4. THE Taskline_Server SHALL interleave server log messages and command output on the same stdout stream
5. THE Taskline_Server SHALL log the following events at INFO level: server started (port, state file path, notifications enabled or disabled, tasks loaded count), task started (name, command), task completed (name, duration), task failed (name, exit code, duration), queue state transitions, shutdown initiated, state persisted
6. THE Taskline_Server SHALL log the following events at WARN level: state file corrupted on load, NOTIFY_EMAIL not configured
7. THE Taskline_Server SHALL log the following events at ERROR level: state file write failure, SMTP delivery failure, command spawn failure
8. THE Taskline_Server SHALL NOT use stderr for any output
9. THE Taskline_Server SHALL preserve PTY and terminal capabilities for child processes so that commands producing colored output with ANSI escape codes are displayed correctly

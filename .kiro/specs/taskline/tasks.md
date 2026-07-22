# Implementation Plan: Taskline

## Overview

Taskline is a sequential command queue server (HTTP daemon + CLI) implemented as two separate Go modules within the trayline monorepo. The implementation follows a bottom-up approach: core data structures and interfaces first, then server logic with worker goroutine, then HTTP handlers, then CLI client. Property-based tests use `pgregory.net/rapid`.

## Tasks

- [ ] 1. Set up project structure and core interfaces
  - [ ] 1.1 Initialize server Go module and project scaffolding
    - Create `taskline/server/` directory with `go.mod` (module path: matching monorepo convention)
    - Add dependencies: `github.com/joho/godotenv`, `pgregory.net/rapid` (test)
    - Create `.env.example` with `APP_PORT=9090`, `STATE_FILE=./taskline-state.json`, `NOTIFY_EMAIL=`, `SMTP_HOST=`, `SMTP_PORT=587`, `SMTP_USER=`, `SMTP_PASSWORD=`, `SMTP_FROM=`
    - Create `scripts/build.sh` and `scripts/install.sh` following monorepo conventions
    - _Requirements: 16.1, 16.2_

  - [ ] 1.2 Initialize CLI Go module and project scaffolding
    - Create `taskline/cli/` directory with `go.mod`
    - Add dependencies: `github.com/joho/godotenv`, `pgregory.net/rapid` (test)
    - Create `scripts/build.sh` and `scripts/install.sh` following monorepo conventions
    - Create `completions/_taskline` zsh completion script stub
    - _Requirements: 16.3, 16.4, 16.5, 16.6, 16.7_

  - [ ] 1.3 Implement server config loading and validation
    - Create `server/config.go` with `LoadConfig()` function
    - Load `.env` via godotenv, read `APP_PORT`, `STATE_FILE`, `NOTIFY_EMAIL`, `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASSWORD`, `SMTP_FROM`
    - Validate port range 1–65535, default to 9090 if unset
    - Default `STATE_FILE` to `./taskline-state.json`
    - Default `SMTP_FROM` to `SMTP_USER` if not set
    - Determine notifications enabled: require `NOTIFY_EMAIL` + all of `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASSWORD`
    - Return typed `Config` struct or error on invalid values
    - _Requirements: 1.1, 1.2, 1.3, 4.7, 4.8, 4.9, 11.3, 11.4_

  - [ ]* 1.4 Write property test for config port validation
    - **Property 1: Config port validation**
    - **Validates: Requirements 1.1, 1.3**

- [ ] 2. Implement queue data structures and task management
  - [ ] 2.1 Implement Task and Queue types with core operations
    - Create `server/queue.go` with `Task`, `TaskStatus`, `Queue`, `QueueState` types
    - Implement `NewQueue()`, `AddTask(command, name, position)`, `RemoveTask(id)`, `FindTask(identifier)`
    - Implement mutex-based thread safety on all queue operations
    - Implement position-based insertion logic for pending tasks
    - Implement identifier resolution: check Task_ID first, then Task_Name (case-sensitive)
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 15.3, 15.4_

  - [ ] 2.2 Implement name generator and validation
    - Create `server/names.go` with adjective-noun name generation (Docker-style)
    - Implement `GenerateID()` producing 8-char lowercase alphanumeric strings
    - Maintain used-IDs and used-names sets to prevent reuse within session
    - Implement `ValidateName(name)` checking: max 64 chars, lowercase letters/digits/hyphens only, starts with letter
    - _Requirements: 2.6, 15.1, 15.2, 15.5, 15.6_

  - [ ]* 2.3 Write property test for task creation structure
    - **Property 2: Task creation produces valid structure**
    - **Validates: Requirements 2.1, 15.1**

  - [ ]* 2.4 Write property test for position insertion correctness
    - **Property 3: Position insertion correctness**
    - **Validates: Requirements 2.2, 2.3, 2.4**

  - [ ]* 2.5 Write property test for name generation format
    - **Property 5: Name generation format**
    - **Validates: Requirements 2.6, 15.2**

  - [ ]* 2.6 Write property test for name uniqueness invariant
    - **Property 6: Name uniqueness invariant**
    - **Validates: Requirements 2.10, 9.7, 15.3, 15.5**

  - [ ]* 2.7 Write property test for name validation rules
    - **Property 7: Name validation rules**
    - **Validates: Requirements 15.6**

  - [ ]* 2.8 Write property test for identifier resolution order
    - **Property 14: Identifier resolution order**
    - **Validates: Requirements 15.4**

- [ ] 3. Implement queue state machine and recovery operations
  - [ ] 3.1 Implement queue state transitions and recovery
    - Add `Resume()`, `Retry()`, `Skip()`, `MarkFailed(exitCode)`, `MarkComplete()` methods to Queue
    - Implement state machine: idle ↔ running ↔ halted transitions
    - Retry: reset failed task to pending at position 0, queue → running
    - Skip: remove failed task, queue → running
    - Resume: queue → running (with validation for already running, halted, empty)
    - _Requirements: 3.1, 3.2, 3.3, 4.1, 4.2, 6.1, 6.2, 6.3, 6.4, 6.5, 6.6, 6.7, 6.8_

  - [ ] 3.2 Implement task update and deletion logic
    - Add `UpdateTask(identifier, command, name)` method to Queue
    - Add `DeleteTask(identifier)` with status-based validation (reject running, allow failed)
    - Validate name uniqueness on update, reject empty updates
    - On deleting failed task: transition queue to idle
    - _Requirements: 8.1, 8.2, 8.3, 8.4, 9.1, 9.2, 9.3, 9.4, 9.5, 9.6, 9.7, 9.8, 9.9_

  - [ ]* 3.3 Write property test for failure halts queue
    - **Property 8: Failure halts queue**
    - **Validates: Requirements 4.1, 4.2**

  - [ ]* 3.4 Write property test for retry resets failed task
    - **Property 9: Retry resets failed task**
    - **Validates: Requirements 6.1**

  - [ ]* 3.5 Write property test for skip removes failed task
    - **Property 10: Skip removes failed task**
    - **Validates: Requirements 6.3**

  - [ ]* 3.6 Write property test for task list ordering
    - **Property 11: Task list ordering**
    - **Validates: Requirements 7.4**

  - [ ]* 3.7 Write property test for task update applies fields correctly
    - **Property 18: Task update applies fields correctly**
    - **Validates: Requirements 9.1, 9.2, 9.3**

- [ ] 4. Checkpoint - Ensure all queue logic tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 5. Implement state persistence
  - [ ] 5.1 Implement JSON state file read/write with atomic operations
    - Create `server/state.go` with `SaveState(queue, path)` and `LoadState(path)` functions
    - Implement atomic write: write to temp file, then rename
    - Handle corrupted files: rename with `.corrupted` suffix, log warning, return empty queue
    - Handle missing file: return empty queue in idle state
    - Handle write errors: log error, continue operating (never block API)
    - _Requirements: 11.1, 11.2, 11.3, 11.4, 11.5, 11.6, 11.7_

  - [ ]* 5.2 Write property test for state persistence round-trip
    - **Property 13: State persistence round-trip**
    - **Validates: Requirements 11.1, 11.6**

- [ ] 6. Implement worker and command execution
  - [ ] 6.1 Implement worker goroutine with command execution
    - Create `server/worker.go` with `Worker` struct and `Run()` method
    - Define `CommandRunner` interface for testability
    - Implement `sh -c` command execution with stdout/stderr piped to server stdout
    - Implement process termination: SIGTERM → wait 5s → SIGKILL
    - On command completion (exit 0): remove task, advance queue or transition to idle
    - On command failure (non-zero): mark task failed, halt queue, trigger notification and state persist
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 4.1, 4.2, 5.1, 5.2, 5.3_

  - [ ] 6.2 Implement notification service
    - Create `server/notify.go` with `Notifier` interface and `EmailNotifier` implementation
    - Send email via `net/smtp` using SMTP_HOST, SMTP_PORT, SMTP_USER, SMTP_PASSWORD, SMTP_FROM config
    - SMTP_FROM defaults to SMTP_USER if not set
    - Send to `NOTIFY_EMAIL` on task failure (Task_Name, Task_ID, command, exit code)
    - Skip if `NOTIFY_EMAIL` not configured or any SMTP variable missing (log warning at startup)
    - Log error on delivery failure, never retry, never block queue
    - _Requirements: 4.3, 4.5, 4.6, 4.7, 4.8, 4.9, 5.4_

- [ ] 7. Implement HTTP handlers
  - [ ] 7.1 Implement task CRUD handlers
    - Create `server/handler.go` with route registration on `net/http` ServeMux
    - `POST /tasks` — create task with command, optional name/position, validate inputs
    - `GET /tasks` — list all tasks ordered by queue position
    - `DELETE /tasks/{identifier}` — delete task with status validation
    - `PATCH /tasks/{identifier}` — update pending task fields
    - Return proper HTTP status codes (201, 200, 400, 404, 409) with JSON error responses
    - Trigger state persistence on every mutation
    - _Requirements: 2.1–2.10, 7.1–7.5, 8.1–8.4, 9.1–9.9_

  - [ ] 7.2 Implement queue control handlers
    - `POST /tasks/retry` — retry failed task
    - `POST /tasks/skip` — skip failed task
    - `POST /tasks/stop` — stop running task (SIGTERM → SIGKILL)
    - `POST /queue/resume` — resume queue execution
    - `GET /queue/status` — return queue state, pending count, current/failed task info
    - `GET /health` — return `{"status": "ok"}` (lightweight, no queue lock)
    - Return proper HTTP status codes with JSON error responses
    - _Requirements: 5.1–5.5, 6.1–6.8, 10.1–10.5, 17.1, 17.2, 17.3_

  - [ ]* 7.3 Write property test for command validation
    - **Property 4: Command validation rejects empty and whitespace**
    - **Validates: Requirements 2.7**

  - [ ]* 7.4 Write property test for queue status response structure
    - **Property 12: Queue status response structure**
    - **Validates: Requirements 10.1, 10.2, 10.3, 10.4, 10.5**

- [ ] 8. Implement server entry point and signal handling
  - [ ] 8.1 Implement server main with lifecycle management
    - Create `server/main.go` with HTTP server startup on configured port
    - Load config, load state file, initialize queue, start worker goroutine
    - On startup: if loaded state contains a task with status "running", transition it to "failed" (exit code -2), set queue to "halted", send failure notification
    - Implement structured logging to stdout: "YYYY-MM-DD HH:MM:SS [LEVEL] message" format
    - Log at startup: port, state file path, notifications enabled/disabled, tasks loaded count
    - Log task lifecycle events (started, completed, failed), queue state transitions, shutdown, state persisted
    - Log WARN for corrupted state file, missing NOTIFY_EMAIL
    - Log ERROR for state write failures, SMTP failures, command spawn failures
    - All output (logs + command output) goes to stdout only, no stderr usage
    - Preserve ANSI color codes from child process output (do not strip terminal escape sequences)
    - Handle SIGTERM/SIGINT: stop accepting connections, wait up to 30s for running task, persist state, exit 0
    - If task doesn't finish in 30s: SIGKILL the process, mark task failed, persist state
    - If no task running: persist state and exit within 5s
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7, 1.8, 1.9, 1.10, 18.1, 18.2, 18.3, 18.4, 18.5, 18.6, 18.7, 18.8, 18.9_

- [ ] 9. Checkpoint - Ensure server builds and all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 10. Implement CLI client
  - [ ] 10.1 Implement CLI config and HTTP client
    - Create `cli/config.go` with `TASKLINE_URL` resolution (default `http://localhost:9090`)
    - Validate URL scheme (must start with `http://` or `https://`)
    - Create `cli/client.go` with HTTP client wrapper (10s timeout)
    - Methods for each API endpoint, structured error parsing from response body
    - _Requirements: 13.1, 13.2, 13.3, 13.6_

  - [ ]* 10.2 Write property test for URL scheme validation
    - **Property 17: URL scheme validation**
    - **Validates: Requirements 13.3**

  - [ ] 10.3 Implement CLI output formatter
    - Create `cli/format.go` with column-aligned table output
    - Implement command truncation to 40 chars with ellipsis ("…")
    - Implement colored STATUS column: green (running), yellow (pending), red (failed)
    - Respect `NO_COLOR` env var and non-TTY detection
    - Format timestamps to "YYYY-MM-DD HH:MM" in local timezone
    - _Requirements: 14.1, 14.2, 14.3, 14.4, 14.5, 14.6, 14.7_

  - [ ]* 10.4 Write property test for command truncation
    - **Property 15: Command truncation**
    - **Validates: Requirements 14.2**

  - [ ]* 10.5 Write property test for timestamp formatting
    - **Property 16: Timestamp formatting**
    - **Validates: Requirements 14.7**

  - [ ] 10.6 Implement CLI subcommands
    - Create `cli/commands.go` with all subcommands: `add`, `list`, `delete`, `update`, `retry`, `skip`, `stop`, `resume`, `status`
    - `add` — command arg required, optional `--name` and `--position` flags
    - `list` — display table, handle empty queue message
    - `delete` — identifier arg required
    - `update` — identifier arg + at least one of `--command`/`--name` flags required
    - `retry`, `skip`, `stop`, `resume`, `status` — no args required
    - Print errors to stderr, exit 1 on server errors, exit 2 on usage errors
    - _Requirements: 12.1–12.15, 13.4, 13.5_

  - [ ] 10.7 Implement CLI entry point
    - Create `cli/main.go` with subcommand dispatch
    - Support `--help`/`-h` and `--version`/`-v` flags
    - Load `.env` via godotenv for `TASKLINE_URL`
    - _Requirements: 13.4, 13.5_

  - [ ] 10.8 Create zsh completion script
    - Create `cli/completions/_taskline` with completions for all subcommands and their flags
    - Subcommand completions on first argument
    - Flag completions per subcommand (e.g., `--name`, `--position` for `add`)
    - _Requirements: 16.5, 16.6, 16.7_

- [ ] 11. Final checkpoint - Ensure full build and all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties using `pgregory.net/rapid`
- Unit tests validate specific examples and edge cases
- The server uses `net/http` stdlib (no external router) consistent with monorepo patterns
- Both modules use `godotenv` for `.env` loading consistent with monorepo convention
- Command execution uses interface-based mocking (`CommandRunner`) for testability
- Notification uses interface-based mocking (`Notifier`) for testability

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "1.2"] },
    { "id": 1, "tasks": ["1.3", "2.2"] },
    { "id": 2, "tasks": ["1.4", "2.1"] },
    { "id": 3, "tasks": ["2.3", "2.4", "2.5", "2.6", "2.7", "2.8"] },
    { "id": 4, "tasks": ["3.1", "3.2"] },
    { "id": 5, "tasks": ["3.3", "3.4", "3.5", "3.6", "3.7"] },
    { "id": 6, "tasks": ["5.1"] },
    { "id": 7, "tasks": ["5.2", "6.1"] },
    { "id": 8, "tasks": ["6.2"] },
    { "id": 9, "tasks": ["7.1", "7.2"] },
    { "id": 10, "tasks": ["7.3", "7.4", "8.1"] },
    { "id": 11, "tasks": ["10.1", "10.3"] },
    { "id": 12, "tasks": ["10.2", "10.4", "10.5", "10.6"] },
    { "id": 13, "tasks": ["10.7", "10.8"] }
  ]
}
```

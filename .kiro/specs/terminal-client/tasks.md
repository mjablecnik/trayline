# Implementation Plan: Terminal Client

## Overview

Build a Go CLI tool (`trayline-client`) in the `client/` directory that provides interactive WebSocket chat and one-shot REST task execution against the Trayline Agent API Server. Implementation follows a bottom-up approach: project setup → config → API client → formatter → commands → chat → run → scripts/completions.

## Tasks

- [ ] 1. Set up project structure and core interfaces
  - [ ] 1.1 Initialize Go module and project skeleton
    - Create `client/` directory with `go.mod` (module name: `trayline-client`)
    - Add dependencies: `github.com/gorilla/websocket`, `github.com/joho/godotenv`, `pgregory.net/rapid`
    - Create `.env.example` with `TRAYLINE_SERVER_URL` and `TRAYLINE_API_TOKEN` placeholders
    - Create empty `main.go` with package declaration and minimal `main()` stub
    - Create `bin/` directory (gitignored)
    - _Requirements: 1.1, 1.2, 1.5_

  - [ ] 1.2 Define data model types
    - Create type definitions in a new file or within `api.go`: `Config`, `RunRequest`, `RunResponse`, `RunAcceptedResponse`, `TaskSummary`, `SessionSummary`, `ErrorResponse`, `WSClientMessage`, `WSServerMessage`
    - Use JSON struct tags matching the server's API contract
    - _Requirements: 5.1, 6.1, 7.1, 3.4_

- [ ] 2. Implement configuration resolution
  - [ ] 2.1 Implement config resolver (`config.go`)
    - Load `.env` file silently using `godotenv.Load()` (no error if missing)
    - Resolve server URL with priority: `--server` flag → `TRAYLINE_SERVER_URL` env → `.env` → default `http://localhost:8080`
    - Resolve token with priority: `--token` flag → `TRAYLINE_API_TOKEN` env → `.env` → error (exit code 2)
    - Validate URL scheme (must start with `http://` or `https://`), exit code 2 on failure
    - Strip trailing slashes from resolved URL
    - Validate `--quiet` and `--verbose` are mutually exclusive, exit code 2 on conflict
    - Return typed `Config` struct or descriptive error
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 9.5_

  - [ ]* 2.2 Write property test: Configuration resolution follows priority chain
    - **Property 1: Configuration resolution follows priority chain**
    - Generate random combinations of flag, env var, .env file, and absent values using `rapid`
    - Assert resolved value always equals highest-priority present source
    - Assert error returned when no token source is present
    - **Validates: Requirements 1.1, 1.2, 1.3, 1.5**

  - [ ]* 2.3 Write property test: URL validation and normalization
    - **Property 2: URL validation and normalization**
    - Generate URLs with various schemes (http, https, ftp, missing) and trailing slashes
    - Assert only `http://` and `https://` schemes are accepted
    - Assert trailing slashes are stripped from accepted URLs
    - **Validates: Requirements 1.4, 1.6**

- [ ] 3. Implement API client layer
  - [ ] 3.1 Implement HTTP/WebSocket client (`api.go`)
    - Create `APIClient` struct wrapping `net/http.Client` with auth header injection
    - Implement `Health()` method: GET `/health` with 5s timeout
    - Implement `PostRun()` method: POST `/run` with 30s timeout
    - Implement `GetRun(id)` method: GET `/run/{id}` with 30s timeout
    - Implement `GetRuns()` method: GET `/runs` with 30s timeout
    - Implement `CancelRun(id)` method: POST `/run/{id}/cancel` with 30s timeout
    - Implement `GetSessions()` method: GET `/sessions` with 30s timeout
    - Implement `TerminateSession(id)` method: POST `/sessions/{id}/terminate` with 30s timeout
    - Implement `DialWebSocket()` method for WebSocket connections with 10s dial timeout
    - Return structured errors with HTTP status context
    - Support verbose mode: log method, URL, status, timing to stderr
    - _Requirements: 2.1, 3.1, 3.13, 3.14, 5.1, 6.1, 6.2, 6.3, 7.1, 7.3, 9.7_

  - [ ]* 3.2 Write unit tests for API client
    - Use `httptest.Server` to mock REST endpoints
    - Test auth header injection on all requests
    - Test timeout behavior (5s health, 30s general)
    - Test structured error parsing from server error responses
    - Test verbose output format
    - _Requirements: 2.1, 2.2, 2.3, 3.13, 3.14_

- [ ] 4. Implement output formatter
  - [ ] 4.1 Implement formatting functions (`format.go`)
    - Implement TTY detection for stdout/stderr
    - Implement `NO_COLOR` environment variable check (any value including empty disables color)
    - Implement color functions: green (success), red (error), yellow (warning), cyan (info)
    - Implement table formatter with column alignment and consistent spacing
    - Implement column value truncation at 36 characters with ellipsis (`…`)
    - Implement timestamp formatting (`YYYY-MM-DD HH:MM`)
    - Implement prompt prefix rendering (`> `) to stderr
    - Inject `isTerminal` function for testability
    - _Requirements: 8.1, 8.2, 8.3, 8.4, 8.5, 8.6_

  - [ ]* 4.2 Write property test: Table formatter produces aligned columns
    - **Property 5: Table formatter produces aligned columns with all fields**
    - Generate random lists of table rows with varying field lengths
    - Assert all columns start at the same character position across rows
    - Assert every row contains all specified fields
    - **Validates: Requirements 6.1, 7.1, 8.4**

  - [ ]* 4.3 Write property test: Color output control
    - **Property 6: Color output is disabled when NO_COLOR is set or output is non-TTY**
    - Generate messages with random content and vary NO_COLOR/TTY state
    - Assert no ANSI escape sequences when NO_COLOR set or non-TTY
    - Assert correct color codes per message type when colors enabled
    - **Validates: Requirements 8.1, 8.2, 8.3**

  - [ ]* 4.4 Write property test: Column value truncation
    - **Property 7: Column value truncation at 36 characters**
    - Generate strings of varying lengths (0 to 200 chars)
    - Assert strings >36 chars are truncated to exactly 36 with trailing `…`
    - Assert strings ≤36 chars appear unmodified
    - **Validates: Requirements 8.6**

- [ ] 5. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 6. Implement command dispatch and help system
  - [ ] 6.1 Implement subcommand definitions and dispatch (`commands.go`)
    - Define all subcommands: `health`, `chat`, `run`, `tasks`, `task`, `cancel`, `sessions`, `terminate`
    - Parse global flags: `--server`, `--token`, `--quiet`, `--verbose`, `--help`, `--version`
    - Implement `--help` / `-h` output: program name, description, usage syntax, subcommand list, global flags, examples
    - Implement subcommand-level `--help` with syntax, flags, and examples
    - Implement `--version` / `-v` output in semver format
    - Handle unknown subcommands and invalid flag combinations (exit code 2, suggest `--help`)
    - Dispatch to appropriate handler function
    - _Requirements: 9.1, 9.2, 9.3, 9.4, 9.5, 9.6, 9.7_

  - [ ] 6.2 Implement main entry point (`main.go`)
    - Set up signal handling (SIGINT, SIGTERM)
    - Call config resolution
    - Call command dispatch
    - Wire exit codes from handlers
    - _Requirements: 10.1, 10.2_

- [ ] 7. Implement chat command (WebSocket)
  - [ ] 7.1 Implement interactive chat handler (`chat.go`)
    - Establish WebSocket connection to `/chat` with agent, model, system as query params
    - Support reconnection via `--session` flag → connect to `/chat/{id}`
    - Add Bearer token to WebSocket upgrade request headers
    - Handle `session_started` message: print session ID to stderr, show prompt
    - Handle `session_resumed` message: print session ID, enter input mode
    - Handle `output` message: write data to stdout immediately (no buffering)
    - Handle `done` message: print newline, redisplay prompt
    - Handle `error` message: write to stderr
    - Handle `context_compacted` message: write info notice to stderr
    - Read user input line-by-line, filter empty/whitespace-only lines
    - Send non-empty input as `{"type": "message"}`
    - Handle `/quit` command → send `{"type": "terminate"}`, close, exit 0
    - Handle first SIGINT → send `{"type": "interrupt"}`
    - Handle second SIGINT → close connection, exit 130
    - Handle SIGTERM → send `{"type": "terminate"}`, wait up to 5s for response, close, exit 0
    - Handle unexpected WebSocket close → error message to stderr, exit 1
    - Handle HTTP 404 (session not found) → error, exit 1
    - Handle HTTP 409 (session in use) → error, exit 1
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8, 3.9, 3.10, 3.11, 3.12, 3.13, 3.14, 4.1, 4.2, 4.3, 4.4, 10.3, 10.4, 10.5_

  - [ ]* 7.2 Write property test: WebSocket URL construction
    - **Property 3: WebSocket URL construction includes provided parameters**
    - Generate random agent, model, and system values (including empty strings)
    - Assert agent always present as query parameter
    - Assert model/system included only when non-empty
    - Assert all values are URL-encoded
    - **Validates: Requirements 3.1, 3.2, 3.3**

  - [ ]* 7.3 Write property test: Empty input filtering
    - **Property 4: Empty input lines are filtered**
    - Generate whitespace-only strings and strings with non-whitespace content
    - Assert whitespace-only inputs are discarded (no message sent)
    - Assert inputs with non-whitespace characters are sent as messages
    - **Validates: Requirements 3.5**

  - [ ]* 7.4 Write unit tests for chat handler
    - Use `httptest.Server` with WebSocket upgrader to mock server
    - Test full session lifecycle: connect → session_started → send → output → done → /quit
    - Test reconnection flow: session_resumed
    - Test error responses (404, 409)
    - Test signal state transitions (first SIGINT → interrupt, second → exit)
    - _Requirements: 3.4, 3.6, 3.7, 3.8, 3.11, 3.12, 4.2, 4.3, 4.4, 10.3, 10.4_

- [ ] 8. Implement run command (REST)
  - [ ] 8.1 Implement one-shot task handler (`run.go`)
    - Send POST `/run` with prompt, agent, optional model/system/format
    - Handle HTTP 200 (immediate result): display result to stdout, status and elapsed time to stderr
    - Handle HTTP 202 (accepted): print task ID to stderr, poll `GET /run/{id}` every 2s
    - Handle completed status: display result to stdout
    - Handle failed status: display error to stderr, exit 1
    - Handle cancelled status: display message to stderr, exit 1
    - Handle `valid: false` with `--format json`: print warning to stderr
    - Enforce 10-minute polling timeout, exit 1 on timeout
    - Handle server errors during initial request or polling, exit 1
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5, 5.6, 5.7, 5.8, 5.9, 5.10, 5.11, 5.12_

  - [ ]* 8.2 Write unit tests for run handler
    - Use `httptest.Server` to mock `/run` and `/run/{id}` endpoints
    - Test immediate 200 response (display result + elapsed time)
    - Test 202 → polling → completed flow
    - Test 202 → polling → failed flow
    - Test 202 → polling → cancelled flow
    - Test valid=false warning for JSON format
    - Test polling timeout behavior
    - _Requirements: 5.5, 5.6, 5.7, 5.8, 5.9, 5.11, 5.12_

- [ ] 9. Implement task and session management commands
  - [ ] 9.1 Implement task management commands
    - `tasks`: GET `/runs`, display table (ID, status, agent, creation time)
    - `task <id>`: GET `/run/{id}`, display detailed task info
    - `cancel <id>`: POST `/run/{id}/cancel`, display updated status
    - Handle missing ID argument → error, exit 2
    - Handle 404 → error, non-zero exit
    - Handle conflict (already terminal) → error from server, non-zero exit
    - Handle server errors → error message, non-zero exit
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5, 6.6, 6.7_

  - [ ] 9.2 Implement session management commands
    - `sessions`: GET `/sessions`, display table (session ID, agent, model, created, last message)
    - Handle empty sessions list → informational message
    - `terminate <id>`: POST `/sessions/{id}/terminate`, display terminated status
    - Handle missing ID argument → error, exit 2
    - Handle 404 → error, non-zero exit
    - Handle server unreachable → error, non-zero exit
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5, 7.6_

  - [ ]* 9.3 Write unit tests for task and session commands
    - Test tasks list table output formatting
    - Test task detail display with all fields
    - Test cancel success and conflict scenarios
    - Test sessions list and empty state
    - Test terminate success and not-found scenarios
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 7.1, 7.2, 7.3, 7.5_

- [ ] 10. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 11. Implement build scripts and shell completion
  - [ ] 11.1 Create build and install scripts
    - Create `client/scripts/build.sh`: compile binary to `client/bin/trayline-client`
    - Use Script Portability pattern (`SCRIPT_DIR` / `PROJECT_DIR` / `cd`)
    - Use `set -euo pipefail`
    - Create `client/scripts/install.sh`: run build, copy binary to `~/.local/bin/`, install zsh completion to `~/.zsh/completions/`
    - Make scripts executable
    - _Requirements: 9.1, 9.3_

  - [ ] 11.2 Create zsh completion script
    - Create `client/completions/_trayline-client`
    - Complete all subcommands: health, chat, run, tasks, task, cancel, sessions, terminate
    - Complete global flags: --server, --token, --quiet, --verbose, --help, --version
    - Complete subcommand-specific flags (e.g., --agent, --model, --system for chat/run)
    - _Requirements: 9.1, 9.2_

- [ ] 12. Final checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties from the design document
- Unit tests validate specific examples and edge cases
- The API client uses `net/http/httptest` for test mocking, consistent with the existing server and orchestrator patterns
- All scripts follow the Script Portability pattern per project steering

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1"] },
    { "id": 1, "tasks": ["1.2"] },
    { "id": 2, "tasks": ["2.1"] },
    { "id": 3, "tasks": ["2.2", "2.3", "3.1"] },
    { "id": 4, "tasks": ["3.2", "4.1"] },
    { "id": 5, "tasks": ["4.2", "4.3", "4.4", "6.1"] },
    { "id": 6, "tasks": ["6.2"] },
    { "id": 7, "tasks": ["7.1", "8.1", "9.1", "9.2"] },
    { "id": 8, "tasks": ["7.2", "7.3", "7.4", "8.2", "9.3"] },
    { "id": 9, "tasks": ["11.1", "11.2"] }
  ]
}
```

# AI Log

## 2026-07-01 07:53 — manual
- Project: server + orchestrator (agent-api-server, Go)
- Added a full suite of unit/integration/edge-case tests across both Go modules, derived from `TEST_SPEC.md` and tracked in `TASKS.md` (Phases 0–3).
- Phase 0: removed stale `pgregory.net/rapid` failure-seed artifacts under `server/testdata/rapid/`.
- Phase 1 (HIGH): created `server/state_recover_sessions_test.go`, `server/session_idle_test.go`, `orchestrator/checkpoint_ratelimit_test.go`, `orchestrator/checkpoint_test.go`, `orchestrator/flow_test.go`, and extended `server/container_test.go` (RunOneShot/waitAndCapture/limitWriter).
- Phase 2 (MEDIUM): added `server/health_test.go`, `server/router_test.go`, `orchestrator/llm_logger_test.go`; extended `state_test.go` (recoverTasks), `session_handler_test.go` (isContextCompaction), `ratelimit_test.go` (clientIP/cleanup), `task_handler_test.go` (executeTask), and `orchestrator/executor_test.go`.
- Phase 3 (LOW): added `server/task_store_test.go`, `server/session_store_test.go`; extended `logger_test.go`, `config_test.go` (defaults), and orchestrator usage-text tests.
- Result: 326 tests passing (200 orchestrator + 126 server), 0 failures; no external services required (Docker/LLM/filesystem mocked).

## 2026-07-01 07:05 — manual
- Project: server (agent-api-server, Go)
- Conducted a full code review of the `server/` Go module against the `agent-api-server` spec (build, vet, and tests passing), documented in `CODE_REVIEW.md` with 10 severity-ranked issues.
- Resolved the CRITICAL and HIGH issues (commit 06ba499): wired `StateManager` into task/session handlers so state is persisted on every change; rejected at-capacity chat sessions with HTTP 503 via a non-blocking `TryAcquireSlot` before the WebSocket upgrade; fixed `recoverSessions` to re-attach container stdin/stdout and restart streaming/cleanup goroutines; and emitted per-turn `{"type":"done"}` in `streamOutput`.
- Resolved the MEDIUM issues (commit d1d3a97): made chat "interrupt" send a real SIGINT, added lifecycle logging for task status transitions and session create/terminate, and force-stopped orphaned one-shot task containers during graceful shutdown.
- Left the three LOW issues (long-poll 202 status wording, `go mod tidy` indirect deps, error-log source location) open in `TASKS.md`.

## 2026-07-01 08:39 — manual
- Project: server (agent-api-server, Go)
- Implemented the agent API server end-to-end from the `agent-api-server` spec across tasks 1–12.
- Created project structure, Go module, and core types (task 1); added middleware and a health endpoint (task 2); built the container manager with a FIFO semaphore (task 4).
- Implemented one-shot task handlers with property tests (task 5), WebSocket chat session handlers (task 7), and state persistence and recovery (task 8).
- Wired up server lifecycle, the router, and workspace validation (task 9); added the Dockerfile and deployment scripts (task 11).
- Added `auth.go`, `config.go`, and `container.go` with accompanying `_test.go` suites.
- Fixed an APP_PORT config test to skip when `os.Setenv` rejects null bytes; all tests passing.

# AI Log

## 2026-11-15 — Agent integration guide
- Added `docs/ADDING_AN_AGENT.md` with the integration contract, file/function map, separate YAML/wrapper/direct execution paths, all credential mounts, native/chat/OpenAI protocols, frontend/client surfaces, test coverage, live smoke checks, deployment and a reusable completion checklist.
- Added root `AGENTS.md` guidance and linked the maintained guide from README, CLAUDE, orchestrator and remote documentation. Keep the map updated when integration points change.
- Verified referenced repository paths, local Markdown links, code fences and diff whitespace. Documentation-only change; no runtime tests required.

## 2026-11-15 — Complete Codex integration
- Enabled Codex YAML steps, model/effort forwarding (`max` → `xhigh`), native task/chat endpoints, project/assistant chat and dashboard agent selectors. Provider changes clear stale model IDs.
- Added a sandbox Python adapter for persistent chat using exact-thread `exec resume`, per-message output, interruption, error recovery and structured protocol compatibility. Added native Codex JSONL handling for OpenAI SSE and a default `codex` registry entry.
- Unified CLI/server/workflow credentials as read-only auth.json-only mounts and resolved server host credentials in start-docker.sh. Rebuild the sandbox and restart services after updating.
- Added regression tests for YAML parsing/execution, task acceptance, SSE filtering/failure handling, credential mounts and adapter multi-turn/error/interrupt behavior.
- Validation: full orchestrator and remote Go suites passed; dashboard check/build and 32 existing plus 2 new frontend tests passed; 4 adapter protocol tests and mocked CLI Docker-argument checks passed. Formatting/lint checked for changed frontend files. Live Docker/provider smoke test was unavailable because `trayline-proxy:2375` could not be resolved in this environment.
- Supersedes the incomplete-support findings recorded in the documentation review below.

## 2026-11-15 — Codex, sandbox versions, and Zsh completions
- Reviewed today's 16 commits (`79b1746` through `dbd79cb`) and synchronized related documentation with the final implementation.
- Added Codex CLI usage and host-login authentication: CLI mounts only `~/.codex/auth.json` read-only, disables the daemon and inner approvals/sandbox, forwards model selection, and does not forward effort or API-key authentication.
- Recorded the incomplete integration boundary: YAML validation and public remote task/chat validation still excludes Codex and the default model registry omits it, despite new executor/container-builder branches. Server builders still mount the full `CODEX_HOST_DIR` read-write.
- Documented sandbox build-argument version defaults and their limits, plus Oh My Zsh completion installation, LF normalization, BOM removal, and Codex completion.
- Documentation update only; no runtime code changes.

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

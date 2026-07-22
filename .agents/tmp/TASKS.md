# Taskline — Test Tasks

Derived from TEST_SPEC.md. Each task is one test file or one logical group.
Follow existing conventions: `package main`, `<source>_test.go`, standard
`testing`, `pgregory.net/rapid` for invariants (server), `httptest` for HTTP,
and the existing fakes (`fakeRunner`/`fakeProcess`/`fakeNotifier`). All tests
must run without external services (mock SMTP, use `t.TempDir()` for the
filesystem, `httptest.Server` for the CLI). No E2E/browser/visual tests.

## Phase 0 — Cleanup
- [x] Remove the stale `rapid` failure-seed artifact `server/testdata/rapid/TestProperty_QueueStatusResponseStructure/TestProperty_QueueStatusResponseStructure-20260722154644-3585.fail` — its test currently passes and the file is an untracked leftover reproduction seed.

## Phase 1 — High priority (core logic without coverage)
- [x] [HIGH] Create `cli/client_test.go` — test every `Client` method and `do` against an `httptest.Server`: correct method+path, request-body encoding, success decode, 4xx/5xx → `*APIError` from `{error,message}`, malformed/empty error body → `APIError{Code:"UNKNOWN"}` (trimmed raw text), empty success body (no decode), identifier path escaping, `NewClient` trailing-slash trim, connection error wrapping, `APIError.Error()`.
- [x] [HIGH] Create `cli/commands_test.go` — test `parseArgs` (positional, `--k v`, `--k=v`, flag mid-list, unknown flag, missing value), `Execute` unknown-subcommand, and each `cmd*` handler (arg-count/validation → exit 2, success output line, server error → exit 1) driving a real `Client` against an `httptest.Server` with captured stdout/stderr; include `cmdResume` with and without `message`, and `usageError`/`serverError`.
- [x] [HIGH] Create `server/main_test.go` — test `recoverRunningTask` (Req 1.10): running task in loaded queue → `failed`/exit `-2`, queue `halted`, `fakeNotifier` invoked once, state file written (via `t.TempDir()`); no running task → no-op, notifier not called; empty `stateFile` → no write/no panic.

## Phase 2 — Medium priority (edge cases, error handling)
- [x] [MEDIUM] Create `cli/main_test.go` — test `run`: no args → usage/exit 2; `-h`/`--help` → usage/exit 0; `-v`/`--version` → `taskline 1.0.0`/exit 0; invalid `TASKLINE_URL` (`t.Setenv`) → exit 2; valid subcommand dispatches to `Execute`.
- [x] [MEDIUM] Extend `cli/format_test.go` — add `FormatTaskList` (empty → `"No tasks in queue."`, header + column padding, `color=false` has no ANSI, `color=true` colorizes STATUS with reset), `TruncateCommand` >40-rune truncation (rune-counted, multibyte), and `padRight`.
- [x] [MEDIUM] Extend `server/worker_test.go` — add `ForceKill` (immediate SIGKILL only, task `failed`/exit `-1`; no running task → `ErrNoRunningTask`) and `finishTask` branches (no notify on success, one notify on failure, persist error to unwritable `stateFile` logged without panic/block).
- [x] [MEDIUM] Create `server/shellrunner_test.go` — integration test for `ShellRunner.Start`/`osProcess.Wait`/`Signal` using the local `sh` (skip if absent): `echo` → exit 0 with captured output, `exit 7` → exit 7, output goes to the provided writer, signal delivery.
- [x] [MEDIUM] Extend `server/queue_test.go` — add error/edge branches: `DeleteTask` (not found, running conflict, failed→idle), `UpdateTask` (invalid name, name taken, running/failed immutable, same-name allowed), `Resume` (halted, already-running, no-pending→idle+empty), `Snapshot`/`RemoveTask`/`FindTask` not-found, `StartNext` (not running / already running / no pending), `MarkComplete`/`MarkFailed` no-running, `CurrentTask`/`FailedTaskInfo` nil.
- [x] [MEDIUM] Extend `server/state_test.go` — add `SaveState` unwritable-dir error, `LoadState` read error when path is a directory (non-ENOENT), corrupted-JSON with rename failure wrapping `ErrCorruptedState`, and `valid` rejections (unknown state, unknown status, nil task, empty ID, whitespace command).
- [x] [MEDIUM] Extend `server/handler_test.go` — add a state-file-backed handler: `persist` writes after create; `handleRetry` success body + worker notify; `handleSkip` success `{id,name}`; `handleCreateTask` negative position → 400; `handleUpdateTask` malformed JSON → 400; `handleResume` → `state:"running"` with pending tasks; `taskPosition` → -1 for unknown id.
- [x] [MEDIUM] Extend `server/main_test.go` — add `waitForIdle` (true when idle; false after short timeout while a task stays running) and `enabledLabel(true/false)`.

## Phase 3 — Low priority (utilities, simple logic)
- [x] [LOW] Extend `cli/format_test.go` — add `statusColor` (running/pending/failed → color codes, other → "") and `ColorEnabled` (false when `NO_COLOR` set via `t.Setenv`; false when stdout is not a char device).
- [x] [LOW] Extend `server/names_test.go` — add `ReserveName` (true then false for the same name; reserved name never auto-generated) and `MarkUsed` empty-id/name no-op.
- [x] [LOW] Create `server/log_test.go` — capture `os.Stdout` and assert `logInfo`/`logWarn`/`logError` emit one line matching `YYYY-MM-DD HH:MM:SS [LEVEL] message` with the correct level and formatted args.
</content>

# Taskline — Test Specification

Scope: unit, integration, edge-case, and input-validation tests for the two Go
modules under `taskline/` (`server/` and `cli/`). E2E/browser/visual tests are
explicitly out of scope (handled by the ui-tests pipeline).

## 1. Detected tooling & conventions

- **Language / runtime:** Go 1.23, two independent modules (`taskline/server`,
  `taskline/cli`), both `package main`.
- **Test runner:** standard `go test ./...` (run from each module directory).
- **Assertions:** plain `testing` package (`t.Fatalf`/`t.Errorf`), no assertion
  library.
- **Property-based testing:** `pgregory.net/rapid` v1.2.0 (server module only),
  used for invariants; test names prefixed `TestProperty_*`. Race tests run with
  `CC=cc CGO_ENABLED=1 go test -race ./...`.
- **HTTP tests:** `net/http/httptest` (`httptest.NewRecorder` + a registered
  `*http.ServeMux` for the server; drive the CLI against an `httptest.Server`).
- **Test doubles (server):** hand-written fakes in `worker_test.go` —
  `fakeProcess` (Process), `fakeRunner` (CommandRunner), `fakeNotifier`
  (Notifier). Helpers: `newTestHandler`, `newTestQueue`, `doRequest`,
  `pollUntil`.
- **File naming:** `<source>_test.go` co-located with source in the same package.
- **Constraint:** all tests must run without external services — mock SMTP, the
  filesystem via `t.TempDir()`, and command execution via `fakeRunner`. The one
  intentional exception is a `ShellRunner` integration test that shells out to
  the local `sh` only (no network/service dependency).

## 2. Current coverage overview

Measured with `go test ./... -cover`:

| Module | Coverage | State |
|--------|----------|-------|
| server | 73.5% | Good on queue/state/handler/notify/names; gaps in `main.go`, real `ShellRunner`, worker edge paths |
| cli    | 4.1%  | Only `config.go` (LoadConfig) and part of `format.go` covered; `client.go`, `commands.go`, `main.go` entirely untested |

### Covered
- **server:** `queue.go` core paths, `state.go` round-trip + corruption,
  `handler.go` most endpoints, `notify.go` fully, `names.go` generation/validation,
  `config.go` fully, worker success/failure/stop-grace/sigkill/sequential.
- **cli:** `config.go` LoadConfig (default + scheme validation), `format.go`
  `FormatTimestamp` and part of `TruncateCommand`.

### Uncovered / partially covered
- **cli/client.go — 0%:** every HTTP method + `do` (request build, JSON encode,
  response decode, 4xx/5xx `APIError` parsing, malformed-error fallback,
  connection error), `NewClient` trailing-slash trim, `APIError.Error`.
- **cli/commands.go — 0%:** `parseArgs`, `Execute` dispatch, all nine `cmd*`
  handlers (arg validation, output formatting, server-error paths),
  `usageError`, `serverError`.
- **cli/main.go — 0%:** `run` (no-args usage, `--help`, `--version`, config
  error, subcommand dispatch).
- **cli/format.go — partial:** `FormatTaskList`, `padRight`, `statusColor` (0%),
  `TruncateCommand` truncation branch (75%), `ColorEnabled`/`isTerminal` (0%).
- **server/main.go — 0%:** `recoverRunningTask`, `waitForIdle`, `enabledLabel`
  (testable); `main`/`shutdown` call `os.Exit` and manage signals — not unit
  testable without refactor (documented, no task).
- **server/worker.go:** `ForceKill` (0%), `ShellRunner.Start`/`osProcess.Wait`/
  `Signal` (0%, real process), `finishTask` notify/persist-error branches (66.7%).
- **server/queue.go:** error/edge branches of `DeleteTask` (63.6%), `Resume`
  (72.7%), `UpdateTask` (79.2%), `Snapshot`, `RemoveTask`, `StartNext`,
  `MarkComplete`, `MarkFailed`, `FailedTaskInfo`.
- **server/state.go:** `SaveState` error paths (63.6%), `LoadState` read-error /
  rename-error branches (81.2%), `valid` rejection branches (75%).
- **server/handler.go:** `persist` with a real state file (50%), success paths of
  `handleRetry`/`handleSkip` (57.1%), negative-position + malformed-JSON
  branches, `taskPosition` miss.
- **server/names.go:** `ReserveName` already-taken branch (83.3%).
- **server/log.go:** `logWarn` (0%) / format assertion.

### Broken / orphaned artifacts
- `server/testdata/rapid/TestProperty_QueueStatusResponseStructure/…-20260722154644-3585.fail`
  — a stale `rapid` failure-reproduction seed from a prior run. The test
  currently passes and the file is untracked by git. Remove it (Phase 0).

## 3. Missing test groups (sorted by priority, then file)

### [HIGH] cli/client.go — `Client` HTTP methods and `do`
- **Behavior:** each method issues the correct method+path, encodes the request
  body, decodes the success body; `do` parses non-2xx responses into `*APIError`.
- **Mocking:** drive a real `Client` against an `httptest.Server` returning canned
  JSON/status; no live network.
- **Cases:** `CreateTask` (with/without name/position, position omitted vs set),
  `ListTasks` (array decode, empty), `DeleteTask`/`UpdateTask` path escaping of
  identifiers with spaces/slashes, `Retry`/`Skip`/`Stop`/`Resume`/`Status` decode;
  4xx → `APIError{Code,Message}` from `{error,message}` body; malformed/empty
  error body → `APIError{Code:"UNKNOWN"}` with trimmed raw text; success body
  empty (no decode); connection refused → wrapped error; `NewClient` trims a
  trailing `/` from baseURL; `APIError.Error()` returns Message.
- **Inputs/outputs:** e.g. server returns 409 `{"error":"CONFLICT","message":"x"}`
  → method returns `*APIError` with Code `CONFLICT`, Message `x`.

### [HIGH] cli/commands.go — argument parsing, dispatch, and command handlers
- **Behavior:** `parseArgs` classifies flags/positionals; `Execute` routes to the
  right handler; each `cmd*` validates args, calls the client, and formats output
  or returns the right exit code.
- **Mocking:** back the `*Client` with an `httptest.Server`; capture
  stdout/stderr via `bytes.Buffer`.
- **Cases:**
  - `parseArgs`: positional only; `--name value` and `--name=value`; flag in the
    middle of positionals; unknown flag → error; flag missing value → error.
  - `Execute`: unknown subcommand → `usageError` (exit 2).
  - `cmdAdd`: no command arg (exit 2); more than one positional (exit 2);
    non-integer `--position` (exit 2); success prints the "created" line;
    server error → exit 1.
  - `cmdList`: extra args (exit 2); success prints table.
  - `cmdDelete`: wrong arg count (exit 2); success line.
  - `cmdUpdate`: wrong arg count; neither `--command` nor `--name` (exit 2);
    success line.
  - `cmdRetry`/`cmdSkip`/`cmdStop`/`cmdResume`: reject extra args; success output;
    `cmdResume` with vs without `message`.
  - `cmdStatus`: reject extra args; prints State/Pending and optional
    current/failed task lines.
  - `usageError` → exit 2 + "Error:" on stderr; `serverError` → exit 1.

### [HIGH] server/main.go — `recoverRunningTask`
- **Behavior (Requirement 1.10):** a task left `running` in a loaded queue is
  transitioned to `failed` with exit code `-2`, the queue is halted, a failure
  notification is sent, and state is persisted before the worker starts.
- **Mocking:** in-memory `Queue` with one running task; `fakeNotifier`;
  `t.TempDir()` state file.
- **Cases:** running task present → status `failed`, `ExitCode == -2`, state
  `halted`, notifier invoked once, state file written; no running task → no-op
  (queue unchanged, notifier not called); empty `stateFile` → no write, no panic.

### [MEDIUM] cli/main.go — `run`
- **Behavior:** entry-point dispatch and top-level flags.
- **Cases:** no args → usage on stderr, exit 2; `-h`/`--help` → usage on stdout,
  exit 0; `-v`/`--version` → `taskline 1.0.0`, exit 0; invalid `TASKLINE_URL`
  (via `t.Setenv`) → "Error:" on stderr, exit 2; valid subcommand dispatches to
  `Execute` (may exit 1 on connection error to an unused port — acceptable).

### [MEDIUM] cli/format.go — `FormatTaskList`, `TruncateCommand`, `padRight`
- **Behavior:** column-aligned table; empty list message; truncation ellipsis;
  padding.
- **Cases:** empty slice → `"No tasks in queue."`; header row present and
  columns padded to widest value; `color=false` has no ANSI codes; `color=true`
  wraps the STATUS cell of running/pending/failed rows in the matching color code
  and reset; `TruncateCommand` returns input unchanged at ≤40 runes and appends
  `…` (total 40 runes) when longer, counting runes not bytes (multibyte input);
  `padRight` pads short strings and leaves long ones unchanged.

### [MEDIUM] server/worker.go — `ForceKill` and `finishTask` branches
- **Behavior:** `ForceKill` sends SIGKILL immediately (no SIGTERM grace) and
  blocks until the task is `failed` with `ExitCodeStopped`; `finishTask` sends a
  failure notification only on non-zero exit and logs (never returns) a persist
  error.
- **Mocking:** `fakeProcess`/`fakeRunner`/`fakeNotifier`.
- **Cases:** `ForceKill` with a running task → only SIGKILL in `signalsReceived`,
  task `failed`/exit `-1`; `ForceKill` with no running task → `ErrNoRunningTask`;
  successful finish sends no notification; failed finish invokes notifier once;
  persist to an unwritable `stateFile` is logged but does not panic or block.

### [MEDIUM] server/worker.go — `ShellRunner` real-process integration
- **Behavior (Reqs 3.4–3.6):** `ShellRunner.Start` runs a command via `sh -c`,
  piping stdout+stderr to the writer; `osProcess.Wait` returns 0 on success and
  the real exit code on failure; `Signal` delivers signals.
- **Mocking:** none — uses the local `sh` only (no network/service). Guard with a
  `sh` lookup skip if unavailable.
- **Cases:** `echo hi` → exit 0, output captured; `sh -c 'exit 7'` → exit 7;
  a nonexistent binary run via `sh` → non-zero exit (not spawn failure, since
  `sh` starts); `Start` of an unrunnable program directly is a spawn error path
  (documented — hard to force since `sh` almost always starts).

### [MEDIUM] server/queue.go — error and edge branches
- **Behavior:** exercise the untested error returns and state transitions.
- **Mocking:** in-memory `Queue` via `newTestQueue`.
- **Cases:** `DeleteTask` — not found → `ErrTaskNotFound`; running task →
  `ErrTaskRunning`; deleting the failed task → queue `idle`. `UpdateTask` —
  invalid new name → validation error; name collision with another task →
  `ErrNameTaken`; running → `ErrTaskRunning`; failed → `ErrTaskFailedImmutable`;
  updating to the same name is allowed. `Resume` — halted → `ErrQueueHalted`;
  already running → `ErrQueueAlreadyRunning`; no pending → `idle` + `empty=true`.
  `Snapshot`/`RemoveTask`/`FindTask` not-found → `ErrTaskNotFound`. `StartNext` —
  returns nil when not running, when a task already running, and when no pending.
  `MarkComplete`/`MarkFailed` with no running task → `ErrNoRunningTask`.
  `CurrentTask`/`FailedTaskInfo` → nil when none.

### [MEDIUM] server/state.go — error paths and `valid`
- **Behavior:** persistence failure handling and schema validation.
- **Mocking:** `t.TempDir()`; use an unwritable directory / a path that is a
  directory to force errors.
- **Cases:** `SaveState` to a nonexistent/unwritable directory → error returned;
  `LoadState` when path is a directory (read error, non-ENOENT) → empty queue +
  wrapped error; corrupted JSON when rename fails (e.g. read-only dir) → error
  wraps `ErrCorruptedState`; `valid` rejects unknown queue state, unknown task
  status, nil task, empty ID, and whitespace-only command.

### [MEDIUM] server/handler.go — persistence and success paths
- **Behavior:** handler mutations persist to disk and return correct bodies.
- **Mocking:** `newTestHandler` variant with a `t.TempDir()` state file.
- **Cases:** `persist` writes the state file after a create; `handleRetry`
  success returns the retried task JSON and notifies the worker; `handleSkip`
  success returns `{id,name}`; `handleCreateTask` negative position → 400
  `VALIDATION_ERROR`; `handleUpdateTask` malformed JSON → 400; `handleResume`
  returns `state:"running"` when pending tasks remain; `taskPosition` returns -1
  for an unknown id.

### [MEDIUM] server/main.go — `waitForIdle` and `enabledLabel`
- **Cases:** `waitForIdle` returns true immediately when no task is running;
  returns false after the timeout while a task stays running (use a short
  timeout and a queue with a running task); `enabledLabel(true)` → "enabled",
  `enabledLabel(false)` → "disabled".

### [LOW] cli/format.go — `statusColor` and `ColorEnabled`
- **Cases:** `statusColor` maps running/pending/failed → green/yellow/red and
  anything else → ""; `ColorEnabled` returns false when `NO_COLOR` is set (via
  `t.Setenv`) and false when stdout is not a char device (the test pipe case).

### [LOW] server/names.go — `ReserveName` / `MarkUsed` edges
- **Cases:** `ReserveName` returns true then false for the same name; a reserved
  name is never produced by `GenerateName`; `MarkUsed` with empty id/name is a
  no-op.

### [LOW] server/log.go — log formatting
- **Cases:** capture `os.Stdout` and assert `logInfo`/`logWarn`/`logError`
  emit a single line matching `^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} \[LEVEL\] msg$`
  with the correct level token and formatted args.
</content>
</invoke>

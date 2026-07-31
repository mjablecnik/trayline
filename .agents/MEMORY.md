# Memory

## git show <ref>:<path> succeeds for directory paths and returns a tree listing, not an error
- Project: trayline (remote, 001-dashboard-api-projects spec)
- Problem: `git show <ref>:<path>` does not fail when `<path>` resolves to a directory (tree) rather than a file (blob) — it exits 0 and prints a plain-text tree listing to stdout instead. A naive `git.Blob(repoPath, ref, path)` built on `git show` alone would silently return that listing as if it were file content for any directory path, instead of erroring, which would misbehave badly in the future blob HTTP endpoint (task 11): a client requesting `/blob/<ref>/src` (a directory) would get 200 with fake "file content" instead of a 404.
- Solution: Before calling `git show`, resolve the object type with `git cat-file -t <ref>:<path>` and require it to be exactly `"blob"`; treat anything else (`"tree"`, missing object, bad ref) as `ErrNotFound`. See `remote/git/tree.go`'s `Blob()`.
- Source: create-code, 2026-07-30 16:20

## Splitting a flat package main into layered packages strands cross-cutting test-only and CLI-only helpers
- Project: orchestrator (orchestrator-packages spec)
- Problem: When a flat `package main` (orchestrator/) is split into `core`/`llm`/`engine`/`cmd`, several small unexported helpers that were implicitly shared because everything compiled into one package become unreachable across the new boundaries: (1) `mockEvaluator` (a ConditionEvaluator test double defined in the old `executor_test.go`) was also used by `llm_logger_test.go` — after the split, `llm/logger_test.go` needed its own copy since it can't import engine's unexported test type. (2) Two dry-run tests in `variables_test.go` (`TestDryRunShowsResolvedVariables`, `TestDryRunNoVariablesSectionWhenEmpty`) plus one rapid property test called `Executor.printDryRun()` (unexported) — these test engine behavior, not core behavior, so they had to be deleted from `core/variables_test.go` rather than "moved with qualified types", since core must not depend on engine. (3) `flow.go`'s `runFlow`(→`RunFlow`) internally used `programName()`, `varFlags`, and `findLifecycleFile()` — all three were designated in the design doc to "stay in cmd/" (CLI-specific), but the flow subcommand's actual logic needs them at runtime and engine cannot import cmd (that's backwards). (4) `cmd/main.go`'s `runWithLifecycle` used the ANSI color constants (`colorRed`, `colorDim`, etc.) which the design explicitly keeps unexported in `engine`. (5) Both the old `main_test.go` and the old `flow_test.go` each had their own near-duplicate `usageText()`/`flowUsageText()` non-empty tests (different test names, e.g. `TestUsageTextNonEmpty` vs `TestUsageText_NonEmpty`) — after the split only one of each is reachable per package.
- Solution: For (1)/(3)/(4), duplicate the tiny trivial helper/const-block into each package that needs it (a few lines each — `programName`, `varFlags`, `findLifecycleFile`, the ANSI color consts, `mockEvaluator`) rather than exporting internals or introducing a shared "common" package; this preserves the design doc's stated per-package dependency isolation (Req 5, 9) at the cost of small, harmless duplication. For (2)/(5), delete the test from whichever package doesn't own the behavior being tested and keep/adjust the copy in the package that does (e.g. keep `TestFlowUsageText_NonEmpty` in `engine/flow_test.go`, keep `TestUsageTextNonEmpty` in `cmd/main_test.go`, drop the other side) — don't try to satisfy both by cross-importing test packages. Also watch for a compiler-feedback loop with `go build ./package` iterated per package (leaf packages `core`/`llm` first, then `engine`, then `cmd`) — it surfaces every one of these strandings as an `undefined: X` error, which is far faster than trying to plan all qualifications up front.
- Source: create-code, 2026-07-29 16:20

## pgregory.net/rapid v1.x uses generic Draw() API
- Project: server
- Problem: Tests written with old rapid API used type assertions like `.Draw(t, "name").(string)`, which fail to compile in rapid v1.2.0 because `Draw` now returns the generic type directly (not `interface{}`).
- Solution: Remove all type assertions after `.Draw(t, "name")` calls. The value is already the correct type.
- Source: create-code, 2026-07-01

## github.com/docker/docker module path with go 1.23
- Project: server
- Problem: `github.com/docker/docker/client` has been moved to `github.com/moby/moby/client`. The latest `github.com/docker/docker` module (v28.x) redirects to the new path and fails to resolve with `go get`. Also, `golang.org/x/time` v0.15.0 requires go >= 1.25.0.
- Solution: Use `github.com/docker/docker@v24.0.9+incompatible` (pre-moby-rename version) and `golang.org/x/time@v0.9.0` (compatible with go 1.23).
- Source: create-code, 2026-07-01

## rapid StringMatching("[^0-9]+") generates null bytes, breaking os.Setenv
- Project: server
- Problem: `rapid.StringMatching("[^0-9]+")` can produce strings with null bytes (e.g. `"\x00\x01+"`). `os.Setenv` on Linux returns an error for such strings but the test didn't check it, so the env var kept its previous valid value and `LoadConfig()` returned nil instead of an error, failing the property test.
- Solution: Guard the `os.Setenv` call with `if err := os.Setenv(...); err != nil { t.Skip(...) }`, matching the pattern already used in the `invalid MAX_CONCURRENT_TASKS` subtest.
- Source: check-build, 2026-07-01

## State persistence exists but is never called on state changes
- Project: server
- Problem: `StateManager.Save()` is implemented and wired into `main.go`, but no handler ever calls it on task/session state changes (and the one `Save()` in `Recover()` is skipped on a fresh start). The recovery logic (Req 19) is therefore dead code — nothing is ever written to `state.json` at runtime. Build and tests still pass because persistence has no unit coverage tying saves to mutations.
- Solution: Inject `StateManager` into the task and session handlers and call `Save()` after every mutation (add/status-change/create/terminate). When reviewing "atomic write + recovery" specs, always verify the save is actually *triggered*, not just implemented.
- Source: code-review, 2026-07-01 07:10

## WebSocket capacity check must happen before the upgrade
- Project: server
- Problem: `HandleChat` upgrades the WebSocket first, then blocks on a semaphore (`acquireSlot`) when at MAX_CONCURRENT_TASKS. Once upgraded, an HTTP 503 can no longer be returned (Req 14.5), so at-capacity clients hang silently instead of being rejected.
- Solution: Check for a free slot (non-blocking) BEFORE `upgrader.Upgrade`; return 503 JSON if none. Same pattern applies to any pre-upgrade validation that needs an HTTP status code.
- Source: code-review, 2026-07-01 07:10

## Recovered chat sessions must re-attach stdin/stdout and set ctx/cancel
- Project: server
- Problem: `recoverSessions` restores only session metadata (no `Stdin`, no `Ctx`/`CancelFunc`, no `streamOutput` goroutine). Recovered sessions can't forward messages, stream output, be terminated, or time out — they leak (Req 19.6, 12.3, 20.1).
- Solution: On recovery of a running-container session, mirror `HandleChat`: WithCancel context, `AttachChatContainer`, store `Stdin`, start `streamOutput` + the `<-ctx.Done()` cleanup goroutine. `StateManager` needs a `sessionH *SessionHandler` field (set via `SetSessionHandler`) so `recoverSessions` can call `sessionH.streamOutput`. Wire this in `main.go` before calling `Recover`.
- Source: code-review-fix, 2026-07-01

## StartChatContainer must not acquire slot — pre-acquire before WebSocket upgrade
- Project: server
- Problem: The old `StartChatContainer` called `acquireSlot` (blocking). `HandleChat` upgraded the WebSocket first, then called it — so when at capacity, the WebSocket was already upgraded and HTTP 503 could no longer be sent. Clients hung silently.
- Solution: Add `TryAcquireSlot() bool` (non-blocking) to ContainerManager. Call it in `HandleChat` BEFORE upgrade; return 503 if denied. Remove slot acquisition from `StartChatContainer` (caller now owns the slot). Track `slotAcquired` bool in recovery path for correct `ReleaseChatSlot` on cleanup.
- Source: code-review-fix, 2026-07-01

## parseSegment requires flags before pipeline path positional arg
- Project: orchestrator
- Problem: `parseSegment(["proc/p1", "--var", "a=1"])` gives vars={} because Go's `flag.FlagSet.Parse` stops at the first non-flag argument ("proc/p1"). Flags after the path are silently treated as extra positional args.
- Solution: For tests and correct usage, pass `--var` flags BEFORE the positional path: `["--var", "a=1", "proc/p1"]`. The common CLI pattern `pipeline --var k=v` is broken by this limitation. Tests assert this behavior without fixing it.
- Source: create-tests, 2026-07-01

## checkIdleSessions does not remove sessions from store
- Project: server
- Problem: `checkIdleSessions` cancels the session's `CancelFunc` and closes `Conn`, but does NOT call `store.Remove`. Session removal only happens in the HandleChat goroutine that watches `<-ctx.Done()`. Tests of `checkIdleSessions` should assert context cancellation, not store eviction.
- Solution: Tests use `context.WithCancel` and check `ctx.Done()` to verify termination, not store membership.
- Source: create-tests, 2026-07-01

## limitWriter returns len(truncated_p) not original len(p)
- Project: server
- Problem: `limitWriter.Write` slices `p` to `p[:remaining]` before writing, so `len(p)` at return is the truncated length, not the original. Violates the io.Writer contract (short write without error), but callers (stdcopy) ignore the return value.
- Solution: Tests assert `n == len(truncated_p)` (current behavior) with a comment noting the contract deviation.
- Source: create-tests, 2026-07-01

## Checkpoint tests must change working directory (not inject path)
- Project: orchestrator
- Problem: `checkpointDir` and `flowCheckpointFile` are hardcoded constants relative to CWD. Tests cannot inject a temp path via env vars or arguments.
- Solution: Use `os.Chdir(t.TempDir())` + `t.Cleanup(func() { os.Chdir(orig) })`. Do NOT call `t.Parallel()` in these tests to avoid concurrent CWD mutations.
- Source: create-tests, 2026-07-01

## streamOutput done-per-turn via idle timeout
- Project: server
- Problem: `streamOutput` only sent `{"type":"done"}` when the container exited (scanner loop end). For interactive sessions, the container never exits between turns so clients never got turn boundaries.
- Solution: Use a goroutine+channel pattern: producer reads scanner lines into a buffered channel. Consumer selects on `lineCh` vs `time.After(500ms)`. When idle for 500ms after the last output line, send `done`. Channel closure (container exit) also sends a final `done`.
- Source: code-review-fix, 2026-07-01

## stateTestMock in store/state_test.go was incomplete
- Project: server
- Problem: `stateTestMock` was missing `ContainerAttach`, `ContainerStop`, `ContainerRemove`, `ContainerWait`, `ContainerKill` methods, and `ContainerCreate`/`ContainerLogs` had wrong signatures (used `interface{}` instead of real Docker types). This caused the entire `server/store` test package to fail to compile.
- Solution: Added all missing methods with correct Docker SDK type signatures and changed `ContainerLogs` to return `io.NopCloser(strings.NewReader(""))` (not `io.NopCloser(nil)`, which panics when stdcopy reads it).
- Source: check-build, 2026-07-04

## rapid.StringMatching for "non-empty command" generators can produce whitespace-only strings
- Project: taskline
- Problem: A rapid regex like `[a-zA-Z0-9 _/.-]{1,40}` used to generate test command strings can produce a single space `" "`, which is whitespace-only and gets rejected by the "command must not be empty/whitespace" validation (`strings.TrimSpace(command) == ""`). Property tests that seed queues with generated commands then fail intermittently with "command must not be empty".
- Solution: Anchor the regex so the first character can never be whitespace, e.g. `[a-zA-Z0-9_/.-][a-zA-Z0-9 _/.-]{0,39}` (first char from a non-space class, rest unrestricted). Apply this pattern anywhere a "valid, non-empty, non-whitespace" string generator is needed (worker/handler tests later in this spec).
- Source: create-code, 2026-07-22

## CGO_ENABLED race detector needs CC=cc (no gcc) in this environment
- Project: taskline
- Problem: `go test -race ./...` fails with `cgo: C compiler "gcc" not found`, even after `CGO_ENABLED=1`, because this environment only has `/usr/bin/cc`, not `gcc`, on PATH.
- Solution: Run race-detector tests with `CC=cc CGO_ENABLED=1 go test -race ./...`.
- Source: create-code, 2026-07-22

## Queue.State/Task pointers need lock-protected accessors once a concurrent Worker exists
- Project: taskline
- Problem: `Queue.FindTask`/`Queue.List` return raw `*Task` pointers, and `Queue.State` is a plain exported field. Once `worker.go` added a goroutine that mutates `Task.Status`/`ExitCode` and `Queue.State` under `q.mu`, any other goroutine (tests, later HTTP handlers) reading those same fields without going through the mutex triggers `go test -race` failures, even though the values themselves are never wrong.
- Solution: Added `Queue.CurrentState() QueueState` and `Queue.Snapshot(identifier) (Task, error)` (returns a copy, not a pointer) to `queue.go`. Use these instead of direct field access or `FindTask`/`List` whenever a read can race a running Worker — this will matter again in the handler.go task (7.x) for `/queue/status` and `/tasks`.
- Source: create-code, 2026-07-22

## rapid.Check callbacks can't reuse *testing.T-based polling helpers
- Project: taskline
- Problem: `worker_test.go` has a `waitFor(t *testing.T, timeout, cond)` helper used by worker tests. Inside a `rapid.Check(t, func(t *rapid.T) {...})` callback only a `*rapid.T` is in scope, not a `*testing.T`; passing a throwaway `&testing.T{}` compiles but is unsafe (its internal state isn't initialized by the test runner, so `Fatalf`/`FailNow` on it can misbehave).
- Solution: For polling inside a property test, write a local `pollUntil(timeout, cond) bool` that just loops/sleeps and returns a bool (no `TB` dependency), then call `t.Fatalf(...)` on the real `*rapid.T` if it returns false. `*rapid.T` supports `Fatalf` directly, same as `*testing.T`.
- Source: create-code, 2026-07-22

## fakeProcess in worker_test.go requires an explicit .finish() call
- Project: taskline
- Problem: `newFakeProcess(exitCode)` blocks `Wait()` on an internal channel until `.finish()` is called; forgetting to call it (e.g. when reusing the fakeRunner/fakeProcess doubles from worker_test.go inside a new handler_test.go scenario) makes the Worker's `executeTask` hang forever waiting for the process to "exit", so any test polling for a status transition (e.g. queue reaching "halted") times out after the full poll timeout — with a 100-iteration rapid.Check this multiplies into a very slow, always-failing test.
- Solution: Every `fakeProcess` enqueued via `fakeRunner.enqueue` needs `.finish()` called (either immediately for tasks that should complete right away, or later/never for tasks meant to stay "running" during the test).
- Source: create-code, 2026-07-22

## Shutdown SIGKILL escalation must bypass Worker.Stop's own SIGTERM grace period
- Project: taskline
- Problem: Requirement 1.5 says that if the running Task hasn't finished within 30s of SIGTERM/SIGINT, the server must send SIGKILL directly. Reusing the existing `Worker.Stop()` (SIGTERM -> wait up to `stopGraceTimeout` (5s) -> SIGKILL) from `main.go`'s shutdown path would tack on an extra SIGTERM + up to 5s wait after the 30s budget already elapsed, which doesn't match the spec and duplicates a step that was never asked for at shutdown time.
- Solution: Added `Worker.ForceKill()` to worker.go — same locking/`runningTask.done` pattern as `Stop()`, but signals SIGKILL immediately with no SIGTERM step. `main.go`'s shutdown handler polls `queue.CurrentTask()` for up to 30s (`shutdownGraceTimeout`), then calls `ForceKill()` only if the task is still running after that.
- Source: create-code, 2026-07-22

## handleRetry/handleSkip read Task pointers unsynchronized with the Worker
- Project: taskline (server)
- Problem: `handler.go`'s `handleRetry` calls `h.worker.Notify()` and then immediately reads the `*Task` returned by `h.queue.Retry()` via `toTaskResponse(task)` — without holding `q.mu`. If a Worker goroutine is already running and picks up the retried task right after `Notify()`, it mutates that same `*Task`'s `Status`/`ExitCode` fields concurrently (under lock, but the handler's read isn't), so `go test -race` flags a data race. This is a genuine pre-existing bug in production code, not a test artifact — reachable any time a retry/resume races the Worker's next `StartNext()`.
- Solution: Did not modify handler.go (out of scope for a test-only task). In `server/handler_test.go`'s `TestHandleRetry_Success`, start the Worker goroutine only *after* the HTTP response has been read (not before/concurrently), so the test itself doesn't trip the race detector while still exercising the retry-then-completes flow. A real fix would have `handleRetry`/`handleSkip`/similar handlers copy needed fields (e.g. via the existing `Queue.Snapshot`/`Queue.CurrentState` lock-protected accessors) before calling `Notify()`.
- Source: create-tests, 2026-07-22 16:55

## scripts/ held 6 files, spec/task list only named 3
- Project: trayline (root monorepo restructure)
- Problem: `.kiro/specs/monorepo-restructure/tasks.md` task 1 says to `git mv` only `scripts/trayline`, `scripts/trayline-agent`, and `scripts/sync.sh` into `runtime/`, then "remove the now-empty `scripts/` directory" — but `scripts/` actually also contained `install-pipelines.sh`, `reinstall.sh`, and `uninstall.sh`, which the spec never mentions, so `scripts/` would not be empty after task 1's literal steps.
- Solution: Judged these 3 extra scripts as installation-domain (they call `install.sh`/build `~/.trayline`), not runtime-domain, so moved them into `setup/` alongside `install.sh` in task 3 instead of `runtime/`. This keeps `scripts/` empty and removable per requirement 1.7, without forcing them into `runtime/` where they don't conceptually belong. If a future task's content-update pass (task 7+) touches `setup/install.sh` paths, also check `setup/reinstall.sh` and `setup/install-pipelines.sh` for now-stale `$REPO_DIR/scripts/...` references — they still point at the old scripts/ location and need updating to `runtime/`.
- Source: create-code, 2026-07-29 13:10

## server/ and client/ had untracked/gitignored files beyond the spec's file list
- Project: trayline (root monorepo restructure)
- Problem: Requirements/design/tasks docs for the server+client merge into `remote/` only enumerate tracked `.go`/script/doc files. In practice `server/` also had an untracked-but-not-ignored `.env` (real local config, 1090 bytes) and a gitignored `testdata/rapid/*.fail` debris dir; `client/` had a gitignored `bin/trayline-client` build artifact. None of these can go through `git mv` (not tracked by git).
- Solution: Used a plain `mv` for `server/.env` -> `remote/.env` (real content worth keeping), and `rm -rf` for the gitignored debris (`server/testdata`, `client/bin/`) since they're regenerable build/test artifacts. Also deleted (via `git rm`, not `git mv`) `server/go.mod`/`go.sum` and `client/go.mod`/`go.sum` — per design.md these are superseded by a newly-created unified `remote/go.mod` in the next task, not renamed, so `remote/` intentionally has no `go.mod` between the tasks-4/5 commit and the task-6 commit.
- Source: create-code, 2026-07-29 13:10

## go build ./... inside orchestrator/ overwrites a tracked binary named orchestrator/orchestrator
- Project: trayline (orchestrator)
- Problem: The orchestrator repo has a pre-existing tracked binary at `orchestrator/orchestrator` (checked into git, like the taskline and client binaries elsewhere in this repo). Running `go build ./...` from inside `orchestrator/` writes a new `orchestrator` binary at the module root, silently overwriting that tracked file's on-disk content, and a cleanup `rm -f orchestrator/orchestrator` after the build then shows as a tracked deletion in `git status`.
- Solution: After building/testing `orchestrator/` for verification, run `git checkout -- orchestrator/orchestrator` to restore the tracked binary rather than leaving it deleted or rebuilt-but-uncommitted. When verifying this module in future runs, prefer `go build -o /tmp/orchestrator-verify ./...` to avoid touching the tracked binary at all.
- Source: create-code, 2026-07-29 13:10

## Verifying a self-locating install.sh requires running it from its real repo path, and zsh isn't installed here
- Project: trayline (setup/install.sh)
- Problem: `setup/install.sh` derives `REPO_ROOT="$(dirname "$SCRIPT_DIR")"` from its own `$0`, so copying it elsewhere (e.g. `/tmp/install_test.sh`) to dry-run breaks all path resolution — `REPO_ROOT` no longer points at the actual repo. Separately, this sandbox has no `zsh` binary and no working `apt-get`/`sudo` to install one, so the script (which uses zsh-only glob qualifiers like `*.yaml(N)`) can't be executed with its real shebang here.
- Solution: To dry-run this kind of self-locating script, copy it to a scratch filename *inside its real directory* (`setup/install_test.sh`), not to `/tmp`, so `$0`'s directory still resolves correctly — then delete the scratch copy after. For the shebang/zsh-glob problem specifically, make a throwaway copy with `#!/bin/bash` + `shopt -s nullglob` and the `(N)` qualifier stripped, run that copy in-place, then discard it; never edit the tracked `install.sh` for this purpose.
- Source: create-code, 2026-07-29 13:35

## remote/scripts/build-client.sh still assumes the pre-merge client/ layout
- Project: trayline (remote)
- Problem: After task 5 moved `client/*.go` into `remote/cmd/client/`, `remote/scripts/build-client.sh` still does `cd "${PROJECT_DIR}"` (= `remote/`) then `go build -o bin/trayline-client .` — but `.` at `remote/` root has no `main` package anymore (main.go is at `remote/cmd/client/main.go`), so this script is broken. It wasn't in scope for tasks 6-10 (which only cover go.mod/imports, install.sh, gitignore, README, CLAUDE.md), so it was left unfixed.
- Solution: Fixed in task 14. Changed to `go build -o bin/trayline-client ./cmd/client`. Also see [[monorepo-restructure-task14-stale-references]] for the full set of stale-reference bugs found and fixed in the same pass.
- Source: create-code, 2026-07-29 13:35

## Task-14 stale-reference sweep found 6 broken scripts/docs beyond build-client.sh
- Project: trayline (root monorepo restructure)
- Problem: A grep-based sweep for old paths (scripts/, server/, client/ as top-level dirs) turned up several more references that survived tasks 1-10 unfixed, none of them caught by `go build`/`go test` because they're only exercised by shell scripts or docs: (1) `remote/Dockerfile` still did `go build -o server .` at the `remote/` root, which has no main package post-merge; (2) `remote/scripts/install-client.sh` called `"${SCRIPT_DIR}/build.sh"` (the *server* docker-build script, since the client's own build script was renamed to `build-client.sh` during the merge) instead of `build-client.sh`, and copied completions from `${PROJECT_DIR}/completions/_trayline-client` which no longer exists (moved to `remote/cmd/client/completions/`); (3) `setup/install-pipelines.sh` (one of the 3 extra scripts noted in [[monorepo-restructure-scripts-extra-files]]) still read `$REPO_DIR/scripts/trayline-agent`, `$REPO_DIR/scripts/sync.sh` (scripts/ no longer exists) and `$REPO_DIR/.rsyncignore` (moved to `setup/.rsyncignore`); (4) `setup/reinstall.sh` called `"$SCRIPT_DIR/../install.sh"` — but since reinstall.sh and install.sh now live in the *same* `setup/` directory, `../install.sh` resolves one level too high (repo root, where install.sh no longer exists); (5) `remote/README.md`'s "Running locally" and "Running Tests" sections said `cd server` — server/ no longer exists, and .env/.env.example now live at `remote/` root (loaded via `godotenv.Load()` from CWD), so the correct invocation is `go run ./cmd/server` run from `remote/` directly, not a `cd`.
- Solution: Fixed all of the above (Dockerfile: `go build -o server ./cmd/server`; install-client.sh: call `build-client.sh`, copy from `cmd/client/completions/`; install-pipelines.sh: read from `runtime/` and `$SCRIPT_DIR/.rsyncignore`; reinstall.sh: call `"$SCRIPT_DIR/install.sh"`; README.md: drop the `cd server`, use `go run ./cmd/server`). General lesson: `go build`/`go test` passing does NOT prove shell scripts and markdown docs got their path references updated after a directory merge — a targeted grep sweep (`grep -rn 'scripts/\|server/\|client/\|\.\./install'`) across `*.sh`/`*.md`/`Dockerfile` is required as a separate verification step, since these references are invisible to the Go toolchain.
- Source: create-code, 2026-07-29 13:50

## tools/tunnel/README.md described a WireGuard+Caddy design that was migrated to chisel
- Project: trayline (tools/tunnel)
- Problem: `tools/tunnel/README.md` documented a WireGuard-server + Caddy-reverse-proxy architecture (WireGuard key generation, `wg0` interface, `Caddyfile`, `WG_*` env vars, `/health` JSON with `wireguard`/`peer_handshake_seconds_ago` fields). The actual implementation (`relay/entrypoint.sh`, `home-agent/entrypoint.sh`, `relay/health.sh`, both `.env.example` files, `relay/fly.toml`) is entirely chisel-based (reverse tunnel via `chisel server --reverse` / `chisel client`), with no Caddyfile, no WireGuard tooling, and a health check that returns `{"chisel": "running"|"stopped"}`. The doc also still used stale `tunnel/...` path prefixes (e.g. `cd tunnel/relay`) left over from before `git mv tunnel tools/tunnel` in the monorepo restructure (`.kiro/specs/monorepo-restructure/tasks.md` task 2) — a dev running the documented commands from `tools/tunnel/` would `cd` into a nonexistent path.
- Solution: Rewrote `tools/tunnel/README.md` end-to-end to match the actual chisel implementation (architecture diagram, prerequisites table, directory tree, setup/env-var steps, local testing, Fly.io deploy, troubleshooting) and fixed all `tunnel/...` paths to be relative to the README's own directory. `go build`/`go test`/`docker build` never touch README prose, so this kind of architecture-level doc drift is invisible to every other verification step — a scan for "does this doc's described architecture match the actual entrypoint/health/env files" is needed whenever a service's core mechanism (not just its file layout) has changed.
- Source: check-build, 2026-07-29 13:35

## Spec 002 depends on a spec 001 helper (project name validation) that was never actually implemented
- Project: trayline (remote, 002-dashboard-api-git spec)
- Problem: `.kiro/specs/002-dashboard-api-git/requirements.md` states "Dependencies: 001-dashboard-api-projects (git package, router, project validation)", and design.md's `GitHandler` doc comment says 404 handling "reuses validation from spec 001". But `.kiro/specs/001-dashboard-api-projects/tasks.md` tasks 6-12 (path security helpers `resolveProjectPath`/`validateSubPath`, `ProjectHandler`, and all `/projects` routes) are still unchecked `[ ]` — only tasks 1-5 (CORS, config, git package foundation/branch/tree/blob) were ever completed. There is no `resolveProjectPath` or `ProjectHandler` anywhere in `remote/api` to import.
- Solution: Added a minimal, scoped-to-002 `resolveProjectPath(name string) (string, error)` method directly on `GitHandler` in `remote/api/git_handler.go` (regex allowlist for the name, `os.Stat` + `git.IsRepo` check under `projectsDir`) rather than trying to build out all of spec 001's task 6/7 (that's a larger, separate unit of work belonging to spec 001, not 002). When spec 001's real `ProjectHandler`/`resolveProjectPath` eventually get built, `git_handler.go`'s copy should be deleted and replaced with the shared one — don't let both persist long-term.
- Source: create-code, 2026-07-30 (002-dashboard-api-git tasks 1-5)

## git diff-tree -p omits the initial commit's diff unless --root is passed
- Project: trayline (remote/git)
- Problem: `git diff-tree -p <hash>` (used for `Show`'s unified diff, spec 002 task 2) diffs a commit against its parent tree. For the repo's very first commit (no parent), the default behavior is to print nothing at all — not an error, just an empty diff — even though the commit clearly added files. Any repo whose commit history is being shown from the beginning (or any test fixture with only 1-2 commits) would silently get an empty `Diff` field for that commit.
- Solution: Always pass `--root` to `git diff-tree -p --root <hash>` in `git/show.go`'s `Show()`. It has no effect on non-root commits (still diffs against the real parent) and makes root commits diff against the empty tree, producing the expected "everything added" diff. Verified with `TestShowRootCommit` in `git/show_test.go`, which asserts the root commit's diff actually contains `+hello`.
- Source: create-code, 2026-07-30 (002-dashboard-api-git task 2)

## projectNameRe allows "." and ".." as standalone project names, letting resolveProjectPath escape projectsDir
- Project: trayline (remote/api, 001-dashboard-api-projects spec)
- Problem: The project-name allowlist regex `^[A-Za-z0-9._-]+$` (originally in `git_handler.go`, spec 002) permits both `.` and `-` characters, so the strings `"."` and `".."` fully match it even though they contain no separator. `resolveProjectPath` then does `filepath.Join(projectsDir, name)`, so a request with project name `".."` resolves to the *parent* of `projectsDir` (and `"."` resolves to `projectsDir` itself) — bypassing the "reject any path containing .. segments" rule from REQ-7 for the name segment specifically (the existing test only covered `"../myproject"`, which the `/` correctly fails the regex on, so this slipped through unnoticed in spec 002).
- Solution: Added an explicit `name == "." || name == ".."` rejection in `resolveProjectPath` (now shared in `remote/api/project_security.go`) before the regex check. When spec 001 built the real, shared `resolveProjectPath`/`validateSubPath` for tasks 6-10, the old duplicate copy in `git_handler.go` (see the "Spec 002 depends on a spec 001 helper..." entry above) was deleted and `git_handler.go`'s `HandleGetCommits` now calls the shared package-level function. If any other code still has its own copy of this regex-based name check, apply the same `.`/`..` guard there too.
- Source: create-code, 2026-07-30 (001-dashboard-api-projects tasks 6-10)

## eslint-plugin-svelte's no-navigation-without-resolve rule doesn't see through indirection to a resolve() call
- Project: trayline (dashboard, 004-dashboard-frontend-setup spec)
- Problem: `svelte/no-navigation-without-resolve` (from `eslint-plugin-svelte`, required by this project's `eslint.config.js`) flags `<a href={tab.href}>` as "Unexpected href link without resolve()" even when `tab.href` was itself built with `resolve('/[project]/commits', { project })` — e.g. inside a `$derived([{ href: resolve(...) }, ...])` array rendered via `{#each}`. The rule's `isValueAllowed` only recognizes a direct `resolve(...)` `CallExpression` (via `expressionIsResolveCall`) or, for a bare `Identifier`, a TS-checker lookup against the `ResolvedPathname` type (`expressionIsAllowedType`) — a `MemberExpression` like `tab.href` on an each-block loop variable doesn't reliably map back through the Svelte-template TS node map, so the checker gives up and the rule fires even though `svelte-check`/`tsc` consider the type correct.
- Solution: Don't store pre-resolved hrefs in a data structure and dereference them in the template. Call `resolve(...)` directly inline in each `href={...}` attribute (unrolling a loop into explicit `<a>` tags if needed, e.g. `dashboard/src/routes/[project]/+layout.svelte`'s tab nav). Same applies to `goto(...)` calls — pass `resolve(...)` directly as the argument, not a variable that merely holds the result of an earlier `resolve()` call in some cases (plain `const x = resolve(...); goto(x)` does work since the rule's Identifier-path type check handles that case, but a member/property access off a loop or array variable does not).
- Source: create-code, 2026-07-30 (004-dashboard-frontend-setup tasks 6-7)

## resolve() supports a query string appended to the route-id literal — use that instead of concatenating; native history.replaceState sidesteps the rule for dynamic-route URL tweaks
- Project: trayline (dashboard, 005-dashboard-frontend-projects spec)
- Problem: Extending the `no-navigation-without-resolve`-safe pattern (see the entry above) to a tab bar that must preserve a `?ref=<branch>` query param across links: writing `href={resolve('/[project]/commits', { project }) + query}` still trips `linkWithoutResolve`, because the rule's `expressionIsResolveCall` only accepts a bare `resolve(...)` `CallExpression` (or an `Identifier` whose init is one) — wrapping it in a `BinaryExpression` (string concatenation) is invisible to the check even though the runtime result is a valid URL. Separately, a branch-switcher needs to update just the current page's `?ref=` query param without knowing which of the 4 tab routes (or the commit-detail route) is currently active, so there's no single literal route-id string to pass to `resolve()`/`goto()`/`replaceState()` at that call site.
- Solution: (1) For static per-tab links, build the query string *inside* the template literal passed as `resolve()`'s first argument — `resolve(`/[project]/commits?ref=${refParam}`, { project })` — since SvelteKit's `RouteIdWithSearchOrHash` type explicitly allows `` `${RouteId}?${string}` ``, and the whole expression is still one `resolve(...)` `CallExpression`, so both `svelte-check` and the eslint rule accept it. (2) For the branch-selector's "update the ref on whatever route we're currently on" case, don't use SvelteKit's `goto`/`pushState`/`replaceState` from `$app/navigation` at all (the rule specifically tracks those three imports) — use the native `window.history.replaceState(window.history.state, '', url)` instead. It updates the visible URL/query string without triggering a SvelteKit navigation (so no unwanted re-fetch of layout data) and is outside the rule's tracked import list entirely. Keep the source of truth for the current ref in a plain Svelte store (not `page.url`, which won't reflect a native `history.replaceState` call), and only re-derive from `page.url.searchParams` on initial mount/project change.
- Source: create-code, 2026-07-30 (005-dashboard-frontend-projects tasks 4-6)

## Shiki's line-numbering relies on the raw newlines between `.line` spans, not on the spans being block-level
- Project: trayline (dashboard, 006-dashboard-frontend-files spec)
- Problem: Shiki's `codeToHtml` output wraps each source line in `<span class="line">...</span>` but separates consecutive spans with a literal `\n` text node (not inside any span) — line breaks in the rendered `<pre>` come from that raw whitespace being preserved (`white-space: pre`), not from the `.line` spans themselves, which are plain inline `<span>`s with no layout role. Adding CSS like `.line { display: inline-block; width: 100% }` to force each line onto its own row (a natural-looking way to get a numbered-gutter effect) actually breaks nothing visually by itself, but combined with a `::before` counter for line numbers it produces doubled vertical spacing, because the width:100% inline-block already forces a line break and the literal `\n` sibling text node then renders as a second, blank line break.
- Solution: Don't touch `.line`'s `display` at all — leave it as the default inline span. Add only `.line::before { counter-increment: line; content: counter(line); display: inline-block; width: ...; ... }` for the gutter number; it renders inline before each line's tokens, and the real `\n` characters between spans (preserved by the `<pre>`) do the actual line-breaking. When building a plain-text fallback for unsupported languages (`FileViewer.svelte`), replicate the exact same structure — `<span class="line">{line}</span>` joined by literal `\n` text nodes between them, not inside — so the identical CSS counter rule numbers both the Shiki path and the fallback path correctly.
- Source: create-code, 2026-07-30 (006-dashboard-frontend-files tasks 3-4)

## No backend endpoint exists to fetch raw file bytes for binary/truncated files
- Project: trayline (dashboard + remote, 006-dashboard-frontend-files spec)
- Problem: `dashboard/SPEC.md` FR-3 says binary and >1MB-truncated files should show a message "with a download link", but the only backend endpoint (`GET /projects/{name}/blob/{ref}/{path}`) returns JSON with `"content": null` for exactly those two cases — there is no raw-bytes/octet-stream endpoint to point a download link at, and the endpoint requires a Bearer token header a plain `<a href>`/`download` link can't attach. For normal text files this isn't a problem (the JSON `content` string is already in hand, so "Raw" can build a client-side `Blob` URL and `window.open` it — implemented in `FileViewer.svelte`'s `handleRaw`), but binary/truncated files have no bytes on the client at all.
- Solution: Not fixed in tasks 1-5 (frontend components only). When task 6 (edge cases) is implemented, either accept that binary/truncated messages have no working download action (show the message only, no button) or — if a real download is required — add a new raw-bytes backend endpoint (e.g. `GET /projects/{name}/raw/{ref}/{path}` streaming `Content-Type: application/octet-stream` with token-in-query-param auth so a plain link works, similar to how other download links in this app would need to work around Bearer-header auth). That backend work is out of scope for a frontend-only spec and would need its own task/spec.
- Source: create-code, 2026-07-30 (006-dashboard-frontend-files task 5, discovered while scoping task 6)
- Update (task 6, 2026-07-31): Went with the "message only, no button" option. `FileViewer.svelte` now takes `content: string | null` plus `binary`/`truncated` booleans; when either flag is set it renders the `files.binary`/`files.truncated` message (interpolated via `.replace('{size}', formatFileSize(size))`, same pattern as `commits.detail.files`) and omits the Raw button entirely (`{#if content !== null}` around the button). Requirement REQ-4's "download link" wording is therefore only satisfied for text files (via the existing client-side Blob-URL `handleRaw`); binary/truncated files show text with no action, which is an intentional, spec-deviating call given the backend gap — flag it if a later spec adds the raw-bytes endpoint described above, since the button could then be re-added.

## Naming a $state()-bound variable exactly `state` breaks svelte-check when a second $state() call exists in the same script block
- Project: trayline (dashboard, 007-dashboard-frontend-git spec)
- Problem: `let state = $state<State>({ status: 'loading' }); let loadingMore = $state(false);` (two `$state()` calls in one `<script>`, the first assigned to a variable literally named `state`) makes `svelte-check`/`svelte-kit sync`'s generated virtual TSX misparse the file: it reports `'state' implicitly has type 'any' because it... is referenced... in its own initializer`, `Block-scoped variable '$state' used before its declaration`, and `Untyped function calls may not accept type arguments` — all anchored at the `let state = $state<State>(...)` line itself, even though nothing in the file actually references `state` before its declaration. This reproduces with a two-line minimal file (just the two `$state()` declarations, no functions/effects/template logic), and is order-independent (putting `loadingMore` first still fails, with one more error). Every other page in this codebase (`[project]/+layout.svelte`, `tree/[...path]/+page.svelte`) only has ONE `$state()` call per script block and never hit this.
- Solution: Don't name a `$state()`-bound variable `state` in any script block that also has a second, later `$state()` call (or vice versa) — rename it to something more specific (e.g. `commitsState`, `pageState`). Renaming alone (no other code change) makes the identical logic type-check with 0 errors. Check for this whenever a Svelte 5 rune-mode component has more than one top-level `$state()` binding.
- Source: create-code, 2026-07-30 (007-dashboard-frontend-git tasks 1-5)

## eslint-plugin-svelte's svelte/prefer-writable-derived rejects the `$state` + `$effect`-sync pattern for prop-derived-but-locally-mutable lists
- Project: trayline (dashboard, 008-dashboard-frontend-env spec)
- Problem: `EnvEditor.svelte` needs a rows array that (a) reinitializes from a `variables` prop whenever a different file is loaded, but (b) supports local mutation in between (push a new row, filter one out) without waiting for the prop to change. The obvious Svelte pattern — `let rows = $state<T[]>([]); $effect(() => { rows = toEditable(variables); });` — type-checks fine under `svelte-check` (0 errors) but fails this repo's `eslint .` (part of `npm run lint`) with `svelte/prefer-writable-derived`, because Svelte 5's `$derived` values are themselves reassignable (a direct assignment overrides the derived value until its tracked dependencies next change) and the lint rule requires that pattern instead of `$state`+`$effect` for this exact "recompute from a dependency, but overridable" shape.
- Solution: Replace with `let rows = $derived(toEditable(variables));` (no `$effect` needed) — `rows.push(...)`/`rows = rows.filter(...)` still work on a `$derived` array (deep reactivity applies the same as `$state`), and it recomputes automatically whenever `variables` changes. Only reach for `$state`+`$effect` when the initial value depends on something that must NOT force a resync on every dependency change (rare) — for the common "seed local editable state from a prop, but let the prop's re-fetch reset it" shape, `$derived` is the correct and lint-required tool.
- Source: create-code, 2026-07-30 (008-dashboard-frontend-env tasks 1-5)

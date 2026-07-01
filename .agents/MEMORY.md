# Memory

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

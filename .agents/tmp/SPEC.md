# Spec: Code Review Fixes for Taskline Multi-Project (012)

## Overview

Fix five issues identified during code review of the taskline multi-project implementation. These span the taskline server (Go), taskline CLI (Go), and the trayline shell wrapper.

## Fix 1 — Worker must call SetCurrentTask before executing

**File:** `tools/taskline/server/worker.go` → `executeTask()`

**Problem:** The `output` field is an `io.Writer`, but at runtime it's always a `*ProjectLog` (passed from `registry.go:startInstanceLocked`). The `ProjectLog.Write()` method prefixes each line with `[timestamp] [currentTask]`, but `executeTask` never calls `SetCurrentTask(task.Name)` — so every log line gets an empty task prefix.

**Fix:** Before calling `w.runner.Start(...)`, type-assert `w.output` to an interface with `SetCurrentTask(string)` and call it with `task.Name`. Use a small interface check (not a concrete type assertion) so tests using `*bytes.Buffer` still work without panicking.

```go
// Inside executeTask, before w.runner.Start:
type taskLabeler interface {
    SetCurrentTask(name string)
}
if tl, ok := w.output.(taskLabeler); ok {
    tl.SetCurrentTask(task.Name)
}
```

## Fix 2 — Bump CLI version to 2.0.0

**File:** `tools/taskline/cli/main.go`

**Problem:** The `version` constant is `"1.1.0"` but the multi-project API is a breaking change (paths moved from `/tasks` to `/projects/{project}/tasks`).

**Fix:** Change `const version = "1.1.0"` to `const version = "2.0.0"`.

## Fix 3 — Forward extra arguments in `schedule logs`

**File:** `runtime/trayline`

**Problem:** The `logs)` case hardcodes `--follow` and doesn't forward any extra user arguments (e.g., `--tail N`).

**Fix:** Change from:
```bash
exec taskline logs --project "$PROJECT" --follow
```
to:
```bash
exec taskline logs --project "$PROJECT" --follow "$@"
```

This forwards any remaining arguments after the `logs` sub-action to the taskline CLI.

## Fix 4 — Add TODO comment for fragile sed parsing in `schedule cancel`

**File:** `runtime/trayline`

**Problem:** The `cancel` case parses `taskline status` output with `sed` to extract the current task ID. This is fragile and will break if the output format changes.

**Fix:** Add a `# TODO:` comment noting this should use a machine-readable output mode (e.g., `--json` flag) when the taskline CLI supports it. No code change needed now — this is a future improvement flagged for awareness.

## Fix 5 — Normalize project name in trayline wrapper

**File:** `runtime/trayline`

**Problem:** `PROJECT="$(basename "$(pwd)")"` passes the raw directory name. If the directory has uppercase letters or spaces, the server's `ValidateProjectName` rejects it (only allows `[a-z0-9_-]`, max 64 chars).

**Fix:** After computing `PROJECT`, normalize it:
```bash
PROJECT="$(basename "$(pwd)")"
# Normalize: lowercase, replace spaces/invalid chars with hyphens, trim to 64 chars
PROJECT="$(echo "$PROJECT" | tr '[:upper:]' '[:lower:]' | tr ' ' '-' | sed 's/[^a-z0-9_-]/-/g' | cut -c1-64)"
```

Apply the same normalization after the `--project` flag value is extracted (the user-provided value should also be normalized, or at minimum validated).

## Constraints

- No new dependencies.
- All changes must pass existing tests.
- The `executeTask` fix must not break existing worker tests that pass `*bytes.Buffer` as output.
- The trayline wrapper is a bash script — keep changes POSIX-safe for the constructs used (the script already uses bash arrays so bash is fine).

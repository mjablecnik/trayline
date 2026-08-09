# Tasks: Code Review Fixes for Taskline Multi-Project (012)

Derived from SPEC.md. Each task is one independently completable fix.

- [x] 1. [HIGH] In `tools/taskline/server/worker.go` `executeTask()`, add a `taskLabeler` interface check and call `SetCurrentTask(task.Name)` on `w.output` before `w.runner.Start(...)`. The interface should be `interface { SetCurrentTask(string) }` so `*bytes.Buffer` in tests is unaffected.
- [x] 2. [HIGH] In `tools/taskline/cli/main.go`, change `const version = "1.1.0"` to `const version = "2.0.0"`.
- [x] 3. [MEDIUM] In `runtime/trayline`, change the `logs)` case from `exec taskline logs --project "$PROJECT" --follow` to `exec taskline logs --project "$PROJECT" --follow "$@"` to forward extra arguments like `--tail N`.
- [x] 4. [LOW] In `runtime/trayline`, add a `# TODO: use machine-readable output (e.g. --json) when available` comment above the `sed` line in the `cancel)` case.
- [x] 5. [MEDIUM] In `runtime/trayline`, after `PROJECT="$(basename "$(pwd)")"`, add normalization: lowercase via `tr`, replace spaces/invalid chars with hyphens, trim to 64 chars. Apply the same normalization after `--project` flag extraction.
- [x] 6. [MEDIUM] Run existing tests (`go test ./...` in `tools/taskline/`) to verify fixes 1 and 2 don't break anything.

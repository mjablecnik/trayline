# Tasks: Taskline Multi-Project Support

## Phase 1: Server — Multi-Project Core

- [x] 1. Create `tools/taskline/server/registry.go` — ProjectRegistry struct with `GetOrCreate(project)`, `List()`, `Shutdown()` methods. Includes project name validation (lowercase alphanumeric, hyphens, underscores, 1-64 chars).
- [x] 2. Create `tools/taskline/server/project_log.go` — ProjectLog struct that implements `io.Writer`, writes timestamped output to `<LOG_DIR>/<project>.log`, and broadcasts to SSE subscribers.
- [x] 3. Update `tools/taskline/server/config.go` — Replace `STATE_FILE` with `STATE_DIR` (default `./state/`) and add `LOG_DIR` (default `./logs/`). Update `.env.example`.
- [x] 4. Update `tools/taskline/server/state.go` — Add `ScanStateDir(dir, names)` function that finds `taskline-*.json` files and returns a map of project→Queue. Update `SaveState` and `LoadState` to accept per-project file paths.
- [x] 5. Update `tools/taskline/server/worker.go` — Worker now accepts an `io.Writer` parameter (the ProjectLog) instead of always using `os.Stdout`. No other logic changes.
- [x] 6. Update `tools/taskline/server/main.go` — Replace single Queue+Worker with Registry. On startup: create STATE_DIR/LOG_DIR if needed, scan for existing state, start workers for restored projects. Shutdown: iterate all projects, stop workers, flush logs, save state.
- [x] 7. Update `tools/taskline/server/handler.go` — Refactor all routes to use `/projects/{project}/...` prefix. Handler receives `*Registry`. Add `GET /projects` endpoint. Add `GET /projects/{project}/logs` (with `?tail=N`) and `GET /projects/{project}/logs/stream` (SSE) endpoints.

## Phase 2: CLI — `--project` Support

- [x] 8. Update `tools/taskline/cli/client.go` — Add `project` field to Client. All API methods construct paths as `/projects/{project}/tasks/...`. Add `ListProjects()` and `GetLogs(tail int, follow bool)` methods.
- [x] 9. Update `tools/taskline/cli/main.go` — Parse global `--project` flag (before subcommand dispatch). Default to `filepath.Base(os.Getwd())`. Add `projects` and `logs` subcommands to dispatch.
- [x] 10. Update `tools/taskline/cli/commands.go` — Add `cmdProjects` and `cmdLogs` functions. `cmdLogs` supports `--follow` (SSE streaming) and `--tail N`. Update usage text.
- [x] 11. Update `tools/taskline/cli/format.go` — Add `FormatProjectsList` for the `projects` subcommand output.

## Phase 3: Trayline Wrapper — Schedule Redirect

- [x] 12. Update `runtime/trayline` — Rewrite the `schedule)` case to translate schedule commands into taskline CLI calls. Parse sub-actions (list, status, logs, cancel, delete, retry, stop) and pipeline scheduling. Resolve pipeline paths, construct `trayline run ...` command strings, call `taskline add/list/delete/stop/retry/logs` with `--project`.

## Phase 4: Zsh Completions

- [x] 13. Update `setup/completions/_trayline` — Update `_trayline_schedule` function: replace `show` with `status`, add `stop` sub-action, keep pipeline and `--var` completion intact.
- [x] 14. Update `tools/taskline/cli/completions/_taskline` — Add `--project` completion to all subcommands, add `projects` and `logs` subcommands, add `--follow` and `--tail` to logs completion.

## Phase 5: Testing & Documentation

- [x] 15. Update server tests — Test Registry creation, multi-project parallel execution, per-project state persistence, log capture, and SSE streaming. Update existing handler tests for new route structure.
- [x] 16. Update CLI tests — Test `--project` flag resolution (explicit, CWD default), `projects` and `logs` subcommands, path construction with project prefix.
- [x] 17. Update `tools/taskline/README.md` — Document multi-project support, new env vars (STATE_DIR, LOG_DIR), updated CLI usage, `--project` flag, `projects` and `logs` subcommands.

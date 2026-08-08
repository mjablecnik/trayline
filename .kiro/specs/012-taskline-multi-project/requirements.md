# Requirements: Taskline Multi-Project Support

## Overview

Transform Taskline from a single-queue sequential command runner into a multi-project parallel pipeline scheduler. Each project gets its own independent queue and worker, enabling parallel execution across projects while maintaining sequential execution within a project. The `trayline schedule` command will be redirected from the remote server client to the local taskline CLI.

## Functional Requirements

### FR-1: Multi-Project Queue Registry (Server)

- FR-1.1: The server MUST support multiple independent queues, each identified by a project name (string).
- FR-1.2: Each project queue MUST have its own dedicated Worker goroutine that processes tasks sequentially.
- FR-1.3: Tasks in different project queues MUST execute in parallel (one concurrent task per project).
- FR-1.4: Project queues MUST be created on-demand when the first task is added to a project.
- FR-1.5: Project names MUST be validated: lowercase alphanumeric, hyphens, underscores, 1-64 characters.
- FR-1.6: Task names MUST be unique within a project (not globally).

### FR-2: Per-Project State Persistence (Server)

- FR-2.1: Each project MUST have its own state file: `<state_dir>/taskline-<project>.json`.
- FR-2.2: The `STATE_DIR` environment variable MUST define the directory for state files (default: `./state/`).
- FR-2.3: The existing `STATE_FILE` variable MUST be removed (replaced by `STATE_DIR`).
- FR-2.4: On startup, the server MUST scan `STATE_DIR` for existing `taskline-*.json` files and restore all project queues.
- FR-2.5: Each state file MUST be written atomically (temp + rename) as it is today.

### FR-3: Per-Project Logging (Server)

- FR-3.1: The server MUST capture stdout/stderr of each task into a per-project log file: `<log_dir>/<project>.log`.
- FR-3.2: The `LOG_DIR` environment variable MUST define the directory for log files (default: `./logs/`).
- FR-3.3: Log output MUST be appended continuously (not per-task rotation) — one continuous log per project.
- FR-3.4: The server MUST NOT print task output to its own stdout. Server stdout is reserved for server operational logs only.
- FR-3.5: Each log entry MUST be prefixed with a timestamp and task identifier for readability.

### FR-4: HTTP API Changes (Server)

- FR-4.1: All task-related endpoints MUST include the project as a path parameter: `/projects/{project}/tasks`, `/projects/{project}/tasks/{identifier}`, etc.
- FR-4.2: Queue control endpoints MUST include the project: `/projects/{project}/queue/resume`, `/projects/{project}/queue/status`.
- FR-4.3: A new endpoint `GET /projects` MUST list all known projects with their queue state and pending count.
- FR-4.4: A new endpoint `GET /projects/{project}/logs` MUST return the project's log content (with optional `?tail=N` for last N lines).
- FR-4.5: A new endpoint `GET /projects/{project}/logs/stream` MUST stream log output in real-time via Server-Sent Events (SSE) or chunked transfer.
- FR-4.6: The `POST /projects/{project}/tasks` request body MUST accept an optional `cwd` field (as it does today).
- FR-4.7: `GET /health` MUST remain at the root path (no project prefix).
- FR-4.8: A new endpoint `POST /projects/{project}/tasks/stop` MUST stop the currently running task for that project.
- FR-4.9: A new endpoint `POST /projects/{project}/tasks/retry` MUST retry the failed task for that project.
- FR-4.10: A new endpoint `POST /projects/{project}/tasks/skip` MUST skip the failed task for that project.

### FR-5: CLI `--project` Flag (Taskline CLI)

- FR-5.1: Every subcommand (`add`, `list`, `delete`, `update`, `retry`, `skip`, `stop`, `resume`, `status`, `logs`) MUST accept a `--project` flag.
- FR-5.2: If `--project` is not provided, the CLI MUST default to the basename of the current working directory (e.g., `/home/user/projects/dashboard` → `dashboard`).
- FR-5.3: The CLI MUST construct API paths using the resolved project name (e.g., `/projects/dashboard/tasks`).
- FR-5.4: A new subcommand `taskline projects` MUST list all projects known to the server.
- FR-5.5: A new subcommand `taskline logs [--project NAME] [--follow] [--tail N]` MUST display project logs.
- FR-5.6: `taskline logs --follow` MUST stream logs in real-time (using SSE endpoint).

### FR-6: Trayline Schedule Redirect (Trayline Wrapper)

- FR-6.1: `trayline schedule <pipeline> [--var k=v]...` MUST execute `taskline add "trayline run <pipeline> [--var k=v]..." --project <project>` instead of calling `trayline-client schedule`.
- FR-6.2: The `--project` value MUST default to the basename of the current working directory.
- FR-6.3: `trayline schedule list` MUST execute `taskline list --project <project>`.
- FR-6.4: `trayline schedule cancel <id>` MUST execute `taskline stop` (if the task is running) or `taskline delete <id> --project <project>` (if pending).
- FR-6.5: `trayline schedule delete <id>` MUST execute `taskline delete <id> --project <project>`.
- FR-6.6: `trayline schedule logs` MUST execute `taskline logs --project <project> --follow`.
- FR-6.7: `trayline schedule retry` MUST execute `taskline retry --project <project>`.
- FR-6.8: `trayline schedule status` MUST execute `taskline status --project <project>`.
- FR-6.9: The pipeline name in `trayline schedule <pipeline>` MUST be resolved to a full path (same as `trayline run`) before being passed to taskline.
- FR-6.10: `--var` flags MUST be forwarded as part of the constructed `trayline run` command string.

### FR-7: Zsh Completion Updates

- FR-7.1: The `_trayline_schedule` completion function MUST be updated to reflect the new sub-actions (replacing `show` with `status`).
- FR-7.2: Pipeline completion and `--var` completion MUST remain functional for `trayline schedule <pipeline>`.
- FR-7.3: The `--project` flag MUST be completable on all schedule sub-actions.
- FR-7.4: The taskline CLI MUST have its own zsh completion file updated with `--project` support on all subcommands and the new `projects` and `logs` subcommands.

## Non-Functional Requirements

### NFR-1: Backward Compatibility

- NFR-1.1: The `trayline-client schedule` command MUST remain unchanged (it still talks to the remote server for users who use it directly).
- NFR-1.2: If the taskline server has existing single-queue state from the old `STATE_FILE` format, migration is NOT required — fresh start is acceptable.

### NFR-2: Performance

- NFR-2.1: Adding a task to one project's queue MUST NOT block other projects' workers.
- NFR-2.2: State persistence for one project MUST NOT lock other projects' state files.

### NFR-3: Graceful Shutdown

- NFR-3.1: On SIGTERM/SIGINT, the server MUST signal all running workers to stop, wait up to 30 seconds for graceful completion, then SIGKILL remaining processes.
- NFR-3.2: All project state files MUST be persisted before exit.
- NFR-3.3: All project log files MUST be flushed before exit.

### NFR-4: Log Management

- NFR-4.1: Log files MUST NOT be rotated automatically by the server (external tooling handles rotation).
- NFR-4.2: The SSE/streaming log endpoint MUST support multiple concurrent readers without blocking task execution.

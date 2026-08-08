# Design: Taskline Multi-Project Support

## Architecture Overview

```
┌─────────────────────────────────────────────────────┐
│                  Taskline Server                      │
│                                                      │
│  ┌──────────────────────────────────────────────┐   │
│  │           Project Registry                    │   │
│  │  map[string]*ProjectInstance                  │   │
│  └──────┬───────────┬────────────┬──────────────┘   │
│         │           │            │                   │
│  ┌──────▼─────┐ ┌───▼──────┐ ┌──▼──────────┐       │
│  │ "dashboard"│ │ "backend"│ │ "frontend"  │  ...   │
│  │  Queue     │ │  Queue   │ │  Queue      │       │
│  │  Worker    │ │  Worker  │ │  Worker     │       │
│  │  LogFile   │ │  LogFile │ │  LogFile    │       │
│  │  StateFile │ │ StateFile│ │  StateFile  │       │
│  └────────────┘ └──────────┘ └─────────────┘       │
└─────────────────────────────────────────────────────┘

┌─────────────┐        ┌──────────────────────┐
│ taskline CLI│──HTTP──▶│  /projects/{name}/…  │
│ --project X │        └──────────────────────┘
└─────────────┘

┌─────────────────────────┐         ┌─────────────┐
│ trayline schedule <pipe>│────────▶│ taskline add │
│   (wrapper script)      │         │ --project X  │
└─────────────────────────┘         └─────────────┘
```

## Server Design

### ProjectRegistry

New top-level struct that replaces the single Queue+Worker:

```go
type ProjectInstance struct {
    Name      string
    Queue     *Queue
    Worker    *Worker
    LogWriter *ProjectLog
    StateFile string
}

type Registry struct {
    mu        sync.RWMutex
    projects  map[string]*ProjectInstance
    stateDir  string
    logDir    string
    names     *NameGenerator  // shared for ID generation (globally unique IDs)
    notifier  Notifier
}
```

**On-demand creation:** When a request arrives for a project not yet in the map, the registry creates the Queue, Worker, LogWriter, and state file — then starts the Worker goroutine.

**Startup scan:** On boot, the registry scans `STATE_DIR` for `taskline-*.json` files, extracts project names from filenames, and restores each.

### Per-Project Log Files

```go
type ProjectLog struct {
    mu   sync.Mutex
    file *os.File
    // Subscribers for real-time streaming
    subs map[chan []byte]struct{}
}
```

- Worker's output writer is the ProjectLog (instead of os.Stdout).
- ProjectLog writes to file AND broadcasts to any active SSE subscribers.
- Each line is prefixed: `[2025-01-15T10:30:00Z] [task-name] <output>`
- Log file path: `<LOG_DIR>/<project>.log`

### HTTP API Routes

```
GET  /health
GET  /projects

GET  /projects/{project}/tasks
POST /projects/{project}/tasks
DELETE /projects/{project}/tasks/{id}
PATCH  /projects/{project}/tasks/{id}
POST /projects/{project}/tasks/retry
POST /projects/{project}/tasks/skip
POST /projects/{project}/tasks/stop

POST /projects/{project}/queue/resume
GET  /projects/{project}/queue/status

GET  /projects/{project}/logs?tail=N
GET  /projects/{project}/logs/stream  (SSE)
```

### Handler Changes

The Handler receives a `*Registry` instead of a single `*Queue` + `*Worker`. Each route handler:
1. Extracts `{project}` from the path
2. Calls `registry.GetOrCreate(project)` to get the ProjectInstance
3. Operates on that instance's Queue/Worker

### State Files

Directory structure:
```
~/.trayline/state/
├── taskline-dashboard.json
├── taskline-backend.json
└── taskline-frontend.json
```

Each file has the same schema as today:
```json
{
  "state": "running",
  "tasks": [...]
}
```

### Configuration Changes

| Variable | Default | Description |
|----------|---------|-------------|
| `APP_PORT` | `9090` | Server port (unchanged) |
| `STATE_DIR` | `./state/` | Directory for per-project state files |
| `LOG_DIR` | `./logs/` | Directory for per-project log files |
| `NOTIFY_EMAIL` | (empty) | Email for failure notifications (unchanged) |
| `SMTP_*` | (empty) | SMTP settings (unchanged) |

`STATE_FILE` is removed.

### Shutdown Sequence

1. Stop accepting new HTTP requests
2. For each ProjectInstance:
   - Call Worker.Shutdown() (prevents new tasks from starting)
3. Wait up to 30s for all running tasks to finish
4. For any still-running workers: ForceKill()
5. For each ProjectInstance:
   - Flush and close LogWriter
   - SaveState to state file
6. Exit 0

## CLI Design

### `--project` Flag Resolution

```go
func resolveProject(flagValue string) string {
    if flagValue != "" {
        return flagValue
    }
    cwd, err := os.Getwd()
    if err != nil {
        return "default"
    }
    return filepath.Base(cwd)
}
```

### Updated Usage

```
Usage: taskline <subcommand> [arguments]

Manage the Taskline sequential command queue.

Subcommands:
  add <command> [--name N] [--position N] [--project P]  Add a task
  list [--project P]                                      List tasks
  delete <id> [--project P]                               Delete a task
  update <id> [--command C] [--name N] [--project P]      Update a task
  retry [--project P]                                     Retry failed task
  skip [--project P]                                      Skip failed task
  stop [--project P]                                      Stop running task
  resume [--project P]                                    Resume queue
  status [--project P]                                    Show queue status
  projects                                                List all projects
  logs [--project P] [--follow] [--tail N]                Show project logs

Options:
  --project P    Project namespace (default: current directory name)
  -h, --help     Show help
  -v, --version  Show version
```

### API Path Construction

The Client prepends `/projects/{project}` to all paths:

```go
func (c *Client) basePath() string {
    return "/projects/" + url.PathEscape(c.project)
}

// CreateTask sends POST /projects/{project}/tasks
func (c *Client) CreateTask(...) { ... }
```

### `taskline logs` Command

```
taskline logs                     # follow logs for CWD project
taskline logs --project backend   # follow logs for "backend"
taskline logs --tail 50           # last 50 lines, then exit
taskline logs --follow            # stream in real-time (default)
taskline logs --follow --tail 50  # last 50 lines, then continue streaming
```

Implementation: connects to `GET /projects/{project}/logs/stream` (SSE) for `--follow`, or `GET /projects/{project}/logs?tail=N` for non-follow.

### `taskline projects` Command

```
taskline projects
```

Output:
```
PROJECT     STATE    PENDING  RUNNING
dashboard   running  3        trayline run processes/4-create-code...
backend     idle     0        -
frontend    halted   2        -
```

## Trayline Wrapper Changes

### `trayline schedule` Case

The `case "schedule")` block in `runtime/trayline` changes from:
```bash
exec trayline-client schedule "$@"
```

To a new implementation that translates schedule commands into taskline CLI calls:

```bash
schedule)
    # Resolve --project (from flag or CWD basename)
    PROJECT=$(basename "$(pwd)")
    # ... parse args, extract sub-action or pipeline ...

    case "$SUB" in
        list)
            exec taskline list --project "$PROJECT"
            ;;
        status)
            exec taskline status --project "$PROJECT"
            ;;
        logs)
            exec taskline logs --project "$PROJECT" --follow
            ;;
        cancel|delete)
            exec taskline delete "$ID" --project "$PROJECT"
            ;;
        retry)
            exec taskline retry --project "$PROJECT"
            ;;
        stop)
            exec taskline stop --project "$PROJECT"
            ;;
        *)
            # Default: schedule a pipeline
            PIPELINE=$(resolve_pipeline "$SUB")
            CMD="trayline run ${PIPELINE}${VAR_ARGS}"
            exec taskline add "$CMD" --project "$PROJECT"
            ;;
    esac
    ;;
```

### Command Construction for `add`

When scheduling a pipeline, the constructed command is:
```
trayline run <resolved-pipeline-path> --var key1=val1 --var key2=val2
```

This is passed as a single string to `taskline add "..."` — the taskline worker will execute it via `sh -c`.

## Zsh Completion Changes

### `_trayline_schedule` Function

Updated sub-actions:
```
list     → taskline list
status   → taskline status  (replaces "show")
cancel   → taskline delete / stop
delete   → taskline delete
retry    → taskline retry
stop     → taskline stop
logs     → taskline logs --follow
```

Pipeline completion and `--var` completion remain identical.

### Taskline CLI Completion (`_taskline`)

Add `--project` to all subcommands and add new `projects` and `logs` subcommands.

## Migration Notes

- No automatic migration from old single-queue `STATE_FILE`. Users start fresh or manually rename.
- The `trayline-client schedule` command remains functional for direct remote server usage — unaffected.
- The server binary name remains `taskline-server`.
- The CLI binary name remains `taskline`.

## File Changes Summary

| File | Change |
|------|--------|
| `tools/taskline/server/main.go` | Use Registry instead of single Queue+Worker |
| `tools/taskline/server/registry.go` | NEW — ProjectRegistry implementation |
| `tools/taskline/server/project_log.go` | NEW — Per-project log writer + SSE broadcast |
| `tools/taskline/server/handler.go` | Refactor to use Registry, new routes |
| `tools/taskline/server/config.go` | STATE_DIR, LOG_DIR instead of STATE_FILE |
| `tools/taskline/server/state.go` | Per-project file naming, directory scan |
| `tools/taskline/server/worker.go` | Accept io.Writer (ProjectLog) per instance |
| `tools/taskline/server/.env.example` | Updated variables |
| `tools/taskline/cli/main.go` | Add --project, projects, logs subcommands |
| `tools/taskline/cli/commands.go` | --project on all commands, new commands |
| `tools/taskline/cli/client.go` | Project-scoped API paths |
| `tools/taskline/cli/config.go` | No changes (TASKLINE_URL stays) |
| `tools/taskline/cli/completions/_taskline` | Updated completion |
| `runtime/trayline` | Rewrite schedule case |
| `setup/completions/_trayline` | Update _trayline_schedule |

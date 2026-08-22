# Taskline

Taskline is a multi-project sequential command queue: a server runs shell
commands one at a time per project from independent FIFO queues (one
project's queue never blocks another's), and a CLI talks to it over HTTP to
add, inspect, and control tasks.

This repo has two independent Go modules:

- `server/` — the Taskline server (project registry, per-project queue/worker,
  HTTP API, per-project state persistence and logging).
- `cli/` — the `taskline` command-line client.

## Prerequisites

- Go 1.23+

## Server

```bash
cd server
go build ./...      # or: ./scripts/build.sh (outputs bin/taskline-server)
go test ./...
```

Configuration is via environment variables (optionally loaded from a `.env`
file in `server/`); see `server/.env.example` for the full list (`APP_PORT`,
`BIND_ADDR`, `APP_TOKEN`, `STATE_DIR`, `LOG_DIR`, and optional SMTP settings
for failure notifications). Copy it to get started:

```bash
cp .env.example .env
```

Run the server:

```bash
go run .
# or, after ./scripts/build.sh:
./bin/taskline-server
```

**Security note:** every task submitted to this server runs as an
unrestricted shell command (`sh -c <command>`) — that's the tool's entire
job, there is no sandboxing or command allowlist. It listens on `BIND_ADDR`
(default `127.0.0.1` — loopback-only, unreachable from other hosts) and
`APP_PORT` (default `9090`). If you set `BIND_ADDR` to anything other than a
loopback address, you **must** also set `APP_TOKEN`, or the server refuses to
start — every request except `GET /health` then requires
`Authorization: Bearer <APP_TOKEN>`. Set `TASKLINE_TOKEN` to the same value
in the CLI's environment (see below) so it can authenticate.

Every project queue is created on-demand — the first task added to (or
request for) a project name spins up its own `Queue` + `Worker` + log file +
state file, and different projects' tasks run in parallel while tasks within
one project still run sequentially. Routes:

- `GET /health`
- `GET /projects` — list all known projects with queue state and pending count
- `GET|POST /projects/{project}/tasks`, `DELETE|PATCH /projects/{project}/tasks/{identifier}`
- `POST /projects/{project}/tasks/retry`, `/skip`, `/stop`
- `POST /projects/{project}/queue/resume`, `GET /projects/{project}/queue/status`
- `GET /projects/{project}/logs?tail=N` — project log content
- `GET /projects/{project}/logs/stream` — real-time log streaming (SSE)

On startup the server scans `STATE_DIR` for existing `taskline-<project>.json`
files and restores every project's queue and worker. On shutdown it stops
accepting requests, waits up to 30s for running tasks to finish gracefully
(then force-kills any still running), and flushes each project's log and
state file before exiting.

To install the server binary to `~/.local/bin`:

```bash
./scripts/install.sh
```

## CLI

```bash
cd cli
go build ./...       # or: ./scripts/build.sh (outputs bin/taskline)
go test ./...
```

The CLI talks to the server via `TASKLINE_URL` (default
`http://localhost:9090`) and, if the server requires one, `TASKLINE_TOKEN`
(sent as `Authorization: Bearer <token>` on every request — must match the
server's `APP_TOKEN`). Every subcommand accepts `--project P`; if omitted,
the project defaults to the basename of the current working directory:

```bash
go run . add "echo hello"          # project = basename of $PWD
go run . add "echo hello" --project backend
go run . list
go run . status
go run . projects                  # list all projects known to the server
go run . logs --follow             # stream this project's log in real time
go run . logs --tail 50            # print the last 50 lines and exit
go run . --help
```

To install the CLI binary and its zsh completion:

```bash
./scripts/install.sh
```

## Testing

Each module's tests can be run independently with `go test ./...` from that
module's directory. The server module also has property-based tests
(`pgregory.net/rapid`); to run its concurrency tests with the race detector:

```bash
cd server
CC=cc CGO_ENABLED=1 go test -race ./...
```

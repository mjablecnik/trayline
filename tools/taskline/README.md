# Taskline

Taskline is a sequential command queue: a server runs shell commands one at a
time from a FIFO queue, and a CLI talks to it over HTTP to add, inspect, and
control tasks.

This repo has two independent Go modules:

- `server/` — the Taskline server (queue, worker, HTTP API, state persistence).
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
`STATE_FILE`, and optional SMTP settings for failure notifications). Copy it
to get started:

```bash
cp .env.example .env
```

Run the server:

```bash
go run .
# or, after ./scripts/build.sh:
./bin/taskline-server
```

It listens on `APP_PORT` (default `9090`) and exposes `/health`, `/tasks`,
`/tasks/{identifier}`, `/tasks/retry`, `/tasks/skip`, `/tasks/stop`,
`/queue/resume`, and `/queue/status`.

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
`http://localhost:9090`):

```bash
go run . add "echo hello"
go run . list
go run . status
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

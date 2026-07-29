# CLAUDE.md

Agent-facing reference for the trayline monorepo. See `README.md` for user-facing docs.

## Service Directories

| Directory | Purpose |
|-----------|---------|
| `runtime/` | CLI wrapper, agent runner, sync script, sandbox Dockerfile |
| `orchestrator/` | Go module `orchestrator` — pipeline orchestrator (`trayline-run`) |
| `remote/` | Go module `remote` — merged agent API server + CLI client (`cmd/server`, `cmd/client`) |
| `tools/taskline/server/` | Go module `server` — task queue server |
| `tools/taskline/cli/` | Go module `cli` — task queue CLI |
| `tools/tunnel/` | Relay + home-agent tunnel utilities (Docker-based, no Go module) |
| `setup/` | Installer (`install.sh`), config template, shell completions |

## Build & Test Commands

```bash
cd orchestrator && go build ./... && go test ./...
cd remote && go build ./... && go test ./...
cd tools/taskline/server && go build ./... && go test ./...
cd tools/taskline/cli && go build ./... && go test ./...
```

## Dependency Direction Rules

- `setup/` is the only directory allowed to reference paths in every other directory (via `install.sh`).
- `orchestrator/` may invoke `runtime/` scripts and read `pipelines/` YAML at execution time only — never as a compile-time Go import.
- `remote/` may build/run the `runtime/sandbox/Dockerfile` image at runtime only — never as a compile-time Go import. It must not import `orchestrator/` or `tools/`.
- `tools/` is fully self-contained — no imports or path references into `runtime/`, `orchestrator/`, `remote/`, `pipelines/`, or `setup/`.
- `runtime/` and `pipelines/` contain no Go import statements or path references into any other directory.

## Other Key Paths

- `pipelines/` — YAML pipeline definitions read by the orchestrator at runtime (`tasks/`, `processes/`, `workflows/`, `lifecycle.yaml`)
- `.kiro/specs/` — Kiro spec-driven development specs (requirements.md, design.md, tasks.md per feature)
- `.agents/` — AI agent working files: `MEMORY.md`, `AI_LOG.md`, `tmp/`, `checkpoints/`
- `setup/config.example` — config template installed to `~/.trayline/config`
- `.gitignore` — build artifact and secret patterns for the current directory layout

# Trayline

Unified CLI for AI agent pipelines. Run [Kiro CLI](https://kiro.dev) and [Claude Code](https://docs.anthropic.com/en/docs/claude-code) agents in sandboxed Docker containers, orchestrate multi-step workflows with YAML pipelines, and sync projects with remote servers.

## Installation

### 1. Log in on the host (one-time)

```bash
# Kiro
curl -fsSL https://kiro.dev/install.sh | bash
kiro-cli login

# Claude Code
npm install -g @anthropic-ai/claude-code
claude login
```

### 2. Run the installer

```bash
./install.sh
```

Builds the Docker image and installs `trayline` to `~/bin` with all internal tools in `~/.trayline/`.

## Usage

```
trayline <command> [options]
```

### Commands

| Command | Description |
|---------|-------------|
| `agent` | Run an AI agent in a Docker sandbox |
| `run`   | Run a YAML pipeline (orchestrator) |
| `sync`  | Sync project with a remote server via rsync |
| `install` | Re-run installation |
| `version` | Print version info |
| `help`  | Show help |

### agent

Run an AI coding agent in a sandboxed Docker container with filtered access to the host Docker daemon.

```bash
trayline agent kiro "Show me running containers"
trayline agent kiro -p ~/my-project "Add a /health endpoint"
trayline agent kiro -i
trayline agent claude -p ~/my-project -i
trayline agent claude -p ~/my-project "Fix the tests"
```

Options:
- `-p DIR` — path to project directory (default: current directory)
- `-i` — interactive mode (opens a chat session)

### run

Execute a YAML pipeline that orchestrates multiple agent steps, shell commands, and LLM-driven loops.

```bash
trayline run --pipeline code-review --verbose
trayline run --pipeline ./local-pipeline.yaml --dry-run
trayline run --pipeline create-code --var specs-name=my-spec --var path=./src
```

Pipeline names are resolved from `~/.trayline/pipelines/`. Use a path with `/` or `.yaml` extension for local files.

Options:
- `--pipeline` — pipeline name or path (required)
- `--var key=value` — set or override a pipeline variable (repeatable)
- `--dry-run` — print steps without executing
- `--verbose` — stream agent output in real time

See [orchestrator/README.md](orchestrator/README.md) for the full pipeline YAML format.

### sync

Sync the current directory with a remote host via rsync.

```bash
trayline sync push
trayline sync pull --verbose
```

## How it works

```
trayline agent  →  Sandbox container (Kiro CLI / Claude Code + tools)
                      ↓ TCP :2375
                  docker-socket-proxy (filters Docker API)
                      ↓ socket
                  Host Docker daemon
```

Proxy allows: `ps`, `logs`, `build`, `start/stop/restart`, `exec`, `images`, `networks`.
Proxy denies: `volumes`, `secrets`, `swarm`, `auth`, `nodes`, `configs`.

## Project Structure

```
trayline/
├── trayline              # Main CLI wrapper (installed to ~/bin/)
├── trayline-agent        # Docker sandbox runner for AI agents
├── install.sh            # Installer script
├── Dockerfile            # Sandbox container image
├── sync.sh               # Rsync wrapper
├── orchestrator/         # Go pipeline orchestrator (trayline-run)
├── pipelines/            # Default pipeline definitions
└── completions/          # Zsh completions
```

## Default Pipelines

| Pipeline | Description |
|----------|-------------|
| `default` | Full workflow: create code → verify build → create tests → code review |
| `create-code` | Create code + tests + code review loop |
| `code-review` | Standalone code review with iterative fixes |
| `quick` | Create code only (no tests or review) |

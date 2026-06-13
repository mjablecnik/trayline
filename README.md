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

```
trayline run <pipeline> [options]
```

```bash
trayline run processes/8-code-review --verbose
trayline run workflows/feature-implementation --var specs-name=my-spec
trayline run tasks/check-build --no-lifecycle
```

Pipeline names are resolved from `~/.trayline/pipelines/`. Use a path with `/` or `.yaml` extension for local files.

Options:
- `--var key=value` — set or override a pipeline variable (repeatable)
- `--dry-run` — print steps without executing
- `--verbose` — stream agent output in real time
- `--no-lifecycle` — skip lifecycle.yaml before/after steps
- `--restart` — ignore checkpoint and start from the beginning
- `--log-llm` — log all LLM requests and responses to llm-debug.log

Pipelines automatically resume from where they left off if interrupted or rate-limited. Use `--restart` to force a fresh start.

See [orchestrator/README.md](orchestrator/README.md) for the full pipeline YAML format.

### sync

Sync the current directory with a remote server. Uses git-based sync (bare repo) by default with rsync as a fallback.

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
├── install.sh            # Installer script
├── Dockerfile            # Sandbox container image
├── orchestrator/         # Go pipeline orchestrator (trayline-run)
├── pipelines/
│   ├── lifecycle.yaml    # Before/after hooks for every run
│   ├── tasks/            # Atomic operations (check-build, release, sync)
│   ├── processes/        # Standalone processes (create-code, code-review, etc.)
│   └── workflows/        # Composed processes (feature-implementation, etc.)
├── completions/          # Zsh completions
└── scripts/
    ├── trayline          # Main CLI wrapper (installed to ~/bin/)
    ├── trayline-agent    # Docker sandbox runner for AI agents
    └── sync.sh           # Git + rsync sync wrapper
```

## Default Pipelines

### Tasks

| Pipeline | Description |
|----------|-------------|
| `tasks/check-build` | Verifies project builds, runs, lints. Fixes issues until clean. |
| `tasks/release` | Bumps version, updates CHANGELOG.md, creates git tag. |
| `tasks/sync-pull` | Pulls from bare repo with conflict resolution. |
| `tasks/sync-push` | Pushes to bare repo with conflict resolution. |
| `tasks/update-ai-log` | Updates .agents/AI_LOG.md from .agents/tmp/ or git history. |

### Processes

| Pipeline | Description |
|----------|-------------|
| `processes/1-design-to-code` | Converts .design/ files into pixel-perfect web pages. |
| `processes/2-data-refactor` | Extracts hardcoded strings into i18n + repository layer. |
| `processes/3-ui-refactor` | Decomposes pages into component hierarchy with theme tokens. |
| `processes/4-create-code` | Implements code from a Kiro spec, verifies build, runs code review. |
| `processes/5-create-from-brief` | Generates spec from a brief file and implements it. |
| `processes/6-ui-tests` | Creates/maintains E2E tests and component stories. |
| `processes/7-create-tests` | Creates unit/integration tests for uncovered code. |
| `processes/8-code-review` | Reviews code against spec, fixes critical/high/medium issues. |
| `processes/9-improvements` | Finds and applies validation, DX, and test improvements. |

### Workflows

| Pipeline | Description |
|----------|-------------|
| `workflows/design-implementation` | Design → data refactor → UI refactor (1→2→3). |
| `workflows/feature-implementation` | Create code → review → tests → UI tests (4→8→7→6). |
| `workflows/bug-fixing` | Create from brief → tests → UI tests (5→7→6). |
| `workflows/tests-implementation` | UI tests → unit tests (6→7). |
| `workflows/refactoring` | Code review → improvements (8→9). |

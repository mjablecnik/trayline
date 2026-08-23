# Trayline

Unified CLI for AI agent pipelines. Run [Kiro CLI](https://kiro.dev), [Claude Code](https://docs.anthropic.com/en/docs/claude-code), and [Cline CLI](https://docs.cline.bot) agents in sandboxed Docker containers, orchestrate multi-step workflows with YAML pipelines, and sync projects with remote servers.

## Installation

### 1. Log in on the host (one-time)

```bash
# Kiro
curl -fsSL https://kiro.dev/install.sh | bash
kiro-cli login

# Claude Code
npm install -g @anthropic-ai/claude-code
claude login

# Cline (uses ClinePass provider — $9.99/month subscription for open coding models)
npm install -g cline
cline auth clinepass

# Fly CLI (flyctl) — lets agents inside the sandbox deploy/manage apps on fly.io
curl -L https://fly.io/install.sh | sh
fly auth login
```

### 2. Run the installer

```bash
./setup/install.sh
```

Builds the `trayline-sandbox` and `trayline-server` Docker images, installs the `trayline` wrapper to `~/bin/`, its supporting tools (`trayline-run`, `trayline-agent`, `sync.sh`) to `~/.trayline/`, and `trayline-client`/`taskline`/`taskline-server` to `~/.local/bin/`.

## Usage

```
trayline <command> [options]
```

### Commands

| Command | Description |
|---------|-------------|
| `agent` | Run an AI agent in a Docker sandbox |
| `run`   | Run a YAML pipeline (orchestrator) |
| `flow`  | Run multiple pipelines sequentially (ad-hoc) |
| `schedule` | Queue a pipeline (or manage the queue) via the local taskline server |
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
trayline agent cline -t high "Refactor auth module"
trayline agent cline -p ~/my-project -i
trayline agent claude remote                    # remote-control mode (foreground)
trayline agent claude remote -p ~/my-project -n "My Project"
```

Options:
- `-p DIR` — path to project directory (default: current directory)
- `-m MODEL` — model to use (overrides agent's default)
- `-t LEVEL` — thinking/effort level: low, medium, high, xhigh, max (optional)
- `-i` — interactive mode (opens a chat session)
- `-n NAME` — session name for remote-control mode (default: project directory basename)

`claude remote` is a separate mode (Claude only): it registers the session with Anthropic's own remote-control service instead of running one-shot or waiting for local input, so it's reachable from claude.ai/code or the Claude mobile app. This is unrelated to trayline's own `remote/` server module below.

### run

Execute a YAML pipeline that orchestrates multiple agent steps, shell commands, and LLM-driven loops.

```
trayline run <pipeline> [options]
```

```bash
trayline run processes/8-code-review --verbose
trayline run workflows/feature-impl --var specs-name=my-spec
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

### flow

Run multiple pipelines sequentially without creating a workflow YAML file. Each pipeline segment gets its own `--var` flags, separated by `--then`.

```
trayline flow <pipeline> [--var key=value ...] [--then <pipeline> [--var key=value ...]] ...
```

```bash
# Code review followed by improvements:
trayline flow processes/8-code-review --var path=. --var number=5 \
  --then processes/9-improvements --var path=. --var number=5

# Full custom pipeline:
trayline flow processes/4-create-code --var specs-name=my-feature --var path=. \
  --then processes/8-code-review --var specs-name=my-feature --var path=. \
  --then processes/7-create-tests --var specs-name=my-feature --var path=.

# Preview what would run:
trayline flow processes/8-code-review --var path=. \
  --then processes/10-security-audit --var path=. --dry-run
```

Global flags (`--verbose`, `--dry-run`, `--no-lifecycle`, `--restart`, `--log-llm`) apply to all pipelines in the flow. Lifecycle (sync-pull/push) wraps the entire flow, not each individual pipeline.

Use `flow` for ad-hoc sequences. Use workflows for repeatable sequences with skip flags.

### schedule

Queue a pipeline to run through the local [taskline](tools/taskline/) server instead of running it in the foreground, or manage that queue. Tasks are project-scoped (by the current directory's basename, overridable with `--project`) and run one at a time per project.

```bash
trayline schedule workflows/feature-impl --var specs-name=010
trayline schedule list
trayline schedule status
trayline schedule logs
trayline schedule retry
trayline schedule stop
trayline schedule cancel <id>
trayline schedule delete <id>
```

Requires `taskline-server` running (installed to `~/.local/bin/taskline-server` by `setup/install.sh`, but not started automatically — run it yourself, e.g. `taskline-server &`). Sub-actions (`list`, `status`, `logs`, `retry`, `stop`, `cancel`, `delete`) delegate to the `taskline` CLI; anything else is treated as a pipeline name and queued as `trayline run <pipeline> [--var ...]`.

### sync

Sync the current directory with a remote server. Uses git-based sync (bare repo) by default with rsync as a fallback.

```bash
trayline sync push
trayline sync pull --verbose
```

## Dashboard

`dashboard/` is a SvelteKit web UI for browsing projects, git history, files, env vars, agent chat sessions, and workflow runs on a `remote/` server.

```bash
cd dashboard
npm install
cp .env.example .env   # set PUBLIC_API_URL to your remote server
npm run dev             # local dev server
npm run build           # static production build (output: build/)
npm run check           # svelte-check
npm run lint            # prettier + eslint
npm run test            # vitest
```

## How it works

```
trayline agent  →  Sandbox container (Kiro CLI / Claude Code / Cline CLI + tools)
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
├── .agents/              # AI agent working files: memory, logs, checkpoints
├── .kiro/                # Kiro spec-driven development specs
├── runtime/              # Execution artifacts: CLI wrapper, agent runner, sandbox image
│   ├── sandbox/
│   │   └── Dockerfile    # Sandbox container image
│   ├── trayline          # Main CLI wrapper (installed to ~/bin/)
│   ├── trayline-agent    # Docker sandbox runner for AI agents
│   └── sync.sh           # Git + rsync sync wrapper
├── orchestrator/         # Go pipeline orchestrator (trayline-run) that runs pipelines
├── dashboard/            # SvelteKit web UI for the remote/ agent API server
├── pipelines/            # YAML pipeline definitions consumed by the orchestrator
│   ├── lifecycle.yaml    # Before/after hooks for every run
│   ├── tasks/            # Atomic operations (check-build, release, sync)
│   ├── processes/        # Standalone processes (create-code, code-review, etc.)
│   └── workflows/        # Composed processes (feature-implementation, etc.)
├── remote/               # Merged server + client Go module for the agent API
├── tools/                # Independent utilities usable outside trayline
│   ├── taskline/         # Task queue server + CLI
│   └── tunnel/           # Tunnel/relay utilities
└── setup/                # Installer, config template, and shell completions
    ├── install.sh        # Installer script
    ├── config.example    # Config template
    ├── .rsyncignore      # Rsync exclude list
    └── completions/      # Zsh completions
```

### Dependency Direction

- `setup/` references every other directory via path lookups in `install.sh` — it's the only directory allowed to do so.
- `orchestrator/` invokes `runtime/` scripts and reads `pipelines/` YAML at execution time only, never as compile-time imports.
- `remote/` builds/runs the `runtime/sandbox/Dockerfile` image at runtime; it has no compile-time dependency on `orchestrator/` or `tools/`.
- `runtime/` and `tools/` have no dependencies on any other top-level directory — they are self-contained.

## Default Pipelines

### Tasks

| Pipeline | Description |
|----------|-------------|
| `tasks/check-build` | Verifies project builds, runs, lints. Fixes issues until clean. |
| `tasks/cleanup` | Discards all git changes, or commits and pushes them. |
| `tasks/release` | Bumps version, updates CHANGELOG.md, creates git tag. |
| `tasks/squash-commits` | Reorganizes local unpushed commits into clean, logical commits. |
| `tasks/sync-docs` | Synchronizes README.md, DOCS.md, and CLAUDE.md with the current codebase. |
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
| `processes/10-security-audit` | Audits the codebase for security vulnerabilities, fixes issues by severity. |
| `processes/11-seo-audit` | Audits technical SEO/web optimization, fixes issues by severity. |
| `processes/12-create-spec` | Generates a Kiro spec (requirements/design/tasks) from a brief. |
| `processes/13-verify-workflow` | Runs the app locally, verifies user workflows via Playwright, fixes broken code until all pass. |

### Workflows

| Pipeline | Description |
|----------|-------------|
| `workflows/design-impl` | Design → data refactor → UI refactor (1→2→3). |
| `workflows/feature-impl` | Create code → review → tests → UI tests (4→8→7→6). |
| `workflows/fix-bugs` | Create from brief → tests → UI tests (5→7→6). |
| `workflows/write-tests` | UI tests → unit tests (6→7). |
| `workflows/refactoring` | Code review → improvements → security audit → SEO audit (8→9→10→11). |
| `workflows/maintenance` | Refactoring → write tests → check build → sync docs. |

## Author

Martin Jablečník, website: [www.jablecnik.com](https://www.jablecnik.com), GitHub: [@mjablecnik](https://github.com/mjablecnik)

## Show your support

Give a ⭐️ if this project helped you!

## License

Copyright (C) 2026 Martin Jablečník

This program is licensed under the [GNU General Public License v3.0](LICENSE).

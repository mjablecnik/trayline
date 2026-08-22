# CLAUDE.md

Agent-facing reference for the trayline monorepo. See `README.md` for user-facing docs.

## Service Directories

| Directory | Purpose |
|-----------|---------|
| `runtime/` | CLI wrapper, agent runner (kiro/claude/cline), sync script, sandbox Dockerfile |
| `orchestrator/` | Go module `orchestrator` — pipeline orchestrator (`trayline-run`) |
| `remote/` | Go module `remote` — merged agent API server + CLI client (`cmd/server`, `cmd/client`) |
| `tools/taskline/server/` | Go module `server` — task queue server |
| `tools/taskline/cli/` | Go module `cli` — task queue CLI |
| `tools/tunnel/` | Relay + home-agent tunnel utilities (Docker-based, no Go module) |
| `setup/` | Unified installer, uninstaller, config templates, shell completions |
| `dashboard/` | SvelteKit web UI for the `remote/` agent API server (npm project) |

## Installation

```bash
./setup/install.sh              # Full install (Docker images + binaries + env + pipelines + zsh completions)
./setup/install.sh --skip-docker # Skip building the trayline-sandbox/trayline-server Docker images
./setup/install-pipelines.sh    # Pipelines + orchestrator + runtime tools only
./setup/reinstall.sh            # Backs up ~/.trayline/env/, runs uninstall.sh then install.sh, restores the backup
./setup/uninstall.sh            # Removes installed binaries, ~/.trayline/, and completions — does NOT remove the Docker images (rmi manually if needed)
```

`install.sh` has no `--force` flag; `reinstall.sh` only forwards its own arguments (e.g. `--skip-docker`) to `install.sh`.

## Runtime Configuration

All environment configs are installed to `~/.trayline/env/`:

| File | Service |
|------|---------|
| `orchestrator.env` | Pipeline orchestrator (OpenRouter API key) |
| `server.env` | Agent API server (Docker, loaded via --env-file) |
| `taskline.env` | Task queue server |
| `tunnel-relay.env` | Tunnel relay |
| `tunnel-agent.env` | Tunnel home-agent |

## Build & Test Commands

```bash
cd orchestrator && go build ./... && go test ./...
cd remote && go build ./... && go test ./...
cd tools/taskline/server && go build ./... && go test ./...
cd tools/taskline/cli && go build ./... && go test ./...
cd dashboard && npm run build && npm run check && npm run lint && npm run test
```

## Dependency Direction Rules

- `setup/` is the only directory allowed to reference paths in every other directory (via `install.sh`).
- `orchestrator/` may invoke `runtime/` scripts and read `pipelines/` YAML at execution time only — never as a compile-time Go import.
- `remote/` may build/run the `runtime/sandbox/Dockerfile` image at runtime only — never as a compile-time Go import. It must not import `orchestrator/` or `tools/`.
- `tools/` is fully self-contained — no imports or path references into `runtime/`, `orchestrator/`, `remote/`, `pipelines/`, or `setup/`.
- `runtime/` and `pipelines/` contain no Go import statements or path references into any other directory.
- `dashboard/` talks to `remote/`'s HTTP API only at runtime (via `PUBLIC_API_URL`) — no compile-time or path dependency on any other directory.

## Other Key Paths

- `pipelines/` — YAML pipeline definitions read by the orchestrator at runtime (`tasks/`, `processes/`, `workflows/`, `lifecycle.yaml`)
- `.kiro/specs/` — Kiro spec-driven development specs (requirements.md, design.md, tasks.md per feature)
- `.agents/` — AI agent working files: `MEMORY.md`, `AI_LOG.md`, `tmp/`, `checkpoints/`
- `setup/config.example` — config template installed to `~/.trayline/config`
- `~/.trayline/env/` — centralized environment configs for all services (installed by setup/install.sh)
- Install layout: `install.sh` puts the `trayline` wrapper in `~/bin/`; `trayline-run`, `trayline-agent`, and `sync.sh` in `~/.trayline/`; and `trayline-client`, `taskline`, `taskline-server` in `~/.local/bin/` — not all binaries land under `~/.trayline/`
- `.gitignore` — build artifact and secret patterns for the current directory layout

## Agent Providers

| Agent | Provider | Model format |
|-------|----------|--------------|
| Kiro | Kiro (AWS) | `auto` (recommended — intelligent router) or specific models (see below) |
| Claude Code | Anthropic (direct) | Aliases (`sonnet`, `opus`, `haiku`, `fable`) or full model IDs (see below) |
| Cline | **ClinePass** ($9.99/mo subscription) | `cline-pass/<model>` (e.g. `cline-pass/deepseek-v4-pro`, `cline-pass/qwen3.7-max`, `cline-pass/kimi-k3`) |

Cline is always authenticated via `cline auth clinepass`. Credentials are stored in `~/.cline/` and mounted into the sandbox container.

### Kiro Models

Kiro uses `auto` by default — an intelligent router that picks the optimal model per task (Sonnet 4.5+ quality on free tier, Opus 4.6+ on paid tiers). Available model values for `--model` / `model:` field:

| Model | Credit multiplier | Best for |
|-------|-------------------|----------|
| `auto` | varies | Default router, recommended for most use |
| `gpt-5.6-sol` | 2.4x | Hardest long-horizon refactors, complex terminal tasks |
| `gpt-5.6-terra` | 1.0x | Balanced routine multi-step development |
| `gpt-5.6-luna` | 0.1x | High-frequency tasks, speed + credit efficiency |
| `claude-opus-5` | 2.2x | Multi-agent, full task completion, large refactors |
| `claude-opus-4-8` | — | Self-critical, flags uncertainties |
| `claude-opus-4-7` | — | Adaptive thinking |
| `claude-opus-4-6` | — | Agentic coding, long sessions |
| `claude-sonnet-5` | — | Near-Opus quality at Sonnet price |
| `claude-sonnet-4-6` | — | Token efficient, good for subagents |
| `claude-haiku-4-5` | — | Fastest, near-frontier |
| `minimax-m2-5` | 0.25x | Near-Opus results, fraction of cost |
| `glm-5` | 0.5x | 200K context, cross-file migrations |
| `deepseek-3-2` | 0.25x | Long tool-calling chains |
| `qwen3-coder-next` | 0.05x | Most cost-effective, good error recovery |

### Claude Code Models

Claude Code uses Anthropic's models directly. You can use short **aliases** or **full model IDs** with `--model`:

**Aliases** (resolve to latest version):

| Alias | Resolves to | Best for |
|-------|-------------|----------|
| `sonnet` | Claude Sonnet 5 | Default — balanced speed + quality |
| `opus` | Claude Opus 5 | Complex agentic coding, deep reasoning |
| `haiku` | Claude Haiku 4.5 | Fast + cheap, simple tasks |
| `fable` | Claude Fable 5 | Most capable, demanding reasoning |

**Full model IDs** (current):

| Model | ID |
|-------|-----|
| Claude Fable 5 | `claude-fable-5` |
| Claude Opus 5 | `claude-opus-5` |
| Claude Opus 4.8 | `claude-opus-4-8` |
| Claude Opus 4.7 | `claude-opus-4-7` |
| Claude Opus 4.6 | `claude-opus-4-6` |
| Claude Opus 4.5 | `claude-opus-4-5` |
| Claude Sonnet 5 | `claude-sonnet-5` |
| Claude Sonnet 4.6 | `claude-sonnet-4-6` |
| Claude Sonnet 4.5 | `claude-sonnet-4-5` |
| Claude Haiku 4.5 | `claude-haiku-4-5` |

**Legacy/deprecated IDs** (still work but will be retired):

| Model | ID | Note |
|-------|-----|------|
| Claude Sonnet 4.0 | `claude-sonnet-4-0` | Deprecated — use `sonnet` |
| Claude Opus 4.0 | `claude-opus-4-0` | Deprecated — use `opus` |
| Claude Opus 4.1 | `claude-opus-4-1` | Retires Aug 2026 |

**Date-pinned IDs** (for reproducibility):

Models also accept date-pinned variants like `claude-sonnet-4-5-20250929`, `claude-opus-4-5-20251101`, `claude-haiku-4-5-20251001`. Use these only when you need to pin exact behavior across time.

Default when no model specified: `sonnet` (Claude Sonnet 5).

### ClinePass Models

ClinePass provides curated open-weight coding models at a flat $9.99/month subscription. Model IDs use the `cline-pass/` prefix:

| Model | ID | Best for |
|-------|-----|----------|
| GLM-5.2 | `cline-pass/glm-5.2` | Strong all-round coding |
| Kimi K3 | `cline-pass/kimi-k3` | Complex reasoning, 1M context |
| Kimi K2.7 Code | `cline-pass/kimi-k2.7-code` | Code-specialized, fast |
| Kimi K2.6 | `cline-pass/kimi-k2.6` | General coding |
| DeepSeek V4 Pro | `cline-pass/deepseek-v4-pro` | Deep reasoning, complex tasks |
| DeepSeek V4 Flash | `cline-pass/deepseek-v4-flash` | Fast + cheap, high throughput |
| MiMo-V2.5 | `cline-pass/mimo-v2.5` | Fast + cheap |
| MiMo-V2.5-Pro | `cline-pass/mimo-v2.5-pro` | Balanced speed + quality |
| MiniMax M3 | `cline-pass/minimax-m3` | Multilingual, UI generation |
| Qwen3.7 Max | `cline-pass/qwen3.7-max` | Strong reasoning, supports thinking |
| Qwen3.7 Plus | `cline-pass/qwen3.7-plus` | Cost-effective, supports thinking |

Default when no model specified: whatever is set in `cline config` (last used model).

### Effort / Thinking Levels

The `effort` field in pipeline YAML (or `-t` flag in `trayline-agent`) controls how much reasoning the model applies. Higher levels produce better results but cost more tokens and take longer.

| Level | Speed | Cost | When to use |
|-------|-------|------|-------------|
| `low` | ⚡⚡⚡ | $ | Simple grep/replace, rename variable, change port, add import. Purely mechanical edits. |
| `medium` | ⚡⚡ | $$ | Standard implementation from clear spec, bug fix with obvious cause, single-file code review. Most daily work. |
| `high` | ⚡ | $$$ | Multi-file feature implementation, refactoring with architecture decisions, debugging unclear issues, writing tests with edge cases. |
| `xhigh` | 🐢 | $$$$ | Architecture design from scratch, complex migrations, security audits, multi-service refactoring, performance optimization with trade-off analysis. |
| `max` | 🐢🐢 | $$$$$ | Critical systems where errors are costly, tasks requiring dozens of edge cases considered simultaneously, full feature from spec through implementation to tests, research tasks exploring alternatives. |

**Pipeline recommendations:**

| Pipeline type | Recommended effort |
|---|---|
| Tasks (cleanup, sync-docs, update-ai-log) | `low` or omit (use default) |
| Processes (create-code, code-review, ui-refactor) | `high` |
| Workflows (feature-impl, refactoring) | `high` to `xhigh` |
| Security/SEO audit | `xhigh` |
| Create from brief (greenfield) | `max` |

**Agent defaults when omitted:**

| Agent | Default behavior |
|-------|-----------------|
| Kiro | Uses last saved preference (factory default: `high`) |
| Claude Code | Adaptive — model decides itself based on complexity |
| Cline | `medium` |

# agent-docker

Sandboxed Docker container for [Kiro CLI](https://kiro.dev) and [Claude Code](https://docs.anthropic.com/en/docs/claude-code) with pre-installed Go, Node.js, Bun and Flutter. Agents run in isolation with limited access to the host's Docker daemon via a socket proxy — they can debug and manage other containers but cannot mount the host filesystem or access sensitive Docker APIs.

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

Builds the Docker image and installs `agent-docker` and `agent-docker-sync` to `~/bin`.

## Usage

### agent-docker

```
Usage: agent-docker [--agent=kiro|claude] [-i] [project-dir] <prompt>
       agent-docker [--agent=kiro|claude] -i [project-dir]
```

Options:
- `--agent=kiro|claude` — choose agent (default: `kiro`)
- `-i` — interactive mode (opens a chat session, no prompt required)
- `-h, --help` — show help

Examples:

```bash
agent-docker "Show me running containers"                        # kiro, one-shot
agent-docker --agent=claude "Show me running containers"         # claude, one-shot
agent-docker ~/my-project "Add a /health endpoint"               # kiro, one-shot, project dir
agent-docker --agent=claude -i ~/my-project                      # claude, interactive
agent-docker -i                                                  # kiro, interactive
```

### agent-docker-sync

Syncs the current directory with a remote host via rsync.

```bash
cd ~/Projects/my-app
agent-docker-sync push            # send local changes to remote
agent-docker-sync pull -v         # fetch remote changes (verbose)
```

## How it works

```
agent-docker  →  Sandbox container (Kiro CLI / Claude Code + tools)
                    ↓ TCP :2375
                docker-socket-proxy (filters Docker API)
                    ↓ socket
                Host Docker daemon
```

Proxy allows: `ps`, `logs`, `build`, `start/stop/restart`, `exec`, `images`, `networks`.
Proxy denies: `volumes`, `secrets`, `swarm`, `auth`, `nodes`, `configs`.

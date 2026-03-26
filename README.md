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
Usage: agent-docker <kiro|claude> [-p project-dir] [-i] [prompt]
```

Options:
- `-p DIR` — path to project directory (default: current directory)
- `-i` — interactive mode (opens a chat session, no prompt required)
- `-h` — show help

Examples:

```bash
agent-docker                                                     # show help
agent-docker kiro "Show me running containers"                   # one-shot
agent-docker kiro -p ~/my-project "Add a /health endpoint"       # one-shot with project
agent-docker kiro -i                                             # interactive
agent-docker claude -p ~/my-project -i                           # claude, interactive
agent-docker claude -p ~/my-project "Fix the tests"              # claude, one-shot
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

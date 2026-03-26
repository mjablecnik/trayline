# kiro-docker

Sandboxed Docker container for [Kiro CLI](https://kiro.dev) with pre-installed Go, Node.js, Bun and Flutter. Kiro runs in isolation with limited access to the host's Docker daemon via a socket proxy — it can debug and manage other containers but cannot mount the host filesystem or access sensitive Docker APIs.

## Installation

### 1. Log in on the host (one-time)

```bash
curl -fsSL https://kiro.dev/install.sh | bash
kiro-cli login
```

### 2. Run the installer

```bash
./install.sh
```

Builds the Docker image and installs `kiro-docker` and `kiro-docker-sync` to `~/bin`.

## Usage

### kiro-docker

```
Usage: kiro-docker [-i] [project-dir] <prompt>
       kiro-docker -i [project-dir]
```

Options:
- `-i` — interactive mode (opens a chat session, no prompt required)
- `-h, --help` — show help

Examples:

```bash
kiro-docker "Show me running containers"              # one-shot, cwd
kiro-docker ~/my-project "Add a /health endpoint"     # one-shot, project dir
kiro-docker -i                                        # interactive, cwd
kiro-docker -i ~/my-project                           # interactive, project dir
```

### kiro-docker-sync

Syncs the current directory with a remote host via rsync.

```bash
cd ~/Projects/my-app
kiro-docker-sync push            # send local changes to remote
kiro-docker-sync pull -v         # fetch remote changes (verbose)
```

## How it works

```
kiro-docker  →  Kiro container (CLI + tools)
                    ↓ TCP :2375
                docker-socket-proxy (filters Docker API)
                    ↓ socket
                Host Docker daemon
```

Proxy allows: `ps`, `logs`, `build`, `start/stop/restart`, `exec`, `images`, `networks`.
Proxy denies: `volumes`, `secrets`, `swarm`, `auth`, `nodes`, `configs`.

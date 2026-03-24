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

Builds the Docker image and installs `kiro-docker` to `~/bin`.

### 3. Usage

```bash
kiro-docker ~/my-project "Add a /health endpoint to main.go"
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

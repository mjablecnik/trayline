# VPN Tunnel Proxy

Provides secure remote access to the Trayline API running on a home server without port forwarding. A [chisel](https://github.com/jpillora/chisel) server runs on Fly.io as the public relay; a chisel client on the home server dials out and opens a reverse tunnel back to it.

```
External Client → Fly.io HTTPS :443 → Chisel Relay (reverse tunnel) → Home Agent → Trayline API
```

## Prerequisites

| Tool | Purpose | Install |
|------|---------|---------|
| Docker | Build and run containers | [docs.docker.com](https://docs.docker.com/get-docker/) |
| Fly.io CLI (`fly`) | Deploy relay to Fly.io | [fly.io/docs/hands-on/install-flyctl](https://fly.io/docs/hands-on/install-flyctl/) |

## Directory Structure

```
tunnel/
├── relay/                        # Fly.io container (chisel server)
│   ├── Dockerfile
│   ├── entrypoint.sh
│   ├── health.sh
│   ├── fly.toml
│   ├── .env.example              # Copy to .env and fill in values
│   ├── .env-prod                 # Production secrets (gitignored)
│   └── scripts/
│       ├── build.sh
│       ├── start-docker.sh       # Run relay locally for testing
│       ├── stop-docker.sh
│       └── deploy.sh             # Deploy to Fly.io
├── home-agent/                   # Home server container (chisel client)
│   ├── Dockerfile
│   ├── entrypoint.sh
│   ├── .env.example              # Copy to .env and fill in values
│   └── scripts/
│       ├── build.sh
│       ├── start-wireguard.sh    # Starts the home agent container (name predates the chisel migration)
│       └── stop-wireguard.sh
└── scripts/
    └── generate-keys.sh          # Generate chisel auth credentials
```

## Setup

### 1. Generate Chisel Auth Credentials

Run from the `tools/tunnel/` directory:

```bash
./scripts/generate-keys.sh
```

Output looks like:

```
# Chisel Authentication Credentials
# Use the same values in relay/.env-prod and home-agent/.env

CHISEL_AUTH_USER=trayline
CHISEL_AUTH_PASS=<random-password>
```

Copy these values into both `.env` files below — the relay and home agent must share the same credentials.

### 2. Configure the Relay

```bash
cp relay/.env.example relay/.env
```

Edit `relay/.env`:

```env
CHISEL_AUTH_USER=trayline
CHISEL_AUTH_PASS=<random-password>
```

### 3. Configure the Home Agent

```bash
cp home-agent/.env.example home-agent/.env
```

Edit `home-agent/.env`:

```env
# Same credentials as the relay:
CHISEL_AUTH_USER=trayline
CHISEL_AUTH_PASS=<random-password>

# Relay address (its public Fly.io URL once deployed):
RELAY_URL=https://trayline-relay.fly.dev

# Local port on the relay that forwards to the Trayline server:
RELAY_PORT=9000

# Trayline server connection (Docker network name and port):
TRAYLINE_HOST=trayline-server
TRAYLINE_PORT=9000
```

## Local Testing

Run both containers locally to verify the tunnel before deploying.

### Start the Relay (Local)

```bash
cd relay
./scripts/start-docker.sh
```

This builds the image and starts the relay on port 8080.

### Start the Home Agent

```bash
cd home-agent
./scripts/start-wireguard.sh
```

This ensures the `trayline-net` Docker network exists and starts the home agent on it.

### Stop Containers

```bash
# Stop relay
relay/scripts/stop-docker.sh

# Stop home agent
home-agent/scripts/stop-wireguard.sh
```

Both stop scripts exit cleanly if the container is not running.

### Check Health

The relay's `health.sh` serves a raw HTTP response on request (used by `fly.toml`'s TCP health check, not exposed as a routed HTTP endpoint by default):

```json
{"chisel": "running"}
```

or, if the chisel process has died:

```json
{"chisel": "stopped"}
```

with HTTP 503.

## Fly.io Deployment

### First Deploy

```bash
# Authenticate with Fly.io
fly auth login

# Copy production env file and fill in secrets
cp relay/.env.example relay/.env-prod
# Edit .env-prod with production values

# Deploy
cd relay
./scripts/deploy.sh
```

The deploy script:
1. Parses the app name from `fly.toml` (`trayline-relay`)
2. Creates the Fly.io app if it does not exist
3. Sets secrets from `.env-prod` (skips keys already in `fly.toml [env]`)
4. Runs `fly deploy`

### Custom Env File

```bash
DPLOY_ENV_FILE=/path/to/custom.env ./relay/scripts/deploy.sh
```

### Point the Home Agent at the Deployed Relay

Set `RELAY_URL` in `home-agent/.env` to the relay's public Fly.io URL (`https://trayline-relay.fly.dev` by default), then restart the home agent container.

## Troubleshooting

### Home agent can't connect to the relay

Check both containers' logs:

```bash
docker logs trayline-home-agent
docker logs trayline-relay
# or on Fly.io:
fly logs --app trayline-relay
```

Common causes:
- `CHISEL_AUTH_USER`/`CHISEL_AUTH_PASS` mismatch between `relay/.env` and `home-agent/.env` — re-run `generate-keys.sh` and update both files
- `RELAY_URL` in `home-agent/.env` does not point at a reachable relay address
- Relay not yet deployed/running

### Health check returns 503 / `{"chisel": "stopped"}`

The chisel process inside that container has exited. Check the container logs for the underlying error (usually an auth failure or the relay being unreachable from the home agent).

### Trayline traffic not reaching the server

The home agent forwards `RELAY_PORT` on the relay's tunnel loopback (`127.0.0.1`) to `TRAYLINE_HOST:TRAYLINE_PORT` via chisel's reverse forwarding (`R:127.0.0.1:<RELAY_PORT>:<TRAYLINE_HOST>:<TRAYLINE_PORT>`). Verify:
- `TRAYLINE_HOST` matches the Docker container name on `trayline-net`
- `TRAYLINE_PORT` matches the port the Trayline server is listening on
- Both the home agent and Trayline server are on the `trayline-net` Docker network

```bash
docker network inspect trayline-net
```

### Stop script errors

Stop scripts (`stop-docker.sh`, `stop-wireguard.sh`) always exit 0, even when the container does not exist. If you see an error, it is coming from another part of your environment.

# VPN Tunnel Proxy

Provides secure remote access to the Trayline API running on a home server without port forwarding. A WireGuard server and Caddy reverse proxy run on Fly.io; a lightweight client container on the home server establishes an outbound tunnel.

```
External Client → Fly.io HTTPS :443 → Caddy → WireGuard tunnel → Home Agent → Trayline API
```

## Prerequisites

| Tool | Purpose | Install |
|------|---------|---------|
| Docker | Build and run containers | [docs.docker.com](https://docs.docker.com/get-docker/) |
| WireGuard tools (`wg`) | Key generation | `apt install wireguard-tools` / `brew install wireguard-tools` |
| Fly.io CLI (`fly`) | Deploy relay to Fly.io | [fly.io/docs/hands-on/install-flyctl](https://fly.io/docs/hands-on/install-flyctl/) |

## Directory Structure

```
tunnel/
├── relay/                        # Fly.io container (WireGuard server + Caddy)
│   ├── Dockerfile
│   ├── Caddyfile
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
├── home-agent/                   # Home server container (WireGuard client)
│   ├── Dockerfile
│   ├── entrypoint.sh
│   ├── .env.example              # Copy to .env and fill in values
│   └── scripts/
│       ├── build.sh
│       ├── start-wireguard.sh
│       └── stop-wireguard.sh
└── scripts/
    └── generate-keys.sh          # Generate WireGuard key pairs
```

## Setup

### 1. Generate WireGuard Keys

Run from any directory — the script uses absolute paths internally:

```bash
./tunnel/scripts/generate-keys.sh
```

Output looks like:

```
# --- Relay Container (tunnel/relay/.env) ---
WG_PRIVATE_KEY=<relay-private-key>
WG_PEER_PUBLIC_KEY=<home-agent-public-key>
WG_PRESHARED_KEY=<preshared-key>

# --- Home Agent (tunnel/home-agent/.env) ---
WG_PRIVATE_KEY=<home-agent-private-key>
WG_PEER_PUBLIC_KEY=<relay-public-key>
WG_PRESHARED_KEY=<preshared-key>
```

Copy each block into its corresponding `.env` file. Keys are never written to disk automatically.

### 2. Configure the Relay

```bash
cp tunnel/relay/.env.example tunnel/relay/.env
```

Edit `tunnel/relay/.env` and fill in the values from step 1, plus:

```env
# From generate-keys.sh output:
WG_PRIVATE_KEY=<relay-private-key>
WG_PEER_PUBLIC_KEY=<home-agent-public-key>
WG_PRESHARED_KEY=<preshared-key>

# Defaults (change if needed):
WG_SERVER_IP=10.0.0.1
WG_SUBNET=24
WG_LISTEN_PORT=51820
WG_PEER_ALLOWED_IPS=10.0.0.2/32
WG_HOME_AGENT_IP=10.0.0.2
UPSTREAM_PORT=8080

# Set your Fly.io app domain:
RELAY_DOMAIN=trayline-relay.fly.dev
```

### 3. Configure the Home Agent

```bash
cp tunnel/home-agent/.env.example tunnel/home-agent/.env
```

Edit `tunnel/home-agent/.env`:

```env
# From generate-keys.sh output:
WG_PRIVATE_KEY=<home-agent-private-key>
WG_PEER_PUBLIC_KEY=<relay-public-key>
WG_PRESHARED_KEY=<preshared-key>

# Tunnel network:
WG_CLIENT_IP=10.0.0.2
WG_SUBNET=24
WG_PEER_ALLOWED_IPS=10.0.0.1/32
WG_KEEPALIVE=25

# After deploying to Fly.io, set this to the relay's public address:
WG_PEER_ENDPOINT=trayline-relay.fly.dev:51820

# Trayline server connection (Docker network name and port):
UPSTREAM_PORT=8080
TRAYLINE_HOST=trayline-server
TRAYLINE_PORT=8080
```

## Local Testing

Run both containers locally to verify the tunnel before deploying.

### Start the Relay (Local)

```bash
cd tunnel/relay
./scripts/start-docker.sh
```

This builds the image and starts the relay on ports 443 (HTTPS) and 51820/UDP (WireGuard).

### Start the Home Agent

```bash
cd tunnel/home-agent
./scripts/start-wireguard.sh
```

This ensures the `trayline-net` Docker network exists and starts the home agent on it.

### Stop Containers

```bash
# Stop relay
tunnel/relay/scripts/stop-docker.sh

# Stop home agent
tunnel/home-agent/scripts/stop-wireguard.sh
```

Both stop scripts exit cleanly if the container is not running.

### Check Health

```bash
curl -k https://localhost/health
```

Expected response when healthy:

```json
{
  "wireguard": "up",
  "proxy": "listening",
  "peer_handshake_seconds_ago": 12
}
```

Returns HTTP 503 with `"status": "degraded"` when the tunnel peer has not completed a handshake in the last 180 seconds.

## Fly.io Deployment

### First Deploy

```bash
# Authenticate with Fly.io
fly auth login

# Copy production env file and fill in secrets
cp tunnel/relay/.env.example tunnel/relay/.env-prod
# Edit .env-prod with production values

# Deploy
cd tunnel/relay
./scripts/deploy.sh
```

The deploy script:
1. Parses the app name from `fly.toml` (`trayline-relay`)
2. Creates the Fly.io app if it does not exist
3. Allocates a dedicated IPv4 address (required for WireGuard UDP)
4. Sets secrets from `.env-prod` (skips keys already in `fly.toml [env]`)
5. Runs `fly deploy`

### Custom Env File

```bash
DPLOY_ENV_FILE=/path/to/custom.env ./tunnel/relay/scripts/deploy.sh
```

### Update Home Agent Endpoint

After the first deploy, get the relay's public IP or hostname:

```bash
fly ips list --app trayline-relay
```

Update `WG_PEER_ENDPOINT` in `tunnel/home-agent/.env` to `<ip>:51820` or `trayline-relay.fly.dev:51820`.

## Troubleshooting

### WireGuard interface fails to start

The relay entrypoint waits up to 30 seconds for `wg0` to come up. If it fails, the container exits with a non-zero code. Check logs:

```bash
docker logs trayline-relay
# or on Fly.io:
fly logs --app trayline-relay
```

Common causes:
- Missing `NET_ADMIN` capability — ensure `--cap-add=NET_ADMIN` is passed
- Invalid `WG_PRIVATE_KEY` — regenerate with `generate-keys.sh`

### Tunnel shows "disconnected" in home agent logs

The home agent polls `wg show wg0 latest-handshakes` every 30 seconds. If the handshake age exceeds 180 seconds:
- Verify `WG_PEER_ENDPOINT` in home agent `.env` points to the correct Fly.io address and port 51820
- Confirm UDP port 51820 is reachable from the home network
- Check relay logs for incoming connection attempts

### Health endpoint returns 503

```bash
curl -k https://<relay-domain>/health
```

- `"wireguard": "down"` — WireGuard interface did not start
- `"proxy": "not listening"` — Caddy is not accepting connections on port 443
- `"status": "degraded"` — WireGuard is up but no handshake within 180 seconds (home agent not connected)

### socat forwarding not working

The home agent uses `socat` to bridge traffic from the WireGuard tunnel IP to the Trayline server. Verify:
- `TRAYLINE_HOST` matches the Docker container name on `trayline-net`
- `TRAYLINE_PORT` matches the port the Trayline server is listening on
- Both the home agent and Trayline server are on the `trayline-net` Docker network

```bash
docker network inspect trayline-net
```

### Key mismatch

If you see WireGuard authentication failures, the public keys in each endpoint's config may not match. Re-run `generate-keys.sh`, update both `.env` files, and restart both containers.

### Stop script errors

Stop scripts (`stop-docker.sh`, `stop-wireguard.sh`) always exit 0, even when the container does not exist. If you see an error, it is coming from another part of your environment.

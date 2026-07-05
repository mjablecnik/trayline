# Implementation Plan: VPN Tunnel Proxy

## Overview

Implement a reverse VPN tunnel using WireGuard to provide secure remote access to the Trayline API server. The implementation creates a `tunnel/` directory at project root containing a Relay Container (Alpine + WireGuard + Caddy, deployed to Fly.io) and a Home Agent Container (Alpine + WireGuard client, runs on trayline-net alongside the existing Trayline server). All configuration uses environment variables, and scripts follow the project's portability patterns.

## Tasks

- [x] 1. Set up project structure and configuration templates
  - [x] 1.1 Create tunnel directory structure and environment templates
    - Create `tunnel/relay/`, `tunnel/home-agent/`, `tunnel/scripts/` directories
    - Create `tunnel/relay/.env.example` with all relay WireGuard and Caddy variables
    - Create `tunnel/home-agent/.env.example` with all home agent WireGuard variables plus UPSTREAM_PORT, TRAYLINE_HOST, and TRAYLINE_PORT
    - Create `tunnel/relay/.dockerignore` excluding `.env`, `.env-prod`, `.git`
    - Create `tunnel/home-agent/.dockerignore` excluding `.env`, `.git`
    - Create `tunnel/relay/.env-prod` placeholder (gitignored)
    - Update root `.gitignore` to cover `tunnel/**/.env`, `tunnel/**/.env-prod`
    - _Requirements: 6.5, 4.4, 4.5_

  - [x] 1.2 Create WireGuard key generation script
    - Create `tunnel/scripts/generate-keys.sh` following script portability pattern (`SCRIPT_DIR`/`PROJECT_DIR`)
    - Check for `wg` command availability, exit with error if missing
    - Generate private/public key pairs for relay and home agent using `wg genkey` / `wg pubkey`
    - Generate a pre-shared key using `wg genpsk`
    - Print all keys to stdout labeled by endpoint and variable name
    - Warn on stderr if existing `.env` files already contain key values, do NOT overwrite
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.6_

- [x] 2. Implement Relay Container
  - [x] 2.1 Create Relay Dockerfile
    - Create `tunnel/relay/Dockerfile` using Alpine base image with WireGuard tools, iptables, iproute2, and Caddy installed
    - Keep image minimal (target under 100 MB)
    - Set entrypoint to `entrypoint.sh`
    - _Requirements: 1.4, 1.3_

  - [x] 2.2 Create Relay entrypoint script (process supervisor)
    - Create `tunnel/relay/entrypoint.sh`
    - Generate `/etc/wireguard/wg0.conf` from environment variables (Interface: PrivateKey, Address, ListenPort; Peer: PublicKey, PresharedKey, AllowedIPs)
    - Bring up WireGuard interface with `wg-quick up wg0`
    - Wait for interface initialization (max 30 seconds), exit non-zero with error log on failure
    - Generate Caddyfile from environment variables (substitute domain, upstream IP, upstream port)
    - Start Caddy in foreground
    - Handle SIGTERM: run `wg-quick down wg0`, stop Caddy, exit
    - _Requirements: 1.5, 1.6, 2.6, 4.1, 3.1, 3.2, 3.3, 3.5, 3.6_

  - [x] 2.3 Create Caddyfile template
    - Create `tunnel/relay/Caddyfile` with reverse proxy config using environment variable placeholders
    - Configure header forwarding (X-Forwarded-For, X-Forwarded-Proto, Host)
    - Support WebSocket upgrade pass-through
    - Set 30-second upstream timeout
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.6_

  - [x] 2.4 Create Relay health check script
    - Create `tunnel/relay/health.sh`
    - Check if `wg0` interface exists via `ip link show wg0`
    - Check if peer has a recent handshake (within 180 seconds) via `wg show wg0 latest-handshakes`
    - Check if Caddy is listening on port 443
    - Return JSON with `wireguard`, `proxy`, `peer_handshake_seconds_ago` fields
    - Return HTTP 200 when all healthy, HTTP 503 with `"status": "degraded"` when handshake > 180s
    - _Requirements: 7.1, 7.2, 7.5_

  - [ ]* 2.5 Write property test for WireGuard config generation (relay)
    - **Property 1: WireGuard config generation produces valid configuration**
    - Test that for any valid set of environment variables, the generated wg0.conf contains exactly one [Interface] section with correct PrivateKey/Address/ListenPort, and exactly one [Peer] section with correct PublicKey/PresharedKey/AllowedIPs
    - Use BATS with randomized input values (100+ iterations)
    - Create `tunnel/relay/test/config_gen_test.bats`
    - **Validates: Requirements 2.3, 4.1**

  - [ ]* 2.6 Write property test for health endpoint logic
    - **Property 2: Health endpoint returns correct status based on system state**
    - Test that for any combination of interface state (exists/not), handshake age (0–600s), and Caddy listener state (active/not), the health script returns correct HTTP status and JSON body
    - Use BATS with randomized inputs (100+ iterations)
    - Create `tunnel/relay/test/health_test.bats`
    - **Validates: Requirements 7.1, 7.2**

- [x] 3. Implement Home Agent Container
  - [x] 3.1 Create Home Agent Dockerfile
    - Create `tunnel/home-agent/Dockerfile` using Alpine base image with WireGuard tools and `socat` installed
    - Keep image minimal
    - Set entrypoint to `entrypoint.sh`
    - _Requirements: 5.1, 5.3, 5.7_

  - [x] 3.2 Create Home Agent entrypoint script
    - Create `tunnel/home-agent/entrypoint.sh`
    - Generate `/etc/wireguard/wg0.conf` from environment variables (Interface: PrivateKey, Address; Peer: PublicKey, PresharedKey, Endpoint, AllowedIPs, PersistentKeepalive)
    - Bring up WireGuard interface with `wg-quick up wg0`
    - Start socat in background to forward TCP traffic from `WG_CLIENT_IP:UPSTREAM_PORT` to `TRAYLINE_HOST:TRAYLINE_PORT` (Docker DNS resolution to Trayline server container)
    - Log initial connection status (connected if handshake within 180s, disconnected otherwise)
    - If initial connection fails within 30 seconds, log error with target endpoint and retry every 10 seconds indefinitely
    - Enter health monitoring loop: check peer handshake every 30 seconds, log state transitions (connected↔disconnected)
    - Handle SIGTERM: stop socat, run `wg-quick down wg0`, exit within 10 seconds
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7, 5.5, 5.7, 5.8, 7.3, 7.4_

  - [ ]* 3.3 Write property test for Home Agent state transition detection
    - **Property 3: Home Agent state transition detection**
    - Test that for any sequence of handshake age readings, a state transition message is logged if and only if state crosses the 180-second threshold between consecutive readings
    - Use BATS with randomized sequences (100+ iterations)
    - Create `tunnel/home-agent/test/monitor_test.bats`
    - **Validates: Requirements 7.4**

- [x] 4. Checkpoint - Ensure containers build and core logic works
  - Ensure all tests pass, ask the user if questions arise.

- [x] 5. Create Fly.io deployment configuration
  - [x] 5.1 Create fly.toml for Relay Container
    - Create `tunnel/relay/fly.toml` with app name `trayline-relay`, region `fra`
    - Configure TCP service on port 443 for HTTPS (Caddy)
    - Configure UDP service on port 51820 for WireGuard
    - Configure health check (HTTP GET `/health` on port 8080)
    - Set VM size to `shared-cpu-1x` with 256MB RAM
    - Add non-sensitive env vars in `[env]` section (WG_SERVER_IP, WG_SUBNET, WG_LISTEN_PORT, WG_HOME_AGENT_IP, WG_PEER_ALLOWED_IPS, UPSTREAM_PORT)
    - _Requirements: 1.1, 1.2, 1.3, 4.3_

  - [x] 5.2 Create deploy script for Relay Container
    - Create `tunnel/relay/scripts/deploy.sh` following script portability pattern
    - Parse APP_NAME from `fly.toml`
    - Load `.env-prod` (or `$DPLOY_ENV_FILE` if set)
    - Check if app exists via `fly apps list`, create if not
    - Allocate dedicated IPv4 for UDP support (`fly ips allocate-v4 --shared`)
    - Set secrets from env file via `fly secrets set` (skip keys in `fly.toml [env]`)
    - Run `fly deploy`
    - _Requirements: 1.1_

- [x] 6. Create local Docker management scripts
  - [x] 6.1 Create Relay start/stop scripts for local testing
    - Create `tunnel/relay/scripts/build.sh` — builds the Docker image with `--no-cache`
    - Create `tunnel/relay/scripts/start-docker.sh` — builds image, stops/removes existing container, starts relay locally with `--env-file .env` and `--cap-add=NET_ADMIN`
    - Create `tunnel/relay/scripts/stop-docker.sh` — stops and removes relay container, exits gracefully if not found
    - All scripts follow portability pattern and are idempotent
    - _Requirements: 8.1, 8.2, 8.5, 8.6, 8.7_

  - [x] 6.2 Create Home Agent start/stop scripts
    - Create `tunnel/home-agent/scripts/build.sh` — builds the Docker image
    - Create `tunnel/home-agent/scripts/start-wireguard.sh` — resolves own directory, loads `.env`, ensures trayline-net exists, removes existing container, starts with `--env-file .env`, `--cap-add=NET_ADMIN`, `--network trayline-net`
    - Create `tunnel/home-agent/scripts/stop-wireguard.sh` — stops and removes home agent container, exits gracefully if not found
    - All scripts follow portability pattern and are idempotent
    - _Requirements: 5.2, 5.4, 5.6, 8.3, 8.4, 8.5, 8.6, 8.7_

- [x] 7. Checkpoint - Verify scripts and deployment config
  - Ensure all tests pass, ask the user if questions arise.

- [x] 8. Documentation and integration
  - [x] 8.1 Create tunnel README
    - Create `tunnel/README.md` with setup guide covering:
      - Prerequisites (Docker, WireGuard tools, Fly.io CLI)
      - Key generation instructions
      - Local testing workflow (relay + home agent)
      - Fly.io deployment instructions
      - Troubleshooting (common issues)
    - _Requirements: 6.5, 1.1, 8.1, 8.3_

  - [ ]* 8.2 Write shell script lint and validation tests
    - Add `shellcheck` validation for all `.sh` files in `tunnel/`
    - Verify all scripts start with portability pattern
    - Verify `.env.example` keys match variables used in entrypoint scripts
    - Add `hadolint` validation for both Dockerfiles
    - _Requirements: 8.5, 5.4, 6.5_

- [x] 9. Final checkpoint - Full integration verification
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties defined in the design
- Shell scripts use `#!/bin/bash` with `set -euo pipefail` and the portability pattern
- All `.env` files are gitignored; only `.env.example` files are committed
- The relay container uses Caddy's built-in environment variable substitution for the Caddyfile
- Health check logic is extracted into `health.sh` for testability

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1"] },
    { "id": 1, "tasks": ["1.2", "2.1", "3.1"] },
    { "id": 2, "tasks": ["2.2", "2.3", "3.2"] },
    { "id": 3, "tasks": ["2.4", "2.5", "3.3"] },
    { "id": 4, "tasks": ["2.6", "5.1"] },
    { "id": 5, "tasks": ["5.2", "6.1", "6.2"] },
    { "id": 6, "tasks": ["8.1", "8.2"] }
  ]
}
```

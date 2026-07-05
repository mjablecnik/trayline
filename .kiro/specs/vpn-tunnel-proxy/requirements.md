# Requirements Document

## Introduction

This feature provides secure remote access to the Trayline server (API + agents) running on a home computer in Docker, without requiring any port forwarding on the home router. A lightweight container deployed on Fly.io acts as a WireGuard server and reverse proxy. The home server runs a WireGuard client in Docker that establishes an outbound tunnel to Fly.io. External requests hit the Fly.io endpoint, which forwards them through the WireGuard tunnel to the home server API.

## Glossary

- **Relay_Container**: The Docker container deployed on Fly.io that runs the WireGuard server and reverse proxy (Caddy). It accepts external HTTPS requests and forwards them through the tunnel to the Home_Agent.
- **Home_Agent**: The WireGuard client Docker container running on the home server alongside the Trayline server. It establishes an outbound connection to the Relay_Container.
- **WireGuard_Tunnel**: The encrypted point-to-point VPN connection between the Home_Agent and the Relay_Container using the WireGuard protocol.
- **Reverse_Proxy**: The Caddy web server running inside the Relay_Container that terminates TLS and forwards HTTP requests through the WireGuard_Tunnel to the Trayline API.
- **Trayline_API**: The existing Trayline server API running in Docker on the home computer, listening on the configured APP_PORT.
- **Peer_Key_Pair**: A WireGuard public/private key pair used to authenticate tunnel endpoints.

## Requirements

### Requirement 1: Relay Container Deployment

**User Story:** As a developer, I want to deploy a lightweight relay container on Fly.io, so that I have a public endpoint that forwards traffic to my home server.

#### Acceptance Criteria

1. THE Relay_Container SHALL be deployable to Fly.io using a single `scripts/deploy.sh` command
2. THE Relay_Container SHALL expose an HTTPS endpoint on port 443 with automatic TLS via Caddy
3. THE Relay_Container SHALL run both the WireGuard server and Caddy reverse proxy within a single container
4. THE Relay_Container SHALL use a minimal base image to keep the deployed image size below 100 MB
5. WHEN the Relay_Container starts, THE Relay_Container SHALL initialize the WireGuard interface within 30 seconds before starting the Caddy reverse proxy
6. IF the WireGuard interface fails to initialize within 30 seconds of container start, THEN THE Relay_Container SHALL exit with a non-zero exit code and log an error message indicating the initialization failure

### Requirement 2: WireGuard Tunnel Establishment

**User Story:** As a developer, I want the home server to automatically connect to the Fly.io relay via WireGuard, so that a secure tunnel is always available without opening ports on my router.

#### Acceptance Criteria

1. WHEN the Home_Agent container starts, THE Home_Agent SHALL establish an outbound WireGuard connection to the Relay_Container within 30 seconds of container initialization
2. THE Home_Agent SHALL use a pre-shared Peer_Key_Pair for mutual authentication between tunnel endpoints
3. THE WireGuard_Tunnel SHALL assign a static IP address to each endpoint within a private subnet configured via environment variables (default 10.0.0.0/24)
4. THE Home_Agent SHALL send WireGuard keepalive packets every 25 seconds to maintain NAT traversal
5. IF the WireGuard_Tunnel connection drops, THEN THE Home_Agent SHALL attempt reconnection automatically using WireGuard's built-in persistence mechanism
6. THE WireGuard_Tunnel SHALL encrypt all traffic between the Home_Agent and the Relay_Container using the WireGuard protocol (ChaCha20, Poly1305)
7. IF the Home_Agent fails to establish the initial WireGuard connection within 30 seconds of container start, THEN THE Home_Agent SHALL log an error message indicating the connection failure and the target endpoint, and SHALL retry the connection indefinitely at 10-second intervals

### Requirement 3: Reverse Proxy Forwarding

**User Story:** As a developer, I want external HTTPS requests to be forwarded through the tunnel to my home Trayline API, so that I can access my API from anywhere.

#### Acceptance Criteria

1. WHEN the Reverse_Proxy receives an HTTPS request, THE Reverse_Proxy SHALL forward the request through the WireGuard_Tunnel to the Trayline_API on the Home_Agent's tunnel IP address and configured port, with an upstream connection timeout of 30 seconds
2. THE Reverse_Proxy SHALL preserve original request headers including Host, X-Forwarded-For, and X-Forwarded-Proto
3. THE Reverse_Proxy SHALL support WebSocket upgrade requests for real-time streaming endpoints
4. WHEN the Trayline_API returns a response, THE Reverse_Proxy SHALL relay the response status code, headers, and body back to the external client without modification
5. IF the WireGuard_Tunnel is unavailable, THEN THE Reverse_Proxy SHALL return HTTP 502 (Bad Gateway) to the client
6. IF the Trayline_API does not respond within the 30-second upstream timeout, THEN THE Reverse_Proxy SHALL return HTTP 504 (Gateway Timeout) to the client

### Requirement 4: Authentication and Security

**User Story:** As a developer, I want the tunnel and proxy to be secure, so that only authorized requests reach my home server.

#### Acceptance Criteria

1. THE Relay_Container SHALL accept only traffic from the authenticated WireGuard peer (Home_Agent) on the tunnel interface by configuring a single AllowedIPs entry matching the Home_Agent's tunnel IP address
2. THE Reverse_Proxy SHALL pass through the existing API_TOKEN bearer authentication to the Trayline_API without modification, adding no additional authentication layer of its own
3. THE Relay_Container SHALL NOT expose the WireGuard UDP listen port to the public internet beyond what is required for the Home_Agent to connect, restricting the WireGuard interface configuration to a single allowed peer
4. THE Home_Agent SHALL store its WireGuard private key in an environment variable defined in the `.env` file (gitignored), not hardcoded in configuration files committed to version control
5. THE Relay_Container SHALL store its WireGuard private key in an environment variable defined in the `.env` file (gitignored), not hardcoded in configuration files committed to version control
6. IF an incoming WireGuard handshake initiation arrives from a public key not matching the configured peer, THEN THE Relay_Container SHALL silently discard the packet without responding

### Requirement 5: Home Agent Docker Integration

**User Story:** As a developer, I want the WireGuard client to run as a Docker container alongside my existing Trayline server, so that it integrates cleanly with my current Docker setup.

#### Acceptance Criteria

1. THE Home_Agent SHALL run as a separate Docker container on the same Docker network as the Trayline server (trayline-net)
2. THE Home_Agent SHALL be startable via a `scripts/start-wireguard.sh` script that uses the script portability pattern (resolves its own directory), loads variables from `.env`, ensures the trayline-net Docker network exists, removes any existing Home_Agent container before starting a new one, and starts the container with `--env-file .env` and `--cap-add=NET_ADMIN`
3. THE Home_Agent SHALL require the NET_ADMIN Linux capability for WireGuard interface management
4. THE Home_Agent SHALL read all configuration from environment variables defined in a `.env` file, with a corresponding `.env.example` documenting all required variable keys
5. WHEN the Home_Agent container receives a SIGTERM signal, THE Home_Agent SHALL remove the WireGuard network interface (via `wg-quick down` or equivalent) and exit within 10 seconds
6. IF a Home_Agent container with the same name already exists when `scripts/start-wireguard.sh` is executed, THEN THE script SHALL stop and remove the existing container before starting a new one
7. THE Home_Agent SHALL forward TCP traffic arriving on its WireGuard tunnel IP (WG_CLIENT_IP) on UPSTREAM_PORT to the Trayline server container identified by the TRAYLINE_HOST environment variable on the port specified by TRAYLINE_PORT, using socat or equivalent TCP forwarder
8. THE Home_Agent SHALL resolve the Trayline server address via Docker DNS (container name on the trayline-net network), allowing the Trayline server to run on any configured port

### Requirement 6: Key Generation and Configuration

**User Story:** As a developer, I want a simple way to generate WireGuard key pairs and configuration, so that I can set up the tunnel without manual cryptographic operations.

#### Acceptance Criteria

1. THE project SHALL provide a `scripts/generate-keys.sh` script that generates WireGuard key pairs (private key and public key) for both the Relay_Container and the Home_Agent, plus a shared pre-shared key for additional symmetric encryption of the tunnel
2. WHEN `scripts/generate-keys.sh` is executed, THE script SHALL print to stdout all generated keys labeled by endpoint (Relay_Container, Home_Agent) and variable name, in a format that can be directly copied into the respective `.env` files
3. IF the `wg` command (WireGuard tools) is not available on the system, THEN THE script SHALL exit with a non-zero exit code and print an error message to stderr indicating that WireGuard tools must be installed
4. IF a `.env` file for either the Relay_Container or Home_Agent already contains WireGuard key values, THEN THE script SHALL warn the user on stderr that existing keys will not be overwritten, and SHALL NOT modify existing `.env` files
5. THE project SHALL provide a `.env.example` file for both the Relay_Container and the Home_Agent documenting all required WireGuard configuration variables including: private key, peer public key, pre-shared key, endpoint address, allowed IPs, tunnel IP address, keepalive interval, and for the Home_Agent additionally: UPSTREAM_PORT (port to listen on for incoming tunnel traffic), TRAYLINE_HOST (Docker DNS name of the Trayline server container), and TRAYLINE_PORT (port on which the Trayline API is listening)
6. THE script SHALL follow the script portability pattern (resolve its own directory using `SCRIPT_DIR` / `PROJECT_DIR`) so it works correctly regardless of the caller's working directory

### Requirement 7: Health Monitoring

**User Story:** As a developer, I want to know if the tunnel is healthy, so that I can diagnose connectivity issues quickly.

#### Acceptance Criteria

1. THE Relay_Container SHALL expose a `/health` endpoint that returns HTTP 200 with a JSON body containing the status of the WireGuard interface (up/down) and reverse proxy (listening/not listening), where "up" means the WireGuard network interface exists and has a valid peer configured, and "listening" means Caddy is accepting TCP connections on port 443
2. WHEN the `/health` endpoint is queried and the WireGuard_Tunnel peer's latest handshake occurred more than 180 seconds ago, THE `/health` endpoint SHALL return HTTP 503 with a JSON body indicating tunnel degradation and the seconds elapsed since the last successful handshake
3. WHEN the Home_Agent container starts, THE Home_Agent SHALL log the initial WireGuard tunnel status (connected/disconnected) to stdout, where "connected" means the peer's latest handshake occurred within the last 180 seconds
4. WHEN the Home_Agent detects a transition between connected and disconnected states by checking the peer's latest handshake age against the 180-second threshold at a polling interval of 30 seconds, THE Home_Agent SHALL log the new status (connected/disconnected) to stdout
5. THE `/health` endpoint SHALL respond within 2 seconds under normal operating conditions

### Requirement 8: Local Docker Start and Stop Scripts

**User Story:** As a developer, I want standard start/stop scripts for both the relay (local testing) and the home agent, so that I can manage the containers consistently with the rest of my project.

#### Acceptance Criteria

1. THE project SHALL provide `scripts/start-docker.sh` for the Relay_Container that builds and runs it locally for testing
2. THE project SHALL provide `scripts/stop-docker.sh` for the Relay_Container that stops and removes the local container
3. THE project SHALL provide `scripts/start-wireguard.sh` for the Home_Agent that builds and starts the WireGuard client container on the trayline-net network
4. THE project SHALL provide `scripts/stop-wireguard.sh` for the Home_Agent that stops and removes the WireGuard client container
5. ALL scripts SHALL follow the script portability pattern (resolve own directory, work from any working directory)
6. ALL start scripts SHALL be idempotent — if a container with the same name already exists, THE script SHALL stop and remove it before starting a new one
7. ALL stop scripts SHALL exit gracefully without error if the target container does not exist

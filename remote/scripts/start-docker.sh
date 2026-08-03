#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

# ---------------------------------------------------------------------------
# Load env: prefer ~/.trayline/env/server.env, fall back to local .env
# ---------------------------------------------------------------------------
TRAYLINE_ENV="${HOME}/.trayline/env/server.env"
if [ -f "$TRAYLINE_ENV" ]; then
  ENV_FILE="$TRAYLINE_ENV"
elif [ -f .env ]; then
  ENV_FILE=".env"
else
  echo "ERROR: No env file found." >&2
  echo "  Expected: $TRAYLINE_ENV" >&2
  echo "  Fallback: $(pwd)/.env" >&2
  echo "  Run './setup/install.sh' to create env templates." >&2
  exit 1
fi

set -a
# shellcheck disable=SC1090
source "$ENV_FILE"
set +a

echo "Using env: $ENV_FILE"

NETWORK_NAME="${TRAYLINE_NETWORK:-trayline-net}"
PROXY_NAME="${TRAYLINE_PROXY:-trayline-proxy}"
PROXY_IMAGE="${TRAYLINE_PROXY_IMAGE:-tecnativa/docker-socket-proxy}"

# ---------------------------------------------------------------------------
# Ensure Docker network exists.
# ---------------------------------------------------------------------------
if ! docker network inspect "$NETWORK_NAME" > /dev/null 2>&1; then
  echo "Creating Docker network: $NETWORK_NAME"
  docker network create "$NETWORK_NAME"
else
  echo "Network $NETWORK_NAME already exists."
fi

# ---------------------------------------------------------------------------
# Ensure docker-socket-proxy is running.
# ---------------------------------------------------------------------------
PROXY_EXISTS=$(docker ps -a --filter "name=^/${PROXY_NAME}$" --format '{{.Names}}')
PROXY_RUNNING=$(docker ps --filter "name=^/${PROXY_NAME}$" --format '{{.Names}}')

if [ -z "$PROXY_EXISTS" ]; then
  # Container does not exist at all — create and start it.
  echo "Starting docker-socket-proxy ($PROXY_NAME)..."
  docker run -d \
    --name "$PROXY_NAME" \
    --network "$NETWORK_NAME" \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -e CONTAINERS=1 \
    -e EXEC=1 \
    -e IMAGES=1 \
    -e NETWORKS=1 \
    -e POST=1 \
    "$PROXY_IMAGE"
elif [ -z "$PROXY_RUNNING" ]; then
  # Container exists but is stopped — just start it.
  echo "Restarting stopped proxy ($PROXY_NAME)..."
  docker start "$PROXY_NAME"
else
  echo "Proxy $PROXY_NAME is already running."
fi

# Make sure it is connected to the network (handles the case where it was
# started manually without --network).
if ! docker network inspect "$NETWORK_NAME" \
    --format '{{range .Containers}}{{.Name}} {{end}}' \
    | grep -qw "$PROXY_NAME"; then
  echo "Connecting $PROXY_NAME to $NETWORK_NAME..."
  docker network connect "$NETWORK_NAME" "$PROXY_NAME"
fi

# ---------------------------------------------------------------------------
# Build the server image.
# ---------------------------------------------------------------------------
APP_PORT="${APP_PORT:-8080}"

# ---------------------------------------------------------------------------
# Build the server image.
# ---------------------------------------------------------------------------
bash scripts/build.sh

# ---------------------------------------------------------------------------
# Stop and remove any existing server container.
# ---------------------------------------------------------------------------
if docker ps -a --filter "name=^/trayline-server$" --format '{{.Names}}' | grep -q "^trayline-server$"; then
  echo "Removing existing trayline-server container..."
  docker rm -f trayline-server
fi

# ---------------------------------------------------------------------------
# Check that APP_PORT is not already in use by another process (after removing
# the old container so we don't conflict with ourselves on restart).
# ---------------------------------------------------------------------------
if ss -tlnp "sport = :${APP_PORT}" 2>/dev/null | grep -q ":${APP_PORT}" || \
   netstat -tlnp 2>/dev/null | grep -q ":${APP_PORT} "; then
  HOLDER=$(ss -tlnp "sport = :${APP_PORT}" 2>/dev/null | grep ":${APP_PORT}" || \
           netstat -tlnp 2>/dev/null | grep ":${APP_PORT} " || true)
  echo "ERROR: Port ${APP_PORT} is already in use by another process:" >&2
  echo "$HOLDER" >&2
  echo "Set a different APP_PORT in .env or stop the conflicting process." >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# Start the server container.
# ---------------------------------------------------------------------------
WORKSPACE_DIR="${WORKSPACE_DIR:-/workspace}"
WORKSPACE_HOST_DIR="${WORKSPACE_HOST_DIR:?WORKSPACE_HOST_DIR must be set in .env}"
PROJECTS_DIR="${PROJECTS_DIR:?PROJECTS_DIR must be set in .env}"

# ---------------------------------------------------------------------------
# Ensure workspace directory exists on the host.
# ---------------------------------------------------------------------------
if [ ! -d "$WORKSPACE_HOST_DIR" ]; then
  echo "Creating workspace directory: $WORKSPACE_HOST_DIR"
  mkdir -p "$WORKSPACE_HOST_DIR"
else
  echo "Workspace directory already exists: $WORKSPACE_HOST_DIR"
fi

docker run -d \
  --name trayline-server \
  --network "$NETWORK_NAME" \
  --env-file "$ENV_FILE" \
  -e DOCKER_HOST="tcp://${PROXY_NAME}:2375" \
  -v "${WORKSPACE_HOST_DIR}:${WORKSPACE_DIR}" \
  -v "${PROJECTS_DIR}:${PROJECTS_DIR}" \
  -p "${APP_PORT}:${APP_PORT}" \
  trayline-server

echo "trayline-server started on port ${APP_PORT}."

#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

# ---------------------------------------------------------------------------
# Load .env so variables are available for volume/port interpolation below.
# ---------------------------------------------------------------------------
if [ -f .env ]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

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
if ! docker ps --filter "name=^/${PROXY_NAME}$" --format '{{.Names}}' | grep -q "^${PROXY_NAME}$"; then
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
else
  # Make sure it is connected to the network (handles the case where it was
  # started manually without --network).
  if ! docker network inspect "$NETWORK_NAME" \
      --format '{{range .Containers}}{{.Name}} {{end}}' \
      | grep -qw "$PROXY_NAME"; then
    echo "Connecting $PROXY_NAME to $NETWORK_NAME..."
    docker network connect "$NETWORK_NAME" "$PROXY_NAME"
  else
    echo "Proxy $PROXY_NAME is already running and connected."
  fi
fi

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
# Start the server container.
# ---------------------------------------------------------------------------
APP_PORT="${APP_PORT:-8080}"
WORKSPACE_DIR="${WORKSPACE_DIR:-/workspace}"
WORKSPACE_HOST_DIR="${WORKSPACE_HOST_DIR:?WORKSPACE_HOST_DIR must be set in .env}"

docker run -d \
  --name trayline-server \
  --network "$NETWORK_NAME" \
  --env-file .env \
  -v "${WORKSPACE_HOST_DIR}:${WORKSPACE_DIR}" \
  -p "${APP_PORT}:${APP_PORT}" \
  trayline-server

echo "trayline-server started on port ${APP_PORT}."

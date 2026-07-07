#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HOME_AGENT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

IMAGE_NAME="trayline-home-agent"
CONTAINER_NAME="trayline-home-agent"
NETWORK_NAME="trayline-net"
ENV_FILE="$HOME_AGENT_DIR/.env"

if [ ! -f "$ENV_FILE" ]; then
    echo "Error: env file '$ENV_FILE' not found." >&2
    echo "Copy '$HOME_AGENT_DIR/.env.example' to '$ENV_FILE' and fill in the values." >&2
    exit 1
fi

# Ensure the trayline-net Docker network exists
if ! docker network ls --format '{{.Name}}' | grep -q "^${NETWORK_NAME}$"; then
    echo "==> Creating Docker network: $NETWORK_NAME"
    docker network create "$NETWORK_NAME"
fi

# Stop and remove existing container if present
if docker ps -a --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; then
    echo "==> Stopping and removing existing container: $CONTAINER_NAME"
    docker stop "$CONTAINER_NAME" 2>/dev/null || true
    docker rm "$CONTAINER_NAME" 2>/dev/null || true
fi

echo "==> Building Docker image: $IMAGE_NAME"
docker build --no-cache -t "$IMAGE_NAME" "$HOME_AGENT_DIR"

echo "==> Starting container: $CONTAINER_NAME"
docker run -d \
    --name "$CONTAINER_NAME" \
    --env-file "$ENV_FILE" \
    --network "$NETWORK_NAME" \
    --restart unless-stopped \
    "$IMAGE_NAME"

echo "==> Container started: $CONTAINER_NAME"

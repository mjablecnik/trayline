#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RELAY_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

IMAGE_NAME="trayline-relay"
CONTAINER_NAME="trayline-relay"
ENV_FILE="$RELAY_DIR/.env"

if [ ! -f "$ENV_FILE" ]; then
    echo "Error: env file '$ENV_FILE' not found." >&2
    echo "Copy '$RELAY_DIR/.env.example' to '$ENV_FILE' and fill in the values." >&2
    exit 1
fi

echo "==> Building Docker image: $IMAGE_NAME"
docker build --no-cache -t "$IMAGE_NAME" "$RELAY_DIR"

# Stop and remove existing container if present
if docker ps -a --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; then
    echo "==> Stopping and removing existing container: $CONTAINER_NAME"
    docker stop "$CONTAINER_NAME" 2>/dev/null || true
    docker rm "$CONTAINER_NAME" 2>/dev/null || true
fi

echo "==> Starting container: $CONTAINER_NAME"
docker run -d \
    --name "$CONTAINER_NAME" \
    --env-file "$ENV_FILE" \
    -p 8080:8080 \
    "$IMAGE_NAME"

echo "==> Container started: $CONTAINER_NAME"

#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DASHBOARD_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

IMAGE_NAME="trayline-dashboard"
CONTAINER_NAME="trayline-dashboard"
ENV_FILE="$DASHBOARD_DIR/.env"

if [ ! -f "$ENV_FILE" ]; then
    echo "Error: env file '$ENV_FILE' not found." >&2
    echo "Copy '$DASHBOARD_DIR/.env.example' to '$ENV_FILE' and fill in the values." >&2
    exit 1
fi

set -a
# shellcheck disable=SC1090
source "$ENV_FILE"
set +a

PUBLIC_API_URL="${PUBLIC_API_URL:?PUBLIC_API_URL must be set in $ENV_FILE}"

echo "==> Building Docker image: $IMAGE_NAME"
docker build --build-arg "PUBLIC_API_URL=${PUBLIC_API_URL}" -t "$IMAGE_NAME" "$DASHBOARD_DIR"

if docker ps -a --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; then
    echo "==> Stopping and removing existing container: $CONTAINER_NAME"
    docker stop "$CONTAINER_NAME" 2>/dev/null || true
    docker rm "$CONTAINER_NAME" 2>/dev/null || true
fi

echo "==> Starting container: $CONTAINER_NAME"
docker run -d \
    --name "$CONTAINER_NAME" \
    -p 8080:8080 \
    "$IMAGE_NAME"

echo "==> Container started: $CONTAINER_NAME"

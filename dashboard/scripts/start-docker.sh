#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DASHBOARD_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

IMAGE_NAME="trayline-dashboard"
CONTAINER_NAME="trayline-dashboard"

# ---------------------------------------------------------------------------
# Load env: prefer ~/.trayline/env/dashboard.env, fall back to local .env
# ---------------------------------------------------------------------------
TRAYLINE_ENV="${HOME}/.trayline/env/dashboard.env"
if [ -f "$TRAYLINE_ENV" ]; then
    ENV_FILE="$TRAYLINE_ENV"
elif [ -f "$DASHBOARD_DIR/.env" ]; then
    ENV_FILE="$DASHBOARD_DIR/.env"
else
    echo "ERROR: No env file found." >&2
    echo "  Expected: $TRAYLINE_ENV" >&2
    echo "  Fallback: $DASHBOARD_DIR/.env" >&2
    echo "  Copy '$DASHBOARD_DIR/.env.example' and fill in the values." >&2
    exit 1
fi

echo "Using env: $ENV_FILE"

set -a
# shellcheck disable=SC1090
source "$ENV_FILE"
set +a

PUBLIC_API_URL="${PUBLIC_API_URL:?PUBLIC_API_URL must be set in $ENV_FILE}"
DASHBOARD_PORT="${DASHBOARD_PORT:-5173}"

echo "==> Building Docker image: $IMAGE_NAME"
docker build --build-arg "PUBLIC_API_URL=${PUBLIC_API_URL}" -t "$IMAGE_NAME" "$DASHBOARD_DIR"

if docker ps -a --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; then
    echo "==> Stopping and removing existing container: $CONTAINER_NAME"
    docker stop "$CONTAINER_NAME" 2>/dev/null || true
    docker rm "$CONTAINER_NAME" 2>/dev/null || true
fi

echo "==> Starting container: $CONTAINER_NAME on port $DASHBOARD_PORT"
docker run -d \
    --name "$CONTAINER_NAME" \
    -p "${DASHBOARD_PORT}:8080" \
    "$IMAGE_NAME"

echo "==> Container started: $CONTAINER_NAME (http://localhost:${DASHBOARD_PORT})"

#!/usr/bin/env bash
set -euo pipefail

CONTAINER_NAME="trayline-dashboard"

if docker ps -a --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; then
    echo "==> Stopping and removing container: $CONTAINER_NAME"
    docker stop "$CONTAINER_NAME" 2>/dev/null || true
    docker rm "$CONTAINER_NAME" 2>/dev/null || true
    echo "==> Container removed: $CONTAINER_NAME"
else
    echo "==> Container '$CONTAINER_NAME' not found, nothing to do"
fi

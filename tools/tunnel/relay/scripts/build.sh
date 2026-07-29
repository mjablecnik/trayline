#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RELAY_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

IMAGE_NAME="trayline-relay"

echo "==> Building Docker image: $IMAGE_NAME"
docker build --no-cache -t "$IMAGE_NAME" "$RELAY_DIR"
echo "==> Build complete: $IMAGE_NAME"

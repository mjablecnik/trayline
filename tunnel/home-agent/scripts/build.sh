#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HOME_AGENT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

IMAGE_NAME="trayline-home-agent"

echo "==> Building Docker image: $IMAGE_NAME"
docker build --no-cache -t "$IMAGE_NAME" "$HOME_AGENT_DIR"
echo "==> Build complete: $IMAGE_NAME"

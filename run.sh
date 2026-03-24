#!/usr/bin/env bash
set -euo pipefail

IMAGE_NAME="kiro-sandbox"
PROJECT_DIR="${1:?Usage: $0 <project-dir> [prompt...]}"
shift
PROMPT="${*:?Usage: $0 <project-dir> <prompt>}"

# Build if image doesn't exist
if ! docker image inspect "$IMAGE_NAME" &>/dev/null; then
  echo "Building $IMAGE_NAME image (first run, may take a while)..."
  docker build -t "$IMAGE_NAME" "$(dirname "$0")"
fi

exec docker run --rm -it \
  --network=host \
  -v "$PROJECT_DIR":/workspace \
  -v "${HOME}/.kiro":/root/.kiro \
  -v "${HOME}/.local/share/kiro-cli":/root/.local/share/kiro-cli \
  -v /var/run/docker.sock:/var/run/docker.sock \
  "$IMAGE_NAME" \
  chat --no-interactive --trust-all-tools "$PROMPT"

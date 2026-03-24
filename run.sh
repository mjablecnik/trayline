#!/usr/bin/env bash
set -euo pipefail

IMAGE_NAME="kiro-sandbox"
PROXY_NAME="kiro-docker-proxy"
NETWORK_NAME="kiro-net"
PROJECT_DIR="${1:?Usage: $0 <project-dir> [prompt...]}"
shift
PROMPT="${*:?Usage: $0 <project-dir> <prompt>}"

# Build if image doesn't exist
if ! docker image inspect "$IMAGE_NAME" &>/dev/null; then
  echo "Building $IMAGE_NAME image (first run, may take a while)..."
  docker build -t "$IMAGE_NAME" "$(dirname "$0")"
fi

# Create network if needed
docker network inspect "$NETWORK_NAME" &>/dev/null || \
  docker network create "$NETWORK_NAME"

# Start docker socket proxy if not running
if ! docker inspect "$PROXY_NAME" &>/dev/null; then
  docker run -d --name "$PROXY_NAME" \
    --network "$NETWORK_NAME" \
    --restart unless-stopped \
    -v /var/run/docker.sock:/var/run/docker.sock:ro \
    -e CONTAINERS=1 \
    -e IMAGES=1 \
    -e NETWORKS=1 \
    -e LOGS=1 \
    -e BUILD=1 \
    -e POST=1 \
    -e EXEC=1 \
    -e ALLOW_START=1 \
    -e ALLOW_STOP=1 \
    -e ALLOW_RESTARTS=1 \
    -e VOLUMES=0 \
    -e NODES=0 \
    -e SERVICES=0 \
    -e SWARM=0 \
    -e SECRETS=0 \
    -e CONFIGS=0 \
    -e AUTH=0 \
    tecnativa/docker-socket-proxy
fi

# Run Kiro sandbox connected to proxy
docker run --rm -it \
  --network "$NETWORK_NAME" \
  -e DOCKER_HOST="tcp://${PROXY_NAME}:2375" \
  -v "$PROJECT_DIR":/workspace \
  -v "${HOME}/.kiro":/root/.kiro \
  -v "${HOME}/.local/share/kiro-cli":/root/.local/share/kiro-cli \
  "$IMAGE_NAME" \
  chat --no-interactive --trust-all-tools "$PROMPT"

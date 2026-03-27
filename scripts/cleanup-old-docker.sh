#!/bin/sh
# One-time cleanup of old Docker resources from the agent-docker era.
# Removes: agent-docker-proxy container, agent-sandbox image, agent-net network.

set -eu

echo "==> Stopping and removing old container: agent-docker-proxy"
docker rm -f agent-docker-proxy 2>/dev/null && echo "    Removed" || echo "    Not found (skipped)"

echo "==> Removing old image: agent-sandbox"
docker rmi agent-sandbox 2>/dev/null && echo "    Removed" || echo "    Not found (skipped)"

echo "==> Removing old network: agent-net"
docker network rm agent-net 2>/dev/null && echo "    Removed" || echo "    Not found (skipped)"

echo ""
echo "Done. Old Docker resources cleaned up."

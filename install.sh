#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BIN_DIR="${HOME}/bin"

echo "Building kiro-sandbox image..."
docker build -t kiro-sandbox "$SCRIPT_DIR"

mkdir -p "$BIN_DIR"
cp "$SCRIPT_DIR/agent-docker" "$BIN_DIR/agent-docker"
chmod +x "$BIN_DIR/agent-docker"
cp "$SCRIPT_DIR/sync.sh" "$BIN_DIR/agent-docker-sync"
chmod +x "$BIN_DIR/agent-docker-sync"

echo "Installed agent-docker and sync to ${BIN_DIR}/"

# Check if ~/bin is in PATH
if [[ ":$PATH:" != *":${BIN_DIR}:"* ]]; then
  echo "⚠️  ${BIN_DIR} is not in your PATH. Add this to your shell profile:"
  echo "  export PATH=\"\${HOME}/bin:\${PATH}\""
fi

#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BIN_DIR="${HOME}/bin"

echo "Building agent-sandbox image..."
docker build -t agent-sandbox "$SCRIPT_DIR"

mkdir -p "$BIN_DIR"
cp "$SCRIPT_DIR/agent-docker" "$BIN_DIR/agent-docker"
chmod +x "$BIN_DIR/agent-docker"
cp "$SCRIPT_DIR/sync.sh" "$BIN_DIR/agent-docker-sync"
chmod +x "$BIN_DIR/agent-docker-sync"

echo "Installed agent-docker and sync to ${BIN_DIR}/"

if command -v go &>/dev/null; then
  echo "Building orchestrator (trayline)..."
  (cd "$SCRIPT_DIR/orchestrator" && go build -ldflags "-X main.version=1.0.0" -o "$BIN_DIR/trayline" .)
else
  echo "Go not found, copying pre-built orchestrator binary..."
  cp "$SCRIPT_DIR/orchestrator/bin/orchestrator" "$BIN_DIR/trayline"
fi
chmod +x "$BIN_DIR/trayline"
echo "Installed trayline to ${BIN_DIR}/"

# Check if ~/bin is in PATH
if [[ ":$PATH:" != *":${BIN_DIR}:"* ]]; then
  echo "⚠️  ${BIN_DIR} is not in your PATH. Add this to your shell profile:"
  echo "  export PATH=\"\${HOME}/bin:\${PATH}\""
fi

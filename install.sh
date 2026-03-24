#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BIN_DIR="${HOME}/bin"

echo "Building kiro-sandbox image..."
docker build -t kiro-sandbox "$SCRIPT_DIR"

mkdir -p "$BIN_DIR"
cp "$SCRIPT_DIR/kiro-docker" "$BIN_DIR/kiro-docker"
chmod +x "$BIN_DIR/kiro-docker"

echo "Installed kiro-docker to ${BIN_DIR}/kiro-docker"

# Check if ~/bin is in PATH
if [[ ":$PATH:" != *":${BIN_DIR}:"* ]]; then
  echo "⚠️  ${BIN_DIR} is not in your PATH. Add this to your shell profile:"
  echo "  export PATH=\"\${HOME}/bin:\${PATH}\""
fi

#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

"$SCRIPT_DIR/build.sh"

BIN_DIR="${HOME}/.local/bin"
mkdir -p "$BIN_DIR"
cp bin/taskline-server "$BIN_DIR/taskline-server"

echo "Installed taskline-server to ${BIN_DIR}/taskline-server"

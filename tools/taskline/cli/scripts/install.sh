#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

"$SCRIPT_DIR/build.sh"

BIN_DIR="${HOME}/.local/bin"
mkdir -p "$BIN_DIR"
cp bin/taskline "$BIN_DIR/taskline"

COMPLETION_DIR="${HOME}/.zsh/completions"
mkdir -p "$COMPLETION_DIR"
cp completions/_taskline "$COMPLETION_DIR/_taskline"

echo "Installed taskline to ${BIN_DIR}/taskline"
echo "Installed zsh completion to ${COMPLETION_DIR}/_taskline"

#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Build first
"${SCRIPT_DIR}/build.sh"

# Install binary
INSTALL_BIN="${HOME}/.local/bin"
mkdir -p "${INSTALL_BIN}"
cp "${PROJECT_DIR}/bin/trayline-client" "${INSTALL_BIN}/trayline-client"
echo "Installed binary to ${INSTALL_BIN}/trayline-client"

# Install zsh completion
COMPLETION_DIR="${HOME}/.zsh/completions"
mkdir -p "${COMPLETION_DIR}"
cp "${PROJECT_DIR}/completions/_trayline-client" "${COMPLETION_DIR}/_trayline-client"
echo "Installed zsh completion to ${COMPLETION_DIR}/_trayline-client"

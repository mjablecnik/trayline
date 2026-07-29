#!/usr/bin/env zsh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

echo "==> Uninstalling..."
"$SCRIPT_DIR/uninstall.sh"

echo ""
echo "==> Installing..."
"$SCRIPT_DIR/install.sh" "$@"

#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TRAYLINE_HOME="${HOME}/.trayline"
ENV_DIR="${TRAYLINE_HOME}/env"

# Backup env files before uninstall (they are never overwritten)
ENV_BACKUP=""
if [[ -d "$ENV_DIR" ]]; then
  ENV_BACKUP="$(mktemp -d)"
  cp -a "$ENV_DIR/." "$ENV_BACKUP/"
fi

echo "==> Uninstalling..."
"$SCRIPT_DIR/uninstall.sh"

# Restore env files
if [[ -n "$ENV_BACKUP" ]]; then
  mkdir -p "$ENV_DIR"
  cp -a "$ENV_BACKUP/." "$ENV_DIR/"
  rm -rf "$ENV_BACKUP"
  echo "    Preserved env files in $ENV_DIR"
fi

echo ""
echo "==> Installing..."
"$SCRIPT_DIR/install.sh" "$@"

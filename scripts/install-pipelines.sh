#!/usr/bin/env zsh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SOURCE_DIR="$SCRIPT_DIR/../pipelines"
DEST_DIR="${HOME}/.trayline/pipelines"

echo "==> Removing old pipelines..."
rm -f "$DEST_DIR"/*.yaml

echo "==> Installing pipelines..."
mkdir -p "$DEST_DIR"

for f in "$SOURCE_DIR"/*.yaml(N); do
  cp "$f" "$DEST_DIR/$(basename "$f")"
  echo "    Installed $(basename "$f")"
done

echo ""
echo "Done! Pipelines installed to $DEST_DIR"

#!/usr/bin/env zsh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$SCRIPT_DIR/.."
TRAYLINE_HOME="${HOME}/.trayline"
PIPELINES_SOURCE="$REPO_DIR/pipelines"
PIPELINES_DEST="$TRAYLINE_HOME/pipelines"

echo "==> Building orchestrator (trayline-run)..."
if command -v go &>/dev/null; then
  (cd "$REPO_DIR/orchestrator" && go build -ldflags "-X main.version=1.2.0" -o "$TRAYLINE_HOME/trayline-run" .)
  chmod +x "$TRAYLINE_HOME/trayline-run"
  echo "    Built and installed trayline-run"
else
  echo "    ERROR: Go is not installed. Cannot build orchestrator." >&2
  exit 1
fi

echo "==> Installing tools..."
sed 's/\r$//' "$REPO_DIR/runtime/trayline-agent" > "$TRAYLINE_HOME/trayline-agent"
chmod +x "$TRAYLINE_HOME/trayline-agent"
echo "    Installed trayline-agent"

sed 's/\r$//' "$REPO_DIR/runtime/sync.sh" > "$TRAYLINE_HOME/sync.sh"
chmod +x "$TRAYLINE_HOME/sync.sh"
echo "    Installed sync.sh"

sed 's/\r$//' "$SCRIPT_DIR/.rsyncignore" > "$TRAYLINE_HOME/.rsyncignore"
echo "    Installed .rsyncignore"

echo "==> Removing old pipelines..."
rm -f "$PIPELINES_DEST"/*.yaml

echo "==> Installing pipelines..."
mkdir -p "$PIPELINES_DEST"

for f in "$PIPELINES_SOURCE"/*.yaml(N); do
  cp "$f" "$PIPELINES_DEST/$(basename "$f")"
  echo "    Installed $(basename "$f")"
done

echo ""
echo "Done! Installed to $TRAYLINE_HOME:"
echo "  trayline-run     (orchestrator)"
echo "  trayline-agent   (agent runner)"
echo "  sync.sh          (rsync wrapper)"
echo "  .rsyncignore     (rsync exclude list)"
echo "  pipelines/       (all pipeline YAML files)"

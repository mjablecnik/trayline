#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
TRAYLINE_HOME="${HOME}/.trayline"
PIPELINES_SOURCE="$REPO_ROOT/pipelines"
PIPELINES_DEST="$TRAYLINE_HOME/pipelines"

# Parse flags
SKIP_TOOLS=false
for arg in "$@"; do
  case "$arg" in
    --skip-tools) SKIP_TOOLS=true ;;
    -h|--help)
      cat <<EOF
Usage: install-pipelines.sh [--skip-tools]

Installs/updates pipelines, orchestrator binary, and runtime tools.
This is the fast-path for iterating on pipeline definitions without
rebuilding Docker images or touching env files.

Flags:
  --skip-tools   Skip building orchestrator and copying runtime scripts
                 (used internally by install.sh to avoid double work)
  -h, --help     Show this help
EOF
      exit 0
      ;;
    *) echo "Unknown flag: $arg" >&2; exit 1 ;;
  esac
done

mkdir -p "$TRAYLINE_HOME"
mkdir -p "$PIPELINES_DEST"

# ---------------------------------------------------------------------------
# Build orchestrator and install runtime tools (unless called from install.sh)
# ---------------------------------------------------------------------------
if ! $SKIP_TOOLS; then
  if command -v go &>/dev/null; then
    echo "==> Building orchestrator (trayline-run)..."
    (cd "$REPO_ROOT/orchestrator" && go build -o "$TRAYLINE_HOME/trayline-run" ./cmd)
    chmod +x "$TRAYLINE_HOME/trayline-run"
  else
    echo "==> Go not found. Skipping orchestrator build." >&2
  fi

  echo "==> Installing runtime tools..."
  sed 's/\r$//' "$REPO_ROOT/runtime/trayline-agent" > "$TRAYLINE_HOME/trayline-agent"
  chmod +x "$TRAYLINE_HOME/trayline-agent"

  sed 's/\r$//' "$REPO_ROOT/runtime/sync.sh" > "$TRAYLINE_HOME/sync.sh"
  chmod +x "$TRAYLINE_HOME/sync.sh"

  sed 's/\r$//' "$SCRIPT_DIR/.rsyncignore" > "$TRAYLINE_HOME/.rsyncignore"
fi

# ---------------------------------------------------------------------------
# Sync pipelines — copy new/updated files, remove deleted ones
# ---------------------------------------------------------------------------
echo "==> Syncing pipelines..."

# Copy lifecycle.yaml
if [[ -f "$PIPELINES_SOURCE/lifecycle.yaml" ]]; then
  cp "$PIPELINES_SOURCE/lifecycle.yaml" "$PIPELINES_DEST/lifecycle.yaml"
  echo "    Updated lifecycle.yaml"
fi

# Sync subdirectories (tasks, processes, workflows)
for dir in tasks processes workflows; do
  SRC_DIR="$PIPELINES_SOURCE/$dir"
  DST_DIR="$PIPELINES_DEST/$dir"

  if [[ ! -d "$SRC_DIR" ]]; then
    continue
  fi

  mkdir -p "$DST_DIR"

  # Copy new/updated files
  for f in "$SRC_DIR"/*.yaml; do
    [[ -f "$f" ]] || continue
    BASENAME="$(basename "$f")"
    dest="$DST_DIR/$BASENAME"
    if [[ ! -f "$dest" ]] || [[ "$f" -nt "$dest" ]]; then
      cp "$f" "$dest"
      echo "    Updated $dir/$BASENAME"
    else
      echo "    Skipped $dir/$BASENAME (up to date)"
    fi
  done

  # Remove files that no longer exist in source
  for f in "$DST_DIR"/*.yaml; do
    [[ -f "$f" ]] || continue
    BASENAME="$(basename "$f")"
    if [[ ! -f "$SRC_DIR/$BASENAME" ]]; then
      rm "$f"
      echo "    Removed $dir/$BASENAME (deleted from source)"
    fi
  done
done

echo ""
echo "Done! Pipelines installed to $PIPELINES_DEST"

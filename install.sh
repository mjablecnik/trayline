#!/usr/bin/env zsh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BIN_DIR="${HOME}/bin"
TRAYLINE_HOME="${HOME}/.trayline"

# Parse flags
BUILD_DOCKER=true
for arg in "$@"; do
  case "$arg" in
    --skip-docker-build) BUILD_DOCKER=false ;;
  esac
done

if $BUILD_DOCKER; then
  echo "==> Building trayline-sandbox Docker image..."
  docker build -t trayline-sandbox "$SCRIPT_DIR"
else
  echo "==> Skipping Docker build (--no-docker)"
fi

echo "==> Setting up directories..."
mkdir -p "$BIN_DIR"
mkdir -p "$TRAYLINE_HOME/pipelines"

# Install internal tools to ~/.trayline/ (strip CRLF for WSL compatibility)
echo "==> Installing tools to ${TRAYLINE_HOME}/"
sed 's/\r$//' "$SCRIPT_DIR/scripts/trayline-agent" > "$TRAYLINE_HOME/trayline-agent"
chmod +x "$TRAYLINE_HOME/trayline-agent"

sed 's/\r$//' "$SCRIPT_DIR/scripts/sync.sh" > "$TRAYLINE_HOME/sync.sh"
chmod +x "$TRAYLINE_HOME/sync.sh"

sed 's/\r$//' "$SCRIPT_DIR/.rsyncignore" > "$TRAYLINE_HOME/.rsyncignore"

# Install config template (don't overwrite existing config)
if [[ ! -f "$TRAYLINE_HOME/config" ]]; then
  sed 's/\r$//' "$SCRIPT_DIR/config.example" > "$TRAYLINE_HOME/config"
  echo "    Created config at ${TRAYLINE_HOME}/config (edit with your agent machine details)"
else
  echo "    Config already exists at ${TRAYLINE_HOME}/config (skipped)"
fi

# Build or copy orchestrator binary
if command -v go &>/dev/null; then
  echo "==> Building orchestrator (trayline-run)..."
  (cd "$SCRIPT_DIR/orchestrator" && go build -ldflags "-X main.version=2.0.0" -o "$TRAYLINE_HOME/trayline-run" .)
else
  echo "==> Go not found, copying pre-built orchestrator binary..."
  cp "$SCRIPT_DIR/orchestrator/bin/orchestrator" "$TRAYLINE_HOME/trayline-run"
fi
chmod +x "$TRAYLINE_HOME/trayline-run"

# Install main trayline wrapper to ~/bin/
echo "==> Installing trayline to ${BIN_DIR}/"
sed 's/\r$//' "$SCRIPT_DIR/scripts/trayline" > "$BIN_DIR/trayline"
chmod +x "$BIN_DIR/trayline"

# Copy default pipelines (overwrite if source is newer)
echo "==> Syncing default pipelines..."
for dir in tasks processes workflows; do
  mkdir -p "$TRAYLINE_HOME/pipelines/$dir"
  for f in "$SCRIPT_DIR"/pipelines/$dir/*.yaml(N); do
    dest="$TRAYLINE_HOME/pipelines/$dir/$(basename "$f")"
    if [[ ! -f "$dest" ]] || [[ "$f" -nt "$dest" ]]; then
      cp "$f" "$dest"
      echo "    Updated $dir/$(basename "$f")"
    else
      echo "    Skipped $dir/$(basename "$f") (up to date)"
    fi
  done
done
# Copy lifecycle.yaml
if [[ -f "$SCRIPT_DIR/pipelines/lifecycle.yaml" ]]; then
  dest="$TRAYLINE_HOME/pipelines/lifecycle.yaml"
  if [[ ! -f "$dest" ]] || [[ "$SCRIPT_DIR/pipelines/lifecycle.yaml" -nt "$dest" ]]; then
    cp "$SCRIPT_DIR/pipelines/lifecycle.yaml" "$dest"
    echo "    Updated lifecycle.yaml"
  else
    echo "    Skipped lifecycle.yaml (up to date)"
  fi
fi

# Install zsh completions (only if zsh is available and is the user's shell)
if command -v zsh &>/dev/null && [[ "$SHELL" == */zsh ]]; then
  echo "==> Installing zsh completions..."
  ZSH_COMP_DIR="${HOME}/.zsh/completions"
  mkdir -p "$ZSH_COMP_DIR"
  # Strip Windows line endings (CRLF -> LF) during copy
  sed 's/\r$//' "$SCRIPT_DIR/completions/_trayline" > "$ZSH_COMP_DIR/_trayline"

  ZSHRC="${HOME}/.zshrc"
  FPATH_LINE='fpath=(~/.zsh/completions $fpath)'

  if ! grep -qF "$FPATH_LINE" "$ZSHRC" 2>/dev/null; then
    # Insert fpath BEFORE oh-my-zsh source line so compinit picks it up
    if grep -q 'source.*oh-my-zsh.sh' "$ZSHRC" 2>/dev/null; then
      sed -i "/source.*oh-my-zsh.sh/i\\
# Trayline completions\\
${FPATH_LINE}" "$ZSHRC"
      echo "    Inserted fpath before oh-my-zsh in ${ZSHRC}"
    else
      # No oh-my-zsh — append with compinit
      echo "" >> "$ZSHRC"
      echo "# Trayline completions" >> "$ZSHRC"
      echo "$FPATH_LINE" >> "$ZSHRC"
      echo "autoload -Uz compinit && compinit" >> "$ZSHRC"
      echo "    Added completion setup to ${ZSHRC}"
    fi
  else
    echo "    Completion fpath already configured"
  fi
else
  echo "==> Skipping zsh completions (zsh not found or not the default shell)"
fi

echo ""
echo "Done! Installed:"
echo "  ~/bin/trayline              (main CLI)"
echo "  ~/.trayline/trayline-agent  (agent runner)"
echo "  ~/.trayline/sync.sh         (sync: git + rsync)"
echo "  ~/.trayline/.rsyncignore    (rsync exclude list)"
echo "  ~/.trayline/config          (agent machine config)"
echo "  ~/.trayline/trayline-run    (orchestrator)"
echo "  ~/.trayline/pipelines/      (global pipelines)"
echo "  ~/.zsh/completions/_trayline (zsh autocomplete)"

# Check if ~/bin is in PATH
if [[ ":$PATH:" != *":${BIN_DIR}:"* ]]; then
  echo ""
  echo "⚠️  ${BIN_DIR} is not in your PATH. Add this to your shell profile:"
  echo "  export PATH=\"\${HOME}/bin:\${PATH}\""
fi

echo ""
echo "Restart your shell or run 'source ~/.zshrc' to enable completions."

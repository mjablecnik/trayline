#!/usr/bin/env zsh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BIN_DIR="${HOME}/bin"
TRAYLINE_HOME="${HOME}/.trayline"

echo "==> Building trayline-sandbox Docker image..."
docker build -t trayline-sandbox "$SCRIPT_DIR"

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

# Build or copy orchestrator binary
if command -v go &>/dev/null; then
  echo "==> Building orchestrator (trayline-run)..."
  (cd "$SCRIPT_DIR/orchestrator" && go build -ldflags "-X main.version=1.2.0" -o "$TRAYLINE_HOME/trayline-run" .)
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
for f in "$SCRIPT_DIR"/pipelines/*.yaml(N); do
  dest="$TRAYLINE_HOME/pipelines/$(basename "$f")"
  if [[ ! -f "$dest" ]] || [[ "$f" -nt "$dest" ]]; then
    cp "$f" "$dest"
    echo "    Updated $(basename "$f")"
  else
    echo "    Skipped $(basename "$f") (up to date)"
  fi
done

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
echo "  ~/.trayline/sync.sh         (rsync wrapper)"
echo "  ~/.trayline/.rsyncignore    (rsync exclude list)"
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

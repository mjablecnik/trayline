#!/usr/bin/env bash
set -euo pipefail

BIN_DIR="${HOME}/bin"
LOCAL_BIN="${HOME}/.local/bin"
TRAYLINE_HOME="${HOME}/.trayline"
ZSH_COMP_DIR="${HOME}/.zsh/completions"
ZSHRC="${HOME}/.zshrc"

echo "==> Removing trayline files..."

# Main CLI wrapper
rm -f "$BIN_DIR/trayline"
echo "    Removed ~/bin/trayline"

# Everything in ~/.trayline/
if [[ -d "$TRAYLINE_HOME" ]]; then
  rm -rf "$TRAYLINE_HOME"
  echo "    Removed ~/.trayline/"
fi

# Go binaries from ~/.local/bin/
rm -f "$LOCAL_BIN/trayline-client"
rm -f "$LOCAL_BIN/taskline"
rm -f "$LOCAL_BIN/taskline-server"
echo "    Removed ~/.local/bin/trayline-client"
echo "    Removed ~/.local/bin/taskline"
echo "    Removed ~/.local/bin/taskline-server"

# Zsh completions
rm -f "$ZSH_COMP_DIR/_trayline"
rm -f "$ZSH_COMP_DIR/_trayline-client"
rm -f "$ZSH_COMP_DIR/_taskline"
echo "    Removed zsh completions"

# Remove fpath line from .zshrc
if [[ -f "$ZSHRC" ]]; then
  sed -i '/# Trayline completions/d' "$ZSHRC"
  sed -i '/fpath=(~\/.zsh\/completions \$fpath)/d' "$ZSHRC"
  echo "    Cleaned up .zshrc"
fi

echo ""
echo "Done! All trayline files removed."
echo ""
echo "Docker images (trayline-sandbox, trayline-server) were NOT removed."
echo "To remove them manually:"
echo "  docker rmi trayline-sandbox trayline-server"
echo ""
echo "Restart your shell or run 'source ~/.zshrc' to apply changes."

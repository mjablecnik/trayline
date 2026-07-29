#!/usr/bin/env zsh
set -euo pipefail

BIN_DIR="${HOME}/bin"
TRAYLINE_HOME="${HOME}/.trayline"
ZSH_COMP_DIR="${HOME}/.zsh/completions"
ZSHRC="${HOME}/.zshrc"

echo "==> Removing trayline files..."

# Remove installed binaries and tools
rm -f "$BIN_DIR/trayline"
rm -rf "$TRAYLINE_HOME"

# Remove zsh completions
rm -f "$ZSH_COMP_DIR/_trayline"

# Remove fpath line from .zshrc
if [[ -f "$ZSHRC" ]]; then
  sed -i '/# Trayline completions/d' "$ZSHRC"
  sed -i '/fpath=(~\/.zsh\/completions \$fpath)/d' "$ZSHRC"
fi

echo ""
echo "Done! Removed:"
echo "  ~/bin/trayline"
echo "  ~/.trayline/"
echo "  ~/.zsh/completions/_trayline"
echo "  Trayline fpath from ~/.zshrc"
echo ""
echo "Restart your shell or run 'source ~/.zshrc' to apply changes."

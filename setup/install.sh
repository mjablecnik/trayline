#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
BIN_DIR="${HOME}/bin"
LOCAL_BIN="${HOME}/.local/bin"
TRAYLINE_HOME="${HOME}/.trayline"
ENV_DIR="${TRAYLINE_HOME}/env"

# ---------------------------------------------------------------------------
# Parse flags
# ---------------------------------------------------------------------------
SKIP_DOCKER=false

for arg in "$@"; do
  case "$arg" in
    --skip-docker) SKIP_DOCKER=true ;;
    -h|--help)
      cat <<EOF
Usage: install.sh [--skip-docker]

Installs the full trayline stack:
  - Docker images (sandbox, trayline-server)
  - Go binaries (trayline-run, taskline, taskline-server, trayline-client)
  - Runtime scripts (trayline, trayline-agent, sync.sh)
  - Pipelines (YAML definitions)
  - Environment config templates
  - Zsh completions

Flags:
  --skip-docker   Skip building Docker images
  -h, --help      Show this help

Environment files in ~/.trayline/env/ are NEVER overwritten.
They are only created if they don't exist yet.
Use 'uninstall.sh' to remove them.
EOF
      exit 0
      ;;
    *) echo "Unknown flag: $arg" >&2; exit 1 ;;
  esac
done

# ---------------------------------------------------------------------------
# Helper: copy env template (never overwrite existing)
# ---------------------------------------------------------------------------
install_env() {
  local src="$1"
  local dest="$2"
  local name="$(basename "$dest")"

  if [[ -f "$dest" ]]; then
    echo "    Skipped $name (already exists)"
  else
    sed 's/\r$//' "$src" > "$dest"
    echo "    Installed $name"
  fi
}

# ---------------------------------------------------------------------------
# Create directories
# ---------------------------------------------------------------------------
echo "==> Setting up directories..."
mkdir -p "$BIN_DIR"
mkdir -p "$LOCAL_BIN"
mkdir -p "$TRAYLINE_HOME"
mkdir -p "$ENV_DIR"
mkdir -p "$TRAYLINE_HOME/pipelines"

# ---------------------------------------------------------------------------
# Docker images
# ---------------------------------------------------------------------------
if ! $SKIP_DOCKER; then
  echo "==> Building trayline-sandbox Docker image..."
  docker build -t trayline-sandbox "$REPO_ROOT/runtime/sandbox"

  echo "==> Building trayline-server Docker image..."
  docker build -t trayline-server "$REPO_ROOT/remote"
else
  echo "==> Skipping Docker builds (--skip-docker)"
fi

# ---------------------------------------------------------------------------
# Go binaries
# ---------------------------------------------------------------------------
if command -v go &>/dev/null; then
  echo "==> Building Go binaries..."

  echo "    Building orchestrator (trayline-run)..."
  (cd "$REPO_ROOT/orchestrator" && go build -o "$TRAYLINE_HOME/trayline-run" ./cmd)
  chmod +x "$TRAYLINE_HOME/trayline-run"

  echo "    Building trayline-client..."
  (cd "$REPO_ROOT/remote" && go build -o "$LOCAL_BIN/trayline-client" ./cmd/client)

  echo "    Building taskline..."
  (cd "$REPO_ROOT/tools/taskline/cli" && go build -o "$LOCAL_BIN/taskline" .)

  echo "    Building taskline-server..."
  (cd "$REPO_ROOT/tools/taskline/server" && go build -o "$LOCAL_BIN/taskline-server" .)
else
  echo "==> Go not found. Copying pre-built orchestrator binary (if available)..."
  if [[ -f "$REPO_ROOT/orchestrator/bin/orchestrator" ]]; then
    cp "$REPO_ROOT/orchestrator/bin/orchestrator" "$TRAYLINE_HOME/trayline-run"
    chmod +x "$TRAYLINE_HOME/trayline-run"
  else
    echo "    WARNING: No pre-built binary found. Install Go to build from source." >&2
  fi
  echo "    WARNING: Cannot build trayline-client, taskline, taskline-server without Go." >&2
fi

# ---------------------------------------------------------------------------
# Runtime scripts
# ---------------------------------------------------------------------------
echo "==> Installing runtime scripts..."
sed 's/\r$//' "$REPO_ROOT/runtime/trayline" > "$BIN_DIR/trayline"
chmod +x "$BIN_DIR/trayline"

# Also install trayline wrapper into TRAYLINE_HOME so Docker containers
# (which mount ~/.trayline at /home/agent/.trayline) can find it in PATH.
sed 's/\r$//' "$REPO_ROOT/runtime/trayline" > "$TRAYLINE_HOME/trayline"
chmod +x "$TRAYLINE_HOME/trayline"

sed 's/\r$//' "$REPO_ROOT/runtime/trayline-agent" > "$TRAYLINE_HOME/trayline-agent"
chmod +x "$TRAYLINE_HOME/trayline-agent"

sed 's/\r$//' "$REPO_ROOT/runtime/sync.sh" > "$TRAYLINE_HOME/sync.sh"
chmod +x "$TRAYLINE_HOME/sync.sh"

sed 's/\r$//' "$SCRIPT_DIR/.rsyncignore" > "$TRAYLINE_HOME/.rsyncignore"

# ---------------------------------------------------------------------------
# Config (sync config — don't overwrite)
# ---------------------------------------------------------------------------
echo "==> Installing config..."
if [[ ! -f "$TRAYLINE_HOME/config" ]]; then
  sed 's/\r$//' "$SCRIPT_DIR/config.example" > "$TRAYLINE_HOME/config"
  echo "    Created config (edit with your agent machine details)"
else
  echo "    Skipped config (already exists)"
fi

# ---------------------------------------------------------------------------
# Environment files
# ---------------------------------------------------------------------------
echo "==> Installing environment configs to ${ENV_DIR}/"
install_env "$REPO_ROOT/orchestrator/.env.example" "$ENV_DIR/orchestrator.env"
install_env "$REPO_ROOT/remote/.env.example" "$ENV_DIR/server.env"
install_env "$REPO_ROOT/tools/taskline/server/.env.example" "$ENV_DIR/taskline.env"
install_env "$REPO_ROOT/tools/tunnel/relay/.env.example" "$ENV_DIR/tunnel-relay.env"
install_env "$REPO_ROOT/tools/tunnel/home-agent/.env.example" "$ENV_DIR/tunnel-agent.env"

# ---------------------------------------------------------------------------
# Pipelines
# ---------------------------------------------------------------------------
echo "==> Syncing pipelines..."
"$SCRIPT_DIR/install-pipelines.sh" --skip-tools

# ---------------------------------------------------------------------------
# Zsh completions
# ---------------------------------------------------------------------------
if command -v zsh &>/dev/null; then
  echo "==> Installing zsh completions..."
  ZSH_COMP_DIR="${HOME}/.zsh/completions"
  mkdir -p "$ZSH_COMP_DIR"

  sed 's/\r$//' "$SCRIPT_DIR/completions/_trayline" > "$ZSH_COMP_DIR/_trayline"
  sed 's/\r$//' "$REPO_ROOT/remote/cmd/client/completions/_trayline-client" > "$ZSH_COMP_DIR/_trayline-client"
  sed 's/\r$//' "$REPO_ROOT/tools/taskline/cli/completions/_taskline" > "$ZSH_COMP_DIR/_taskline"

  ZSHRC="${HOME}/.zshrc"
  FPATH_LINE='fpath=(~/.zsh/completions $fpath)'

  if ! grep -qF "$FPATH_LINE" "$ZSHRC" 2>/dev/null; then
    if grep -q 'source.*oh-my-zsh.sh' "$ZSHRC" 2>/dev/null; then
      sed -i "/source.*oh-my-zsh.sh/i\\
# Trayline completions\\
${FPATH_LINE}" "$ZSHRC"
      echo "    Inserted fpath before oh-my-zsh in ${ZSHRC}"
    else
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
  echo "==> Skipping zsh completions (zsh not found)"
fi

# ---------------------------------------------------------------------------
# Systemd user services
# ---------------------------------------------------------------------------
if command -v systemctl &>/dev/null && systemctl --user status &>/dev/null 2>&1; then
  echo "==> Installing systemd user services..."
  SYSTEMD_DIR="${HOME}/.config/systemd/user"
  mkdir -p "$SYSTEMD_DIR"

  sed 's/\r$//' "$SCRIPT_DIR/systemd/taskline-server.service" > "$SYSTEMD_DIR/taskline-server.service"

  systemctl --user daemon-reload
  systemctl --user enable taskline-server.service
  echo "    Enabled taskline-server.service (starts on login)"
  echo "    Start now with: systemctl --user start taskline-server"
else
  echo "==> Skipping systemd setup (systemd user session not available)"
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
echo "Done! Installed:"
echo "  ~/bin/trayline                  (main CLI)"
echo "  ~/.trayline/trayline-run        (orchestrator)"
echo "  ~/.trayline/trayline-agent      (agent runner)"
echo "  ~/.trayline/sync.sh             (sync wrapper)"
echo "  ~/.trayline/config              (agent machine config)"
echo "  ~/.trayline/env/                (environment configs)"
echo "  ~/.trayline/pipelines/          (pipeline definitions)"
echo "  ~/.local/bin/trayline-client    (API client)"
echo "  ~/.local/bin/taskline           (task queue CLI)"
echo "  ~/.local/bin/taskline-server    (task queue server)"
echo "  ~/.zsh/completions/             (zsh autocomplete)"

# Check if ~/bin is in PATH
if [[ ":$PATH:" != *":${BIN_DIR}:"* ]]; then
  echo ""
  echo "⚠️  ${BIN_DIR} is not in your PATH. Add this to your shell profile:"
  echo "  export PATH=\"\${HOME}/bin:\${PATH}\""
fi

if [[ ":$PATH:" != *":${LOCAL_BIN}:"* ]]; then
  echo ""
  echo "⚠️  ${LOCAL_BIN} is not in your PATH. Add this to your shell profile:"
  echo "  export PATH=\"\${HOME}/.local/bin:\${PATH}\""
fi

echo ""
echo "Edit your env files in ~/.trayline/env/ before starting services."
echo "Restart your shell or run 'source ~/.zshrc' to enable completions."

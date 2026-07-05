#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

RELAY_ENV="$PROJECT_DIR/tunnel/relay/.env"
HOME_AGENT_ENV="$PROJECT_DIR/tunnel/home-agent/.env"

if ! command -v wg >/dev/null 2>&1; then
    echo "Error: 'wg' command not found. Please install WireGuard tools." >&2
    exit 1
fi

# Warn if existing .env files already contain key values
warn_existing_keys() {
    local env_file="$1"
    local label="$2"
    if [ -f "$env_file" ]; then
        for key in WG_PRIVATE_KEY WG_PEER_PUBLIC_KEY WG_PRESHARED_KEY; do
            if grep -qE "^${key}=.+" "$env_file" 2>/dev/null; then
                echo "Warning: $label '$env_file' already contains $key. Existing keys will NOT be overwritten." >&2
            fi
        done
    fi
}

warn_existing_keys "$RELAY_ENV" "Relay"
warn_existing_keys "$HOME_AGENT_ENV" "Home Agent"

# Generate key pairs
RELAY_PRIVATE_KEY="$(wg genkey)"
RELAY_PUBLIC_KEY="$(echo "$RELAY_PRIVATE_KEY" | wg pubkey)"

HOME_AGENT_PRIVATE_KEY="$(wg genkey)"
HOME_AGENT_PUBLIC_KEY="$(echo "$HOME_AGENT_PRIVATE_KEY" | wg pubkey)"

PRESHARED_KEY="$(wg genpsk)"

cat <<EOF
# === WireGuard Keys ===
# Copy these values into the respective .env files.
# Do NOT commit .env files to version control.

# --- Relay Container (tunnel/relay/.env) ---
WG_PRIVATE_KEY=${RELAY_PRIVATE_KEY}
WG_PEER_PUBLIC_KEY=${HOME_AGENT_PUBLIC_KEY}
WG_PRESHARED_KEY=${PRESHARED_KEY}

# --- Home Agent (tunnel/home-agent/.env) ---
WG_PRIVATE_KEY=${HOME_AGENT_PRIVATE_KEY}
WG_PEER_PUBLIC_KEY=${RELAY_PUBLIC_KEY}
WG_PRESHARED_KEY=${PRESHARED_KEY}
EOF

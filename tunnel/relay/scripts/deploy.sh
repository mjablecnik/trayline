#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RELAY_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# Parse APP_NAME from fly.toml
APP_NAME=$(grep '^app' "$RELAY_DIR/fly.toml" | head -1 | sed 's/app[[:space:]]*=[[:space:]]*"\(.*\)"/\1/')

if [ -z "$APP_NAME" ]; then
    echo "Error: could not parse app name from $RELAY_DIR/fly.toml" >&2
    exit 1
fi

# Determine env file
ENV_FILE="${DPLOY_ENV_FILE:-$RELAY_DIR/.env-prod}"

if [ ! -f "$ENV_FILE" ]; then
    echo "Error: env file '$ENV_FILE' not found." >&2
    echo "Copy '$RELAY_DIR/.env.example' to '$ENV_FILE' and fill in the secret values." >&2
    exit 1
fi

echo "==> App: $APP_NAME"
echo "==> Env file: $ENV_FILE"

# Check if app exists, create if not
if fly apps list 2>/dev/null | grep -qE "(^|[[:space:]])${APP_NAME}([[:space:]]|$)"; then
    echo "==> App '$APP_NAME' already exists"
else
    echo "==> Creating app: $APP_NAME"
    fly apps create "$APP_NAME"
fi

# Allocate dedicated IPv4 (required for UDP WireGuard traffic — shared IPv4 does NOT support UDP)
echo "==> Ensuring dedicated IPv4 allocation (required for WireGuard UDP)..."
if fly ips list --app "$APP_NAME" 2>/dev/null | grep -q "v4.*dedicated"; then
    echo "==> Dedicated IPv4 already allocated"
else
    echo "==> Allocating dedicated IPv4 ($2/month)..."
    fly ips allocate-v4 --app "$APP_NAME"
fi

# Extract keys already defined as plain env vars in fly.toml [env] — these should not be secrets
TOML_ENV_KEYS=$(awk '/^\[env\]/{found=1; next} found && /^\[/{found=0} found && /^[[:space:]]*[A-Z_][A-Z0-9_]*[[:space:]]*=/{key=$0; gsub(/[[:space:]]*=.*$/, "", key); gsub(/^[[:space:]]+/, "", key); print key}' "$RELAY_DIR/fly.toml")

# Build a filtered env string: only lines whose key is NOT in fly.toml [env]
SECRETS=$(grep -v '^[[:space:]]*#' "$ENV_FILE" | grep -v '^[[:space:]]*$' | while IFS= read -r line; do
    key="${line%%=*}"
    skip=false
    for toml_key in $TOML_ENV_KEYS; do
        if [ "$key" = "$toml_key" ]; then
            skip=true
            break
        fi
    done
    if [ "$skip" = false ]; then
        printf '%s\n' "$line"
    fi
done)

if [ -n "$SECRETS" ]; then
    echo "==> Setting secrets..."
    printf '%s\n' "$SECRETS" | fly secrets import --app "$APP_NAME"
else
    echo "==> No secrets to set"
fi

# Deploy
echo "==> Deploying $APP_NAME..."
cd "$RELAY_DIR"
fly deploy

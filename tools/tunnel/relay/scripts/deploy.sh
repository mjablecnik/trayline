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

# Determine env file: centralized > local fallback
TRAYLINE_ENV="${HOME}/.trayline/env/tunnel-relay.env"
LOCAL_ENV="$RELAY_DIR/.env-prod"

if [ -f "$TRAYLINE_ENV" ]; then
    ENV_FILE="$TRAYLINE_ENV"
elif [ -f "$LOCAL_ENV" ]; then
    ENV_FILE="$LOCAL_ENV"
else
    echo "Error: no env file found." >&2
    echo "  Expected: $TRAYLINE_ENV" >&2
    echo "  Fallback: $LOCAL_ENV" >&2
    echo "  Run './setup/install.sh' and edit ~/.trayline/env/tunnel-relay.env" >&2
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

# Set secrets from env file
SECRETS=$(grep -v '^[[:space:]]*#' "$ENV_FILE" | grep -v '^[[:space:]]*$' | grep '=.' || true)

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

#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DASHBOARD_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

APP_NAME=$(grep '^app' "$DASHBOARD_DIR/fly.toml" | head -1 | sed 's/app[[:space:]]*=[[:space:]]*"\(.*\)"/\1/')

if [ -z "$APP_NAME" ]; then
    echo "Error: could not parse app name from $DASHBOARD_DIR/fly.toml" >&2
    exit 1
fi

TRAYLINE_ENV="${HOME}/.trayline/env/dashboard.env"
LOCAL_ENV="$DASHBOARD_DIR/.env"

if [ -f "$TRAYLINE_ENV" ]; then
    ENV_FILE="$TRAYLINE_ENV"
elif [ -f "$LOCAL_ENV" ]; then
    ENV_FILE="$LOCAL_ENV"
else
    echo "Error: no env file found." >&2
    echo "  Expected: $TRAYLINE_ENV" >&2
    echo "  Fallback: $LOCAL_ENV" >&2
    echo "  Copy .env.example to .env and fill in the values." >&2
    exit 1
fi

set -a
# shellcheck disable=SC1090
source "$ENV_FILE"
set +a

PUBLIC_API_URL="${PUBLIC_API_URL:?PUBLIC_API_URL must be set in $ENV_FILE}"

echo "==> App: $APP_NAME"
echo "==> Env file: $ENV_FILE"

if fly apps list 2>/dev/null | grep -qE "(^|[[:space:]])${APP_NAME}([[:space:]]|$)"; then
    echo "==> App '$APP_NAME' already exists"
else
    echo "==> Creating app: $APP_NAME"
    fly apps create "$APP_NAME"
fi

echo "==> Deploying $APP_NAME..."
cd "$DASHBOARD_DIR"
fly deploy --build-arg "PUBLIC_API_URL=${PUBLIC_API_URL}"

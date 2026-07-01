#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

bash scripts/build.sh

docker run -d \
  --name trayline-server \
  --network trayline-net \
  --env-file .env \
  -v "${WORKSPACE_HOST_DIR:-./workspace}:${WORKSPACE_DIR:-/workspace}" \
  -p "${APP_PORT:-8080}:${APP_PORT:-8080}" \
  trayline-server

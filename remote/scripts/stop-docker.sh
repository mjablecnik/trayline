#!/usr/bin/env bash
set -euo pipefail

docker stop trayline-server 2>/dev/null || true
docker rm trayline-server 2>/dev/null || true

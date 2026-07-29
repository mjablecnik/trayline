#!/bin/bash
set -euo pipefail

CHISEL_PID=""

cleanup() {
    echo "[relay] Received signal, shutting down..."
    if [ -n "${CHISEL_PID}" ]; then
        kill "${CHISEL_PID}" 2>/dev/null || true
        wait "${CHISEL_PID}" 2>/dev/null || true
    fi
    echo "[relay] Shutdown complete"
    exit 0
}
trap cleanup SIGTERM SIGINT

echo "[relay] Starting chisel server on port 8080..."
echo "[relay] Auth: ${CHISEL_AUTH_USER}:<hidden>"
echo "[relay] Non-chisel HTTP requests will be proxied to 127.0.0.1:9000 (reverse tunnel target)"

chisel server \
    --port 8080 \
    --reverse \
    --proxy "http://127.0.0.1:9000" \
    --auth "${CHISEL_AUTH_USER}:${CHISEL_AUTH_PASS}" &
CHISEL_PID=$!

echo "[relay] Chisel server started with PID ${CHISEL_PID}"
wait "${CHISEL_PID}"
EXIT_CODE=$?
echo "[relay] Chisel exited with code ${EXIT_CODE}"
exit "${EXIT_CODE}"

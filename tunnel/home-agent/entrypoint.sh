#!/bin/bash
set -euo pipefail

CHISEL_PID=""

cleanup() {
    echo "[home-agent] Received signal, shutting down..."
    if [ -n "${CHISEL_PID}" ]; then
        kill "${CHISEL_PID}" 2>/dev/null || true
        wait "${CHISEL_PID}" 2>/dev/null || true
    fi
    echo "[home-agent] Shutdown complete"
    exit 0
}
trap cleanup SIGTERM SIGINT

echo "[home-agent] Connecting to relay: ${RELAY_URL}"
echo "[home-agent] Forwarding: relay:${RELAY_PORT} -> ${TRAYLINE_HOST}:${TRAYLINE_PORT}"

chisel client \
    --auth "${CHISEL_AUTH_USER}:${CHISEL_AUTH_PASS}" \
    --keepalive 25s \
    "${RELAY_URL}" \
    "R:0.0.0.0:${RELAY_PORT}:${TRAYLINE_HOST}:${TRAYLINE_PORT}" &
CHISEL_PID=$!

echo "[home-agent] Chisel client started with PID ${CHISEL_PID}"
wait "${CHISEL_PID}"
EXIT_CODE=$?
echo "[home-agent] Chisel client exited with code ${EXIT_CODE}"
exit "${EXIT_CODE}"

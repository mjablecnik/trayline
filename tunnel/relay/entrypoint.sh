#!/bin/bash
set -euo pipefail

CADDY_PID=""
HEALTH_PID=""

cleanup() {
    echo "[entrypoint] Received signal, shutting down..."
    if [ -n "${HEALTH_PID}" ]; then
        kill "${HEALTH_PID}" 2>/dev/null || true
    fi
    if [ -n "${CADDY_PID}" ]; then
        kill "${CADDY_PID}" 2>/dev/null || true
        wait "${CADDY_PID}" 2>/dev/null || true
    fi
    wg-quick down wg0 2>/dev/null || true
    echo "[entrypoint] Shutdown complete"
    exit 0
}
trap cleanup SIGTERM SIGINT

echo "[entrypoint] Generating WireGuard configuration..."
mkdir -p /etc/wireguard
cat > /etc/wireguard/wg0.conf <<EOF
[Interface]
PrivateKey = ${WG_PRIVATE_KEY}
Address = ${WG_SERVER_IP}/${WG_SUBNET}
ListenPort = ${WG_LISTEN_PORT}

[Peer]
PublicKey = ${WG_PEER_PUBLIC_KEY}
PresharedKey = ${WG_PRESHARED_KEY}
AllowedIPs = ${WG_PEER_ALLOWED_IPS}
EOF
chmod 600 /etc/wireguard/wg0.conf

echo "[entrypoint] Bringing up WireGuard interface..."
wg-quick up wg0

echo "[entrypoint] Waiting for WireGuard interface initialization (max 30s)..."
MAX_WAIT=30
elapsed=0
while ! ip link show wg0 > /dev/null 2>&1; do
    if [ "${elapsed}" -ge "${MAX_WAIT}" ]; then
        echo "[entrypoint] ERROR: WireGuard interface wg0 failed to initialize within ${MAX_WAIT} seconds" >&2
        wg-quick down wg0 2>/dev/null || true
        exit 1
    fi
    sleep 1
    elapsed=$((elapsed + 1))
done
echo "[entrypoint] WireGuard interface is up"

echo "[entrypoint] Starting health server on port 8080..."
socat TCP-LISTEN:8080,reuseaddr,fork EXEC:/app/health.sh &
HEALTH_PID=$!

echo "[entrypoint] Starting Caddy..."
caddy run --config /app/Caddyfile &
CADDY_PID=$!

echo "[entrypoint] Caddy started with PID ${CADDY_PID}, running..."
wait "${CADDY_PID}"
EXIT_CODE=$?
echo "[entrypoint] Caddy exited with code ${EXIT_CODE}"
exit "${EXIT_CODE}"

#!/bin/bash
set -euo pipefail

SOCAT_PID=""

cleanup() {
    echo "[home-agent] Received signal, shutting down..."
    if [ -n "${SOCAT_PID}" ]; then
        kill "${SOCAT_PID}" 2>/dev/null || true
        wait "${SOCAT_PID}" 2>/dev/null || true
    fi
    wg-quick down wg0 2>/dev/null || true
    echo "[home-agent] Shutdown complete"
    exit 0
}
trap cleanup SIGTERM SIGINT

get_handshake_age() {
    local ts
    ts=$(wg show wg0 latest-handshakes 2>/dev/null | awk '{print $2}' | head -1)
    if [ -z "${ts}" ] || [ "${ts}" = "0" ]; then
        echo "999999"
        return
    fi
    local now
    now=$(date +%s)
    echo $((now - ts))
}

is_connected() {
    local age
    age=$(get_handshake_age)
    [ "${age}" -le 180 ]
}

echo "[home-agent] Generating WireGuard configuration..."
mkdir -p /etc/wireguard
cat > /etc/wireguard/wg0.conf <<EOF
[Interface]
PrivateKey = ${WG_PRIVATE_KEY}
Address = ${WG_CLIENT_IP}/${WG_SUBNET}

[Peer]
PublicKey = ${WG_PEER_PUBLIC_KEY}
PresharedKey = ${WG_PRESHARED_KEY}
Endpoint = ${WG_PEER_ENDPOINT}
AllowedIPs = ${WG_PEER_ALLOWED_IPS}
PersistentKeepalive = ${WG_KEEPALIVE}
EOF
chmod 600 /etc/wireguard/wg0.conf

echo "[home-agent] Bringing up WireGuard interface..."
wg-quick up wg0

echo "[home-agent] Starting socat port forwarder (${WG_CLIENT_IP}:${UPSTREAM_PORT} -> ${TRAYLINE_HOST}:${TRAYLINE_PORT})..."
socat TCP-LISTEN:"${UPSTREAM_PORT}",bind="${WG_CLIENT_IP}",fork,reuseaddr \
      TCP:"${TRAYLINE_HOST}":"${TRAYLINE_PORT}" &
SOCAT_PID=$!
echo "[home-agent] socat started with PID ${SOCAT_PID}"

echo "[home-agent] Waiting for initial WireGuard connection (max 30s)..."
elapsed=0
connected=false
while [ "${elapsed}" -lt 30 ]; do
    if is_connected; then
        connected=true
        break
    fi
    sleep 1
    elapsed=$((elapsed + 1))
done

if ${connected}; then
    echo "[home-agent] Status: connected"
    LAST_STATE="connected"
else
    echo "[home-agent] ERROR: Failed to establish WireGuard connection within 30 seconds. Target endpoint: ${WG_PEER_ENDPOINT}" >&2
    echo "[home-agent] Status: disconnected"
    LAST_STATE="disconnected"

    # Retry every 10 seconds until the tunnel comes up
    while ! is_connected; do
        sleep 10
    done
    echo "[home-agent] Status: connected"
    LAST_STATE="connected"
fi

echo "[home-agent] Entering health monitoring loop (polling every 30s)..."
while true; do
    sleep 30
    if is_connected; then
        CURRENT_STATE="connected"
    else
        CURRENT_STATE="disconnected"
    fi

    if [ "${CURRENT_STATE}" != "${LAST_STATE}" ]; then
        echo "[home-agent] Status changed: ${LAST_STATE} -> ${CURRENT_STATE}"
        LAST_STATE="${CURRENT_STATE}"
    fi
done

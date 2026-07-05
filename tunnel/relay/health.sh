#!/bin/sh

HANDSHAKE_THRESHOLD=180

# Check WireGuard interface
wireguard_status="down"
if ip link show wg0 > /dev/null 2>&1; then
    wireguard_status="up"
fi

# Check peer handshake age
peer_handshake_seconds_ago=-1
if [ "$wireguard_status" = "up" ]; then
    handshake_time=$(wg show wg0 latest-handshakes 2>/dev/null | awk 'NR==1{print $2}')
    if [ -n "$handshake_time" ] && [ "$handshake_time" != "0" ]; then
        now=$(date +%s)
        peer_handshake_seconds_ago=$((now - handshake_time))
    fi
fi

# Check if Caddy is listening on port 443
proxy_status="not listening"
if ss -tnlp 2>/dev/null | grep -q ':443'; then
    proxy_status="listening"
fi

# Determine if degraded: no handshake recorded or handshake older than threshold
degraded=false
if [ "$peer_handshake_seconds_ago" -lt 0 ] || [ "$peer_handshake_seconds_ago" -gt "$HANDSHAKE_THRESHOLD" ]; then
    degraded=true
fi

# Build JSON body
if [ "$degraded" = "true" ]; then
    json="{\"wireguard\": \"${wireguard_status}\", \"proxy\": \"${proxy_status}\", \"peer_handshake_seconds_ago\": ${peer_handshake_seconds_ago}, \"status\": \"degraded\"}"
    http_status="503 Service Unavailable"
else
    json="{\"wireguard\": \"${wireguard_status}\", \"proxy\": \"${proxy_status}\", \"peer_handshake_seconds_ago\": ${peer_handshake_seconds_ago}}"
    http_status="200 OK"
fi

content_length=${#json}

printf "HTTP/1.1 %s\r\n" "$http_status"
printf "Content-Type: application/json\r\n"
printf "Content-Length: %d\r\n" "$content_length"
printf "Connection: close\r\n"
printf "\r\n"
printf "%s" "$json"

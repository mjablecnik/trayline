#!/bin/bash
set -euo pipefail

# Generate a random auth password for chisel
echo "# Chisel Authentication Credentials"
echo "# Use the same values in relay/.env-prod and home-agent/.env"
echo ""
echo "CHISEL_AUTH_USER=trayline"
echo "CHISEL_AUTH_PASS=$(head -c 32 /dev/urandom | base64 | tr -d '/+=' | head -c 32)"

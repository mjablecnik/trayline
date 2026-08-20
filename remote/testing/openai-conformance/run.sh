#!/usr/bin/env bash
# Run the OpenAI-compatible API conformance suite.
#
#   ./run.sh                          # against the built-in fake server (no Docker, no credits)
#   ./run.sh -k streaming             # any extra args are passed to pytest
#   ./run.sh --live --base-url URL --token TOKEN
#
# The virtualenv is created once in .venv/ and reused.
set -euo pipefail

cd "$(dirname "$0")"

VENV=".venv"
LIVE=0
PYTEST_ARGS=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --live) LIVE=1; shift ;;
    *) PYTEST_ARGS+=("$1"); shift ;;
  esac
done

if [[ ! -d "$VENV" ]]; then
  echo "==> creating virtualenv"
  python3 -m venv "$VENV"
  "$VENV/bin/pip" install --quiet --upgrade pip
  "$VENV/bin/pip" install --quiet -r requirements.txt
fi

if ! command -v go >/dev/null 2>&1; then
  echo "error: go is required to build the fake server" >&2
  exit 1
fi

if [[ "$LIVE" -eq 1 ]]; then
  echo "==> live mode: running only tests marked 'live'"
  exec "$VENV/bin/pytest" -m live "${PYTEST_ARGS[@]}"
fi

echo "==> fake mode: building and running the scripted server"
exec "$VENV/bin/pytest" "${PYTEST_ARGS[@]}"

#!/usr/bin/env zsh

REMOTE="martin@sandbox"
REMOTE_BASE="/home/martin/Projects"
EXCLUDE_FROM="/home/martin/.rsyncignore"
DIR_NAME="${PWD##*/}"

usage() {
  cat <<EOF
Usage: sync.sh <push|pull> [--verbose]

Syncs the current directory with ${REMOTE}:${REMOTE_BASE}/<current_dir_name>

Commands:
  push    Send local changes to remote
  pull    Fetch remote changes to local

Options:
  --verbose, -v   Show detailed rsync output
  --help, -h      Show this help

Examples:
  cd ~/Projects/my-app && sync.sh push          # push my-app to remote
  cd ~/Projects/my-app && sync.sh pull -v        # pull my-app from remote (verbose)
EOF
  exit 0
}

VERBOSE=""
ACTION=""

for arg in "$@"; do
  case "$arg" in
    push|pull) ACTION="$arg" ;;
    --verbose|-v) VERBOSE="-v" ;;
    --help|-h) usage ;;
    *) echo "Unknown argument: $arg"; usage ;;
  esac
done

[[ -z "$ACTION" ]] && { echo "Error: specify push or pull"; usage; }

RSYNC_OPTS=(-a $VERBOSE --exclude-from="$EXCLUDE_FROM")

case "$ACTION" in
  push) rsync "${RSYNC_OPTS[@]}" ./ "${REMOTE}:${REMOTE_BASE}/${DIR_NAME}/" ;;
  pull) rsync "${RSYNC_OPTS[@]}" "${REMOTE}:${REMOTE_BASE}/${DIR_NAME}/" ./ ;;
esac

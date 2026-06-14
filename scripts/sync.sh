#!/usr/bin/env zsh
set -euo pipefail

CONFIG_FILE="${HOME}/.trayline/config"
EXCLUDE_FROM="${HOME}/.trayline/.rsyncignore"
DIR_NAME="${PWD##*/}"

# Load config
if [[ -f "$CONFIG_FILE" ]]; then
  source "$CONFIG_FILE"
else
  echo "Error: Config file not found at $CONFIG_FILE" >&2
  echo "Create it with AGENT_HOST, AGENT_REPOS, and AGENT_PROJECTS variables." >&2
  exit 1
fi

: "${AGENT_HOST:?AGENT_HOST not set in $CONFIG_FILE}"
: "${AGENT_REPOS:?AGENT_REPOS not set in $CONFIG_FILE}"
: "${AGENT_PROJECTS:?AGENT_PROJECTS not set in $CONFIG_FILE}"

BARE_REPO="${AGENT_HOST}:${AGENT_REPOS}/${DIR_NAME}.git"
REMOTE_PROJECT="${AGENT_HOST}:${AGENT_PROJECTS}/${DIR_NAME}"

usage() {
  cat <<EOF
Usage: sync.sh <command> [options]

Syncs the current project with the agent machine via git or rsync.

Commands:
  push              Git: auto-commit + push to bare repo on agent machine
  pull              Git: pull from bare repo (rebase)
  setup             Create bare repo + working repo on agent machine (one-time per project)

Options:
  --rsync           Use rsync instead of git (legacy mode)
  --full            (pull --rsync) Pull everything including .kiro/
  --specs-only      (push --rsync) Push only .kiro/ directory
  --agents-only     (pull --rsync) Pull only .agents/ directory
  --code-only       (pull --rsync) Pull only code (excludes .kiro/ and .agents/)
  --verbose, -v     Show detailed output
  --dry-run         Show what would happen without doing it
  --help, -h        Show this help

Git mode (default):
  sync.sh push                Push committed work to bare repo
  sync.sh pull                Pull from bare repo with rebase
  sync.sh setup               Initialize bare + working repo on agent machine

Rsync mode (fallback):
  sync.sh push --rsync        Send files directly via rsync
  sync.sh pull --rsync        Fetch files (protects .kiro/ by default)
  sync.sh push --rsync --specs-only   Send only .kiro/ to remote
  sync.sh pull --rsync --full         Fetch everything (overwrite .kiro/ too)

Config: $CONFIG_FILE
  AGENT_HOST="user@host"
  AGENT_REPOS="/path/to/bare/repos"
  AGENT_PROJECTS="/path/to/working/repos"
EOF
  exit 0
}

VERBOSE=""
ACTION=""
USE_RSYNC=false
FULL=false
SPECS_ONLY=false
AGENTS_ONLY=false
CODE_ONLY=false
DRY_RUN=""

for arg in "$@"; do
  case "$arg" in
    push|pull|setup) ACTION="$arg" ;;
    --rsync) USE_RSYNC=true ;;
    --verbose|-v) VERBOSE="-v" ;;
    --full) FULL=true ;;
    --specs-only) SPECS_ONLY=true ;;
    --agents-only) AGENTS_ONLY=true ;;
    --code-only) CODE_ONLY=true ;;
    --dry-run) DRY_RUN="--dry-run" ;;
    --help|-h) usage ;;
    *) echo "Unknown argument: $arg" >&2; usage ;;
  esac
done

[[ -z "$ACTION" ]] && { echo "Error: specify push, pull, or setup" >&2; usage; }

# ─── GIT MODE ───────────────────────────────────────────────────────────────────

git_push() {
  # Auto-commit if there are uncommitted changes
  if [[ -n "$(git status --porcelain 2>/dev/null)" ]]; then
    git add -A
    git commit -m "sync: $(date '+%Y-%m-%d %H:%M:%S')"
    [[ -n "$VERBOSE" ]] && echo "Auto-committed local changes."
  fi

  if [[ -n "$DRY_RUN" ]]; then
    echo "[dry-run] Would push to $BARE_REPO"
    git log --oneline "origin/main..HEAD" 2>/dev/null || git log --oneline -3
    return
  fi

  git push "$BARE_REPO" main $VERBOSE
  echo "Pushed to agent bare repo."
}

git_pull() {
  if [[ -n "$DRY_RUN" ]]; then
    echo "[dry-run] Would pull from $BARE_REPO"
    git fetch "$BARE_REPO" main
    git log --oneline "HEAD..FETCH_HEAD"
    return
  fi

  # Auto-commit before rebase to avoid dirty tree errors
  if [[ -n "$(git status --porcelain 2>/dev/null)" ]]; then
    git add -A
    git commit -m "sync: local changes before pull $(date '+%Y-%m-%d %H:%M:%S')"
    [[ -n "$VERBOSE" ]] && echo "Auto-committed local changes before rebase."
  fi

  git pull "$BARE_REPO" main --rebase $VERBOSE

  if [[ $? -ne 0 ]]; then
    echo ""
    echo "Rebase conflict detected. Resolve conflicts, then run:"
    echo "  git rebase --continue"
    echo "Or abort with:"
    echo "  git rebase --abort"
    exit 1
  fi

  echo "Pulled from agent bare repo."
}

git_setup() {
  echo "Setting up bare repo and working repo on agent machine..."

  # Create bare repo on remote
  ssh "$AGENT_HOST" "
    mkdir -p ${AGENT_REPOS}
    if [ ! -d ${AGENT_REPOS}/${DIR_NAME}.git ]; then
      git init --bare ${AGENT_REPOS}/${DIR_NAME}.git
      echo 'Created bare repo: ${AGENT_REPOS}/${DIR_NAME}.git'
    else
      echo 'Bare repo already exists: ${AGENT_REPOS}/${DIR_NAME}.git'
    fi
  "

  # Push current state to bare repo
  git push "$BARE_REPO" main $VERBOSE 2>/dev/null || {
    # If main doesn't exist yet, push whatever branch we're on
    local BRANCH=$(git branch --show-current)
    git push "$BARE_REPO" "${BRANCH}:main" $VERBOSE
  }

  # Create working repo (clone from bare) on remote
  ssh "$AGENT_HOST" "
    if [ ! -d ${AGENT_PROJECTS}/${DIR_NAME} ]; then
      git clone ${AGENT_REPOS}/${DIR_NAME}.git ${AGENT_PROJECTS}/${DIR_NAME}
      echo 'Created working repo: ${AGENT_PROJECTS}/${DIR_NAME}'
    else
      echo 'Working repo already exists: ${AGENT_PROJECTS}/${DIR_NAME}'
      cd ${AGENT_PROJECTS}/${DIR_NAME}
      # Ensure 'agent' remote points to bare repo
      git remote set-url agent ${AGENT_REPOS}/${DIR_NAME}.git 2>/dev/null || \
        git remote add agent ${AGENT_REPOS}/${DIR_NAME}.git
      git fetch agent
      git checkout main 2>/dev/null || git checkout -b main agent/main
      git reset --hard agent/main
    fi
  "

  echo ""
  echo "Setup complete."
  echo "  Bare repo:    ${AGENT_HOST}:${AGENT_REPOS}/${DIR_NAME}.git"
  echo "  Working repo: ${AGENT_HOST}:${AGENT_PROJECTS}/${DIR_NAME}"
}

# ─── RSYNC MODE ─────────────────────────────────────────────────────────────────

rsync_push() {
  local RSYNC_OPTS=(-a --delete $VERBOSE $DRY_RUN --exclude-from="$EXCLUDE_FROM")

  if $SPECS_ONLY; then
    rsync -a --delete $VERBOSE $DRY_RUN \
      .kiro/ "${REMOTE_PROJECT}/.kiro/"
  else
    rsync "${RSYNC_OPTS[@]}" ./ "${REMOTE_PROJECT}/"
  fi
}

rsync_pull() {
  local RSYNC_OPTS=(-a --delete $VERBOSE $DRY_RUN --exclude-from="$EXCLUDE_FROM")

  if $AGENTS_ONLY; then
    rsync -a $VERBOSE $DRY_RUN \
      "${REMOTE_PROJECT}/.agents/" .agents/
  elif $CODE_ONLY; then
    rsync "${RSYNC_OPTS[@]}" \
      --exclude='.kiro/' --exclude='.agents/' \
      "${REMOTE_PROJECT}/" ./
  elif $FULL; then
    rsync "${RSYNC_OPTS[@]}" "${REMOTE_PROJECT}/" ./
  else
    rsync "${RSYNC_OPTS[@]}" \
      --exclude='.kiro/' \
      "${REMOTE_PROJECT}/" ./
  fi
}

# ─── DISPATCH ───────────────────────────────────────────────────────────────────

case "$ACTION" in
  setup)
    git_setup
    ;;
  push)
    if $USE_RSYNC; then
      rsync_push
    else
      git_push
    fi
    ;;
  pull)
    if $USE_RSYNC; then
      rsync_pull
    else
      git_pull
    fi
    ;;
esac

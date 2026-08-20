#!/usr/bin/env bash
# Dependency-free smoke test for the OpenAI-compatible API.
#
# Needs only curl and jq — no Python, no venv, no Go — so it can run against a
# production deployment, from CI, or from a post-deploy hook.
#
#   ./openai-smoke.sh https://your-host "$API_TOKEN" [model]
#
# Exits non-zero on the first failed check. For the exhaustive suite, use
# openai-conformance/run.sh instead.
set -uo pipefail

BASE_URL="${1:-http://127.0.0.1:8080}"
TOKEN="${2:-${API_TOKEN:-}}"
MODEL="${3:-kiro}"

BASE_URL="${BASE_URL%/}"
BASE_URL="${BASE_URL%/v1}"

if [[ -z "$TOKEN" ]]; then
  echo "usage: $0 <base-url> <api-token> [model]" >&2
  exit 64
fi

for tool in curl jq; do
  command -v "$tool" >/dev/null 2>&1 || { echo "error: $tool is required" >&2; exit 69; }
done

PASS=0
FAIL=0

ok()   { printf '  \033[32mok\033[0m   %s\n' "$1"; PASS=$((PASS + 1)); }
bad()  { printf '  \033[31mFAIL\033[0m %s\n' "$1"; [[ -n "${2:-}" ]] && printf '       %s\n' "$2"; FAIL=$((FAIL + 1)); }

check() { # check <description> <actual> <expected>
  if [[ "$2" == "$3" ]]; then ok "$1"; else bad "$1" "got '$2', want '$3'"; fi
}

api() { # api <method> <path> [json-body]
  local method="$1" path="$2" body="${3:-}"
  if [[ -n "$body" ]]; then
    curl -sS -X "$method" "$BASE_URL$path" \
      -H "Authorization: Bearer $TOKEN" \
      -H "Content-Type: application/json" \
      -d "$body"
  else
    curl -sS -X "$method" "$BASE_URL$path" -H "Authorization: Bearer $TOKEN"
  fi
}

status_of() { # status_of <method> <path> [json-body] [auth-header]
  # Note the ${4-…} form (no colon): an explicitly empty fourth argument means
  # "send no Authorization header at all", while omitting it uses the token.
  local method="$1" path="$2" body="${3:-}" auth="${4-Bearer $TOKEN}"
  local args=(-sS -o /dev/null -w '%{http_code}' -X "$method" "$BASE_URL$path"
              -H "Content-Type: application/json")
  [[ -n "$auth" ]] && args+=(-H "Authorization: $auth")
  [[ -n "$body" ]] && args+=(-d "$body")
  curl "${args[@]}"
}

echo "OpenAI API smoke test → $BASE_URL (model: $MODEL)"
echo

echo "health & discovery"
check "health returns 200" "$(curl -sS -o /dev/null -w '%{http_code}' "$BASE_URL/health")" "200"

MODELS="$(api GET /v1/models)"
check "GET /v1/models has object=list" "$(jq -r '.object // "missing"' <<<"$MODELS")" "list"
MODEL_COUNT="$(jq -r '.data | length' <<<"$MODELS" 2>/dev/null || echo 0)"
if [[ "$MODEL_COUNT" -gt 0 ]]; then ok "registry advertises $MODEL_COUNT model(s)"; else bad "registry is empty"; fi
check "GET /v1/models/$MODEL returns it" "$(jq -r '.id // "missing"' <<<"$(api GET "/v1/models/$MODEL")")" "$MODEL"

echo
echo "authentication"
check "missing token → 401" "$(status_of POST /v1/chat/completions '{"model":"'"$MODEL"'","messages":[{"role":"user","content":"hi"}]}' '')" "401"
check "bad token → 401" "$(status_of POST /v1/chat/completions '{"model":"'"$MODEL"'","messages":[{"role":"user","content":"hi"}]}' 'Bearer nope')" "401"
AUTH_ERR="$(curl -sS -X POST "$BASE_URL/v1/chat/completions" -H "Authorization: Bearer nope" \
  -H "Content-Type: application/json" -d '{"model":"'"$MODEL"'","messages":[{"role":"user","content":"hi"}]}')"
check "401 uses the OpenAI error shape" "$(jq -r '.error.type // "missing"' <<<"$AUTH_ERR")" "invalid_request_error"

echo
echo "validation"
check "missing model → 400" "$(status_of POST /v1/chat/completions '{"messages":[{"role":"user","content":"hi"}]}')" "400"
check "empty messages → 400" "$(status_of POST /v1/chat/completions '{"model":"'"$MODEL"'","messages":[]}')" "400"
check "unknown model → 404" "$(status_of POST /v1/chat/completions '{"model":"definitely-not-a-model","messages":[{"role":"user","content":"hi"}]}')" "404"
UNKNOWN="$(api POST /v1/chat/completions '{"model":"definitely-not-a-model","messages":[{"role":"user","content":"hi"}]}')"
check "unknown model code" "$(jq -r '.error.code // "missing"' <<<"$UNKNOWN")" "model_not_found"

echo
echo "chat completions (this invokes a real agent and may take a while)"
COMPLETION="$(api POST /v1/chat/completions \
  '{"model":"'"$MODEL"'","messages":[{"role":"user","content":"Reply with exactly the word PONG and nothing else."}]}')"

check "object" "$(jq -r '.object // "missing"' <<<"$COMPLETION")" "chat.completion"
check "one choice" "$(jq -r '.choices | length' <<<"$COMPLETION" 2>/dev/null || echo 0)" "1"
check "role" "$(jq -r '.choices[0].message.role // "missing"' <<<"$COMPLETION")" "assistant"
check "finish_reason" "$(jq -r '.choices[0].finish_reason // "missing"' <<<"$COMPLETION")" "stop"

ID="$(jq -r '.id // ""' <<<"$COMPLETION")"
if [[ "$ID" == chatcmpl-* ]]; then ok "id is chatcmpl-prefixed"; else bad "id prefix" "got '$ID'"; fi

USAGE_OK="$(jq -r '(.usage.total_tokens == (.usage.prompt_tokens + .usage.completion_tokens))' <<<"$COMPLETION" 2>/dev/null)"
check "usage totals add up" "$USAGE_OK" "true"

CONTENT="$(jq -r '.choices[0].message.content // ""' <<<"$COMPLETION")"
if [[ -n "$CONTENT" ]]; then ok "agent produced content (${#CONTENT} chars)"; else bad "agent produced no content"; fi

echo
echo "streaming"
STREAM="$(curl -sS -N -X POST "$BASE_URL/v1/chat/completions" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"model":"'"$MODEL"'","messages":[{"role":"user","content":"Count from 1 to 3."}],"stream":true}')"

CHUNKS="$(grep -c '^data: {' <<<"$STREAM" || true)"
if [[ "$CHUNKS" -ge 2 ]]; then ok "received $CHUNKS SSE chunks"; else bad "expected at least 2 SSE chunks, got $CHUNKS"; fi
check "terminated with [DONE]" "$(grep -c '^data: \[DONE\]' <<<"$STREAM" || true)" "1"

FIRST_CHUNK="$(grep -m1 '^data: {' <<<"$STREAM" | sed 's/^data: //')"
check "first chunk carries the role" "$(jq -r '.choices[0].delta.role // "missing"' <<<"$FIRST_CHUNK")" "assistant"
check "chunk object type" "$(jq -r '.object // "missing"' <<<"$FIRST_CHUNK")" "chat.completion.chunk"

LAST_CHUNK="$(grep '^data: {' <<<"$STREAM" | tail -1 | sed 's/^data: //')"
check "final chunk finish_reason" "$(jq -r '.choices[0].finish_reason // "missing"' <<<"$LAST_CHUNK")" "stop"

echo
echo "─────────────────────────────"
printf 'passed: %d   failed: %d\n' "$PASS" "$FAIL"
[[ "$FAIL" -eq 0 ]] || exit 1

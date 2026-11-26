#!/usr/bin/env bash
# Console API client, check counters and cleanup hooks shared by the acceptance suites.
# Source it after lab_env; CONSOLE_JAR names the cookie jar the requests use.

CONSOLE_BASE=${SAFEAGENT_BASE_URL:-http://127.0.0.1:8080}
CONSOLE_API="$CONSOLE_BASE/console/api"
CONSOLE_JAR=${CONSOLE_JAR:-"$LAB_RUNTIME/cookies-lab.txt"}
CONSOLE_MARKER=(-H 'Content-Type: application/json' -H 'X-SafeAgent-Request: 1' -H "Origin: $CONSOLE_BASE")

console_login() {
  local user=${1:-admin} password=${2:-${SAFEAGENT_ADMIN_PASSWORD:-}}
  umask 077
  curl -fsS -c "$CONSOLE_JAR" -X POST "$CONSOLE_API/session" "${CONSOLE_MARKER[@]}" \
    -d "$(jq -nc --arg user "$user" --arg password "$password" '{username:$user,password:$password}')" >/dev/null
}

api_request() {
  local jar=$1 method=$2 path=$3 body=${4:-}
  if [ "$method" = "GET" ]; then
    curl -fsS -b "$jar" "$CONSOLE_API$path"
  elif [ -n "$body" ]; then
    curl -fsS -b "$jar" "${CONSOLE_MARKER[@]}" -X "$method" "$CONSOLE_API$path" -d "$body"
  else
    curl -fsS -b "$jar" "${CONSOLE_MARKER[@]}" -X "$method" "$CONSOLE_API$path"
  fi
}

api_get() {
  api_request "${2:-$CONSOLE_JAR}" GET "$1"
}

api_post() {
  api_request "${3:-$CONSOLE_JAR}" POST "$1" "$2"
}

api_put() {
  api_request "${3:-$CONSOLE_JAR}" PUT "$1" "$2"
}

api_delete() {
  api_request "${2:-$CONSOLE_JAR}" DELETE "$1" >/dev/null 2>&1 || true
}

api_status() {
  local jar=$1 method=$2 path=$3 body=${4:-} file=${5:-/dev/null}
  local args=(-sS -o "$file" -w '%{http_code}' -b "$jar" "${CONSOLE_MARKER[@]}" -X "$method" "$CONSOLE_API$path")
  if [ -n "$body" ]; then
    args+=(-d "$body")
  fi
  curl "${args[@]}"
}

api_delete_status() {
  api_status "${3:-$CONSOLE_JAR}" DELETE "$1" "" "$2"
}

lab_status=0
lab_passed=0
lab_failed=0
lab_skipped=0

pass() {
  printf 'PASS %s\n' "$1"
  lab_passed=$((lab_passed + 1))
}

fail() {
  printf 'FAIL %s\n' "$1"
  lab_failed=$((lab_failed + 1))
  lab_status=1
}

skip() {
  printf 'SKIP %s%s\n' "$1" "${2:+: $2}"
  lab_skipped=$((lab_skipped + 1))
}

check() {
  if [ "$2" = "$3" ]; then
    pass "$1"
  else
    fail "$1: got $2, want $3"
  fi
}

summary() {
  printf '\n%d passed, %d failed' "$lab_passed" "$lab_failed"
  if [ "$lab_skipped" -gt 0 ]; then
    printf ', %d skipped' "$lab_skipped"
  fi
  printf '\n'
  return "$lab_status"
}

cleanup_hooks=()

cleanup_run() {
  local hook
  for hook in "${cleanup_hooks[@]}"; do
    "$hook" || true
  done
}

cleanup_register() {
  cleanup_hooks+=("$1")
  trap cleanup_run EXIT
}

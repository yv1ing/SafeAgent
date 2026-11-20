#!/usr/bin/env bash
set -euo pipefail

lab=$(cd "$(dirname "$0")/.." && pwd)
runtime="$lab/runtime"
cd "$lab"
set -a
# shellcheck disable=SC1091
. "$runtime/env"
set +a
export SAFEAGENT_LAB_UID="${SAFEAGENT_LAB_UID:-1000}" SAFEAGENT_LAB_GID="${SAFEAGENT_LAB_GID:-1000}"

mkdir -p "$runtime/logs"
docker compose run --rm -T --entrypoint agent-versions claude > "$runtime/logs/agent-versions.txt" 2>&1 || true

formats=($SAFEAGENT_AVAILABLE_FORMATS)
declare -A route
for format in "${formats[@]}"; do
  upper=${format^^}
  var="SAFEAGENT_${upper}_ROUTE"
  route[$format]=${!var}
done

declare -A key_name=([claude]=SAFEAGENT_CLAUDE_KEY [codex]=SAFEAGENT_CODEX_KEY [gemini]=SAFEAGENT_GEMINI_KEY [pi]=SAFEAGENT_PI_KEY)
declare -A native=([claude]=messages [codex]=responses [gemini]=gemini [pi]=chat)
status=0
passed=0
failed=0

run() {
  local agent=$1 format=$2 expected=$3 prompt=$4 suffix=${5:-}
  local workspace="$runtime/workspace/$agent"
  local log="$runtime/logs/$agent-$format$suffix.log"
  local keyvar=${key_name[$agent]}
  local key=${!keyvar}
  local model=${route[$format]}
  rm -f "$workspace/answer.txt"
  printf '=== %s -> %s\n' "$agent" "$format"
  set +e
  case "$agent" in
    claude)
      SAFEAGENT_KEY=$key timeout 300 docker compose run --rm -T claude -p "$prompt" \
        --model "$model" --allowedTools "Read Write Edit" >"$log" 2>&1
      ;;
    codex)
      SAFEAGENT_KEY=$key timeout 300 docker compose run --rm -T codex exec \
        --skip-git-repo-check --ephemeral -C /workspace -m "$model" "$prompt" >"$log" 2>&1
      ;;
    gemini)
      SAFEAGENT_KEY=$key timeout 300 docker compose run --rm -T \
        -e GEMINI_MODEL="$model" gemini -p "$prompt" -m "$model" \
        --approval-mode=yolo --output-format=text >"$log" 2>&1
      ;;
    pi)
      SAFEAGENT_KEY=$key timeout 300 docker compose run --rm -T pi -p \
        --provider safeagent --model "$model" --api-key "$key" --no-session "$prompt" >"$log" 2>&1
      ;;
  esac
  local code=$?
  set -e
  if [ -f "$workspace/answer.txt" ]; then
    cp "$workspace/answer.txt" "$runtime/logs/$agent-$format$suffix.answer"
  fi
  if [ "$code" -eq 0 ] && [ -f "$workspace/answer.txt" ] && [ "$(cat "$workspace/answer.txt")" = "$expected" ]; then
    printf 'PASS %s -> %s\n' "$agent" "$format"
    passed=$((passed + 1))
  else
    printf 'FAIL %s -> %s (exit %s, see %s)\n' "$agent" "$format" "$code" "$log"
    tail -20 "$log" | sed 's/^/  /'
    failed=$((failed + 1))
    status=1
  fi
}

for agent in claude codex gemini pi; do
  for format in "${formats[@]}"; do
    run "$agent" "$format" "bluebird from $agent-$format" \
      "Read hello.txt. Then create answer.txt whose only line is the word from hello.txt followed by ' from $agent-$format'. Use your file tools."
  done
  deep=${native[$agent]}
  if [[ " ${formats[*]} " == *" $deep "* ]]; then
    run "$agent" "$deep" "deep-bluebird" \
      "Read prefix.txt and hello.txt. Then create answer.txt whose only line is the text from prefix.txt immediately followed by the word from hello.txt. Use your file tools." \
      -deep
  else
    printf 'SKIP deep %s: %s upstream is not available\n' "$agent" "$deep"
  fi
done

printf '\n%d passed, %d failed\n' "$passed" "$failed"
exit "$status"

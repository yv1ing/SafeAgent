#!/usr/bin/env bash
set -euo pipefail

# shellcheck source=../lib/lab.sh
. "$(dirname "$0")/../lib/lab.sh"
require_lab
# shellcheck source=../lib/console.sh
. "$LAB/lib/console.sh"
umask 077
cd "$LAB"
export SAFEAGENT_LAB_UID="${SAFEAGENT_LAB_UID:-1000}" SAFEAGENT_LAB_GID="${SAFEAGENT_LAB_GID:-1000}"

runtime="$LAB_RUNTIME"
mkdir -p "$runtime/logs"
lab_compose run --rm -T --entrypoint agent-versions claude > "$runtime/logs/agent-versions.txt" 2>&1 || true

formats=($SAFEAGENT_AVAILABLE_FORMATS)
declare -A route
load_routes

declare -A key_name=([claude]=SAFEAGENT_CLAUDE_KEY [codex]=SAFEAGENT_CODEX_KEY [gemini]=SAFEAGENT_GEMINI_KEY [pi]=SAFEAGENT_PI_KEY)
declare -A native=([claude]=messages [codex]=responses [gemini]=gemini [pi]=chat)

run() {
  local agent=$1 format=$2 expected=$3 prompt=$4 suffix=${5:-}
  local workspace="$runtime/workspace/$agent"
  local log="$runtime/logs/$agent-$format$suffix.log"
  local keyvar=${key_name[$agent]}
  local key=${!keyvar}
  local model=${route[$format]}
  rm -f "$workspace/answer.txt" "$workspace/prefix.txt"
  if [ "$suffix" = "-deep" ]; then
    printf 'deep-' > "$workspace/prefix.txt"
    chown "$SAFEAGENT_LAB_UID:$SAFEAGENT_LAB_GID" "$workspace/prefix.txt" 2>/dev/null || true
  fi
  printf '=== %s -> %s\n' "$agent" "$format"
  set +e
  case "$agent" in
    claude)
      SAFEAGENT_KEY=$key timeout 300 "${LAB_COMPOSE[@]}" run --rm -T claude -p "$prompt" \
        --model "$model" --allowedTools "Read Write Edit" >"$log" 2>&1
      ;;
    codex)
      SAFEAGENT_KEY=$key timeout 300 "${LAB_COMPOSE[@]}" run --rm -T codex exec \
        --skip-git-repo-check --ephemeral -C /workspace -m "$model" "$prompt" >"$log" 2>&1
      ;;
    gemini)
      SAFEAGENT_KEY=$key timeout 300 "${LAB_COMPOSE[@]}" run --rm -T \
        -e GEMINI_MODEL="$model" gemini -p "$prompt" -m "$model" \
        --approval-mode=yolo --output-format=text >"$log" 2>&1
      ;;
    pi)
      SAFEAGENT_KEY=$key timeout 300 "${LAB_COMPOSE[@]}" run --rm -T pi -p \
        --provider safeagent --model "$model" --api-key "$key" --no-session "$prompt" >"$log" 2>&1
      ;;
  esac
  local code=$?
  set -e
  if [ -f "$workspace/answer.txt" ]; then
    cp "$workspace/answer.txt" "$runtime/logs/$agent-$format$suffix.answer"
  fi
  if [ "$code" -eq 0 ] && [ -f "$workspace/answer.txt" ] && [ "$(cat "$workspace/answer.txt")" = "$expected" ]; then
    pass "$agent -> $format"
  else
    fail "$agent -> $format (exit $code, see $log)"
    tail -20 "$log" | sed 's/^/  /'
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
      "Read prefix.txt and hello.txt. Then create answer.txt whose only line is the exact contents of prefix.txt followed directly by the word from hello.txt, keeping every character of prefix.txt including punctuation. Use your file tools." \
      -deep
  else
    skip "deep $agent" "$deep upstream is not available"
  fi
done

summary

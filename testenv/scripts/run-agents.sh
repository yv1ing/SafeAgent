#!/usr/bin/env bash
set -euo pipefail

lab=$(cd "$(dirname "$0")/.." && pwd)
runtime="$lab/runtime"
cd "$lab"
set -a
# shellcheck disable=SC1091
. "$runtime/env"
set +a

mkdir -p "$runtime/logs"
status=0
routes=("$SAFEAGENT_CHAT_MODEL" "$SAFEAGENT_RESPONSES_MODEL" "$SAFEAGENT_MESSAGES_MODEL")

run() {
  local agent=$1 route=$2
  local workspace="$runtime/workspace/$agent"
  local expected="bluebird from $agent-${route##*-}"
  local log="$runtime/logs/$agent-${route##*-}.log"
  rm -f "$workspace/answer.txt"
  printf '=== %s -> %s\n' "$agent" "$route"
  case "$agent" in
    claude)
      SAFEAGENT_KEY=$SAFEAGENT_CLAUDE_KEY timeout 300 docker compose run --rm -T claude -p \
        "Read hello.txt. Then create answer.txt whose only line is the word from hello.txt followed by ' from $agent-${route##*-}'." \
        --model "$route" --allowedTools "Read Write Edit Bash" >"$log" 2>&1 || true
      ;;
    codex)
      SAFEAGENT_KEY=$SAFEAGENT_CODEX_KEY timeout 300 docker compose run --rm -T codex exec \
        --skip-git-repo-check --ephemeral -C /workspace -m "$route" \
        "Read hello.txt. Then create answer.txt whose only line is the word from hello.txt followed by ' from $agent-${route##*-}'. Use your tools." >"$log" 2>&1 || true
      ;;
    gemini)
      SAFEAGENT_KEY=$SAFEAGENT_GEMINI_KEY timeout 300 docker compose run --rm -T \
        -e GEMINI_MODEL="$route" gemini -p \
        "Read hello.txt. Then create answer.txt whose only line is the word from hello.txt followed by ' from $agent-${route##*-}'. Use the read_file and write_file tools." \
        -m "$route" --approval-mode=yolo --output-format=text >"$log" 2>&1 || true
      ;;
    pi)
      SAFEAGENT_KEY=$SAFEAGENT_PI_KEY timeout 300 docker compose run --rm -T pi -p \
        --provider safeagent --model "$route" --api-key "$SAFEAGENT_PI_KEY" --no-session \
        "Read hello.txt. Then create answer.txt whose only line is the word from hello.txt followed by ' from $agent-${route##*-}'. Use the read and write tools." >"$log" 2>&1 || true
      ;;
  esac
  if [ -f "$workspace/answer.txt" ] && [ "$(cat "$workspace/answer.txt")" = "$expected" ]; then
    printf 'PASS %s -> %s\n' "$agent" "$route"
  else
    printf 'FAIL %s -> %s (see %s)\n' "$agent" "$route" "$log"
    tail -20 "$log" | sed 's/^/  /'
    status=1
  fi
}

for agent in claude codex gemini pi; do
  for route in "${routes[@]}"; do
    run "$agent" "$route"
  done
done
exit "$status"

#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
lab="$root/testenv"
out="$lab/runtime/load/$(date +%Y%m%dT%H%M%S)"

if [ -f "$lab/.env" ]; then
  # shellcheck disable=SC1091
  set -a; . "$lab/.env"; set +a
fi
if [ ! -f "$lab/runtime/env" ]; then
  echo "the lab is not set up; run make lab-setup first" >&2
  exit 1
fi
# shellcheck disable=SC1091
set -a; . "$lab/runtime/env"; set +a

: "${SAFEAGENT_UPSTREAM_KEY:?set SAFEAGENT_UPSTREAM_KEY in testenv/.env or the environment}"
: "${SAFEAGENT_MATRIX_KEY:?testenv/runtime/env is incomplete; run make lab-setup}"
: "${SAFEAGENT_CHAT_MODEL:?testenv/runtime/env is incomplete; run make lab-setup}"
: "${SAFEAGENT_UPSTREAM_CHAT_URL:?testenv/runtime/env is incomplete; run make lab-setup}"
: "${SAFEAGENT_UPSTREAM_MODEL:?testenv/runtime/env is incomplete; run make lab-setup}"

base=${SAFEAGENT_BASE_URL:-http://127.0.0.1:8080}
direct_url=$SAFEAGENT_UPSTREAM_CHAT_URL
direct_model=$SAFEAGENT_UPSTREAM_MODEL
requests=${LOAD_REQUESTS:-300}
concurrency=${LOAD_CONCURRENCY:-4}
warmup=${LOAD_WARMUP:-3}
max_tokens=${LOAD_MAX_TOKENS:-16}

if ! curl -fsS "$base/health" >/dev/null; then
  echo "the gateway is not healthy at $base; run make lab-setup or make lab-restart" >&2
  exit 1
fi

pending="unknown"
for _ in $(seq 1 60); do
  pending=$(docker compose -f "$lab/docker-compose.yml" exec -T database \
    psql -U safeagent -d safeagent -tAc \
    "SELECT count(*) FROM analysis_jobs WHERE state IN ('pending','running')" 2>/dev/null | tr -d '[:space:]')
  [ "$pending" = "0" ] && break
  sleep 2
done
if [ "$pending" != "0" ]; then
  echo "the analysis queue did not drain; wait for the worker before measuring performance" >&2
  exit 1
fi

mkdir -p "$out"
cd "$root"
GOFLAGS="${GOFLAGS:--buildvcs=false}" go build -trimpath -o "$out/loadgen" ./testenv/load

measure() {
  local label=$1 endpoint=$2 model=$3 stream=$4 key=$5 report=$6
  SAFEAGENT_LOAD_KEY="$key" "$out/loadgen" \
    -label "$label" -endpoint "$endpoint" -model "$model" -stream="$stream" \
    -requests "$requests" -concurrency "$concurrency" -warmup "$warmup" \
    -max-tokens "$max_tokens" -report "$report"
}

printf 'profile: %d requests per run, concurrency %d, warm-up %d, max tokens %d\n' \
  "$requests" "$concurrency" "$warmup" "$max_tokens"

status=0
scenario() {
  local stream=$1 name=$2 first=$3
  local baseline="$out/baseline-chat-$name.json"
  local gateway="$out/gateway-chat-$name.json"
  printf '\n=== chat %s\n' "$name"
  if [ "$first" = "gateway" ]; then
    measure "gateway-chat-$name" "$base/v1/chat/completions" "$SAFEAGENT_CHAT_MODEL" "$stream" "$SAFEAGENT_MATRIX_KEY" "$gateway"
    measure "baseline-chat-$name" "$direct_url" "$direct_model" "$stream" "$SAFEAGENT_UPSTREAM_KEY" "$baseline"
  else
    measure "baseline-chat-$name" "$direct_url" "$direct_model" "$stream" "$SAFEAGENT_UPSTREAM_KEY" "$baseline"
    measure "gateway-chat-$name" "$base/v1/chat/completions" "$SAFEAGENT_CHAT_MODEL" "$stream" "$SAFEAGENT_MATRIX_KEY" "$gateway"
  fi
  if ! "$out/loadgen" compare "$baseline" "$gateway"; then
    status=1
  fi
}

scenario false non-stream direct
scenario true stream gateway

printf '\nreports written to %s\n' "$out"
exit "$status"

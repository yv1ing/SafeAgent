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

base=${SAFEAGENT_BASE_URL:-http://127.0.0.1:8080}
requests=${LOAD_REQUESTS:-50}
concurrency=${LOAD_CONCURRENCY:-2}
warmup=${LOAD_WARMUP:-1}
max_tokens=${LOAD_MAX_TOKENS:-64}
cross_requests=${LOAD_CROSS_REQUESTS:-10}

if ! curl -fsS "$base/health" >/dev/null; then
  echo "the gateway is not healthy at $base; run make lab-setup or make lab-restart" >&2
  exit 1
fi

upstream() {
  local format=$1 suffix=$2
  local upper=${format^^}
  local specific="SAFEAGENT_UPSTREAM_${upper}_${suffix}"
  local value=${!specific:-}
  if [ -z "$value" ]; then
    case "$suffix" in
      KEY|MODEL|PROVIDER)
        local shared="SAFEAGENT_UPSTREAM_${suffix}"
        value=${!shared:-}
        ;;
    esac
  fi
  printf '%s' "$value"
}

wait_queue() {
  local pending="unknown"
  for _ in $(seq 1 150); do
    pending=$(docker compose -f "$lab/docker-compose.yml" exec -T database \
      psql -U safeagent -d safeagent -tAc \
      "SELECT count(*) FROM analysis_jobs WHERE state IN ('pending','running')" 2>/dev/null | tr -d '[:space:]')
    [ "$pending" = "0" ] && return 0
    sleep 2
  done
  echo "the analysis queue did not drain; wait for the worker before measuring" >&2
  return 1
}

mkdir -p "$out"
cd "$root"
GOFLAGS="${GOFLAGS:--buildvcs=false}" go build -trimpath -o "$out/loadgen" ./testenv/load

gateway_endpoint() {
  local format=$1 route=$2 stream=$3
  case "$format" in
    chat) printf '%s' "$base/v1/chat/completions" ;;
    responses) printf '%s' "$base/v1/responses" ;;
    messages) printf '%s' "$base/v1/messages" ;;
    gemini)
      if [ "$stream" = "true" ]; then
        printf '%s' "$base/v1beta/models/$route:streamGenerateContent?alt=sse"
      else
        printf '%s' "$base/v1beta/models/$route:generateContent"
      fi
      ;;
  esac
}

measure() {
  local label=$1 protocol=$2 endpoint=$3 model=$4 stream=$5 key=$6 report=$7 count=$8
  SAFEAGENT_LOAD_KEY="$key" "$out/loadgen" \
    -label "$label" -protocol "$protocol" -endpoint "$endpoint" -model "$model" -stream="$stream" \
    -requests "$count" -concurrency "$concurrency" -warmup "$warmup" \
    -max-tokens "$max_tokens" -report "$report"
}

printf 'profile: %d requests per native run, concurrency %d, warm-up %d, max tokens %d; %d cross requests\n' \
  "$requests" "$concurrency" "$warmup" "$max_tokens" "$cross_requests"

status=0
formats=($SAFEAGENT_AVAILABLE_FORMATS)
declare -A route
for format in "${formats[@]}"; do
  upper=${format^^}
  var="SAFEAGENT_${upper}_ROUTE"
  route[$format]=${!var}
done

for format in "${formats[@]}"; do
  upper=${format^^}
  route_name=${route[$format]}
  direct_url=$(upstream "$format" ENDPOINT)
  if [ -z "$direct_url" ]; then
    echo "SAFEAGENT_UPSTREAM_${format^^}_ENDPOINT is required for the direct baseline" >&2
    exit 1
  fi
  direct_model=$(upstream "$format" MODEL)
  direct_key=$(upstream "$format" KEY)
  for stream in false true; do
    name=$format-$([ "$stream" = "true" ] && echo stream || echo json)
    baseline="$out/baseline-$name.json"
    gateway="$out/gateway-$name.json"
    printf '\n=== %s native\n' "$name"
    measure "baseline-$name" "$format" "$direct_url" "$direct_model" "$stream" "$direct_key" "$baseline" "$requests"
    wait_queue
    measure "gateway-$name" "$format" "$(gateway_endpoint "$format" "$route_name" "$stream")" "$route_name" "$stream" "$SAFEAGENT_MATRIX_KEY" "$gateway" "$requests"
    if ! "$out/loadgen" compare "$baseline" "$gateway"; then
      status=1
    fi
  done
done

printf '\n=== cross-format sample\n'
for client in chat responses messages gemini; do
  for format in "${formats[@]}"; do
    route_name=${route[$format]}
    for stream in false true; do
      report="$out/cross-$client-$format-$([ "$stream" = "true" ] && echo stream || echo json).json"
      measure "cross-$client-$format" "$client" "$(gateway_endpoint "$client" "$route_name" "$stream")" "$route_name" "$stream" "$SAFEAGENT_MATRIX_KEY" "$report" "$cross_requests" >/dev/null
      if jq -e '.failed == 0 and .succeeded == .requests' "$report" >/dev/null; then
        printf 'PASS cross %s->%s %s\n' "$client" "$format" "$([ "$stream" = "true" ] && echo stream || echo json)"
      else
        printf 'FAIL cross %s->%s %s: %s\n' "$client" "$format" "$([ "$stream" = "true" ] && echo stream || echo json)" \
          "$(jq -c '{succeeded,failed,error_counts}' "$report")"
        status=1
      fi
    done
  done
done

printf '\nreports written to %s\n' "$out"
exit "$status"

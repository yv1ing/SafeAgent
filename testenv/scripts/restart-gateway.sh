#!/usr/bin/env bash
set -euo pipefail

# shellcheck source=../lib/lab.sh
. "$(dirname "$0")/../lib/lab.sh"
if [ ! -f "$LAB_RUNTIME/config.toml" ]; then
  echo "the lab is not set up; run make lab-setup first" >&2
  exit 1
fi
lab_env
base=${SAFEAGENT_BASE_URL:-http://127.0.0.1:8080}

if [ "${SKIP_WEB:-0}" != "1" ]; then
  ( cd "$LAB_ROOT" && npm --prefix web run build )
fi
( cd "$LAB_ROOT" && CGO_ENABLED=0 go build -trimpath -tags embedded -o build/bin/safeagent ./cmd/safeagent )

port=$(sed -n 's/^listen = ".*:\([0-9][0-9]*\)"/\1/p' "$LAB_RUNTIME/config.toml" 2>/dev/null | head -1)
if [ -f "$LAB_RUNTIME/gateway.pid" ] || [ -n "$(lab_gateway_pids)" ]; then
  stop_lab_gateway
  free_lab_port "${port:-8080}"
fi

cd "$LAB_ROOT"
nohup ./build/bin/safeagent -config "$LAB_RUNTIME/config.toml" serve >> "$LAB_RUNTIME/gateway.log" 2>&1 < /dev/null &
gateway_pid=$!
disown "$gateway_pid" 2>/dev/null || true
echo "$gateway_pid" > "$LAB_RUNTIME/gateway.pid"

for _ in $(seq 1 50); do
  if ! kill -0 "$gateway_pid" 2>/dev/null; then
    echo "the gateway exited during startup" >&2
    tail -20 "$LAB_RUNTIME/gateway.log" >&2
    exit 1
  fi
  if curl -fsS "$base/health" >/dev/null 2>&1; then
    exit 0
  fi
  sleep 0.2
done
echo "the gateway did not become healthy" >&2
tail -20 "$LAB_RUNTIME/gateway.log" >&2
exit 1

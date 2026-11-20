#!/usr/bin/env bash
set -euo pipefail

lab=$(cd "$(dirname "$0")/.." && pwd)
root=$(cd "$lab/.." && pwd)
runtime="$lab/runtime"

if [ "${SKIP_WEB:-0}" != "1" ]; then
  ( cd "$root" && npm --prefix web run build )
fi
( cd "$root" && CGO_ENABLED=0 go build -trimpath -tags embedded -o build/bin/safeagent ./cmd/safeagent )

if [ -f "$runtime/gateway.pid" ]; then
  pid=$(cat "$runtime/gateway.pid")
  if kill -0 "$pid" 2>/dev/null; then
    kill "$pid"
    for _ in $(seq 1 20); do
      kill -0 "$pid" 2>/dev/null || break
      sleep 0.2
    done
    if kill -0 "$pid" 2>/dev/null; then
      kill -9 "$pid"
      sleep 0.2
    fi
  fi
fi
rm -f "$runtime/gateway.pid"

cd "$root"
nohup ./build/bin/safeagent -config "$lab/runtime/config.toml" serve >> "$runtime/gateway.log" 2>&1 < /dev/null &
gateway_pid=$!
disown "$gateway_pid" 2>/dev/null || true
echo "$gateway_pid" > "$runtime/gateway.pid"

for _ in $(seq 1 50); do
  if ! kill -0 "$gateway_pid" 2>/dev/null; then
    echo "the gateway exited during startup" >&2
    tail -20 "$runtime/gateway.log" >&2
    exit 1
  fi
  if curl -fsS http://127.0.0.1:8080/health >/dev/null 2>&1; then
    exit 0
  fi
  sleep 0.2
done
echo "the gateway did not become healthy" >&2
tail -20 "$runtime/gateway.log" >&2
exit 1

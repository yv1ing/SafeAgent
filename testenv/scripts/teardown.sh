#!/usr/bin/env bash
set -euo pipefail

lab=$(cd "$(dirname "$0")/.." && pwd)
runtime="$lab/runtime"
# shellcheck source=lib.sh
. "$lab/scripts/lib.sh"
lab_env "$lab"

if [ -f "$runtime/gateway.pid" ]; then
  pid=$(cat "$runtime/gateway.pid")
  if kill -0 "$pid" 2>/dev/null; then
    kill "$pid"
    for _ in $(seq 1 40); do
      kill -0 "$pid" 2>/dev/null || break
      sleep 0.25
    done
    kill -0 "$pid" 2>/dev/null && kill -9 "$pid" || true
  fi
fi
if [ -f "$runtime/fault-server.pid" ]; then
  pid=$(cat "$runtime/fault-server.pid")
  kill "$pid" 2>/dev/null || true
  rm -f "$runtime/fault-server.pid"
fi

down=(down --volumes --remove-orphans)
if [ "${PURGE:-0}" = "1" ]; then
  down+=(--rmi local)
fi
docker compose -f "$lab/docker-compose.yml" --profile agents --profile browser "${down[@]}"

rm -rf "$runtime"
printf 'acceptance lab stopped; generated runtime files removed\n'

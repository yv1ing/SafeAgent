#!/usr/bin/env bash
set -euo pipefail

# shellcheck source=../lib/lab.sh
. "$(dirname "$0")/../lib/lab.sh"
lab_env

stop_gateway() {
  local port
  if [ ! -f "$LAB_RUNTIME/gateway.pid" ] && [ -z "$(lab_gateway_pids)" ]; then
    return 0
  fi
  port=$(sed -n 's/^listen = ".*:\([0-9][0-9]*\)"/\1/p' "$LAB_RUNTIME/config.toml" 2>/dev/null | head -1)
  stop_lab_gateway
  free_lab_port "${port:-8080}"
}

stop_gateway
stop_lab_process "$LAB_RUNTIME/faults/fault-server.pid"

down=(down --volumes --remove-orphans)
if [ "${PURGE:-0}" = "1" ]; then
  down+=(--rmi local)
fi
lab_compose --profile agents --profile browser "${down[@]}"

rm -rf "$LAB_RUNTIME"
printf 'acceptance lab stopped; generated runtime files removed; evidence in %s is preserved\n' "$LAB_EVIDENCE"

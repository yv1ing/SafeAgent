#!/usr/bin/env bash
# Paths and environment shared by the acceptance lab. Source this file first.

LAB_LIB_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
LAB=$(cd "$LAB_LIB_DIR/.." && pwd)
LAB_ROOT=$(cd "$LAB/.." && pwd)
LAB_RUNTIME="$LAB/runtime"
LAB_EVIDENCE="$LAB_ROOT/build/lab"
LAB_COMPOSE=(docker compose -f "$LAB/docker-compose.yml")

lab_env() {
  local root=${1:-$LAB}
  if [ -f "$root/.env" ]; then
    # shellcheck disable=SC1091
    set -a; . "$root/.env"; set +a
  fi
  if [ -f "$root/runtime/env" ]; then
    # shellcheck disable=SC1091
    set -a; . "$root/runtime/env"; set +a
  fi
}

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

load_routes() {
  local format upper variable
  route=()
  for format in ${SAFEAGENT_AVAILABLE_FORMATS:-}; do
    upper=${format^^}
    variable="SAFEAGENT_${upper}_ROUTE"
    route[$format]=${!variable:-}
  done
}

require_lab() {
  lab_env
  if [ ! -f "$LAB_RUNTIME/env" ]; then
    echo "the lab is not set up; run make lab-setup first" >&2
    exit 1
  fi
}

lab_compose() {
  "${LAB_COMPOSE[@]}" "$@"
}

stop_lab_process() {
  local pid_file=$1 pid
  if [ ! -f "$pid_file" ]; then
    return 0
  fi
  pid=$(cat "$pid_file" 2>/dev/null || true)
  if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
    kill "$pid" 2>/dev/null || true
    for _ in $(seq 1 40); do
      kill -0 "$pid" 2>/dev/null || break
      sleep 0.25
    done
    if kill -0 "$pid" 2>/dev/null; then
      kill -9 "$pid" 2>/dev/null || true
    fi
  fi
  rm -f "$pid_file"
}

lab_gateway_pids() {
  pgrep -f "build/bin/safeagent.*$LAB_RUNTIME/config.toml" 2>/dev/null || true
}

stop_lab_gateway() {
  local pid
  stop_lab_process "$LAB_RUNTIME/gateway.pid"
  for pid in $(lab_gateway_pids); do
    kill "$pid" 2>/dev/null || true
  done
}

free_lab_port() {
  local port=$1
  if command -v fuser >/dev/null 2>&1; then
    fuser -k "$port/tcp" 2>/dev/null || true
  fi
}

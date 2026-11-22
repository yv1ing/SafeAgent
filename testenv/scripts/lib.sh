#!/usr/bin/env bash
# Shared helpers for the acceptance lab. Source this file after computing the lab path.

lab_env() {
  local root=$1
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
  for format in $SAFEAGENT_AVAILABLE_FORMATS; do
    upper=${format^^}
    variable="SAFEAGENT_${upper}_ROUTE"
    route[$format]=${!variable}
  done
}

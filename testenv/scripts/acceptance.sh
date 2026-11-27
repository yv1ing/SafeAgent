#!/usr/bin/env bash
set -euo pipefail

# shellcheck source=../lib/lab.sh
. "$(dirname "$0")/../lib/lab.sh"
lab_env
mkdir -p "$LAB_EVIDENCE"
log="$LAB_EVIDENCE/acceptance.log"
: > "$log"
cd "$LAB_ROOT"

if [ ! -f "$LAB_RUNTIME/env" ]; then
  make lab-setup
fi

run() {
  local name=$1
  shift
  printf '\n=== %s\n' "$name" | tee -a "$log"
  if "$@" 2>&1 | tee -a "$log"; then
    printf 'PASS %s\n' "$name" | tee -a "$log"
  else
    printf 'FAIL %s\n' "$name" | tee -a "$log"
    printf '\nthe lab was kept for inspection; log: %s\n' "$log" >&2
    exit 1
  fi
}

run verify make verify
run integration make lab-integration
run control-plane make lab-control-plane
run protocol make lab-protocol
run auxiliary make lab-auxiliary
run agents make lab-agents
run policy make lab-policy
run threat make lab-threat
run security make lab-security
run faults make lab-faults
run trajectory make lab-trajectory
run browser make lab-browser
run performance make lab-load
run audit make lab-audit

printf '\nacceptance passed; stopping the lab\n' | tee -a "$log"
if [ "${KEEP_LAB:-0}" = "1" ]; then
  printf 'KEEP_LAB=1: leaving the lab running\n'
else
  make lab-down
fi

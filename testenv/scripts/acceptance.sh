#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
lab="$root/testenv"
runtime="$lab/runtime"
log="$runtime/acceptance/acceptance.log"
cd "$root"

if [ ! -f "$runtime/env" ]; then
  make lab-setup
fi
mkdir -p "$runtime/acceptance"
: > "$log"

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
run protocol make lab-protocol
run agents make lab-agents
run policy make lab-policy
run analysis make lab-analysis
run security make lab-security
run faults make lab-faults
run browser make lab-browser
run performance make lab-load

printf '\nacceptance passed; stopping the lab\n' | tee -a "$log"
if [ "${KEEP_LAB:-0}" = "1" ]; then
  printf 'KEEP_LAB=1: leaving the lab running\n'
else
  make lab-down
fi

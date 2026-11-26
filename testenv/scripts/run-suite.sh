#!/usr/bin/env bash
# Runs one Node or browser acceptance suite with the lab environment loaded.
set -euo pipefail

# shellcheck source=../lib/lab.sh
. "$(dirname "$0")/../lib/lab.sh"
require_lab

case "${1:-}" in
  protocol) exec node "$LAB/protocol/matrix.mjs" ;;
  auxiliary) exec node "$LAB/protocol/auxiliary.mjs" ;;
  trajectory) exec node "$LAB/protocol/trajectory.mjs" ;;
  browser)
    lab_compose build browser
    lab_compose run --rm browser
    ;;
  *)
    echo "usage: $0 {protocol|auxiliary|trajectory|browser}" >&2
    exit 2
    ;;
esac

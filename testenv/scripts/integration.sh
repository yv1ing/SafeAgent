#!/usr/bin/env bash
set -euo pipefail

# shellcheck source=../lib/lab.sh
. "$(dirname "$0")/../lib/lab.sh"
cd "$LAB_ROOT"

database="postgres://safeagent:safeagent@127.0.0.1:55432/safeagent_integration?sslmode=disable"
lab_compose up -d database >/dev/null
for _ in $(seq 1 60); do
  if lab_compose exec -T database pg_isready -U safeagent -d safeagent >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
lab_compose exec -T database pg_isready -U safeagent -d safeagent >/dev/null

exists=$(lab_compose exec -T database \
  psql -U safeagent -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname = 'safeagent_integration'")
if [ "$exists" != "1" ]; then
  lab_compose exec -T database createdb -U safeagent safeagent_integration
fi

SAFEAGENT_TEST_DATABASE="$database" go test -tags integration -count=1 ./internal/storage/repository/

#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
lab="$root/testenv"
database="postgres://safeagent:safeagent@127.0.0.1:55432/safeagent_integration?sslmode=disable"

cd "$root"
docker compose -f "$lab/docker-compose.yml" up -d database >/dev/null
for _ in $(seq 1 60); do
  if docker compose -f "$lab/docker-compose.yml" exec -T database pg_isready -U safeagent -d safeagent >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
docker compose -f "$lab/docker-compose.yml" exec -T database pg_isready -U safeagent -d safeagent >/dev/null

exists=$(docker compose -f "$lab/docker-compose.yml" exec -T database \
  psql -U safeagent -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname = 'safeagent_integration'")
if [ "$exists" != "1" ]; then
  docker compose -f "$lab/docker-compose.yml" exec -T database createdb -U safeagent safeagent_integration
fi

SAFEAGENT_TEST_DATABASE="$database" go test -tags integration -count=1 ./internal/storage/repository/

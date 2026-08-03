#!/usr/bin/env bash
set -euo pipefail

services=(postgres mysql)

if [[ "${SCHEMAPILOT_TEST_SQLSERVER:-}" == "1" ]]; then
  services+=(sqlserver)
  export COMPOSE_PROFILES=sqlserver
fi

cleanup() {
  docker compose down
}
trap cleanup EXIT

docker compose up -d --wait "${services[@]}"

postgres_address="$(docker compose port postgres 5432)"
mysql_address="$(docker compose port mysql 3306)"
export SCHEMAPILOT_POSTGRES_DSN="postgres://schemapilot:schemapilot@${postgres_address}/schemapilot?sslmode=disable"
export SCHEMAPILOT_MYSQL_DSN="schemapilot:schemapilot@tcp(${mysql_address})/schemapilot?parseTime=true&multiStatements=true"
if [[ "${SCHEMAPILOT_TEST_SQLSERVER:-}" == "1" ]]; then
  sqlserver_address="$(docker compose port sqlserver 1433)"
  export SCHEMAPILOT_SQLSERVER_DSN="sqlserver://sa:SchemaPilot_dev_2026%21@${sqlserver_address}?database=master&encrypt=disable"
fi

go test -tags=integration -count=1 ./internal/database

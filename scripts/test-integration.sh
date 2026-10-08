#!/usr/bin/env bash
set -euo pipefail

# Point at existing databases with SCHEMAPILOT_TEST_POSTGRES / SCHEMAPILOT_TEST_MYSQL,
# e.g. postgres://user:pass@host:5432/db?sslmode=disable and mysql://user:pass@host:3306/db.
# Without them, throwaway containers from compose.yaml are started locally.
if [[ -z "${SCHEMAPILOT_TEST_POSTGRES:-}" && -z "${SCHEMAPILOT_TEST_MYSQL:-}" ]]; then
  trap 'docker compose down' EXIT
  docker compose up -d --wait postgres mysql
  export SCHEMAPILOT_TEST_POSTGRES="postgres://schemapilot:schemapilot@$(docker compose port postgres 5432)/schemapilot?sslmode=disable"
  export SCHEMAPILOT_TEST_MYSQL="mysql://schemapilot:schemapilot@$(docker compose port mysql 3306)/schemapilot"
fi

go test -tags=integration -count=1 ./internal/runner

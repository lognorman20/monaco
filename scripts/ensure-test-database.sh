#!/usr/bin/env bash
# Create and migrate `{dbname}_test` on the same local Postgres as DATABASE_URL.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

source "$root/scripts/assert-local-database-url.sh"

test_url="$(DATABASE_URL="$DATABASE_URL" "$root/scripts/derive-test-database-url.sh")"
test_name="${test_url##*/}"
test_name="${test_name%%\?*}"

psql_user="${POSTGRES_USER:-monaco}"
psql_db="${POSTGRES_DB:-monaco}"

exists="$(
  docker compose exec -T postgres psql -U "$psql_user" -d "$psql_db" -tAc \
    "SELECT 1 FROM pg_database WHERE datname = '${test_name//\'/\'\'}' LIMIT 1"
)"
if [[ "${exists// /}" != "1" ]]; then
  echo "creating database ${test_name}..."
  docker compose exec -T postgres psql -U "$psql_user" -d "$psql_db" -v ON_ERROR_STOP=1 \
    -c "CREATE DATABASE \"${test_name}\""
fi

echo "migrating test database ${test_name}..."
DATABASE_URL="$test_url" "$root/scripts/apply-migrations.sh"

echo "ok: tests use ${test_url}"
echo "ok: app still uses ${DATABASE_URL}"

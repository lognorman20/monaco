#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

migrations_dir="supabase/migrations"
if [[ ! -d "$migrations_dir" ]]; then
  echo "error: missing $migrations_dir"
  exit 1
fi

psql_user="${POSTGRES_USER:-monaco}"
psql_db="${POSTGRES_DB:-monaco}"

docker compose exec -T postgres psql -U "$psql_user" -d "$psql_db" -v ON_ERROR_STOP=1 <<'SQL'
CREATE TABLE IF NOT EXISTS schema_migrations (
  version TEXT PRIMARY KEY,
  applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
SQL

shopt -s nullglob
files=("$migrations_dir"/*.sql)
if ((${#files[@]} == 0)); then
  echo "no migration files in $migrations_dir"
  exit 0
fi

for file in $(printf '%s\n' "${files[@]}" | sort); do
  version="$(basename "$file")"
  applied="$(
    docker compose exec -T postgres psql -U "$psql_user" -d "$psql_db" -tAc \
      "SELECT 1 FROM schema_migrations WHERE version = '${version//\'/\'\'}' LIMIT 1"
  )"
  if [[ "${applied// /}" == "1" ]]; then
    echo "skip $version"
    continue
  fi

  echo "apply $version"
  docker compose exec -T postgres psql -U "$psql_user" -d "$psql_db" -v ON_ERROR_STOP=1 <"$file"
  docker compose exec -T postgres psql -U "$psql_user" -d "$psql_db" -v ON_ERROR_STOP=1 \
    -c "INSERT INTO schema_migrations (version) VALUES ('${version//\'/\'\'}')"
done

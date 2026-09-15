#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

source "$root/scripts/assert-local-database-url.sh"

psql_user="${POSTGRES_USER:-monaco}"
psql_db="${POSTGRES_DB:-monaco}"

docker compose exec -T postgres psql -U "$psql_user" -d "$psql_db" -v ON_ERROR_STOP=1 -c \
  "SELECT current_database() AS db, current_user AS user;"

container_id="$(docker compose ps -q postgres)"
if [[ -z "$container_id" ]]; then
  echo "error: monaco postgres container is not running"
  exit 1
fi

if [[ "$(docker inspect -f '{{.Name}}' "$container_id")" != "/monaco-postgres" ]]; then
  echo "error: unexpected postgres container name (expected monaco-postgres)"
  exit 1
fi

echo "ok: connected to local Docker Postgres (project monaco, not hosted Supabase)"

#!/usr/bin/env bash
# Wipe local Docker Compose Postgres volume and re-apply migrations (localhost only).
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

./scripts/require-docker.sh
source ./scripts/assert-local-database-url.sh

echo "resetting local Postgres (docker compose down -v)..."
docker compose down -v

echo "starting Postgres..."
docker compose up -d --wait

./scripts/apply-migrations.sh
./scripts/verify-local-db.sh

echo "local Postgres reset complete."
echo "  DATABASE_URL=${DATABASE_URL}"

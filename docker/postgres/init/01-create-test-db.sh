#!/bin/bash
set -euo pipefail
test_db="${POSTGRES_DB}_test"
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-EOSQL
SELECT 'CREATE DATABASE ' || quote_ident('${test_db}') || ' OWNER ' || quote_ident('${POSTGRES_USER}')
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = '${test_db}')\gexec
EOSQL

#!/usr/bin/env bash
# Print `{dbname}_test` DSN derived from DATABASE_URL. Do not set a second env var.
set -euo pipefail

database_url="${DATABASE_URL:-}"
if [[ -z "$database_url" ]]; then
  echo "error: DATABASE_URL is not set" >&2
  exit 1
fi

base="${database_url%%\?*}"
query=""
if [[ "$database_url" == *"?"* ]]; then
  query="?${database_url#*\?}"
fi

dbname="${base##*/}"
if [[ -z "$dbname" ]]; then
  echo "error: DATABASE_URL has no database name" >&2
  exit 1
fi
if [[ "$dbname" != *_test ]]; then
  dbname="${dbname}_test"
fi
prefix="${base%/*}"
printf '%s\n' "${prefix}/${dbname}${query}"

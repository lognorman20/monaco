#!/usr/bin/env bash
# Stop Monaco API (port listener + go run / bin/monaco-api).
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

api_port() {
  local port=8080
  if [[ -n "${API_ADDR:-}" ]]; then
    port="${API_ADDR##*:}"
  elif command -v dotenvx >/dev/null 2>&1 && [[ -f .env.local ]]; then
    local addr
    addr="$(dotenvx get API_ADDR -f .env.local 2>/dev/null || true)"
    if [[ -n "$addr" ]]; then
      port="${addr##*:}"
    fi
  fi
  echo "$port"
}

port="$(api_port)"
"$root/scripts/kill-listeners.sh" "$port"
"$root/scripts/kill-listeners.sh" 8081
if pkill -f '[n]pm run dev' 2>/dev/null; then
  echo "stopped signer npm run dev"
fi

if pkill -f '[g]o run ./cmd/api' 2>/dev/null; then
  echo "stopped go run ./cmd/api"
fi
if pkill -f '[b]in/monaco-api' 2>/dev/null; then
  echo "stopped bin/monaco-api"
fi

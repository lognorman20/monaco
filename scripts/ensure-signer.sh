#!/usr/bin/env bash
# Start apps/signer on 8081 unless something is already listening.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
port="${SIGNER_PORT:-8081}"
code="$(curl -s -o /dev/null -w '%{http_code}' --connect-timeout 1 "http://127.0.0.1:${port}/healthz" || true)"
if [[ "$code" == "200" || "$code" == "401" ]]; then
  echo "signer already listening on 127.0.0.1:${port}"
  exit 0
fi
cd "$root/apps/signer"
if [[ ! -d node_modules ]]; then
  npm ci
fi
npm run dev

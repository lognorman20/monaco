#!/usr/bin/env bash
# Run a command with repo-root .env.local decrypted via dotenvx.
# Usage: scripts/with-dotenv-local.sh <command> [args...]
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

if [[ ! -f .env.local ]]; then
  echo "error: .env.local missing — copy .env.example to .env.local and set values (dotenvx set … -f .env.local)." >&2
  exit 1
fi

if ! command -v dotenvx >/dev/null 2>&1; then
  echo "error: dotenvx not on PATH. Install: https://dotenvx.com/docs/install" >&2
  exit 1
fi

if [[ $# -lt 1 ]]; then
  echo "usage: scripts/with-dotenv-local.sh <command> [args...]" >&2
  exit 1
fi

exec dotenvx run -f .env.local -- "$@"

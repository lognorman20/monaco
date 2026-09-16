#!/usr/bin/env bash
# Decrypt .env.local via dotenvx, generate Privy xcconfig, export SIMCTL_CHILD_* for sim launch.
# Usage: scripts/with-ios-privy-env.sh <command> [args...]
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

if [[ ! -f .env.local ]]; then
  echo "error: .env.local missing — copy .env.example and set Privy keys (dotenvx set … -f .env.local)." >&2
  exit 1
fi

if ! command -v dotenvx >/dev/null 2>&1; then
  echo "error: dotenvx not on PATH. Install: https://dotenvx.com/docs/install" >&2
  exit 1
fi

if [[ $# -lt 1 ]]; then
  echo "usage: scripts/with-ios-privy-env.sh <command> [args...]" >&2
  exit 1
fi

bash -c '
  set -euo pipefail
  # shellcheck disable=SC1091
  source ./scripts/ensure-ios-privy-config.sh
  generate_xcconfig
  export_launch_env
  exec "$@"
' bash "$@"

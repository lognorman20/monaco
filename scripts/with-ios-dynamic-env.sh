#!/usr/bin/env bash
# Load Dynamic from .env.local, write xcconfig, export SIMCTL_CHILD_*, then exec command.
# Usage: scripts/with-ios-dynamic-env.sh <command> [args...]
# Optional: WITH_IOS_DYNAMIC_CWD=<dir> — cd there before exec.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

if [[ $# -lt 1 ]]; then
  echo "usage: scripts/with-ios-dynamic-env.sh <command> [args...]" >&2
  exit 1
fi

if [[ "${MONACO_DYNAMIC_ENV:-}" != "1" ]]; then
  exec ./scripts/with-dotenv-local.sh env MONACO_DYNAMIC_ENV=1 "$0" "$@"
fi

# shellcheck disable=SC1091
source ./scripts/ensure-ios-dynamic-config.sh
generate_xcconfig
export_launch_env

if [[ -n "${WITH_IOS_DYNAMIC_CWD:-}" ]]; then
  cd "$WITH_IOS_DYNAMIC_CWD"
fi

export MONACO_DYNAMIC_ENV=1
exec "$@"

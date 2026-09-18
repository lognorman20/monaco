#!/usr/bin/env bash
# Load Privy from .env.local, write xcconfig, export SIMCTL_CHILD_*, then exec command.
# Usage: scripts/with-ios-privy-env.sh <command> [args...]
# Optional: WITH_IOS_PRIVY_CWD=<dir> — cd there before exec.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

if [[ $# -lt 1 ]]; then
  echo "usage: scripts/with-ios-privy-env.sh <command> [args...]" >&2
  exit 1
fi

# Always enter through dotenvx once so other .env.local keys are available to the
# launch command. Privy iOS creds themselves come from dotenvx get in ensure-ios-privy-config.
if [[ "${MONACO_PRIVY_ENV:-}" != "1" ]]; then
  exec ./scripts/with-dotenv-local.sh env MONACO_PRIVY_ENV=1 "$0" "$@"
fi

# shellcheck disable=SC1091
source ./scripts/ensure-ios-privy-config.sh
generate_xcconfig
export_launch_env

if [[ -n "${WITH_IOS_PRIVY_CWD:-}" ]]; then
  cd "$WITH_IOS_PRIVY_CWD"
fi

export MONACO_PRIVY_ENV=1
exec "$@"

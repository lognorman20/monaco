#!/usr/bin/env bash
# Export SIMCTL_CHILD_* Privy vars for simctl launch (after dotenvx decrypt).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck disable=SC1091
source "$ROOT/scripts/ensure-ios-privy-config.sh"
generate_xcconfig
export_launch_env

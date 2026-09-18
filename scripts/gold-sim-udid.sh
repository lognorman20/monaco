#!/usr/bin/env bash
# Print SIMSLIM_UDID. Agent QA and XcodeBuildMCP use this. Fail if unset or missing.
# Human just run/build/stop uses scripts/resolve-ios-sim.sh instead (stock fallback).
set -euo pipefail

if [[ -z "${SIMSLIM_UDID:-}" ]]; then
  echo "error: SIMSLIM_UDID is unset." >&2
  echo "Agent QA needs one gold simulator. Export SIMSLIM_UDID=<udid> (shell rc or plain .env)." >&2
  echo "just run / just build mobile / ./scripts/ios-sim use resolve-ios-sim.sh and fall back to a stock sim." >&2
  echo "See README: SimSlim (gold simulator)." >&2
  exit 1
fi

if ! xcrun simctl list devices available | grep -Fq "$SIMSLIM_UDID"; then
  echo "error: SIMSLIM_UDID=${SIMSLIM_UDID} is not an available simulator." >&2
  exit 1
fi

printf '%s\n' "$SIMSLIM_UDID"

#!/usr/bin/env bash
# Print this machine's gold slim simulator UDID.
# Apple assigns a new UUID on `simctl create` — never commit one.
set -euo pipefail

if [[ -n "${SIMSLIM_UDID:-}" ]]; then
  printf '%s\n' "$SIMSLIM_UDID"
  exit 0
fi

echo "error: SIMSLIM_UDID is unset." >&2
echo "Slim one iOS 18.5+ simulator, then export SIMSLIM_UDID=<udid> (shell rc or plain .env)." >&2
echo "See README: SimSlim (gold simulator)." >&2
exit 1

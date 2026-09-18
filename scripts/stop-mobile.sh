#!/usr/bin/env bash
# Stop Monaco on every available simulator that has the app. Never simctl erase.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

bundle_id="com.monaco.app"
udids="$(xcrun simctl list devices available | grep -Eo '[A-F0-9-]{36}' | sort -u || true)"

if [[ -z "$udids" ]]; then
  echo "no available iOS simulators"
else
  while IFS= read -r udid; do
    [[ -n "$udid" ]] || continue
    if xcrun simctl terminate "$udid" "$bundle_id" 2>/dev/null; then
      echo "terminated ${bundle_id} on ${udid}"
    fi
    if xcrun simctl uninstall "$udid" "$bundle_id" 2>/dev/null; then
      echo "uninstalled ${bundle_id} on ${udid} (cleared app container + Privy session)"
    fi
  done <<<"$udids"
fi

if pkill -f '[x]codebuild.*Monaco\.xcodeproj' 2>/dev/null; then
  echo "stopped xcodebuild for Monaco"
fi

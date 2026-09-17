#!/usr/bin/env bash
# Stop Monaco iOS app on gold sim (terminate only — no erase, no new sim).
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

gold_sim="7B30D45E-62FD-42E2-871A-787B19D38CCF"
bundle_id="com.monaco.app"

if xcrun simctl terminate "$gold_sim" "$bundle_id" 2>/dev/null; then
  echo "terminated ${bundle_id} on gold sim"
else
  echo "Monaco not running on gold sim (or sim unavailable)"
fi

if pkill -f '[x]codebuild.*Monaco\.xcodeproj' 2>/dev/null; then
  echo "stopped xcodebuild for Monaco"
fi

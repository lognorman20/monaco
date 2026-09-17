#!/usr/bin/env bash
# Stop Monaco iOS on gold sim: terminate, uninstall app (clears Privy keychain/session), kill xcodebuild.
# Never simctl erase — gold slim sim ($SIMSLIM_UDID) only.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

gold_sim="$(./scripts/gold-sim-udid.sh)"
bundle_id="com.monaco.app"

if xcrun simctl terminate "$gold_sim" "$bundle_id" 2>/dev/null; then
  echo "terminated ${bundle_id} on gold sim"
else
  echo "Monaco not running on gold sim (or sim unavailable)"
fi

if xcrun simctl uninstall "$gold_sim" "$bundle_id" 2>/dev/null; then
  echo "uninstalled ${bundle_id} on gold sim (cleared app container + Privy session)"
else
  echo "Monaco not installed on gold sim (or sim unavailable)"
fi

if pkill -f '[x]codebuild.*Monaco\.xcodeproj' 2>/dev/null; then
  echo "stopped xcodebuild for Monaco"
fi

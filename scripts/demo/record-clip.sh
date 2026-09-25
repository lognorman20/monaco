#!/usr/bin/env bash
# record-clip.sh <clip-name> [seconds]
#
# Records one demo clip from the simulator: launches the app fresh against the demo backend,
# waits for the given number of seconds while the flow is driven (by hand, or by an agent
# through the accessibility tree), and writes docs/demo/clips/<clip-name>.mov.
#
# Requires: a booted simulator in MONACO_SIM_UDID, the Debug app built into MONACO_APP
# (default: the path scripts/ios-build writes), and a backend on MONACO_DEMO_API
# (default http://127.0.0.1:8082) started with DEMO_MODE=1.
#
#   DEMO_MODE=1 CHART_SOURCE=yahoo API_ADDR=127.0.0.1:8082 just run backend   # in one shell
#   scripts/demo/record-clip.sh start-cabal 14                                # in another
set -euo pipefail

name="${1:?clip name}"
seconds="${2:-12}"
udid="${MONACO_SIM_UDID:?set MONACO_SIM_UDID to the recording simulator}"
api="${MONACO_DEMO_API:-http://127.0.0.1:8082}"
root="$(cd "$(dirname "$0")/../.." && pwd)"
out="$root/docs/demo/clips"
mkdir -p "$out"

xcrun simctl boot "$udid" >/dev/null 2>&1 || true
xcrun simctl bootstatus "$udid" -b >/dev/null 2>&1
# The film's status bar: 9:41, full bars, full battery.
xcrun simctl status_bar "$udid" override --time "9:41" --batteryState charged --batteryLevel 100 --wifiBars 3 --cellularBars 4 >/dev/null 2>&1 || true
xcrun simctl ui "$udid" appearance light >/dev/null 2>&1 || true

if [[ -n "${MONACO_APP:-}" ]]; then
  xcrun simctl install "$udid" "$MONACO_APP" </dev/null
fi

# Fresh launch against the demo backend, Privy creds the way scripts/ios-sim passes them.
export MONACO_API_BASE_URL="$api"
export MONACO_SIM_UDID="$udid"
xcrun simctl terminate "$udid" com.monaco.app >/dev/null 2>&1 || true
(
  cd "$root"
  # shellcheck source=/dev/null
  source scripts/ensure-ios-privy-config.sh
  generate_xcconfig
  export_launch_env
  xcrun simctl launch "$udid" com.monaco.app >/dev/null
)

file="$out/$name.mov"
rm -f "$file"
echo "recording $name for ${seconds}s → $file"
xcrun simctl io "$udid" recordVideo --codec h264 --force "$file" &
rec=$!
sleep "$seconds"
kill -INT "$rec"
wait "$rec" 2>/dev/null || true
echo "done: $(du -h "$file" | cut -f1)"

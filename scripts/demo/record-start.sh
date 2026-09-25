#!/usr/bin/env bash
# record-start.sh <clip-name>  — starts recording the simulator into docs/demo/clips/<name>.mov
# and leaves it running; record-stop.sh ends it. For driving a take by hand or by an agent.
set -euo pipefail
name="${1:?clip name}"
udid="${MONACO_SIM_UDID:?set MONACO_SIM_UDID}"
root="$(cd "$(dirname "$0")/../.." && pwd)"
out="$root/docs/demo/clips"; mkdir -p "$out"
xcrun simctl status_bar "$udid" override --time "9:41" --batteryState charged --batteryLevel 100 --wifiBars 3 --cellularBars 4 >/dev/null 2>&1 || true
rm -f "$out/$name.mov"
nohup xcrun simctl io "$udid" recordVideo --codec h264 --force "$out/$name.mov" > "$out/.record-$name.log" 2>&1 &
echo $! > "$out/.record.pid"
echo "$name" > "$out/.record.name"
sleep 1
echo "recording $name (pid $(cat "$out/.record.pid"))"

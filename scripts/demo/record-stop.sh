#!/usr/bin/env bash
# record-stop.sh — ends the take record-start.sh began and prints the clip's length.
set -euo pipefail
root="$(cd "$(dirname "$0")/../.." && pwd)"
out="$root/docs/demo/clips"
pid="$(cat "$out/.record.pid")"; name="$(cat "$out/.record.name")"
kill -INT "$pid" 2>/dev/null || true
for _ in $(seq 1 30); do kill -0 "$pid" 2>/dev/null || break; sleep 0.5; done
rm -f "$out/.record.pid" "$out/.record.name"
printf '%s: %ss\n' "$name" "$(ffprobe -v error -show_entries format=duration -of csv=p=0 "$out/$name.mov" 2>/dev/null || echo '?')"

#!/usr/bin/env bash
# Run one command while holding the machine-wide Xcode build lock.
#
# Several xcodebuild / swift-build processes at once exhaust memory on a 16 GB Mac
# (each takes several GB) and the simulators start crashing. Every heavy build or
# simulator test run should go through this wrapper so only one runs at a time:
#
#   scripts/qa/xcode-lock.sh xcodebuild -project ... test
#
# The lock is a directory (mkdir is atomic). A lock whose owner pid is gone is stale
# and is taken over. MONACO_XCODE_LOCK_TIMEOUT (seconds, default 5400) bounds the wait.
set -euo pipefail

lock_dir="${MONACO_XCODE_LOCK_DIR:-/private/tmp/monaco-xcodebuild.lock}"
timeout="${MONACO_XCODE_LOCK_TIMEOUT:-5400}"

if [[ $# -eq 0 ]]; then
  echo "usage: $0 <command> [args...]" >&2
  exit 2
fi

release() {
  if [[ -f "$lock_dir/pid" && "$(cat "$lock_dir/pid" 2>/dev/null)" == "$$" ]]; then
    rm -rf "$lock_dir"
  fi
}

waited=0
until mkdir "$lock_dir" 2>/dev/null; do
  owner="$(cat "$lock_dir/pid" 2>/dev/null || true)"
  if [[ -n "$owner" ]] && ! kill -0 "$owner" 2>/dev/null; then
    echo "xcode-lock: taking over stale lock from pid $owner" >&2
    rm -rf "$lock_dir"
    continue
  fi
  if (( waited >= timeout )); then
    echo "xcode-lock: gave up after ${timeout}s waiting for pid ${owner:-unknown}" >&2
    exit 75
  fi
  if (( waited % 60 == 0 )); then
    echo "xcode-lock: waiting for pid ${owner:-unknown} (${waited}s)" >&2
  fi
  sleep 5
  waited=$((waited + 5))
done

echo "$$" > "$lock_dir/pid"
trap release EXIT INT TERM

"$@"

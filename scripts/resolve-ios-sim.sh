#!/usr/bin/env bash
# Print one iOS Simulator UDID on stdout.
# Prefer SIMSLIM_UDID when that device exists. If SimSlim / PATH ios-sim is missing
# or not ready, warn on stderr and still use that UDID as a stock sim, or pick another.
# Never commits a UDID. Never simctl erase.
set -euo pipefail

warn() {
  echo "warning: $*" >&2
}

simctl() {
  xcrun simctl "$@"
}

device_exists() {
  local udid="$1"
  simctl list devices available | grep -Fq "$udid"
}

slim_ready() {
  local udid="$1"
  if ! command -v simslim >/dev/null 2>&1; then
    return 1
  fi
  local profile="${SIMSLIM_PROFILE:-}"
  if [[ -z "$profile" && -f "${HOME}/.config/simslim/base-slim.json" ]]; then
    profile="${HOME}/.config/simslim/base-slim.json"
  fi
  if [[ -n "$profile" ]]; then
    simslim verify "$udid" --profile "$profile" >/dev/null 2>&1
  else
    simslim verify "$udid" >/dev/null 2>&1
  fi
}

pick_available_udid() {
  if command -v python3 >/dev/null 2>&1; then
    simctl list devices available -j | python3 -c '
import json, sys
data = json.load(sys.stdin)
booted = None
any_iphone = None
any_ios = None
for runtime, devices in data.get("devices", {}).items():
    if "iOS" not in runtime and "SimRuntime.iOS" not in runtime:
        continue
    for dev in devices:
        if not dev.get("isAvailable", True):
            continue
        udid = dev.get("udid") or ""
        name = dev.get("name") or ""
        state = dev.get("state") or ""
        if not udid:
            continue
        if any_ios is None:
            any_ios = udid
        if "iPhone" in name and any_iphone is None:
            any_iphone = udid
        if state == "Booted" and "iPhone" in name and booted is None:
            booted = udid
        elif state == "Booted" and booted is None:
            booted = udid
print(booted or any_iphone or any_ios or "")
'
    return 0
  fi
  local line udid
  line="$(simctl list devices available | grep -E 'iPhone.*\(Booted\)' | head -n 1 || true)"
  if [[ -z "$line" ]]; then
    line="$(simctl list devices available | grep -E 'iPhone \(' | head -n 1 || true)"
  fi
  if [[ -z "$line" ]]; then
    line="$(simctl list devices available | grep -E '\([A-F0-9-]{36}\)' | head -n 1 || true)"
  fi
  udid="$(printf '%s\n' "$line" | grep -Eo '[A-F0-9-]{36}' | head -n 1 || true)"
  printf '%s\n' "$udid"
}

emit() {
  printf '%s\n' "$1"
}

if [[ -n "${SIMSLIM_UDID:-}" ]]; then
  if device_exists "$SIMSLIM_UDID"; then
    if slim_ready "$SIMSLIM_UDID"; then
      emit "$SIMSLIM_UDID"
      exit 0
    fi
    warn "SimSlim not installed or not ready for ${SIMSLIM_UDID}. using that simulator without slim."
    emit "$SIMSLIM_UDID"
    exit 0
  fi
  warn "SIMSLIM_UDID=${SIMSLIM_UDID} is not an available simulator. picking another."
fi

if ! command -v simslim >/dev/null 2>&1; then
  warn "SimSlim not installed. falling back to a stock Xcode simulator. optional setup is in README (SimSlim)."
elif [[ -z "${SIMSLIM_UDID:-}" ]]; then
  warn "SIMSLIM_UDID unset. falling back to a stock Xcode simulator."
fi

picked="$(pick_available_udid)"
if [[ -z "$picked" ]]; then
  echo "error: no iOS Simulator available. Open Xcode > Settings > Platforms, download an iOS 18+ runtime, then create an iPhone simulator." >&2
  exit 1
fi
emit "$picked"

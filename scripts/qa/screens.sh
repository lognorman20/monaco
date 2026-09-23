#!/usr/bin/env bash
# Screenshot every Debug sample-harness screen listed in scripts/qa/sample-screens.txt.
#
#   scripts/qa/screens.sh --check                       # manifest covers every harness?
#   scripts/qa/screens.sh <sim udid> <Monaco.app> <dir> # install, launch each screen, shoot
#
# The manifest is the only list of screens. --check reads the app sources and fails when a
# `-Monaco…Sample`/`…Gallery` launch flag, or a scenario of a Debug harness enum, has no
# manifest line, so a new harness cannot silently drop out of the gallery. Capture runs the
# check too and exits 1 when a screen failed to launch or shoot, or the check failed.
#
# MONACO_QA_SCREEN_SETTLE: seconds to wait after launch before the shot (default 4).
set -uo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
manifest="$root/scripts/qa/sample-screens.txt"
app_src="$root/apps/mobile/Monaco"
settle="${MONACO_QA_SCREEN_SETTLE:-4}"

entries() { # name<TAB>args, comments and blank lines dropped
  sed -E 's/#.*//' "$manifest" | awk 'NF { name=$1; $1=""; sub(/^ +/, ""); print name "\t" $0 }'
}

in_manifest() { # every argument appears, in order, on one manifest line
  local pattern="(^|[[:space:]])$1"; shift
  local arg
  for arg in "$@"; do pattern="${pattern}[[:space:]]+${arg}"; done
  entries | cut -f2 | grep -qE "$pattern([[:space:]]|\$)"
}

# Cases of `enum X: String, CaseIterable` in one file, as their launch values.
enum_cases() {
  awk '
    /enum [A-Za-z0-9_]+: String, CaseIterable/ { inside=1; depth=0 }
    inside {
      line=$0
      opens=gsub(/\{/, "{", line); closes=gsub(/\}/, "}", line)
      depth += opens - closes
      if ($1 == "case") {
        sub(/^[[:space:]]*case[[:space:]]+/, "")
        n=split($0, parts, ",")
        for (i=1; i<=n; i++) {
          c=parts[i]; gsub(/^[[:space:]]+|[[:space:]]+$/, "", c)
          if (c ~ /=/) { sub(/^[^"]*"/, "", c); sub(/".*$/, "", c) }
          if (c != "") print c
        }
      }
      if (depth <= 0 && closes > 0) inside=0
    }' "$1"
}

check() {
  local missing=0 flag file flags count scenario
  while read -r flag; do
    if ! in_manifest "$flag"; then
      echo "screens: $flag has no line in scripts/qa/sample-screens.txt" >&2
      missing=1
    fi
  done < <(grep -rhoE '"-Monaco[A-Za-z]*(Sample|Gallery)[A-Za-z]*"' "$app_src" --include='*.swift' | tr -d '"' | sort -u)

  while read -r file; do
    flags="$(grep -oE '"-Monaco[A-Za-z]*Sample[A-Za-z]*"' "$file" | tr -d '"' | sort -u)"
    count="$(printf '%s\n' "$flags" | grep -c .)"
    if [[ "$count" != 1 ]]; then
      echo "screens: $(basename "$file") has a scenario enum but $count sample flags; cannot pair them" >&2
      missing=1
      continue
    fi
    while read -r scenario; do
      if ! in_manifest "$flags" "$scenario"; then
        echo "screens: $flags $scenario has no line in scripts/qa/sample-screens.txt" >&2
        missing=1
      fi
    done < <(enum_cases "$file")
  done < <(grep -lE 'enum [A-Za-z0-9_]+: String, CaseIterable' "$app_src"/Features/Debug/*.swift)

  (( missing == 0 )) && echo "screens: manifest covers every sample harness ($(entries | wc -l | tr -d ' ') screens)"
  return "$missing"
}

capture() {
  local sim="$1" app="$2" dir="$3" bundle name args failed=0 shot=0 wait
  [[ -d "$app" ]] || { echo "screens: no app at $app" >&2; return 1; }
  bundle="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$app/Info.plist")" || return 1
  mkdir -p "$dir"
  xcrun simctl install "$sim" "$app" </dev/null || { echo "screens: install failed" >&2; return 1; }

  wait=$((settle * 3)) # the first launch after an install is the slow one
  while IFS=$'\t' read -r name args; do
    xcrun simctl terminate "$sim" "$bundle" </dev/null >/dev/null 2>&1
    # $args is split on purpose: it is a list of launch arguments with no spaces inside.
    # shellcheck disable=SC2086
    if ! xcrun simctl launch "$sim" "$bundle" $args </dev/null >/dev/null; then
      echo "screens: $name did not launch" >&2
      failed=$((failed + 1)); continue
    fi
    sleep "$wait"; wait="$settle"
    if xcrun simctl io "$sim" screenshot --type=png "$dir/$name.png" </dev/null >/dev/null 2>&1; then
      shot=$((shot + 1))
    else
      echo "screens: $name could not be shot" >&2
      failed=$((failed + 1))
    fi
  done < <(entries)
  xcrun simctl terminate "$sim" "$bundle" </dev/null >/dev/null 2>&1

  echo "screens: $shot shot, $failed failed, in $dir"
  check || failed=$((failed + 1))
  (( failed == 0 ))
}

case "${1:-}" in
  --check) check ;;
  -h|--help|"") sed -n '2,12p' "$0"; [[ -n "${1:-}" ]] ;;
  *)
    [[ $# -eq 3 ]] || { echo "usage: $0 --check | <sim udid> <Monaco.app> <out dir>" >&2; exit 2; }
    capture "$@"
    ;;
esac

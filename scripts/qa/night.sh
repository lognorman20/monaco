#!/usr/bin/env bash
# Overnight QA runner: run every automated check in a loop while nobody is watching,
# without exhausting the Mac, and leave a report to read in the morning.
#
#   scripts/qa/night.sh                      # one round
#   scripts/qa/night.sh --rounds 6           # six rounds back to back
#   scripts/qa/night.sh --until 07:30        # keep going until 07:30 local time
#   scripts/qa/night.sh --skip-backend --only-ui CabalsTabSampleUITests
#
# What a round does, strictly one heavy job at a time:
#   1. backend: go vet + go test -p 1 ./...      (needs Postgres; see MONACO_QA_DATABASE_URL)
#   2. domain + mobile-core host tests
#   3. app unit tests, then each UI test class on its own, on one slimmed simulator,
#      with a watchdog timeout, one retry when the test runner itself is killed,
#      and a screen recording per class
#
# Safety: holds scripts/qa/xcode-lock.sh around every Xcode job, slims the simulator
# before use, shuts it down at the end, keeps the Mac awake with caffeinate, and stops
# starting new UI classes when free swap runs low.
#
# Output: .logs/qa/<UTC timestamp>/{report.md,*.log,clips/*.mp4}. Exit 1 if anything failed.
set -uo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$root"

rounds=1
until_time=""
skip_backend=0
skip_ui=0
only_ui=""
sim="${MONACO_QA_SIM:-}"
class_timeout="${MONACO_QA_CLASS_TIMEOUT:-1500}"
min_free_swap_mb="${MONACO_QA_MIN_FREE_SWAP_MB:-256}"
boot_timeout="${MONACO_QA_BOOT_TIMEOUT:-180}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --rounds) rounds="$2"; shift 2 ;;
    --until) until_time="$2"; rounds=9999; shift 2 ;;
    --skip-backend) skip_backend=1; shift ;;
    --skip-ui) skip_ui=1; shift ;;
    --only-ui) only_ui="$2"; shift 2 ;;
    --sim) sim="$2"; shift 2 ;;
    -h|--help) sed -n '2,22p' "$0"; exit 0 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

if [[ -z "${MONACO_QA_KEEP_AWAKE:-}" ]] && command -v caffeinate >/dev/null 2>&1; then
  export MONACO_QA_KEEP_AWAKE=1
  exec caffeinate -i "$0" ${rounds:+--rounds "$rounds"} ${until_time:+--until "$until_time"} \
    $([[ $skip_backend == 1 ]] && echo --skip-backend) $([[ $skip_ui == 1 ]] && echo --skip-ui) \
    ${only_ui:+--only-ui "$only_ui"} ${sim:+--sim "$sim"}
fi

stamp="$(date -u +%Y%m%dT%H%M%SZ)"
out="$root/.logs/qa/$stamp"
mkdir -p "$out/clips"
report="$out/report.md"
results="$out/results.tsv"
: > "$results"
lock="$root/scripts/qa/xcode-lock.sh"
# .tools/ is untracked, so inside a git worktree it lives in the main checkout.
main_root="$(cd "$(git rev-parse --git-common-dir)/.." && pwd)"
simslim_bin="${SIMSLIM_BIN:-}"
for candidate in "$root/.tools/bin/simslim" "$main_root/.tools/bin/simslim" "$(command -v simslim || true)"; do
  [[ -z "$simslim_bin" && -x "$candidate" ]] && simslim_bin="$candidate"
done
derived="${MONACO_QA_DERIVED_DATA:-$HOME/Library/Caches/monaco-qa/DerivedData}"

log() { printf '%s %s\n' "$(date -u +%H:%M:%SZ)" "$*" | tee -a "$out/night.log"; }

record() { # round step status seconds detail
  printf '%s\t%s\t%s\t%s\t%s\n' "$1" "$2" "$3" "$4" "$5" >> "$results"
  log "round $1 · $2 · $3 (${4}s) $5"
}

free_swap_mb() {
  sysctl -n vm.swapusage 2>/dev/null | sed -E 's/.*free = ([0-9.]+)M.*/\1/' | cut -d. -f1
}

# run_step <round> <name> <timeout seconds> <command...>
run_step() {
  local round="$1" name="$2" limit="$3"; shift 3
  local logf="$out/r${round}-${name}.log" start=$SECONDS status
  # stdin from /dev/null: xcodebuild would otherwise swallow the class list the caller loops over.
  ( "$@" ) > "$logf" 2>&1 </dev/null &
  local pid=$!
  # The watchdog must not inherit our stdout, or a caller piping this script waits on its sleep.
  ( sleep "$limit"; kill -TERM "$pid" 2>/dev/null; sleep 10; kill -KILL "$pid" 2>/dev/null ) >/dev/null 2>&1 </dev/null &
  local watchdog=$!
  if wait "$pid"; then status=pass; else status=fail; fi
  pkill -P "$watchdog" 2>/dev/null; kill "$watchdog" 2>/dev/null; wait "$watchdog" 2>/dev/null
  local took=$((SECONDS - start))
  (( took >= limit )) && status=timeout
  record "$round" "$name" "$status" "$took" "$(basename "$logf")"
  [[ "$status" == pass ]]
}

resolve_sim() {
  if [[ -z "$sim" ]]; then
    sim="$(xcrun simctl list devices available | sed -nE 's/^ +Monaco Night QA \(([0-9A-F-]{36})\).*/\1/p' | head -1)"
  fi
  if [[ -z "$sim" ]]; then
    sim="$("$root/scripts/resolve-ios-sim.sh" 2>/dev/null || true)"
  fi
  [[ -n "$sim" ]]
}

# `simctl bootstatus` can wait forever on a slimmed simulator whose disabled services never
# report in, so the wait is bounded: past the limit the run carries on and the first Xcode
# step decides whether the simulator is usable.
wait_for_boot() {
  xcrun simctl bootstatus "$sim" -b >/dev/null 2>&1 </dev/null &
  local pid=$! waited=0
  while kill -0 "$pid" 2>/dev/null; do
    if (( waited >= boot_timeout )); then
      kill "$pid" 2>/dev/null
      log "warning: $sim did not report booted within ${boot_timeout}s; continuing"
      break
    fi
    sleep 2; waited=$((waited + 2))
  done
  wait "$pid" 2>/dev/null || true
}

prepare_sim() {
  xcrun simctl boot "$sim" >/dev/null 2>&1 || true
  wait_for_boot
  if [[ -n "$simslim_bin" ]]; then
    "$simslim_bin" verify "$sim" --profile "$root/scripts/simslim-profile.json" >/dev/null 2>&1 \
      || "$simslim_bin" on "$sim" --no-reboot --profile "$root/scripts/simslim-profile.json" >> "$out/night.log" 2>&1 \
      || log "warning: could not slim $sim; continuing on a stock simulator"
  else
    log "warning: simslim not found; continuing on a stock simulator"
  fi
  xcrun simctl status_bar "$sim" override --time 9:41 --batteryState charged --batteryLevel 100 \
    --cellularBars 4 --wifiBars 3 >/dev/null 2>&1 || true
}

xcode_test() { # extra xcodebuild args...
  "$lock" xcodebuild -project apps/mobile/Monaco.xcodeproj -scheme Monaco -configuration Debug \
    -destination "platform=iOS Simulator,id=$sim" -derivedDataPath "$derived" \
    CODE_SIGNING_ALLOWED=NO "$@"
}

ui_classes() {
  if [[ -n "$only_ui" ]]; then tr ',' '\n' <<< "$only_ui"; return; fi
  # Sample-data classes only: they need no backend, no sign-in and move no money.
  grep -lE 'Sample|DesignGallery' apps/mobile/MonacoUITests/*.swift 2>/dev/null \
    | xargs -n1 basename | sed 's/\.swift$//' | grep -v '^DemoTour' | sort
}

run_ui_class() { # round class
  local round="$1" class="$2" clip="$out/clips/r${round}-${class}.mp4" rec attempt
  for attempt in 1 2; do
    xcrun simctl io "$sim" recordVideo --codec=h264 --force "$clip" >/dev/null 2>&1 &
    rec=$!
    sleep 2
    if run_step "$round" "ui-${class}$([[ $attempt == 2 ]] && echo -retry)" "$class_timeout" \
        xcode_test "-only-testing:MonacoUITests/$class" test-without-building; then
      kill -INT "$rec" 2>/dev/null; wait "$rec" 2>/dev/null
      return 0
    fi
    kill -INT "$rec" 2>/dev/null; wait "$rec" 2>/dev/null
    # Retry only when the runner died (memory pressure), not on a real assertion failure.
    grep -qE 'signal kill|Test crashed|failed to launch|Lost connection' \
      "$out"/r${round}-ui-${class}*.log 2>/dev/null || return 1
    log "runner crashed during $class; re-slimming and retrying once"
    prepare_sim
  done
  return 1
}

backend_tests() {
  local url="${MONACO_QA_DATABASE_URL:-}"
  if [[ -n "$url" ]]; then
    ( cd apps/backend && go vet ./... && DATABASE_URL="$url" go test -p 1 ./... )
  else
    just test backend
  fi
}

past_deadline() {
  [[ -n "$until_time" ]] || return 1
  local now target
  now="$(date +%H%M)"; target="${until_time/:/}"
  # The window may cross midnight: stop once the clock is inside the hour after the target.
  (( 10#$now >= 10#$target && 10#$now < 10#$target + 100 ))
}

# A step that failed but whose -retry passed in the same round is flaky, not failing.
hard_failures() {
  awk -F'\t' '
    { key=$1 FS $2; sub(/-retry$/, "", key); if ($3=="pass") ok[key]=1; rows[NR]=$0; keys[NR]=key; st[NR]=$3 }
    END { for (i=1;i<=NR;i++) if ((st[i]=="fail"||st[i]=="timeout") && !ok[keys[i]]) print rows[i] }' "$results"
}

log "output: $out"
round=0
while (( round < rounds )); do
  round=$((round + 1))
  past_deadline && { log "reached --until $until_time"; break; }
  log "=== round $round · free swap $(free_swap_mb) MB ==="

  if (( ! skip_backend )); then
    run_step "$round" backend 2400 backend_tests || true
    run_step "$round" domain 600 bash -c 'cd packages/domain && go test ./...' || true
  fi
  run_step "$round" mobile-core 1200 bash -c 'cd packages/mobile-core && swift test' || true

  if (( ! skip_ui )); then
    if ! resolve_sim; then
      record "$round" simulator fail 0 "no simulator found; create one named 'Monaco Night QA'"
    else
      prepare_sim
      if run_step "$round" app-build 2700 xcode_test build-for-testing; then
        run_step "$round" app-unit 900 xcode_test -only-testing:MonacoTests test-without-building || true
        while read -r class; do
          [[ -z "$class" ]] && continue
          swap="$(free_swap_mb)"
          if [[ -n "$swap" ]] && (( swap < min_free_swap_mb )); then
            record "$round" "ui-$class" skipped 0 "free swap ${swap} MB < ${min_free_swap_mb} MB"
            continue
          fi
          run_ui_class "$round" "$class" || true
        done < <(ui_classes)
      fi
      xcrun simctl shutdown "$sim" >/dev/null 2>&1 || true
    fi
  fi
done

{
  echo "# Monaco overnight QA — $stamp (UTC)"
  echo
  echo "Commit \`$(git rev-parse --short HEAD)\` on \`$(git rev-parse --abbrev-ref HEAD)\` · simulator \`${sim:-none}\`"
  echo
  echo "| Round | Step | Result | Seconds | Log |"
  echo "| --- | --- | --- | --- | --- |"
  awk -F'\t' '{printf "| %s | %s | %s | %s | %s |\n",$1,$2,$3,$4,$5}' "$results"
  echo
  failed="$(hard_failures | wc -l | tr -d ' ')"
  echo "**${failed} failing step(s).** Flaky = a step that failed and then passed on \`-retry\`."
  echo
  echo "First failing lines:"
  echo '```'
  for f in $(hard_failures | awk -F'\t' '{print $5}'); do
    echo "--- $f"
    grep -E 'error:|FAIL|panic:|✘|failed \(|Test crashed' "$out/$f" | head -8
  done
  echo '```'
} > "$report"

log "report: $report"
[[ -z "$(hard_failures)" ]]

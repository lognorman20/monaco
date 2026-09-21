#!/usr/bin/env bash
# Turn a nightly QA result into one GitHub issue labelled nightly-failure.
#
#   QA_RESULT=failure|success  RUN_URL=...  scripts/qa/nightly-alert.sh [<qa output dir>]
#
# failure: comment on the open nightly-failure issue, or open one when there is none.
# success: comment that main recovered and close the open issue, if any.
# The message links the run, names the failing night.sh steps (failures.tsv) and quotes the
# first failing lines from report.md. DRY_RUN=1 prints what it would do and writes nothing.
# Needs gh with GH_TOKEN (issues: write) and GITHUB_REPOSITORY.
set -euo pipefail

result="${QA_RESULT:?QA_RESULT must be success or failure}"
run_url="${RUN_URL:?RUN_URL must be set}"
repo="${GITHUB_REPOSITORY:?GITHUB_REPOSITORY must be set}"
sha="${GITHUB_SHA:-unknown}"
dir="${1:-}"
label="nightly-failure"
dry_run="${DRY_RUN:-0}"

gh_write() { # gh command that changes the repo; printed instead under DRY_RUN
  if [[ "$dry_run" == 1 ]]; then
    printf 'DRY_RUN: gh'; printf ' %s' "$@"; printf '\n'
  else
    gh "$@"
  fi
}

open_issue="$(gh issue list --repo "$repo" --label "$label" --state open \
  --json number --jq 'sort_by(.number) | .[0].number // empty')"

failure_body() {
  local failures="$dir/failures.tsv" report="$dir/report.md"
  echo "Nightly QA failed on \`${sha:0:8}\`: $run_url"
  echo
  if [[ -n "$dir" && -f "$failures" ]]; then
    if [[ -s "$failures" ]]; then
      echo "Failing steps:"
      echo
      awk -F'\t' '{ printf "- `%s` %s (round %s, %ss, log `%s`)\n", $2, $3, $1, $4, $5 }' "$failures"
    else
      echo "night.sh reported no failing step; a workflow step after it failed. See the run."
    fi
    echo
    echo "First failing lines from report.md:"
    echo
    # The fenced block that follows "First failing lines:", capped so the issue stays readable.
    awk '/^First failing lines:/ { on=1; next }
      !on { next }
      /^```$/ { print; if (++fences == 2) exit; next }
      ++lines <= 60 { print; next }
      lines == 61 { print "... (truncated; full logs are in the run artifacts)" }' "$report"
  else
    echo "The run stopped before night.sh wrote a report (a setup step failed). See the run log."
  fi
  echo
  echo "The report, logs, screen recordings and screenshots are artifacts on the run."
  echo "This issue gets a comment for every failing night and closes after the next passing one."
}

case "$result" in
  failure)
    body="$(failure_body)"
    if [[ -n "$open_issue" ]]; then
      echo "commenting on #$open_issue"
      gh_write issue comment "$open_issue" --repo "$repo" --body "$body"
    else
      echo "opening a new $label issue"
      gh_write label create "$label" --repo "$repo" --color B60205 \
        --description "Nightly QA run failed" --force
      gh_write issue create --repo "$repo" --label "$label" \
        --title "Nightly QA is failing on main" --body "$body"
    fi
    ;;
  success)
    if [[ -n "$open_issue" ]]; then
      echo "closing #$open_issue"
      gh_write issue close "$open_issue" --repo "$repo" \
        --comment "Recovered: nightly QA passed on \`${sha:0:8}\`: $run_url"
    else
      echo "passed; no open $label issue"
    fi
    ;;
  *)
    echo "QA_RESULT must be success or failure, not '$result'" >&2
    exit 2
    ;;
esac

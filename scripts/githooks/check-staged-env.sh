#!/usr/bin/env bash
# Fast env-secret gate: inspect listed paths only (no tree walk).
# Usage: check-staged-env.sh [path ...]
#        git diff --cached --name-only --diff-filter=ACM | check-staged-env.sh
set -euo pipefail

is_env_name() {
  local base="$1"
  [[ "$base" == ".env" || "$base" == .env.* ]]
}

is_example_env() {
  local base="$1"
  [[ "$base" == ".env.example" || "$base" == .env.*.example ]]
}

check_one() {
  local file="$1"
  local base
  base="$(basename "$file")"

  if ! is_env_name "$base"; then
    return 0
  fi
  if [[ "$base" == ".env.keys" ]]; then
    echo "error: do not commit $file" >&2
    return 1
  fi
  if is_example_env "$base"; then
    return 0
  fi
  if [[ ! -f "$file" ]]; then
    return 0
  fi
  if ! grep -q 'DOTENV_PUBLIC_KEY' "$file"; then
    echo "error: $file is a plaintext .env file — encrypt with dotenvx or unstage" >&2
    return 1
  fi
  return 0
}

fail=0
if [[ $# -gt 0 ]]; then
  for file in "$@"; do
    check_one "$file" || fail=1
  done
else
  while IFS= read -r file || [[ -n "${file:-}" ]]; do
    [[ -z "$file" ]] && continue
    check_one "$file" || fail=1
  done
fi

exit "$fail"

#!/usr/bin/env bash
# Kill TCP listeners on one or more ports (Monaco dev servers only — not Postgres).
set -euo pipefail

if ((${#@} == 0)); then
  echo "usage: scripts/kill-listeners.sh <port> [port...]" >&2
  exit 1
fi

kill_listeners_on_port() {
  local port=$1
  local pids
  pids="$(lsof -nP -iTCP:"${port}" -sTCP:LISTEN -t 2>/dev/null || true)"
  if [[ -z "$pids" ]]; then
    echo "no listeners on port ${port}"
    return 0
  fi
  echo "killing listeners on port ${port}: ${pids//$'\n'/ }"
  # shellcheck disable=SC2086
  kill ${pids} 2>/dev/null || true
  sleep 0.2
  pids="$(lsof -nP -iTCP:"${port}" -sTCP:LISTEN -t 2>/dev/null || true)"
  if [[ -n "$pids" ]]; then
    # shellcheck disable=SC2086
    kill -9 ${pids} 2>/dev/null || true
  fi
}

for port in "$@"; do
  if [[ ! "$port" =~ ^[0-9]+$ ]]; then
    echo "error: invalid port '${port}'" >&2
    exit 1
  fi
  kill_listeners_on_port "$port"
done

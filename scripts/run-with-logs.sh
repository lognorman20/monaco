#!/usr/bin/env bash
# Stamp dir under .logs/ for just run* — source, do not exec alone.
# Sets MONACO_LOG_DIR (reused when already exported, e.g. full-stack mobile child).
set -euo pipefail

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]:-${0}}")" && pwd)"
_repo_root="$(cd "${_script_dir}/.." && pwd)"

monaco_log_stamp() {
  date +"%Y-%m-%dT%H-%M-%S"
}

monaco_init_logs() {
  if [[ -n "${MONACO_LOG_DIR:-}" && -d "${MONACO_LOG_DIR}" ]]; then
    return 0
  fi
  mkdir -p "${_repo_root}/.logs"
  MONACO_LOG_DIR="${_repo_root}/.logs/$(monaco_log_stamp)"
  mkdir -p "${MONACO_LOG_DIR}"
  export MONACO_LOG_DIR
  echo "Logs → ${MONACO_LOG_DIR}"
}

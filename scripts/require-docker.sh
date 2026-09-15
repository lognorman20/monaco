#!/usr/bin/env bash
set -euo pipefail

if ! command -v docker >/dev/null 2>&1; then
  echo "error: docker is not on PATH. Install Docker Desktop and retry."
  exit 1
fi

if ! docker info >/dev/null 2>&1; then
  echo "error: docker daemon is not running. Start Docker Desktop and retry."
  exit 1
fi

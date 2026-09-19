#!/usr/bin/env bash
# Sweep USDC from Monaco-controlled wallets to --destination.
# Usage: ./scripts/sweep-wallets.sh --destination <wallet> [--source <addr>] [--all] [--dry-run]
# --source: drain only listed wallet(s); repeat or comma-separate in one value.
# --all: Privy app wallets are source of truth (not just DB rows).
# --dry-run: print planned sweeps; send no txs.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

exec scripts/with-dotenv-local.sh go run -C apps/backend ./cmd/sweep-member-to-address "$@"

#!/usr/bin/env bash
# Interactive macOS setup for a clone of this repo.
# Asks before each install. Never installs without a yes.
# Usage:
#   ./scripts/install-dev.sh          # prompt for missing tools
#   ./scripts/install-dev.sh --check  # report only, exit 1 if required tools missing
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

check_only=0
if [[ "${1:-}" == "--check" ]]; then
  check_only=1
fi

missing_required=0
asked_any=0

say() { printf '%s\n' "$*"; }
err() { printf 'error: %s\n' "$*" >&2; }

ask_yes() {
  local prompt="$1"
  local reply
  if [[ "$check_only" -eq 1 ]]; then
    return 1
  fi
  asked_any=1
  if [[ ! -t 0 ]]; then
    err "stdin is not a TTY. rerun without --check in a terminal, or install the tool yourself."
    return 1
  fi
  read -r -p "${prompt} [y/N] " reply
  [[ "$reply" == "y" || "$reply" == "Y" || "$reply" == "yes" ]]
}

have() { command -v "$1" >/dev/null 2>&1; }

run_brew() {
  local pkg="$1"
  if ! have brew; then
    err "Homebrew is not on PATH. install from https://brew.sh then rerun."
    return 1
  fi
  brew install "$pkg"
}

# --- required ---

if ! xcode-select -p >/dev/null 2>&1; then
  missing_required=1
  say "Xcode Command Line Tools are missing."
  if ask_yes "Install Xcode Command Line Tools now?"; then
    xcode-select --install || true
    say "Finish the installer GUI, then rerun ./scripts/install-dev.sh"
  fi
fi

if ! have xcodebuild; then
  missing_required=1
  say "xcodebuild is missing. Install Xcode from the App Store, open it once, then rerun."
fi

if ! have docker; then
  missing_required=1
  say "Docker is missing (needed for local Postgres)."
  if ask_yes "Open Docker Desktop install page?"; then
    open "https://docs.docker.com/desktop/setup/install/mac-install/" || true
  fi
fi

if ! have go; then
  missing_required=1
  say "Go is missing (1.23+)."
  if ask_yes "Install Go with Homebrew (go)?"; then
    run_brew go || missing_required=1
  fi
fi

if ! have just; then
  missing_required=1
  say "just is missing (https://github.com/casey/just)."
  if ask_yes "Install just with Homebrew (just)?"; then
    run_brew just || missing_required=1
  fi
fi

if ! have dotenvx; then
  missing_required=1
  say "dotenvx is missing (decrypts .env.local)."
  if ask_yes "Install dotenvx with Homebrew (dotenvx/brew/dotenvx)?"; then
    if have brew; then
      brew tap dotenvx/brew
      brew trust dotenvx/brew || true
      brew install dotenvx || missing_required=1
    else
      err "Homebrew is not on PATH."
    fi
  fi
fi

# --- optional SimSlim ---
if ! have simslim; then
  say "SimSlim is optional. Stock Xcode Simulator works. just run mobile falls back if slim is missing."
  if ask_yes "Install SimSlim with Homebrew (mobai-app/tap/simslim)?"; then
    if have brew; then
      brew tap mobai-app/tap
      brew install simslim || true
    fi
  fi
fi

# --- optional agent Phantom wallet ---
if [[ "$check_only" -eq 0 ]]; then
  say "A Phantom MCP wallet is optional. Use it only if a coding agent must send mainnet USDC during QA."
  if ask_yes "Print Phantom MCP setup steps (no install)?"; then
    say ""
    say "1. In Cursor, install the phantom-connect plugin (marketplace), or add @phantom/mcp-server to mcp.json."
    say "2. Docs: https://docs.phantom.com/phantom-mcp-server/setup"
    say "3. Do not put PHANTOM_APP_ID in .env.local. Agent wallet is separate from Privy product wallets."
    say "4. Fund the agent address on Solana mainnet only when you need live deposit QA. Refund leftover USDC when done."
    say ""
  fi
fi

# --- git hook ---
if [[ -d .git && ! -f .git/hooks/pre-commit ]]; then
  if ask_yes "Install the pre-commit hook (blocks plaintext .env commits)?"; then
    chmod +x scripts/githooks/pre-commit scripts/githooks/check-staged-env.sh
    ln -sfn ../../scripts/githooks/pre-commit .git/hooks/pre-commit
    say "installed .git/hooks/pre-commit"
  fi
fi

# --- env files ---
if [[ ! -f .env.local ]]; then
  missing_required=1
  err ".env.local is missing."
  say "Put an encrypted .env.local next to .env.keys (from a teammate), or copy .env.example to .env.local and fill Privy + relayer values with dotenvx set."
  if [[ "$check_only" -eq 0 ]] && ask_yes "Copy .env.example to .env.local now (you still must set secrets)?"; then
    cp .env.example .env.local
    say "wrote .env.local from .env.example"
  fi
fi

if [[ ! -f .env.keys ]] && [[ -z "${DOTENV_PRIVATE_KEY:-}" ]]; then
  say "No .env.keys and DOTENV_PRIVATE_KEY is unset. dotenvx may still use macOS Keychain if you created the key on this Mac."
  say "If a teammate encrypted .env.local, place their .env.keys in the repo root (gitignored)."
fi

if have dotenvx && [[ -f .env.local ]]; then
  if ! dotenvx get PRIVY_APP_ID -f .env.local >/dev/null 2>&1; then
    missing_required=1
    err "dotenvx cannot read PRIVY_APP_ID from .env.local. check .env.keys and that the file is encrypted for that key."
  else
    client="$(dotenvx get PRIVY_APP_CLIENT_ID -f .env.local 2>/dev/null || true)"
    auth="$(dotenvx get PRIVY_AUTH_ID -f .env.local 2>/dev/null || true)"
    if [[ -z "$client" && -z "$auth" ]]; then
      missing_required=1
      err "PRIVY_APP_CLIENT_ID (or PRIVY_AUTH_ID) is empty. iOS will show Privy not configured."
    else
      say "Privy app id is readable from .env.local."
    fi
  fi
fi

if [[ -d .git/hooks ]] && [[ ! -f .git/hooks/pre-commit ]] && [[ "$check_only" -eq 1 ]]; then
  say "note: git pre-commit hook is not installed. run ./scripts/install-dev.sh to copy it."
fi

say ""
if [[ "$missing_required" -ne 0 ]]; then
  err "required setup is incomplete. install missing tools, then place .env.local and .env.keys, then rerun --check."
  exit 1
fi

if [[ "$check_only" -eq 1 ]]; then
  say "required tools and Privy env look ready. next: just run"
  exit 0
fi

if [[ "$asked_any" -eq 0 ]]; then
  say "required tools already on PATH."
fi
say "next: just run   (Postgres + API + iOS). just run mobile injects Privy even without SimSlim."
exit 0

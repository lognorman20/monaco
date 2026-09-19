#!/usr/bin/env bash
# Send one buy/sell intent to a Monaco cabal's trading agent endpoint, using the agent's
# minted API key. Built for demos and local dev — it never moves real money by itself; that
# depends entirely on which --api URL you point it at and which cabal's key you use.
#
# Usage:
#   MONACO_AGENT_KEY=k7m2p ./scripts/demo/agent-intent.sh buy  AAPLx 10  --group <group-id>
#   MONACO_AGENT_KEY=k7m2p ./scripts/demo/agent-intent.sh sell AAPLx 0.5 --group <group-id>
#
# Args:
#   buy|sell   Trade side.
#   SYMBOL     xStock symbol, e.g. AAPLx.
#   AMOUNT     For buy: USD, converted to usdcMicros (USD x 1e6).
#              For sell: shares, converted to tokenAmount atomics (shares x 1e8).
#
# Flags:
#   --group <id>   Cabal group id. Required (or set MONACO_GROUP_ID).
#   --api <url>    API base URL. Default: http://127.0.0.1:8080
#   --dry-run      Print the curl command (key redacted) instead of sending it.
#   -h, --help     Show this help.
#
# Env:
#   MONACO_AGENT_KEY   The 5-character key revealed once in the app after the cabal's
#                      add-agent vote passes. Required. Never printed by this script.
#   MONACO_GROUP_ID    Default --group value if the flag is omitted.
set -euo pipefail

usage() {
	sed -n '2,25p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
}

die() {
	echo "error: $*" >&2
	exit 1
}

api="http://127.0.0.1:8080"
group="${MONACO_GROUP_ID:-}"
dry_run=false
side=""
symbol=""
amount=""
positional=()

while [[ $# -gt 0 ]]; do
	case "$1" in
	--group)
		group="${2:-}"
		[[ -n "$group" ]] || die "--group needs a value"
		shift 2
		;;
	--api)
		api="${2:-}"
		[[ -n "$api" ]] || die "--api needs a value"
		shift 2
		;;
	--dry-run)
		dry_run=true
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	--*)
		die "unknown flag: $1"
		;;
	*)
		positional+=("$1")
		shift
		;;
	esac
done

if [[ ${#positional[@]} -ne 3 ]]; then
	usage
	die "expected 3 args: buy|sell SYMBOL AMOUNT (got ${#positional[@]})"
fi
side="${positional[0]}"
symbol="${positional[1]}"
amount="${positional[2]}"

case "$side" in
buy | sell) ;;
*) die "side must be 'buy' or 'sell', got '$side'" ;;
esac

[[ -n "$symbol" ]] || die "SYMBOL is required, e.g. AAPLx"

if ! [[ "$amount" =~ ^[0-9]+([.][0-9]+)?$ ]]; then
	die "AMOUNT must be a positive number, got '$amount'"
fi
if ! awk -v n="$amount" 'BEGIN { exit !(n > 0) }'; then
	die "AMOUNT must be greater than zero, got '$amount'"
fi

[[ -n "$group" ]] || die "missing cabal group id: pass --group <id> or set MONACO_GROUP_ID"

if [[ "$dry_run" != true ]]; then
	[[ -n "${MONACO_AGENT_KEY:-}" ]] || die "missing MONACO_AGENT_KEY env var (the key revealed once in the app after the add-agent vote passes)"
fi

if [[ "$side" == "buy" ]]; then
	# USD -> usdcMicros (USD x 1e6), rounded to the nearest integer micro-dollar.
	usdc_micros="$(awk -v n="$amount" 'BEGIN { printf "%d", (n * 1000000) + 0.5 }')"
	body="{\"side\":\"buy\",\"symbol\":\"${symbol}\",\"usdcMicros\":${usdc_micros}}"
else
	# shares -> tokenAmount atomics (shares x 1e8, xStock 8-decimal precision).
	token_amount="$(awk -v n="$amount" 'BEGIN { printf "%d", (n * 100000000) + 0.5 }')"
	body="{\"side\":\"sell\",\"symbol\":\"${symbol}\",\"tokenAmount\":${token_amount}}"
fi

url="${api%/}/v1/groups/${group}/agents/intents"

echo "POST ${url}"
echo "  X-Monaco-Agent-Key: <redacted>"
echo "  Content-Type: application/json"
echo "  body: ${body}"
echo

if [[ "$dry_run" == true ]]; then
	echo "# --dry-run: not sending. Equivalent curl:"
	echo "curl -sS -X POST \\"
	echo "  -H \"X-Monaco-Agent-Key: \$MONACO_AGENT_KEY\" \\"
	echo "  -H 'Content-Type: application/json' \\"
	echo "  \"${url}\" \\"
	echo "  -d '${body}'"
	exit 0
fi

response="$(curl -sS -w '\n%{http_code}' -X POST \
	-H "X-Monaco-Agent-Key: ${MONACO_AGENT_KEY}" \
	-H 'Content-Type: application/json' \
	"${url}" \
	-d "${body}")"

status="${response##*$'\n'}"
payload="${response%$'\n'*}"

echo "HTTP ${status}"
if command -v jq >/dev/null 2>&1; then
	echo "${payload}" | jq .
elif command -v python3 >/dev/null 2>&1; then
	echo "${payload}" | python3 -m json.tool
else
	echo "${payload}"
fi

case "$status" in
2??) exit 0 ;;
*) exit 1 ;;
esac

#!/usr/bin/env bash
# Send one buy or sell intent for a Monaco cabal's agent. The key alone names the agent
# and its cabal. Built for demos and local dev. Whether real money moves depends on which
# API you point it at and whose key you use.
#
# Usage:
#   MONACO_AGENT_KEY=monaco_ak_... ./scripts/demo/agent-intent.sh buy  AAPLx 1
#   MONACO_AGENT_KEY=monaco_ak_... ./scripts/demo/agent-intent.sh sell AAPLx 0.25
#
# Args:
#   buy|sell   Trade side.
#   SYMBOL     xStock symbol from GET /v1/agent/assets, e.g. AAPLx.
#   AMOUNT     Buy: USD to spend, sent as "usd" (up to 6 decimals).
#              Sell: shares to sell, sent as "shares" (up to 8 decimals).
#
# Flags:
#   --dry-run   Print the request (key redacted) instead of sending it.
#   -h, --help  Show this help.
#
# Env:
#   MONACO_AGENT_KEY  The agent key from Group > Agent in the app. Required. Never printed.
#   MONACO_API        API base URL. Default: http://127.0.0.1:8080
#   IDEMPOTENCY_KEY   Reuse a key to resend the same intent after a timeout. Default: a new uuid.
#   REASON            Why the agent is trading, up to 280 characters. Optional.
set -euo pipefail

usage() {
	sed -n '2,24p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
}

die() {
	echo "error: $*" >&2
	exit 1
}

json_string() {
	local s="$1"
	s="${s//\\/\\\\}"
	s="${s//\"/\\\"}"
	printf '"%s"' "$s"
}

api="${MONACO_API:-http://127.0.0.1:8080}"
dry_run=false
positional=()

while [[ $# -gt 0 ]]; do
	case "$1" in
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
buy) amount_field="usd" ;;
sell) amount_field="shares" ;;
*) die "side must be 'buy' or 'sell', got '$side'" ;;
esac

[[ "$symbol" =~ ^[A-Za-z0-9.]+$ ]] || die "SYMBOL must be an xStock symbol such as AAPLx, got '$symbol'"
[[ "$amount" =~ ^[0-9]+([.][0-9]+)?$ ]] || die "AMOUNT must be a positive decimal, got '$amount'"
[[ "$amount" =~ [1-9] ]] || die "AMOUNT must be greater than zero, got '$amount'"

if [[ "$dry_run" != true ]]; then
	[[ -n "${MONACO_AGENT_KEY:-}" ]] || die "MONACO_AGENT_KEY is not set (copy it from Group > Agent in the app)"
fi

idempotency_key="${IDEMPOTENCY_KEY:-$(uuidgen | tr '[:upper:]' '[:lower:]')}"

body="{\"side\":\"${side}\",\"symbol\":\"${symbol}\",\"${amount_field}\":\"${amount}\",\"idempotencyKey\":$(json_string "$idempotency_key")"
if [[ -n "${REASON:-}" ]]; then
	body+=",\"reason\":$(json_string "$REASON")"
fi
body+="}"

url="${api%/}/v1/agent/intents"

echo "POST ${url}"
echo "  X-Monaco-Agent-Key: <redacted>"
echo "  body: ${body}"
echo "  resend safely with: IDEMPOTENCY_KEY=${idempotency_key}"
echo

if [[ "$dry_run" == true ]]; then
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
else
	echo "${payload}"
fi

case "$status" in
2??) exit 0 ;;
*) exit 1 ;;
esac

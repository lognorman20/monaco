# Agent trading operator guide

Monaco runs trades for a cabal agent. Your bot POSTs intents; Monaco validates and executes through the same swap provider (Jupiter by default, Definitive Flash when `SWAP_PROVIDER=flash`) and Privy treasury path as member votes. Fills stay in the **cabal treasury**.

## Setup

1. Member proposes **add agent** with name + USDC allocation.
2. Cabal votes. On pass, Monaco mints a **5-character** API key (e.g. `k7m2p`).
3. Proposer sees the key in proposal detail (mobile) for 15 minutes after the vote passes. After that, any cabal member can read it on the bot's detail screen until the bot is removed; revoking the bot wipes the stored key. Copy or type into bot env. **Never log the key.**

Existing keys minted before this format are invalid — re-add the agent to get a new key.

## Base URL

Local dev: `http://127.0.0.1:8080`

Use your deployed API host in production.

## Auth

Every agent call:

```http
X-Monaco-Agent-Key: k7m2p
```

No member JWT. Missing/invalid/revoked key → **401**. A key for a different cabal than the URL also gets **401**, same as an unknown key. After 10 wrong keys → **429** with `Retry-After`.

## List assets

```bash
curl -sS \
  -H "X-Monaco-Agent-Key: $MONACO_AGENT_KEY" \
  "http://127.0.0.1:8080/v1/groups/$GROUP_ID/assets?limit=25"
```

Optional: `query`, `limit` (max 100), `offset`.

Use `symbol` / `name` from response. Do not call Jupiter, xStocks, or Solana from your bot.

## Post intent

### Buy (USDC → stock)

`usdcMicros` = USDC × 10⁶. `$10` → `10000000`.

```bash
curl -sS -X POST \
  -H "X-Monaco-Agent-Key: $MONACO_AGENT_KEY" \
  -H "Content-Type: application/json" \
  "http://127.0.0.1:8080/v1/groups/$GROUP_ID/agents/intents" \
  -d '{"side":"buy","symbol":"AAPLx","usdcMicros":10000000}'
```

### Sell (stock → USDC)

`tokenAmount` = xStock atomics (8 decimals). `0.5` share → `50000000`.

```bash
curl -sS -X POST \
  -H "X-Monaco-Agent-Key: $MONACO_AGENT_KEY" \
  -H "Content-Type: application/json" \
  "http://127.0.0.1:8080/v1/groups/$GROUP_ID/agents/intents" \
  -d '{"side":"sell","symbol":"AAPLx","tokenAmount":50000000}'
```

Success: `{ "intentId", "status": "executed", "transactionId" }`. Swap goes pending → confirmed in cabal activity.

## Errors

| HTTP | Meaning |
|------|---------|
| **401** | Bad or revoked key |
| **403** | Agent **paused** |
| **422** | Over allocation, bad symbol, insufficient treasury |
| **429** | Too many wrong keys for this cabal or from this address; wait `Retry-After` seconds |

**Never resend an intent.** Intents take no idempotency key, and the swap runs inside the request: after a timeout or `5xx` it may already have filled, and sending it again can trade twice. Check the cabal's holdings first.

## Pause / resume / revoke

Cabal votes. **Paused**: key still valid, intents **403**. **Revoked**: key **401**. Resume restores same key.

## Allocation

Add-agent vote sets USDC budget. Buys count confirmed + pending agent buys against cap.

## Security

Key in secret manager only. Revoke + new add-agent vote to rotate.

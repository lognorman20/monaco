# Agent trading operator guide

Monaco runs trades for a cabal agent. Your bot POSTs intents; Monaco validates and executes via the same Jupiter + Privy treasury path as member votes. Fills stay in the **cabal treasury**.

## Setup

1. Member proposes **add agent** with name + USDC allocation.
2. Cabal votes. On pass, Monaco mints a **5-character** API key (e.g. `k7m2p`).
3. Proposer sees key **once** in proposal detail (mobile). Copy or type into bot env. **Never log the key.**

Existing keys minted before this format are invalid — re-add the agent to get a new key.

## Base URL

Local dev: `http://127.0.0.1:8080`

Use your deployed API host in production.

## Auth

Every agent call:

```http
X-Monaco-Agent-Key: k7m2p
```

No member JWT. Missing/invalid/revoked key → **401**. Wrong cabal in URL → **403**.

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
| **403** | Wrong group, or agent **paused** |
| **422** | Over allocation, bad symbol, insufficient treasury |

## Pause / resume / revoke

Cabal votes. **Paused**: key still valid, intents **403**. **Revoked**: key **401**. Resume restores same key.

## Allocation

Add-agent vote sets USDC budget. Buys count confirmed + pending agent buys against cap.

## Security

Key in secret manager only. Revoke + new add-agent vote to rotate.

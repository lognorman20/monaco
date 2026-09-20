# Agent trading operator guide

Monaco runs trades for a cabal agent. Your bot POSTs intents; Monaco validates and executes through the same swap provider (Jupiter by default, Definitive Flash when `SWAP_PROVIDER=flash`) and Privy treasury path as member votes. Fills stay in the **cabal treasury**.

## Setup

1. Member proposes **add agent** with name + USDC allocation.
2. Cabal votes. On pass, Monaco mints an API key: `monaco_ak_` followed by 32 random characters (~158 bits from `crypto/rand`), e.g. `monaco_ak_k7m2p9x4…`.
3. Proposer sees the key in proposal detail (mobile) for 15 minutes after the vote passes. After that, any cabal member can read it on the bot's detail screen until the bot is removed; revoking the bot wipes the stored key. Copy or type into bot env. **Never log the key.**

Five-character keys minted before this format keep working. They are guessable in a way the long keys are not, so rotate: vote the bot out and back in to get a long key.

## Base URL

Local dev: `http://127.0.0.1:8080`

Use your deployed API host in production.

## Auth

Every agent call:

```http
X-Monaco-Agent-Key: monaco_ak_k7m2p9x4…
```

No member JWT. Missing/invalid/revoked key → **401**. A key for a different cabal than the URL also gets **401**, same as an unknown key.

Only wrong keys are counted, 10 per cabal and 10 per caller address, refilling one a minute:

- An **address** that has used its 10 gets **429** with `Retry-After` for every call, whatever key it sends. Your bot only lands here by sending wrong keys itself.
- Wrong keys aimed at your **cabal** from elsewhere never lock out a bot on a long key: a call carrying a `monaco_ak_…` key is always checked, and passes if it is right.
- A bot still on a five-character key is refused with **429** while its cabal's allowance is spent. That allowance is what keeps a short key from being guessed across many addresses. Rotate to a long key to be rid of it.

The caller address is the TCP peer, or the left-most `X-Forwarded-For` entry when the API runs with `TRUST_PROXY_HEADERS=true` (the same switch the request rate limiter uses).

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

### Idempotency key

Add `"idempotencyKey": "<up to 128 chars, unique per trade decision>"` to the body. It is optional, and without one every POST is a new trade.

An intent settles a swap before it answers, so a timeout tells you nothing about whether it traded. Resend the **same body with the same key** and Monaco answers with the first intent's outcome instead of trading again:

| First intent | Answer to the resend |
|------|---------|
| executed | **200**, same `intentId` and `transactionId` |
| rejected | **422**, same reason |
| still executing | **409**; ask again shortly |
| failed | **200** with `"status": "failed"`; check cabal activity before trading again |

The same key with a different side, symbol or amount is a **422**. Keys are scoped to the agent.

## Errors

| HTTP | Meaning |
|------|---------|
| **401** | Bad or revoked key |
| **403** | Agent **paused** |
| **409** | An intent with this `idempotencyKey` is still executing |
| **422** | Over allocation, unknown symbol, insufficient treasury, selling more than the agent bought, `idempotencyKey` reused for a different intent |
| **429** | Too many wrong keys from this address (or, for a five-character key, for this cabal); wait `Retry-After` seconds |

**Never resend an intent.** Intents take no idempotency key, and the swap runs inside the request: after a timeout or `5xx` it may already have filled, and sending it again can trade twice. Check the cabal's holdings first.

## Pause / resume / revoke

Cabal votes. **Paused**: key still valid, intents **403**. **Revoked**: key **401**. Resume restores same key.

## Allocation

Add-agent vote sets USDC budget. It is a lifetime spend cap: every buy that is in flight, pending or confirmed counts against it, and **sells do not give budget back**. A bot that has bought its whole allocation can still sell; to let it buy again, vote it out and back in with a new budget.

Intents of one agent are decided one at a time, and a buy reserves its amount before the swap is sent, so concurrent intents cannot overshoot the cap.

## What an agent may sell

Only what it bought itself: the tokens its own confirmed buys returned, less what it has already sold (or is selling). Positions the cabal bought by vote are never the bot's to sell, even though they sit in the same treasury; selling those takes a sell proposal. If a member-voted sell has taken part of what the bot bought, the bot is limited to what the treasury still holds.

## Security

Key in secret manager only. Revoke + new add-agent vote to rotate.

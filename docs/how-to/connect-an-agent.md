# Connect an agent to a cabal

A 10-minute demo of #202: a cabal votes in a trading bot with a USDC budget, the app hands
back a one-time API key, and the bot trades by POSTing intents. No real money moves in this
walkthrough — use `--dry-run` or point `--api` at a local/staging backend only.

## In the app

1. Open a cabal you're a member of, tap **Propose**.
2. Tap **Add a trading bot**.
3. Enter a bot name and a budget (this is the USDC allocation from the pot), then tap **Send to cabal**.
4. Once the cabal votes yes, open the passed proposal. The bot's key appears on the
   proposal, for the proposer only, for 15 minutes after the vote passes. Tap **Copy key**
   — after the window it is purged and cannot be shown again.
5. Store the key in your bot's secret manager as `MONACO_AGENT_KEY`. Never log it.
6. To manage the bot later, use the same **Propose** sheet: **Pause the trading bot**,
   **Turn the trading bot back on**, or **Remove the trading bot** — each is a cabal vote.

## One intent from the terminal

```bash
export MONACO_AGENT_KEY=<the key from step 4>

# See the request without sending it
scripts/demo/agent-intent.sh buy AAPLx 10 --group <group-id> --dry-run

# Send it for real (against a local/dev backend only)
scripts/demo/agent-intent.sh buy AAPLx 10 --group <group-id>
scripts/demo/agent-intent.sh sell AAPLx 0.5 --group <group-id>
```

`buy` amounts are USD; `sell` amounts are shares. The script converts both to the API's
integer units, prints the request with the key redacted, and pretty-prints the response.
Run `scripts/demo/agent-intent.sh --help` for all flags.

## Run the reference bot

`agents/momentum-bot` is a small Go program (standard library only) that trades a cabal's bot
budget with one rule: compare each price to where it was one lookback ago, buy what is up
past a threshold, sell what the bot bought once it is down past one. It is meant to be read
and forked.

```bash
export MONACO_API=http://127.0.0.1:8080
export MONACO_GROUP_ID=<group-id>
export MONACO_AGENT_KEY=<the key from step 4>   # env only; there is no flag for it

cd agents/momentum-bot

# Dry run is the default: real catalog, real prices, nothing sent
go run . --symbols GOOGLx,NVDAx

# One decision, then exit. Short lookback so a recording does not wait five minutes
go run . --symbols GOOGLx --once --interval 10s --lookback 1m --buy-pct 0.05

# Real intents. Asks y/N first; --yes skips the question
go run . --symbols GOOGLx --live --trade-usd 1 --max-spend-usd 5
```

What it prints:

```
14:07:10  GOOGLx  $ 352.10  +0.80% over 5m  buy signal
14:07:10  NVDAx   $ 222.02  +0.02% over 5m  hold
14:07:10  → buy $1.00 of GOOGLx
14:07:13  ✓ filled  tx 5b0c…  intent 91ab…  ($1.00 of $5.00 spent)
```

How it behaves:

- **Symbols come from the cabal.** It reads `GET /v1/groups/{id}/assets` with the agent key
  and only watches routable assets. Without `--symbols` it takes the first five.
- **Prices are public.** Chainlink marks on the backend; the agent reads cabal assets from the API, not a Solana mint list.
- **Two caps of its own**, `--trade-usd` per buy and `--max-spend-usd` per run. Set the total
  below the budget the cabal voted; the server enforces that budget either way. The total is
  per process: it resets on restart, the server's count does not.
- **One trade per tick, one per symbol per lookback.** Sells go before buys, then the biggest move wins.
- **Sells are sized from its own buys.** The API has no holdings endpoint for agents, so the
  bot estimates what each buy returned (less 2%) and sells that. It never sells what members bought.
- **`401` stops it.** Retrying a bad key only trips the wrong-key throttle. **`403`** (paused
  by vote) and **`429`** (honours `Retry-After`) make it stand down and keep watching.
  **`422`** prints the server's reason; "exceeds agent allocation" ends buying for the run.
- **An intent is never resent.** On a timeout or `5xx` the swap may have gone through, so the
  bot counts the buy against its cap and points you at the activity feed.
- **The key is never printed**, in the banner, the log, or an error.

Tests: `cd agents/momentum-bot && go test ./...` (strategy, caps, and every HTTP status above
against `httptest` servers).

## What judges see

- **A vote, not a form.** Adding a bot is a cabal proposal like any buy or sell — same
  quorum, same "the group decides" model, extended to an autonomous trader.
- **The key is shown to the proposer only**, in the app, for 15 minutes after the vote
  passes — then the plaintext is purged. Never emailed; the server keeps only a hash.
- **The budget is enforced server-side.** The bot can't spend past its allocation; a request
  that would exceed it comes back rejected, not silently capped.
- **Guessing the key is throttled.** After 10 wrong keys for a cabal (or from one address)
  the API answers `429` with `Retry-After` and allows one more try per minute. Calls with
  the right key are never throttled, and a key sent to the wrong cabal gets the same `401`
  as an unknown key.
- **The cabal keeps control after install.** Pause, resume, and revoke are each their own
  vote — pausing keeps the key valid but blocks trades, revoking kills the key outright.
- **Same execution path as a member's vote.** Agent trades settle through the identical
  Kyber + Dynamic treasury flow a human-approved buy uses — fills land in the same cabal
  activity feed.

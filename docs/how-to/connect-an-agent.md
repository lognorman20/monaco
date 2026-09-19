# Connect an agent to a cabal

A 10-minute demo of #202: a cabal votes in a trading bot with a USDC budget, the app hands
back a one-time API key, and the bot trades by POSTing intents. No real money moves in this
walkthrough — use `--dry-run` or point `--api` at a local/staging backend only.

## In the app

1. Open a cabal you're a member of, tap **Propose**.
2. Tap **Add a trading bot**.
3. Enter a bot name and a budget (this is the USDC allocation from the pot), then tap **Send to cabal**.
4. Once the cabal votes yes, open the passed proposal. The bot's key appears once under
   "Paste this key into your bot. We only show it once." Tap **Copy key** — it will not be
   shown again.
5. Store the key in your bot's secret manager as `MONACO_AGENT_KEY`. Never log it.
6. To manage the bot later, use the same **Propose** sheet: **Pause the trading bot**,
   **Turn the trading bot back on**, or **Remove the trading bot** — each is a cabal vote.

## From the terminal

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

## What judges see

- **A vote, not a form.** Adding a bot is a cabal proposal like any buy or sell — same
  quorum, same "the group decides" model, extended to an autonomous trader.
- **The key is shown exactly once**, in the app, right after the vote passes — never
  re-displayed, never emailed.
- **The budget is enforced server-side.** The bot can't spend past its allocation; a request
  that would exceed it comes back rejected, not silently capped.
- **The cabal keeps control after install.** Pause, resume, and revoke are each their own
  vote — pausing keeps the key valid but blocks trades, revoking kills the key outright.
- **Same execution path as a member's vote.** Agent trades settle through the identical
  Jupiter + Privy treasury flow a human-approved buy uses — fills land in the same cabal
  activity feed.

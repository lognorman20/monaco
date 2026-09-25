# Demo checklist

A manual end-to-end pass of the main product loop on the simulator. Run it before a demo, and after changes to sign-in, money flows, votes or boards.

Use the simulator from `./scripts/resolve-ios-sim.sh` (what `just run` picks). Agent QA on the gold sim uses `./scripts/gold-sim-udid.sh` and needs `SIMSLIM_UDID`. Never `simctl erase`.

## Before you start

```bash
just test mobile
just run            # Postgres + API + app
```

Sign in with a test account from the [README](../../README.md#privy-test-logins), for example `test-8081@privy.io` with code `465354`. Use a second account for step 3.

To demo without real USDC, start the API with `DEMO_MODE=1` (see [architecture.md](../architecture.md#run-modes)).

## Checklist

1. **Sign in.** SMS or email code. Home loads with its boards.
2. **Create a cabal.** Set the join mode, voter set, threshold and expiry.
3. **Join from a second account.** Both names show on the cabal's member board.
4. **Deposit and fund.** Send USDC to the deposit address shown in the app, wait for the account balance to show it, then fund the cabal. The fund reaches credited and the pot grows.
5. **Propose and vote.** Search `AAPL`, check the quote, propose a buy, and vote yes from a voter-set account. The proposal passes and the buy fills.
6. **Cabal screen.** Pot rows show units, price and value. Your stake shows gain or loss. The member board ranks by return.
7. **Home boards.** The cabal and both people show on the app-wide boards with percent and dollar returns.
8. **Cash out.** Cash out part of a stake, then the rest. The USDC lands in the account balance and every board updates.
9. **Copy check.** The main flow shows no wallet, gas, seed phrase, token address or "NAV" wording.

Done when all nine steps pass.

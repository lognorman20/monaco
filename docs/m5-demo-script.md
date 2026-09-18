# M5 demo script

Run on the simulator from `./scripts/resolve-ios-sim.sh` for a human demo. Agent gold QA uses `./scripts/gold-sim-udid.sh` and requires `SIMSLIM_UDID`. Never `simctl erase`.

## Prerequisites

```bash
dotenvx run -f .env.local -- just run backend
just test mobile
just build mobile
./scripts/ios-sim
```

Privy test account: `test-8081@privy.io` OTP `465354`.

## Checklist

1. **Launch + sign in** — SMS or email OTP → session gate loads home boards.
2. **Create cabal** — Home → + → Create cabal. Set join policy, voter set, threshold, expiry.
3. **Join second account** — Join cabal with cabal ID. Two names on member board.
4. **Add money** — Group detail → Add money. Confirm sweep status reaches credited.
5. **Propose buy** — Search `AAPL`, get quote, propose when routable. Vote yes as voter-set member.
6. **Cabal screen** — Pot rows show units, mark, value. You slice shows P&L. Member board ranks by server order.
7. **Home boards** — Cabals tab and People tab show percent and dollar P&L from backend JSON.
8. **Cash out** — Redeem slider at partial and max. Payout proof collected before submit. Boards refresh after success.
9. **Copy audit** — Main flow has no wallet, gas, seed phrase, mint, or NAV strings. Explorer links only under Settings → Advanced.

## Verification

```bash
just test mobile
curl -s http://127.0.0.1:8080/health
```

Done when all nine steps pass on the gold sim without mobile calls to Jupiter, xStocks, Pyth, or Solana RPC.

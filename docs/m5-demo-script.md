# M5 demo script — gold slim sim

Run on simulator UDID `7B30D45E-62FD-42E2-871A-787B19D38CCF` only. Never `simctl erase`.

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
2. **Create club** — Home → + → Create club. Set join policy, voter set, threshold, expiry.
3. **Join second account** — Join club with group ID (password if required). Two names on member board.
4. **Add money** — Group detail → Add money. Confirm sweep status reaches credited.
5. **Propose buy** — Search `AAPL`, get quote, propose when routable. Vote yes as voter-set member.
6. **Group screen** — Pot rows show units, mark, value. You slice shows P&L. Member board ranks by server order.
7. **Home boards** — Group tab and People tab show percent and dollar P&L from backend JSON.
8. **Cash out** — Redeem slider at partial and max. Payout proof collected before submit. Boards refresh after success.
9. **Copy audit** — Main flow has no wallet, gas, seed phrase, mint, or NAV strings. Explorer links only under Settings → Advanced.

## Verification

```bash
just test mobile
curl -s http://127.0.0.1:8080/health
```

Done when all nine steps pass on the gold sim without mobile calls to Jupiter, xStocks, Pyth, or Solana RPC.

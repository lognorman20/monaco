# Ops: sweep USDC out of Dynamic wallets

One-off helper. Not the product deposit poller. Sells **non-USDC SPL** (B20, etc.) to USDC on Kyber, then moves **mainnet USDC** from Monaco-controlled Dynamic Base wallets to `--destination`.

Product path stays: member inbox → treasury (poller) → **in-app redeem**. Use this when funds are stuck in Dynamic and redeem cannot reach them, or when cleaning QA wallets.

## Danger

- Same Dynamic app as `.env.local`. `--all` lists **every** Base wallet in that app, including treasuries that hold live pots.
- Live run can break share credits, pending deposits, and group NAV. Relayer pays ETH fees.
- Confirm `DATABASE_URL` (default local compose) and Dynamic app id before a live run.
- `--dry-run` does not send txs. Still talks to Dynamic + Base RPC for balances and Kyber for sell quotes.

## Run

From repo root. Loads `.env.local` via dotenvx.

```bash
./scripts/sweep-wallets.sh --destination <base_address> --source <wallet> --dry-run
./scripts/sweep-wallets.sh --destination <base_address> --source <wallet_a> --source <wallet_b>
./scripts/sweep-wallets.sh --destination <base_address> --all --dry-run
./scripts/sweep-wallets.sh --destination <base_address> --all
./scripts/sweep-wallets.sh --destination <base_address>
```

Equivalent:

```bash
scripts/with-dotenv-local.sh go run -C apps/backend ./cmd/sweep-member-to-address \
  --destination <base_address> --all --dry-run
```

Live (no `--dry-run`) prompts twice. Type exactly:

```
I UNDERSTAND THIS MAY MESS WITH PROD
```

Then paste `--destination` again. No `--yes`. Wrong phrase aborts.

## Flags

| Flag | Required | Meaning |
| --- | --- | --- |
| `--destination` | yes | Base58 Base address that receives USDC. |
| `--source` | no | Drain only this wallet. Repeatable; one value may be comma-separated. |
| `--all` | no | Dynamic is source of truth: paginated `GET /v1/wallets?chain_type=base` (no `user_id`). |
| `--dry-run` | no | Log planned Kyber sells + USDC sweeps. Skip sign/send. Skip confirm. |

Do not combine `--all` with `--source`.

Without `--source` or `--all`, sources are Postgres `member_wallets` and `treasuries` for `DATABASE_URL`.

With `--source`, only the listed wallets run through the per-wallet pipeline. Unknown Dynamic addresses fail that wallet and the run continues on the rest.

Per wallet:

1. Skip `RELAYER_PRIVATE_KEY` fee payer (never drain).
2. For each non-USDC SPL balance: Kyber sell → USDC (relayer pays ETH when treasury holds 0 SOL).
3. Sweep all USDC to `--destination`.

Skips: zero USDC after sells, source address equal to destination, relayer. Native ETH is not swept. Amounts are token atomics (USDC micro-units: `1_000_000` = $1).

## Env

`.env.local` must have:

- `PRIVY_APP_ID`, `PRIVY_APP_SECRET`
- `PRIVY_AUTHORIZATION_PRIVATE_KEY`, `PRIVY_AUTHORIZATION_KEY_ID` (server sign)
- `RELAYER_PRIVATE_KEY` (fee payer; needs SOL; never swept)
- `SOLANA_RPC_URL` (optional; recommended for `--all` scans; defaults to public mainnet RPC)
- `DATABASE_URL` (always loaded; classifies member/treasury when `--all` is on)

## After a sweep

Explorer: [solscan.io](https://solscan.io) on printed `tx=`. Group boards will not auto-credit this path. If you drained a treasury that still has share units, fix data or treat it as ops recovery — do not pretend it was a redeem.

# Ops: sweep USDC out of Privy wallets

One-off helper. Not the product deposit poller. Sells **non-USDC SPL** (xStocks, etc.) to USDC on Jupiter, then moves **mainnet USDC** from Monaco-controlled Privy Solana wallets to `--destination`.

The product path stays: fund (member wallet → treasury), then cash out and withdraw in the app. Use this script when funds are stuck in Privy and the app cannot reach them, or when cleaning QA wallets.

## Danger

- Same Privy app as `.env.local`. `--all` lists **every** Solana wallet in that app, including treasuries that hold live pots.
- Live run can break share credits, pending deposits, and group NAV. Relayer pays SOL fees.
- Confirm `DATABASE_URL` (default local compose) and Privy app id before a live run.
- `--dry-run` does not send txs. Still talks to Privy + Solana RPC for balances and Jupiter for sell quotes.

## Run

From repo root. Loads `.env.local` via dotenvx.

```bash
./scripts/sweep-wallets.sh --destination <solana_address> --source <wallet> --dry-run
./scripts/sweep-wallets.sh --destination <solana_address> --source <wallet_a> --source <wallet_b>
./scripts/sweep-wallets.sh --destination <solana_address> --all --dry-run
./scripts/sweep-wallets.sh --destination <solana_address> --all
./scripts/sweep-wallets.sh --destination <solana_address>
```

Equivalent:

```bash
scripts/with-dotenv-local.sh go run -C apps/backend ./cmd/sweep-member-to-address \
  --destination <solana_address> --all --dry-run
```

Live (no `--dry-run`) prompts twice. Type exactly:

```
I UNDERSTAND THIS MAY MESS WITH PROD
```

Then paste `--destination` again. No `--yes`. Wrong phrase aborts.

## Flags

| Flag | Required | Meaning |
| --- | --- | --- |
| `--destination` | yes | Base58 Solana address that receives USDC. |
| `--source` | no | Drain only this wallet. Repeatable; one value may be comma-separated. |
| `--all` | no | Privy is source of truth: paginated `GET /v1/wallets?chain_type=solana` (no `user_id`). |
| `--dry-run` | no | Log planned Jupiter sells + USDC sweeps. Skip sign/send. Skip confirm. |

Do not combine `--all` with `--source`.

Without `--source` or `--all`, sources are Postgres `member_wallets` and `treasuries` for `DATABASE_URL`.

With `--source`, only the listed wallets run through the per-wallet pipeline. Unknown Privy addresses fail that wallet and the run continues on the rest.

Per wallet:

1. Skip `RELAYER_PRIVATE_KEY` fee payer (never drain).
2. For each non-USDC SPL balance: resolve decimals from the catalog (else Jupiter Price, else 8), skip a balance worth under $0.50 (`skip dust`), then Jupiter sell → USDC. Pre-IPO sells use 100 bps slippage. The relayer pays SOL when the treasury holds 0 SOL.
3. Sweep all USDC to `--destination`.

Skips: zero USDC after sells, source address equal to destination, relayer. Native SOL is not swept. Amounts are token atomics (USDC micro-units: `1_000_000` = $1).

## Env

`.env.local` must have:

- `PRIVY_APP_ID`, `PRIVY_APP_SECRET`
- `PRIVY_AUTHORIZATION_PRIVATE_KEY`, `PRIVY_AUTHORIZATION_KEY_ID` (server sign)
- `RELAYER_PRIVATE_KEY` (fee payer; needs SOL; never swept)
- `SOLANA_RPC_URL` (optional; recommended for `--all` scans; defaults to public mainnet RPC)
- `DATABASE_URL` (always loaded; classifies member/treasury when `--all` is on)

## After a sweep

Explorer: [solscan.io](https://solscan.io) on printed `tx=`. Group boards will not auto-credit this path. If you drained a treasury that still has share units, fix data or treat it as ops recovery — do not pretend it was a redeem.

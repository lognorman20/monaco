# Ops: sweep USDC out of Privy wallets

One-off helper. Not the product deposit poller. Moves **mainnet USDC** from Monaco-controlled Privy Solana wallets to `--destination`.

Product path stays: member inbox → treasury (poller) → **in-app redeem**. Use this when funds are stuck in Privy and redeem cannot reach them, or when cleaning QA wallets.

## Danger

- Same Privy app as `.env.local`. `--all` lists **every** Solana wallet in that app, including treasuries that hold live pots.
- Live run can break share credits, pending deposits, and group NAV. Relayer pays SOL fees.
- Confirm `DATABASE_URL` (default local compose) and Privy app id before a live run.
- `--dry-run` does not send txs. Still talks to Privy + RPC for balances.

## Run

From repo root. Loads `.env.local` via dotenvx.

```bash
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
| `--all` | no | Privy is source of truth: paginated `GET /v1/wallets?chain_type=solana` (no `user_id`). |
| `--dry-run` | no | Log planned sweeps. Skip `SubmitSweep`. Skip confirm. |

Without `--all`, sources are Postgres `member_wallets` and `treasuries` for `DATABASE_URL`.

Skips: zero USDC, source address equal to destination. Amounts are USDC micro-units (`1_000_000` = $1).

## Env

`.env.local` must have:

- `PRIVY_APP_ID`, `PRIVY_APP_SECRET`
- `PRIVY_AUTHORIZATION_PRIVATE_KEY`, `PRIVY_AUTHORIZATION_KEY_ID` (server sign)
- `RELAYER_PRIVATE_KEY` (fee payer; needs SOL)
- `DATABASE_URL` (always loaded; only queried when `--all` is off)

## After a sweep

Explorer: [solscan.io](https://solscan.io) on printed `tx=`. Group boards will not auto-credit this path. If you drained a treasury that still has share units, fix data or treat it as ops recovery — do not pretend it was a redeem.

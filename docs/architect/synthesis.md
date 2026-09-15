# Phase D synthesis

Arena base is Luna. One `internal/app` module owns session, deposit, sweep, swap, redeem, and views. `packages/domain` stays pure Go math.

## Base (Luna)

- `internal/app` owns commands and read projections (`Session`, `CreateDeposit`, `ObserveSweep`, `GroupView`, `HomeView`, `Redeem`).
- Dependencies: Postgres store (`internal/postgres`), Privy client (`internal/privy`), Jupiter client (`internal/jupiter`), Pyth price client (`internal/pyth`, M4). Client packages stay private to `app`.
- Handlers decode, authenticate, call one `app` method, encode JSON.
- Worker calls `ObserveSweep` after chain confirm. No handler coordinates sweep plus credit.

## Grafts

**From Composer**

- Signature-keyed idempotency on `deposits.tx_signature`, `transactions.tx_signature` (and optional `execute_request_id`), `withdrawals.tx_signature`. No separate sweep log table.
- `NavMode` in `packages/domain`: M2 deposit credit is 1:1 with swept USDC. `NavMarked` (M4) mints claim units from pot NAV when the group treasury holds xStock.

**From Grok**

- `StartBuy`: who may start a buy. M3 allows only the dev stub HTTP route. M4 allows only a passed vote. Stub deleted with the route.
- `RedeemJob` state machine: `Debited` → `Selling` → `Paying` → `Settled`. Debit is the commit point. Crash resumes from job status.

## Rejections

| Candidate shape | Why not |
|-----------------|---------|
| Composer `ledger` + `service` layers | Split credit policy from app commands. Luna `app` already owns transitions. |
| Grok `kernel` + `identity` split | Two public services for one pot. `app` subsumes both. |
| `sweep_tx_log`, `share_ledger` | Logan locked `deposits` + `positions`. Signature lives on deposit row. |
| `jupiter_orders`, `fills`, `cost_basis` | Logan locked `transactions` with cost basis columns. |
| `member_net_usdc_in`, `cash_events` tables | Net USDC in derives from `positions.amount_deposited` minus `amount_withdrawn`. |
| Event-sourced share ledger | Balance table plus snapshots is enough for M1-M5. |

## Mandatory persistence (Logan)

- `deposits` with `tx_signature` UNIQUE
- `positions` with `share_units`, `amount_deposited`, `amount_withdrawn`
- `withdrawals` schema in M2, writes in M4
- `transactions` for buy/sell (replaces jupiter_orders family)

## Verification note

Each milestone doc keeps its ticket titles and `just test` gates. Sketch choices are proven when M2 duplicate-signature test passes, M3 duplicate-transaction test passes, and M4 redeem resumes from `Debited` without double debit. Open README decisions stay ticket-only (M4-T39 through T41).

## Synthesis decision

Luna `internal/app` is the spine. Composer adds typed NAV mode and row-level signature idempotency on Logan's table names. Grok adds `StartBuy` and `RedeemJob` so M3 stub deletion and redeem crash recovery are explicit in the shape, not buried in handlers. Handlers call one `app` function per route so share invariants stay in one place. Swift never imports Go.

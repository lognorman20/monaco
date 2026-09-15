# M2. Deposits and sweeps

**Goal.** The user funds their Privy member wallet with mainnet USDC. The backend sweeps that USDC into the group treasury, credits the position on confirmed treasury arrival, and never credits on member-wallet balance alone. Sweeps are idempotent on transaction signature.

**Depends on.** M1 complete.

**Owns.** Deposit handlers, `internal/worker` poller, `app.ObserveSweep`, position updates on confirmed sweep, relayer use on sweep txs, related Postgres migrations, thin mobile deposit status UI. Pyth prices, votes, and redeem payout stay out.

## Structure

```text
apps/backend/
  internal/app/
    deposit.go            CreateDeposit, ObserveSweep
  internal/worker/
    sweep_poller.go       member USDC poll → Privy SubmitSweep → ObserveSweep
  internal/postgres/
    deposits.go           signature-keyed upsert on deposits.tx_signature
    positions.go          share_units + amount_deposited
  internal/httpapi/
    deposits.go           POST deposit, GET deposit status
```

Claim-unit minting at marked pot NAV is M4. M2 only increments position columns on sweep confirm.

## Flow

1. Mobile `POST /v1/groups/{id}/deposits { amount }`. Insert `deposits` row (pending, `from_address` = member wallet).
2. Worker polls member wallet USDC via Privy + mainnet RPC. No Privy webhooks.
3. When balance covers intent, Privy client `SubmitSweep`. Relayer pays SOL.
4. RPC confirms sweep. Worker builds `ObservedSweep` (signature, from, to group treasury, amount).
5. `app.ObserveSweep` in one Postgres transaction:
  - If `deposits.tx_signature` already equals this signature, return prior status (no double credit).
  - Set `deposits.tx_signature`, status confirmed.
  - Increment `positions.amount_deposited` and `positions.share_units` by swept USDC (same number in M2).
6. USDC sitting only in the member wallet never calls step 5. Zero position change.
7. `GET /v1/deposits/{id}` returns status and credited share units when done.



## Data models

Product tables for M2. One family with M3 and M4. No `sweep_tx_log` or `share_ledger`.

- **deposits.** `user_id`, `group_id`, `amount`, `from_address` (member wallet), `status`, `tx_signature` (unique idempotency key when sweep confirms), `created_at`. A deposit row is user funding a group: sweep member wallet to treasury. Credit share units only after confirmed treasury arrival.
- **positions.** `user_id`, `group_id`, `share_units`, `amount_deposited` (net USDC in for P and L), `amount_withdrawn` (default 0 until M4 redeem). Primary key `(user_id, group_id)`. Share units are internal claim tickets, not dollar IOUs. In M2 the pot is USDC only, so `share_units` and `amount_deposited` increase by the same swept USDC amount.
- **withdrawals.** `user_id`, `group_id`, `amount`, `to_address` (proven payout pubkey), `status`, `tx_signature` (unique when paid), `created_at`. Schema in the M2 migration. Write path ships in M4 redeem. M2 may persist deposits and positions only.
- **users.** M1 table. Add columns in M2 only if needed. Nothing secret.

M4 adjusts `share_units` on deposit when the treasury holds marked xStock so a later member does not capture unrealized gain. That math is not part of M2.

- `percent return` uses net USDC in: deposits credited minus withdrawals paid (withdrawals stay zero in M2)

USDC mint `EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v`. Poll. Do not use Privy production webhooks.

## Parallelization

Milestone gate: complete **Wave 5** before M3.

| Wave | Tracks (run in parallel within the wave) | Gate for next wave |
|------|------------------------------------------|--------------------|
| **1** | **Schema** M2-T1 · **Types** M2-T2 · **Logging** M2-T12 | M1 |
| **2** | **Deposit API** M2-T3 · **Poller** M2-T4 · **Relayer on sweep** M2-T6 | Wave 1 |
| **3** | **Sweep submit** M2-T5 · **Parent credit** M2-T8 | M2-T4; M2-T5 for T8 |
| **4** | **Read APIs** M2-T11 · **Mobile deposit UI** M2-T13 | M2-T8 |
| **5** | **Unit tests** M2-T14, M2-T15 · **Integration tests** M2-T16, M2-T17 · **just wiring** M2-T18 | Wave 4 |

Sweep pipeline is sequential inside Wave 3: poller (T4) → sweep submit (T5) → credit (T8). Backend read APIs and mobile UI are parallel in Wave 4.

## Tickets

1. M2-T1 Add the migration for deposits, positions, and withdrawals schema.
2. M2-T2 Define domain types for deposit, position, and sweep credit at the API boundary.
3. M2-T3 Implement `POST` deposit keyed by user, group, and amount.  
   Depends on: M2-T1, M2-T2
4. M2-T4 Implement the member-wallet USDC balance poller using Privy and mainnet RPC.  
   Depends on: M2-T1
5. M2-T5 Implement the server-signed USDC sweep from member wallet to group treasury via Privy.  
   Depends on: M2-T4
6. M2-T6 Wire the app relayer as SOL fee payer on sweep transactions.  
   Depends on: M1-T8
7. **Parent** M2-T8 Credit position share units on confirmed treasury credit only, idempotent on deposit signature.  
   Depends on: M2-T5, M2-T6
   - └ **Subissue of M2-T8** M2-T7 Confirm the sweep on-chain and set `tx_signature` on the deposit row with unique upsert
   - └ **Subissue of M2-T8** M2-T9 On confirmed sweep, increment `amount_deposited` and `share_units` by swept USDC (1:1 in M2)
   - └ **Subissue of M2-T8** M2-T10 Reject share credit when USDC sits only in the member wallet
8. M2-T11 Add GET endpoints for deposit status, member share units, and treasury USDC balance.  
   Depends on: M2-T8
9. M2-T12 Add structured logs for sweep attempt, confirmation, and share credit.
10. M2-T13 Add a thin mobile deposit screen to create a deposit and poll status.  
    Depends on: M2-T3, M2-T11
11. M2-T14 Add unit tests that credited `share_units` and `amount_deposited` equal swept USDC.  
    Depends on: M2-T8
12. M2-T15 Add unit tests that multiple deposits sum `share_units` and `amount_deposited` correctly.  
    Depends on: M2-T8
13. M2-T16 Add an integration test that a duplicate sweep signature does not double-credit shares.  
    Depends on: M2-T8
14. M2-T17 Add an integration test that member-wallet-only USDC creates zero position share units.  
    Depends on: M2-T10
15. M2-T18 Wire `just test backend` to cover deposit and sweep tests against local Docker Postgres.  
    Depends on: M2-T14, M2-T15, M2-T16, M2-T17



## Ticket details

#### M2-T1: Add migration for deposits, positions, and withdrawals schema

**Context**
M2 follows M1 wallets. Deposit → sweep → 1:1 share credit must work before Jupiter buys (M3).

Ticket `M2-T1` is wave **1** in `docs/milestones/m2-deposits.md` under milestone **M2 — Deposits**.

**Problem**
SQL tables/columns for `M2-T1` are missing from `supabase/migrations/`; migration runner cannot apply them.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `supabase/migrations/`. No drive-by refactors.
2. Add next sequential SQL file under `supabase/migrations/` with tables/columns from `docs/milestones/m2-deposits.md`.
3. Run migration via existing runner (`apps/backend/internal/postgres/`). Do not hand-apply in prod.
4. Satisfy milestone gate: `just test backend` and/or `just test mobile` exit 0 with no new failing tests.

**API / data contract**
**No HTTP.** SQL migration only. Apply via migration runner invoked by API boot and `just test backend`.
**Test fakes (locked names):**
- `fakePrivyClient` (MemberUSDCBalance, SubmitSweep)
- `fakeSolanaRPC`
- `stubClock`
- `buildObservedSweep(overrides)`
- `integrationApp(t)`

### Scope
- `supabase/migrations/`

**Out of scope (do not touch)**
- Jupiter swaps
- buy proposals
- votes
- redeem payout
- leaderboards
- Pyth marks

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `supabase/migrations/`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `supabase/migrations/`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- None
- **Wave:** 1
#### M2-T2: Define domain types for deposit, position, and sweep credit at API boundary

**Context**
M2 follows M1 wallets. Deposit → sweep → 1:1 share credit must work before Jupiter buys (M3).

Ticket `M2-T2` is wave **1** in `docs/milestones/m2-deposits.md` under milestone **M2 — Deposits**.

**Problem**
Behavior for `Define domain types for deposit, position, and sweep credit at API boundary` is not implemented under `apps/backend/internal/app/deposit.go`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/app/deposit.go`. No drive-by refactors.
2. Satisfy milestone gate: `just test backend` and/or `just test mobile` exit 0 with no new failing tests.

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `fakePrivyClient` (MemberUSDCBalance, SubmitSweep)
- `fakeSolanaRPC`
- `stubClock`
- `buildObservedSweep(overrides)`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/app/deposit.go`

**Out of scope (do not touch)**
- Worker loop
- HTTP handlers

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/deposit.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/deposit.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- None
- **Wave:** 1
#### M2-T3: Implement POST deposit keyed by user, group, and amount

**Context**
M2 follows M1 wallets. Deposit → sweep → 1:1 share credit must work before Jupiter buys (M3).

Ticket `M2-T3` is wave **2** in `docs/milestones/m2-deposits.md` under milestone **M2 — Deposits**.
Blocked until dependencies land: M2-T1, M2-T2.

**Problem**
Behavior for `Implement POST deposit keyed by user, group, and amount` is not implemented under `apps/backend/internal/httpapi/deposits.go`, `apps/backend/internal/app/deposit.go`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/httpapi/deposits.go`, `apps/backend/internal/app/deposit.go`. No drive-by refactors.
2. Add `TestPOST_deposit_createsPendingDepositRow` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
3. Add `TestPOST_deposit_missingAuth_returns401` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
4. Add `TestPOST_deposit_nonMember_returns403` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
5. Add `TestPOST_deposit_zeroOrNegativeAmount_returns400` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `fakePrivyClient` (MemberUSDCBalance, SubmitSweep)
- `fakeSolanaRPC`
- `stubClock`
- `buildObservedSweep(overrides)`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/httpapi/deposits.go`
- `apps/backend/internal/app/deposit.go`

**Out of scope (do not touch)**
- Sweep poller
- Share credit

**Acceptance Criteria**
- [ ] Test `TestPOST_deposit_createsPendingDepositRow` exists, uses AAA comments, and passes.
- [ ] Test `TestPOST_deposit_missingAuth_returns401` exists, uses AAA comments, and passes.
- [ ] Test `TestPOST_deposit_nonMember_returns403` exists, uses AAA comments, and passes.
- [ ] Test `TestPOST_deposit_zeroOrNegativeAmount_returns400` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/httpapi/deposits.go`, `apps/backend/internal/app/deposit.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestPOST_deposit_createsPendingDepositRow` exists, uses AAA comments, and passes.
- [ ] Test `TestPOST_deposit_missingAuth_returns401` exists, uses AAA comments, and passes.
- [ ] Test `TestPOST_deposit_nonMember_returns403` exists, uses AAA comments, and passes.
- [ ] Test `TestPOST_deposit_zeroOrNegativeAmount_returns400` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/httpapi/deposits.go`, `apps/backend/internal/app/deposit.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M2-T1 — **do not start while open**
- M2-T2 — **do not start while open**
- **Wave:** 2
#### M2-T4: Implement member-wallet USDC balance poller using Privy and mainnet RPC

**Context**
M2 follows M1 wallets. Deposit → sweep → 1:1 share credit must work before Jupiter buys (M3).

Ticket `M2-T4` is wave **2** in `docs/milestones/m2-deposits.md` under milestone **M2 — Deposits**.
Blocked until dependencies land: M2-T1.

**Problem**
Behavior for `Implement member-wallet USDC balance poller using Privy and mainnet RPC` is not implemented under `apps/backend/internal/worker/sweep_poller.go`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/worker/sweep_poller.go`. No drive-by refactors.
2. Add `TestSweepPoller_memberBalanceCoversIntent_triggersSubmitSweep` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
3. Add `TestSweepPoller_memberBalanceBelowIntent_doesNotSweep` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `fakePrivyClient` (MemberUSDCBalance, SubmitSweep)
- `fakeSolanaRPC`
- `stubClock`
- `buildObservedSweep(overrides)`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/worker/sweep_poller.go`

**Out of scope (do not touch)**
- Share credit
- Jupiter

**Acceptance Criteria**
- [ ] Test `TestSweepPoller_memberBalanceCoversIntent_triggersSubmitSweep` exists, uses AAA comments, and passes.
- [ ] Test `TestSweepPoller_memberBalanceBelowIntent_doesNotSweep` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/worker/sweep_poller.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestSweepPoller_memberBalanceCoversIntent_triggersSubmitSweep` exists, uses AAA comments, and passes.
- [ ] Test `TestSweepPoller_memberBalanceBelowIntent_doesNotSweep` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/worker/sweep_poller.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M2-T1 — **do not start while open**
- **Wave:** 2
#### M2-T5: Implement server-signed USDC sweep from member wallet to group treasury via Privy

**Context**
M2 follows M1 wallets. Deposit → sweep → 1:1 share credit must work before Jupiter buys (M3).

Ticket `M2-T5` is wave **3** in `docs/milestones/m2-deposits.md` under milestone **M2 — Deposits**.
Blocked until dependencies land: M2-T4.

**Problem**
Behavior for `Implement server-signed USDC sweep from member wallet to group treasury via Privy` is not implemented under `apps/backend/internal/privy/sweep.go`, `apps/backend/internal/worker/`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/privy/sweep.go`, `apps/backend/internal/worker/`. No drive-by refactors.
2. Add `TestSubmitSweep_callsPrivyClientWithMemberAndTreasuryAddresses` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `fakePrivyClient` (MemberUSDCBalance, SubmitSweep)
- `fakeSolanaRPC`
- `stubClock`
- `buildObservedSweep(overrides)`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/privy/sweep.go`
- `apps/backend/internal/worker/`

**Out of scope (do not touch)**
- Position credit

**Acceptance Criteria**
- [ ] Test `TestSubmitSweep_callsPrivyClientWithMemberAndTreasuryAddresses` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/privy/sweep.go`, `apps/backend/internal/worker/`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestSubmitSweep_callsPrivyClientWithMemberAndTreasuryAddresses` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/privy/sweep.go`, `apps/backend/internal/worker/`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M2-T4 — **do not start while open**
- **Wave:** 3
#### M2-T6: Wire app relayer as SOL fee payer on sweep transactions

**Context**
M2 follows M1 wallets. Deposit → sweep → 1:1 share credit must work before Jupiter buys (M3).

Ticket `M2-T6` is wave **2** in `docs/milestones/m2-deposits.md` under milestone **M2 — Deposits**.
Blocked until dependencies land: M1-T8.

**Problem**
Behavior for `Wire app relayer as SOL fee payer on sweep transactions` is not implemented under `apps/backend/internal/privy/sweep.go`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/privy/sweep.go`. No drive-by refactors.
2. Add `TestSubmitSweep_includesRelayerAsFeePayer` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `fakePrivyClient` (MemberUSDCBalance, SubmitSweep)
- `fakeSolanaRPC`
- `stubClock`
- `buildObservedSweep(overrides)`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/privy/sweep.go`

**Out of scope (do not touch)**
- Jupiter fee payer

**Acceptance Criteria**
- [ ] Test `TestSubmitSweep_includesRelayerAsFeePayer` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/privy/sweep.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestSubmitSweep_includesRelayerAsFeePayer` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/privy/sweep.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M1-T8 — **do not start while open**
- **Wave:** 2
#### M2-T8: Credit position share units on confirmed treasury credit only, idempotent on deposit signature

**Context**
M2 follows M1 wallets. Deposit → sweep → 1:1 share credit must work before Jupiter buys (M3).

Ticket `M2-T8` is wave **3** in `docs/milestones/m2-deposits.md` under milestone **M2 — Deposits**.
Parent ticket; subissues: M2-T7, M2-T9, M2-T10.
Blocked until dependencies land: M2-T5, M2-T6.

**Problem**
Parent `M2-T8` orchestration in `apps/backend/internal/app/deposit.go`, `apps/backend/internal/postgres/deposits.go`, `apps/backend/internal/postgres/positions.go` missing; subissues M2-T7, M2-T9, M2-T10 not wired together.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/app/deposit.go`, `apps/backend/internal/postgres/deposits.go`, `apps/backend/internal/postgres/positions.go`. No drive-by refactors.
2. Parent glue: wire subissues M2-T7, M2-T9, M2-T10 into `apps/backend/internal/app/deposit.go` after each subissue lands.
3. Add `TestObserveSweep_confirmedTreasuryArrival_incrementsShareUnitsAndAmountDepositedEqually` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**Parent orchestration.** Public contract is union of subissue routes/symbols: M2-T7, M2-T9, M2-T10.
**Test fakes (locked names):**
- `fakePrivyClient` (MemberUSDCBalance, SubmitSweep)
- `fakeSolanaRPC`
- `stubClock`
- `buildObservedSweep(overrides)`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/app/deposit.go`
- `apps/backend/internal/postgres/deposits.go`
- `apps/backend/internal/postgres/positions.go`

**Out of scope (do not touch)**
- Marked NAV minting (M4)

**Acceptance Criteria**
- [ ] Test `TestObserveSweep_confirmedTreasuryArrival_incrementsShareUnitsAndAmountDepositedEqually` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/deposit.go`, `apps/backend/internal/postgres/deposits.go`, `apps/backend/internal/postgres/positions.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] All subissues (M2-T7, M2-T9, M2-T10) closed before parent closes.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestObserveSweep_confirmedTreasuryArrival_incrementsShareUnitsAndAmountDepositedEqually` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/deposit.go`, `apps/backend/internal/postgres/deposits.go`, `apps/backend/internal/postgres/positions.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] All subissues (M2-T7, M2-T9, M2-T10) closed before parent closes.
- [ ] Subissues closed: M2-T7, M2-T9, M2-T10.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M2-T5 — **do not start while open**
- M2-T6 — **do not start while open**
- **Wave:** 3
- **Subissue:** M2-T7
- **Subissue:** M2-T9
- **Subissue:** M2-T10
#### M2-T7: Confirm sweep on-chain and set tx_signature on deposit row with unique upsert

**Context**
M2 follows M1 wallets. Deposit → sweep → 1:1 share credit must work before Jupiter buys (M3).

Ticket `M2-T7` is wave **3** in `docs/milestones/m2-deposits.md` under milestone **M2 — Deposits**.
Subissue of M2-T8.
Blocked until dependencies land: M2-T5.

**Problem**
Subissue `M2-T7` of `M2-T8`: symbols under `apps/backend/internal/postgres/deposits.go`, `apps/backend/internal/app/deposit.go` not implemented; parent cannot close.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/postgres/deposits.go`, `apps/backend/internal/app/deposit.go`. No drive-by refactors.
2. Subissue of `M2-T8`. Do not add routes or flows owned by parent/sibling subissues.
3. Add `TestObserveSweep_confirmedTreasuryArrival_setsDepositStatusConfirmed` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new public route.** Subissue of `M2-T8`. Internal symbols only in **Owns** paths.
**Test fakes (locked names):**
- `fakePrivyClient` (MemberUSDCBalance, SubmitSweep)
- `fakeSolanaRPC`
- `stubClock`
- `buildObservedSweep(overrides)`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/postgres/deposits.go`
- `apps/backend/internal/app/deposit.go`

**Out of scope (do not touch)**
- Share unit increment logic (M2-T9)

**Acceptance Criteria**
- [ ] Test `TestObserveSweep_confirmedTreasuryArrival_setsDepositStatusConfirmed` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/postgres/deposits.go`, `apps/backend/internal/app/deposit.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestObserveSweep_confirmedTreasuryArrival_setsDepositStatusConfirmed` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/postgres/deposits.go`, `apps/backend/internal/app/deposit.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M2-T5 — **do not start while open**
- **Wave:** 3
- **Parent:** M2-T8
#### M2-T9: On confirmed sweep increment amount_deposited and share_units by swept USDC (1:1 in M2)

**Context**
M2 follows M1 wallets. Deposit → sweep → 1:1 share credit must work before Jupiter buys (M3).

Ticket `M2-T9` is wave **3** in `docs/milestones/m2-deposits.md` under milestone **M2 — Deposits**.
Subissue of M2-T8.
Blocked until dependencies land: M2-T7.

**Problem**
Subissue `M2-T9` of `M2-T8`: symbols under `apps/backend/internal/postgres/positions.go`, `apps/backend/internal/app/deposit.go` not implemented; parent cannot close.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/postgres/positions.go`, `apps/backend/internal/app/deposit.go`. No drive-by refactors.
2. Subissue of `M2-T8`. Do not add routes or flows owned by parent/sibling subissues.
3. Add `TestObserveSweep_confirmedTreasuryArrival_incrementsShareUnitsAndAmountDepositedEqually` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
4. Add `TestObserveSweep_multipleDeposits_sumsShareUnitsAndAmountDeposited` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new public route.** Subissue of `M2-T8`. Internal symbols only in **Owns** paths.
**Test fakes (locked names):**
- `fakePrivyClient` (MemberUSDCBalance, SubmitSweep)
- `fakeSolanaRPC`
- `stubClock`
- `buildObservedSweep(overrides)`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/postgres/positions.go`
- `apps/backend/internal/app/deposit.go`

**Out of scope (do not touch)**
- Marked pot ratio (M4)

**Acceptance Criteria**
- [ ] Test `TestObserveSweep_confirmedTreasuryArrival_incrementsShareUnitsAndAmountDepositedEqually` exists, uses AAA comments, and passes.
- [ ] Test `TestObserveSweep_multipleDeposits_sumsShareUnitsAndAmountDeposited` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/postgres/positions.go`, `apps/backend/internal/app/deposit.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestObserveSweep_confirmedTreasuryArrival_incrementsShareUnitsAndAmountDepositedEqually` exists, uses AAA comments, and passes.
- [ ] Test `TestObserveSweep_multipleDeposits_sumsShareUnitsAndAmountDeposited` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/postgres/positions.go`, `apps/backend/internal/app/deposit.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M2-T7 — **do not start while open**
- **Wave:** 3
- **Parent:** M2-T8
#### M2-T10: Reject share credit when USDC sits only in the member wallet

**Context**
M2 follows M1 wallets. Deposit → sweep → 1:1 share credit must work before Jupiter buys (M3).

Ticket `M2-T10` is wave **3** in `docs/milestones/m2-deposits.md` under milestone **M2 — Deposits**.
Subissue of M2-T8.
Blocked until dependencies land: M2-T8.

**Problem**
Subissue `M2-T10` of `M2-T8`: symbols under `apps/backend/internal/app/deposit.go` not implemented; parent cannot close.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/app/deposit.go`. No drive-by refactors.
2. Subissue of `M2-T8`. Do not add routes or flows owned by parent/sibling subissues.
3. Add `TestObserveSweep_memberWalletOnlyBalance_doesNotCreditPosition` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new public route.** Subissue of `M2-T8`. Internal symbols only in **Owns** paths.
**Test fakes (locked names):**
- `fakePrivyClient` (MemberUSDCBalance, SubmitSweep)
- `fakeSolanaRPC`
- `stubClock`
- `buildObservedSweep(overrides)`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/app/deposit.go`

**Out of scope (do not touch)**
- Jupiter
- Votes

**Acceptance Criteria**
- [ ] Test `TestObserveSweep_memberWalletOnlyBalance_doesNotCreditPosition` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/deposit.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestObserveSweep_memberWalletOnlyBalance_doesNotCreditPosition` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/deposit.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M2-T8 — **do not start while open**
- **Wave:** 3
- **Parent:** M2-T8
#### M2-T11: Add GET endpoints for deposit status, member share units, and treasury USDC balance

**Context**
M2 follows M1 wallets. Deposit → sweep → 1:1 share credit must work before Jupiter buys (M3).

Ticket `M2-T11` is wave **4** in `docs/milestones/m2-deposits.md` under milestone **M2 — Deposits**.
Blocked until dependencies land: M2-T8.

**Problem**
Behavior for `Add GET endpoints for deposit status, member share units, and treasury USDC balance` is not implemented under `apps/backend/internal/httpapi/deposits.go`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/httpapi/deposits.go`. No drive-by refactors.
2. Add `TestGET_depositStatus_returnsCreditedShareUnitsWhenConfirmed` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
3. Add `TestGET_memberShareUnits_reflectsPositionAfterSweep` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
4. Add `TestGET_treasuryUsdcBalance_returnsPostSweepAmount` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `fakePrivyClient` (MemberUSDCBalance, SubmitSweep)
- `fakeSolanaRPC`
- `stubClock`
- `buildObservedSweep(overrides)`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/httpapi/deposits.go`

**Out of scope (do not touch)**
- xStock balances

**Acceptance Criteria**
- [ ] Test `TestGET_depositStatus_returnsCreditedShareUnitsWhenConfirmed` exists, uses AAA comments, and passes.
- [ ] Test `TestGET_memberShareUnits_reflectsPositionAfterSweep` exists, uses AAA comments, and passes.
- [ ] Test `TestGET_treasuryUsdcBalance_returnsPostSweepAmount` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/httpapi/deposits.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestGET_depositStatus_returnsCreditedShareUnitsWhenConfirmed` exists, uses AAA comments, and passes.
- [ ] Test `TestGET_memberShareUnits_reflectsPositionAfterSweep` exists, uses AAA comments, and passes.
- [ ] Test `TestGET_treasuryUsdcBalance_returnsPostSweepAmount` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/httpapi/deposits.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M2-T8 — **do not start while open**
- **Wave:** 4
#### M2-T12: Add structured logs for sweep attempt, confirmation, and share credit

**Context**
M2 follows M1 wallets. Deposit → sweep → 1:1 share credit must work before Jupiter buys (M3).

Ticket `M2-T12` is wave **1** in `docs/milestones/m2-deposits.md` under milestone **M2 — Deposits**.

**Problem**
Behavior for `Add structured logs for sweep attempt, confirmation, and share credit` is not implemented under `apps/backend/internal/worker/`, `apps/backend/internal/app/deposit.go`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/worker/`, `apps/backend/internal/app/deposit.go`. No drive-by refactors.
2. Add structured log lines at attempt/confirm/credit (or quote/execute/poll) transitions using existing logger; fields: `group_id`, `user_id`, `deposit_id`/`tx_signature`/`symbol` as applicable.
3. Satisfy milestone gate: `just test backend` and/or `just test mobile` exit 0 with no new failing tests.

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `fakePrivyClient` (MemberUSDCBalance, SubmitSweep)
- `fakeSolanaRPC`
- `stubClock`
- `buildObservedSweep(overrides)`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/worker/`
- `apps/backend/internal/app/deposit.go`

**Out of scope (do not touch)**
- Metrics stack
- Privy webhooks

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/worker/`, `apps/backend/internal/app/deposit.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/worker/`, `apps/backend/internal/app/deposit.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- None
- **Wave:** 1
#### M2-T13: Add thin mobile deposit screen to create deposit and poll status

**Context**
M2 follows M1 wallets. Deposit → sweep → 1:1 share credit must work before Jupiter buys (M3).

Ticket `M2-T13` is wave **4** in `docs/milestones/m2-deposits.md` under milestone **M2 — Deposits**.
Blocked until dependencies land: M2-T3, M2-T11.

**Problem**
SwiftUI flow in `apps/mobile/Features/Deposit/` is missing or still scaffold; product/API wiring for `Add thin mobile deposit screen to create deposit and poll status` not shipped.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Features/Deposit/`. No drive-by refactors.
2. Build SwiftUI screen in **Owns**; call `MonacoAPIClient` only (no xStocks/Jupiter/Pyth/Solana RPC).
3. Compile gate: `just build mobile` and `just test mobile` exit 0.
4. **Manual (after automated green):** Fund member wallet with mainnet USDC; Confirm sweep in explorer

**API / data contract**
**Mobile consumes existing backend JSON only.** No direct calls to xStocks, Jupiter, Pyth, or Solana RPC from Swift. Use `MonacoAPIClient` methods matching backend routes in milestone doc.
**Test fakes (locked names):**
- `fakePrivyClient` (MemberUSDCBalance, SubmitSweep)
- `fakeSolanaRPC`
- `stubClock`
- `buildObservedSweep(overrides)`
- `integrationApp(t)`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/Features/Deposit/`

**Out of scope (do not touch)**
- Product deposit copy (M5)

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Deposit/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Manual (only for this ticket):**
- Fund member wallet with mainnet USDC
- Confirm sweep in explorer

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Deposit/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M2-T3 — **do not start while open**
- M2-T11 — **do not start while open**
- **Wave:** 4
#### M2-T14: Add unit tests that credited share_units and amount_deposited equal swept USDC

**Context**
M2 follows M1 wallets. Deposit → sweep → 1:1 share credit must work before Jupiter buys (M3).

Ticket `M2-T14` is wave **5** in `docs/milestones/m2-deposits.md` under milestone **M2 — Deposits**.
Blocked until dependencies land: M2-T8.

**Problem**
Locked test names for `M2-T14` are absent or not wired into `just test` recipes; `apps/backend/internal/app/deposit_test.go` incomplete.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/app/deposit_test.go`. No drive-by refactors.
2. Add `TestObserveSweep_confirmedTreasuryArrival_incrementsShareUnitsAndAmountDepositedEqually` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
3. Add `TestProperty_sweptUsdcEqualsShareUnitsAndAmountDeposited` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new API.** Tests and/or `Justfile` wiring only.
**Test fakes (locked names):**
- `fakePrivyClient` (MemberUSDCBalance, SubmitSweep)
- `fakeSolanaRPC`
- `stubClock`
- `buildObservedSweep(overrides)`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/app/deposit_test.go`

**Out of scope (do not touch)**
- Integration duplicate-signature test (M2-T16)

**Acceptance Criteria**
- [ ] Test `TestObserveSweep_confirmedTreasuryArrival_incrementsShareUnitsAndAmountDepositedEqually` exists, uses AAA comments, and passes.
- [ ] Test `TestProperty_sweptUsdcEqualsShareUnitsAndAmountDeposited` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/deposit_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestObserveSweep_confirmedTreasuryArrival_incrementsShareUnitsAndAmountDepositedEqually` exists, uses AAA comments, and passes.
- [ ] Test `TestProperty_sweptUsdcEqualsShareUnitsAndAmountDeposited` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/deposit_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M2-T8 — **do not start while open**
- **Wave:** 5
#### M2-T15: Add unit tests that multiple deposits sum share_units and amount_deposited correctly

**Context**
M2 follows M1 wallets. Deposit → sweep → 1:1 share credit must work before Jupiter buys (M3).

Ticket `M2-T15` is wave **5** in `docs/milestones/m2-deposits.md` under milestone **M2 — Deposits**.
Blocked until dependencies land: M2-T8.

**Problem**
Locked test names for `M2-T15` are absent or not wired into `just test` recipes; `apps/backend/internal/app/deposit_test.go` incomplete.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/app/deposit_test.go`. No drive-by refactors.
2. Add `TestObserveSweep_multipleDeposits_sumsShareUnitsAndAmountDeposited` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
3. Add `TestProperty_multipleDepositsPreserveOneToOneInvariant` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new API.** Tests and/or `Justfile` wiring only.
**Test fakes (locked names):**
- `fakePrivyClient` (MemberUSDCBalance, SubmitSweep)
- `fakeSolanaRPC`
- `stubClock`
- `buildObservedSweep(overrides)`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/app/deposit_test.go`

**Out of scope (do not touch)**
- Mobile tests

**Acceptance Criteria**
- [ ] Test `TestObserveSweep_multipleDeposits_sumsShareUnitsAndAmountDeposited` exists, uses AAA comments, and passes.
- [ ] Test `TestProperty_multipleDepositsPreserveOneToOneInvariant` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/deposit_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestObserveSweep_multipleDeposits_sumsShareUnitsAndAmountDeposited` exists, uses AAA comments, and passes.
- [ ] Test `TestProperty_multipleDepositsPreserveOneToOneInvariant` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/deposit_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M2-T8 — **do not start while open**
- **Wave:** 5
#### M2-T16: Add integration test that duplicate sweep signature does not double-credit shares

**Context**
M2 follows M1 wallets. Deposit → sweep → 1:1 share credit must work before Jupiter buys (M3).

Ticket `M2-T16` is wave **5** in `docs/milestones/m2-deposits.md` under milestone **M2 — Deposits**.
Blocked until dependencies land: M2-T8.

**Problem**
Locked test names for `M2-T16` are absent or not wired into `just test` recipes; `apps/backend/internal/app/deposit_integration_test.go` incomplete.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/app/deposit_integration_test.go`. No drive-by refactors.
2. Add `TestObserveSweep_duplicateSignature_doesNotDoubleCredit` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
3. Add `TestObserveSweep_concurrentDuplicateSignature_creditsOnceOnly` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
4. Add `TestProperty_observeSweepIdempotent` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new API.** Tests and/or `Justfile` wiring only.
**Test fakes (locked names):**
- `fakePrivyClient` (MemberUSDCBalance, SubmitSweep)
- `fakeSolanaRPC`
- `stubClock`
- `buildObservedSweep(overrides)`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/app/deposit_integration_test.go`

**Out of scope (do not touch)**
- Jupiter idempotency

**Acceptance Criteria**
- [ ] Test `TestObserveSweep_duplicateSignature_doesNotDoubleCredit` exists, uses AAA comments, and passes.
- [ ] Test `TestObserveSweep_concurrentDuplicateSignature_creditsOnceOnly` exists, uses AAA comments, and passes.
- [ ] Test `TestProperty_observeSweepIdempotent` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/deposit_integration_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestObserveSweep_duplicateSignature_doesNotDoubleCredit` exists, uses AAA comments, and passes.
- [ ] Test `TestObserveSweep_concurrentDuplicateSignature_creditsOnceOnly` exists, uses AAA comments, and passes.
- [ ] Test `TestProperty_observeSweepIdempotent` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/deposit_integration_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M2-T8 — **do not start while open**
- **Wave:** 5
#### M2-T17: Add integration test that member-wallet-only USDC creates zero position share units

**Context**
M2 follows M1 wallets. Deposit → sweep → 1:1 share credit must work before Jupiter buys (M3).

Ticket `M2-T17` is wave **5** in `docs/milestones/m2-deposits.md` under milestone **M2 — Deposits**.
Blocked until dependencies land: M2-T10.

**Problem**
Locked test names for `M2-T17` are absent or not wired into `just test` recipes; `apps/backend/internal/app/deposit_integration_test.go` incomplete.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/app/deposit_integration_test.go`. No drive-by refactors.
2. Add `TestObserveSweep_memberWalletOnlyBalance_doesNotCreditPosition` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new API.** Tests and/or `Justfile` wiring only.
**Test fakes (locked names):**
- `fakePrivyClient` (MemberUSDCBalance, SubmitSweep)
- `fakeSolanaRPC`
- `stubClock`
- `buildObservedSweep(overrides)`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/app/deposit_integration_test.go`

**Out of scope (do not touch)**
- Treasury-only USDC without deposit row

**Acceptance Criteria**
- [ ] Test `TestObserveSweep_memberWalletOnlyBalance_doesNotCreditPosition` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/deposit_integration_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestObserveSweep_memberWalletOnlyBalance_doesNotCreditPosition` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/deposit_integration_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M2-T10 — **do not start while open**
- **Wave:** 5
#### M2-T18: Wire just test backend to cover deposit and sweep tests against local Docker Postgres

**Context**
M2 follows M1 wallets. Deposit → sweep → 1:1 share credit must work before Jupiter buys (M3).

Ticket `M2-T18` is wave **5** in `docs/milestones/m2-deposits.md` under milestone **M2 — Deposits**.
Blocked until dependencies land: M2-T14, M2-T15, M2-T16, M2-T17.

**Problem**
Locked test names for `M2-T18` are absent or not wired into `just test` recipes; `Justfile` incomplete.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `Justfile`. No drive-by refactors.
2. Update root `Justfile` recipes so locked tests run under `just test backend` and/or `just test mobile`. Exit 0 required.
3. Satisfy milestone gate: `just test backend` and/or `just test mobile` exit 0 with no new failing tests.

**API / data contract**
**No new API.** Tests and/or `Justfile` wiring only.
**Test fakes (locked names):**
- `fakePrivyClient` (MemberUSDCBalance, SubmitSweep)
- `fakeSolanaRPC`
- `stubClock`
- `buildObservedSweep(overrides)`
- `integrationApp(t)`

### Scope
- `Justfile`

**Out of scope (do not touch)**
- just test mobile

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `Justfile`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `Justfile`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M2-T14 — **do not start while open**
- M2-T15 — **do not start while open**
- M2-T16 — **do not start while open**
- M2-T17 — **do not start while open**
- **Wave:** 5

## Automated verification

**Commands.** `just test backend`.

### Test layers

| Layer | Runs under | What it proves |
|-------|------------|----------------|
| **Go unit** | `just test backend` | `app.ObserveSweep`, `CreateDeposit` validation, poller decision logic with **fake Privy**, **fake RPC**, **stub clock** — no Postgres, no network. |
| **Go integration** | `just test backend` | Full deposit → sweep confirm → position credit against local Docker Postgres; idempotency and member-wallet-only negative path. |
| **Concurrency** | `just test backend` | Two goroutines call `ObserveSweep` with the same signature; expect one credit (M2-T16). |

Tests assert literal `share_units`, `amount_deposited`, and `deposits.tx_signature` row counts. AAA comments on every test.

### Test map

**M2-T3 — POST deposit (integration)**

- `TestPOST_deposit_createsPendingDepositRow`
- `TestPOST_deposit_missingAuth_returns401`
- `TestPOST_deposit_nonMember_returns403`
- `TestPOST_deposit_zeroOrNegativeAmount_returns400`

**M2-T4 / M2-T5 — poller and sweep (unit)**

- `TestSweepPoller_memberBalanceCoversIntent_triggersSubmitSweep`
- `TestSweepPoller_memberBalanceBelowIntent_doesNotSweep`
- `TestSubmitSweep_callsPrivyClientWithMemberAndTreasuryAddresses`

**M2-T8 — ObserveSweep credit (unit + integration)** — parent M2-T7, M2-T9, M2-T10

- `TestObserveSweep_confirmedTreasuryArrival_incrementsShareUnitsAndAmountDepositedEqually` (M2-T9, 1:1 in M2)
- `TestObserveSweep_confirmedTreasuryArrival_setsDepositStatusConfirmed` (M2-T7)
- `TestObserveSweep_duplicateSignature_doesNotDoubleCredit` (M2-T16, idempotency)
- `TestObserveSweep_memberWalletOnlyBalance_doesNotCreditPosition` (M2-T10)
- `TestObserveSweep_multipleDeposits_sumsShareUnitsAndAmountDeposited` (M2-T15)
- `TestObserveSweep_concurrentDuplicateSignature_creditsOnceOnly` (race)

**M2-T11 — read APIs (integration)**

- `TestGET_depositStatus_returnsCreditedShareUnitsWhenConfirmed`
- `TestGET_memberShareUnits_reflectsPositionAfterSweep`
- `TestGET_treasuryUsdcBalance_returnsPostSweepAmount`

**M2-T6 — relayer on sweep (unit)**

- `TestSubmitSweep_includesRelayerAsFeePayer`

**Product invariants (must pass)**

- `share_units` increase equals `amount_deposited` increase equals swept USDC micros (M2 has no marked NAV).
- Credit only after confirmed treasury arrival, never on member-wallet balance alone.
- Idempotent on `deposits.tx_signature`.
- No Jupiter, votes, or redeem payout in M2.

### Fixtures / fakes

- `fakePrivyClient` — `MemberUSDCBalance`, `SubmitSweep` returns deterministic signatures.
- `fakeSolanaRPC` — confirm/not-found for sweep signatures.
- `stubClock` — poller tick control.
- `buildDeposit(overrides)`, `buildPosition(overrides)`, `buildObservedSweep(overrides)`.
- `integrationApp(t)` with fake Privy/RPC; real Postgres.

**Testability note.** `ObserveSweep` must accept injected store and run inside `InTx` so unit tests can use an in-memory or test DB without the worker loop.

### Property / invariant tests

Use `testing/quick` or `pgregory.net/rapid` (Go) in `packages/domain` or `internal/app` tests:

- `TestProperty_sweptUsdcEqualsShareUnitsAndAmountDeposited` — for any positive swept USDC in M2, credited `share_units` == `amount_deposited` == swept amount.
- `TestProperty_observeSweepIdempotent` — applying the same `ObservedSweep` N times leaves position unchanged after the first credit.
- `TestProperty_multipleDepositsPreserveOneToOneInvariant` — sum of credits equals sum of swept amounts.

## Manual verification

Mainnet USDC, Privy funding, explorer, and log watching only. Row counts and 1:1 credit are automated above.

1. Sign in on mobile. Fund the member Privy wallet with a small mainnet USDC amount, onramp or external transfer.
2. In the Privy dashboard, note the member wallet address and USDC balance before deposit.
3. Create a deposit for a test group and submit.
4. Watch API logs for poller detection, sweep build, and relayer-signed submit.
5. Open the Solana explorer for the sweep signature. Confirm USDC moved to the group treasury.
6. Confirm treasury USDC rose and member wallet USDC fell by the swept amount in the Privy dashboard or explorer.



## Out of scope

Jupiter swaps, buy proposals, votes, redeem payout, leaderboards, P and L boards, token price marks, Privy production webhooks, user-facing gas or seed phrase flows. Withdrawal rows are not written until M4.

## Open decisions

None for M2.

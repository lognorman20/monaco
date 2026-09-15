# M3. Jupiter buy and sell

**Goal.** The backend quotes and executes Jupiter Swap API v2 on Solana mainnet. Buy swaps treasury USDC for an xStock mint and lands tokens in the group treasury. Sell swaps treasury xStock back to USDC so M4 redeem has a rail. Refuse symbols Jupiter cannot route. Confirm fills by polling `/execute` for `status: Success` and `code: 0`. Persist group trades as transaction rows.

M4 voting does not exist yet. Add a temporary dev-only execute path so you can buy without a vote. Delete that path in M4 when pass is the only gate.

**Depends on.** M2 complete. The treasury holds swept USDC. M1 Privy treasury signing.

**Owns.** Jupiter client, xStocks mint resolver, `StartBuy` gate for the dev stub route, transaction persistence in Postgres, temporary dev route, thin mobile debug control that calls the dev buy endpoint only.

## Structure

```text
apps/backend/
  internal/app/
    swap.go               DevExecuteBuy (M3 only)
    start_buy.go          StartBuy: dev stub route only in M3; passed vote only in M4
  internal/jupiter/         Jupiter client: QuoteBuy, ExecuteBuy, SellToUSDC
  internal/xstocks/         mint resolver from public API
  internal/postgres/
    transactions.go       idempotent on tx_signature / execute_request_id
  internal/httpapi/
    dev_buy.go            POST /v1/dev/groups/{id}/buy — DELETE in M4
```

## Flow

1. Resolve symbol → Solana mint via xStocks API (`deployments` where `network == Solana`).
2. Jupiter client `QuoteBuy` with USDC input mint. No route → refuse, no `transactions` row.
3. Only the dev buy route may call `StartBuy` (dev build flag).
4. `POST /v1/dev/groups/{id}/buy { symbol, usdc }`: group treasury signs via Privy, Jupiter execute, poll until `status: Success` and `code: 0`.
5. Insert `transactions` row with `action = buy`, `tx_signature` UNIQUE, optional `execute_request_id` UNIQUE, `cost_basis_price` and `cost_basis_amount` from fill.
6. Replay same signature: no duplicate row, no double cost basis effect.
7. Sell path (backend-only in M3): group treasury xStock → USDC via Jupiter client, same poll rule, `action = sell`. Prepares M4 redeem liquidation.
8. M4 deletes dev route. Only a passed vote may call `StartBuy`.

## Data models

Same **transactions** table as the M2/M3/M4 family. Replaces `jupiter_orders`, `fills`, and a separate `cost_basis` table.

- **transactions.** `group_id`, `amount`, `action` (`buy` or `sell`), `input_mint`, `output_mint`, `status`, `tx_signature` (unique idempotency key on confirm), `execute_request_id` (optional second unique key), `cost_basis_price`, `cost_basis_amount` (fill-derived, not live mark), `created_at`, `confirmed_at`. Group-level Jupiter swap. Tie to a buy proposal in M4 via nullable `proposal_id`.

Cost basis may also roll up on **positions** for display, but the fill record lives on the transaction row.

xStocks public API is mint metadata only. Resolve with `GET https://api.xstocks.fi/api/v2/public/assets/{symbol}`, then `deployments` where `network == Solana`, then `address`.

Measured examples. AAPLx is `XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp`. TSLAx is `XsDoVfqeBukxuZHWhdvWHBhgEHjGNst4MLodqsJHzoB`.

USDC input mint for buys is `EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v`. No Jupiter WebSocket. No Privy production webhooks.

## Parallelization

Milestone gate: complete **Wave 5** before M4.

| Wave | Tracks (run in parallel within the wave) | Gate for next wave |
|------|------------------------------------------|--------------------|
| **1** | **Schema** M3-T1 · **xStocks client** M3-T2 · **Logging** M3-T14 | M2 |
| **2** | **Jupiter quote** M3-T3, M3-T4 · **xStocks tests** M3-T16 | Wave 1 |
| **3** | **Parent buy execute** M3-T5 · **Sell path** M3-T9 | M3-T3 |
| **4** | **Dev stub route** M3-T11, M3-T12 · **Read APIs** M3-T13 · **Mobile debug** M3-T15 | M3-T5 |
| **5** | **Quote/execute tests** M3-T17–T21 · **just wiring** M3-T22 | Waves 3–4 |

Buy and sell tracks are parallel from Wave 3 once quote client exists. Jupiter quote tests (T17–T19) and integration tests (T20–T21) run in parallel in Wave 5.

## Tickets

1. M3-T1 Add the migration for transactions if not created in M2.
2. M3-T2 Implement the xStocks catalog client that resolves the Solana mint.
3. M3-T3 Implement the Jupiter v2 quote client for USDC `inputMint` and xStock `outputMint`.  
   Depends on: M3-T2
4. M3-T4 Refuse a quote when Jupiter returns no route.  
   Depends on: M3-T3
5. **Parent** M3-T5 Implement the buy transaction builder that signs the treasury via Privy and POSTs Jupiter execute.  
   Depends on: M3-T3
   - └ **Subissue of M3-T5** M3-T6 Poll Jupiter execute until Success with code 0 or a terminal failure, then persist the transaction row
   - └ **Subissue of M3-T5** M3-T7 Make buy execute idempotent on transaction signature or execute request id
   - └ **Subissue of M3-T5** M3-T8 Persist cost basis columns from fill price and output amount on a confirmed buy transaction
6. **Parent** M3-T9 Implement sell quote and execute from treasury xStock mint to USDC `outputMint`.  
   Depends on: M3-T3
   - └ **Subissue of M3-T9** M3-T10 Poll sell execute confirmation and persist USDC proceeds with idempotent signature upsert
7. M3-T11 Add a temporary dev-only `POST` execute-buy stub that takes group id and symbol with no vote gate.  
   Depends on: M3-T5
8. M3-T12 Mark that stub for deletion when M4 vote pass becomes the execute hook.  
   Depends on: M3-T11
9. M3-T13 Add GET endpoints for transaction status, treasury token balances, and cost basis by symbol.  
   Depends on: M3-T5, M3-T9
10. M3-T14 Add structured logs for quote, execute submit, poll transitions, and refusal.
11. M3-T15 Add a thin mobile debug control that calls `POST /v1/dev/groups/{id}/buy` for stub AAPLx buy. No Jupiter or xStocks calls from Swift.  
    Depends on: M3-T11
12. M3-T16 Add unit tests for xStocks mint parsing from sample AAPLx and TSLAx payloads.  
    Depends on: M3-T2
13. M3-T17 Add unit tests for Jupiter quote parsing, including no-route refusal.  
    Depends on: M3-T4
14. M3-T18 Add unit tests that execute success requires `status` Success and `code` 0.  
    Depends on: M3-T6
15. M3-T19 Add unit tests for execute failure and other terminal non-success states.  
    Depends on: M3-T6
16. M3-T20 Add an integration test that a duplicate buy signature does not double-record transaction or cost basis.  
    Depends on: M3-T7
17. M3-T21 Add an integration test that sell reduces xStock balance and increases treasury USDC.  
    Depends on: M3-T10
18. M3-T22 Wire `just test backend` to cover Jupiter quote, execute, and sell tests.  
    Depends on: M3-T16, M3-T17, M3-T18, M3-T19, M3-T20, M3-T21

## Ticket details

#### M3-T1: Add migration for transactions if not created in M2

**Context**
M3 follows M2 treasury USDC. Backend Jupiter buy/sell and dev stub route precede vote-gated execute in M4.

Ticket `M3-T1` is wave **1** in `docs/milestones/m3-jupiter.md` under milestone **M3 — Jupiter**.

**Problem**
SQL tables/columns for `M3-T1` are missing from `supabase/migrations/`; migration runner cannot apply them.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `supabase/migrations/`. No drive-by refactors.
2. Add next sequential SQL file under `supabase/migrations/` with tables/columns from `docs/milestones/m3-jupiter.md`.
3. Run migration via existing runner (`apps/backend/internal/postgres/`). Do not hand-apply in prod.
4. Satisfy milestone gate: `just test backend` and/or `just test mobile` exit 0 with no new failing tests.

**API / data contract**
**No HTTP.** SQL migration only. Apply via migration runner invoked by API boot and `just test backend`.
**Test fakes (locked names):**
- `fakeJupiterClient`
- `fakeXStocksResolver`
- `fakePrivyTreasurySigner`
- `integrationApp(t)`

### Scope
- `supabase/migrations/`

**Out of scope (do not touch)**
- Vote lifecycle
- proposal UI
- member voting
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
#### M3-T2: Implement xStocks catalog client that resolves the Solana mint

**Context**
M3 follows M2 treasury USDC. Backend Jupiter buy/sell and dev stub route precede vote-gated execute in M4.

Ticket `M3-T2` is wave **1** in `docs/milestones/m3-jupiter.md` under milestone **M3 — Jupiter**.

**Problem**
Behavior for `Implement xStocks catalog client that resolves the Solana mint` is not implemented under `apps/backend/internal/xstocks/`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/xstocks/`. No drive-by refactors.
2. Satisfy milestone gate: `just test backend` and/or `just test mobile` exit 0 with no new failing tests.

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `fakeJupiterClient`
- `fakeXStocksResolver`
- `fakePrivyTreasurySigner`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/xstocks/`

**Out of scope (do not touch)**
- Jupiter client
- HTTP handlers

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/xstocks/`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/xstocks/`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- None
- **Wave:** 1
#### M3-T3: Implement Jupiter v2 quote client for USDC inputMint and xStock outputMint

**Context**
M3 follows M2 treasury USDC. Backend Jupiter buy/sell and dev stub route precede vote-gated execute in M4.

Ticket `M3-T3` is wave **2** in `docs/milestones/m3-jupiter.md` under milestone **M3 — Jupiter**.
Blocked until dependencies land: M3-T2.

**Problem**
Behavior for `Implement Jupiter v2 quote client for USDC inputMint and xStock outputMint` is not implemented under `apps/backend/internal/jupiter/quote.go`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/jupiter/quote.go`. No drive-by refactors.
2. Satisfy milestone gate: `just test backend` and/or `just test mobile` exit 0 with no new failing tests.

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `fakeJupiterClient`
- `fakeXStocksResolver`
- `fakePrivyTreasurySigner`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/jupiter/quote.go`

**Out of scope (do not touch)**
- Execute/poll
- Mobile

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/jupiter/quote.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/jupiter/quote.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M3-T2 — **do not start while open**
- **Wave:** 2
#### M3-T4: Refuse quote when Jupiter returns no route

**Context**
M3 follows M2 treasury USDC. Backend Jupiter buy/sell and dev stub route precede vote-gated execute in M4.

Ticket `M3-T4` is wave **2** in `docs/milestones/m3-jupiter.md` under milestone **M3 — Jupiter**.
Blocked until dependencies land: M3-T3.

**Problem**
Behavior for `Refuse quote when Jupiter returns no route` is not implemented under `apps/backend/internal/jupiter/quote.go`, `apps/backend/internal/app/start_buy.go`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/jupiter/quote.go`, `apps/backend/internal/app/start_buy.go`. No drive-by refactors.
2. Add `TestJupiterQuoteBuy_noRoute_returnsRoutableFalse` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `fakeJupiterClient`
- `fakeXStocksResolver`
- `fakePrivyTreasurySigner`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/jupiter/quote.go`
- `apps/backend/internal/app/start_buy.go`

**Out of scope (do not touch)**
- Vote-gated execute

**Acceptance Criteria**
- [ ] Test `TestJupiterQuoteBuy_noRoute_returnsRoutableFalse` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/jupiter/quote.go`, `apps/backend/internal/app/start_buy.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestJupiterQuoteBuy_noRoute_returnsRoutableFalse` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/jupiter/quote.go`, `apps/backend/internal/app/start_buy.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M3-T3 — **do not start while open**
- **Wave:** 2
#### M3-T5: Implement buy transaction builder that signs treasury via Privy and POSTs Jupiter execute

**Context**
M3 follows M2 treasury USDC. Backend Jupiter buy/sell and dev stub route precede vote-gated execute in M4.

Ticket `M3-T5` is wave **3** in `docs/milestones/m3-jupiter.md` under milestone **M3 — Jupiter**.
Parent ticket; subissues: M3-T6, M3-T7, M3-T8.
Blocked until dependencies land: M3-T3.

**Problem**
Parent `M3-T5` orchestration in `apps/backend/internal/app/swap.go`, `apps/backend/internal/jupiter/execute.go` missing; subissues M3-T6, M3-T7, M3-T8 not wired together.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/app/swap.go`, `apps/backend/internal/jupiter/execute.go`. No drive-by refactors.
2. Parent glue: wire subissues M3-T6, M3-T7, M3-T8 into `apps/backend/internal/app/swap.go` after each subissue lands.
3. Satisfy milestone gate: `just test backend` and/or `just test mobile` exit 0 with no new failing tests.

**API / data contract**
**Parent orchestration.** Public contract is union of subissue routes/symbols: M3-T6, M3-T7, M3-T8.
**Test fakes (locked names):**
- `fakeJupiterClient`
- `fakeXStocksResolver`
- `fakePrivyTreasurySigner`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/app/swap.go`
- `apps/backend/internal/jupiter/execute.go`

**Out of scope (do not touch)**
- Vote gate
- Mobile Jupiter calls

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/swap.go`, `apps/backend/internal/jupiter/execute.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] All subissues (M3-T6, M3-T7, M3-T8) closed before parent closes.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/swap.go`, `apps/backend/internal/jupiter/execute.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] All subissues (M3-T6, M3-T7, M3-T8) closed before parent closes.
- [ ] Subissues closed: M3-T6, M3-T7, M3-T8.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M3-T3 — **do not start while open**
- **Wave:** 3
- **Subissue:** M3-T6
- **Subissue:** M3-T7
- **Subissue:** M3-T8
#### M3-T6: Poll Jupiter execute until Success with code 0 or terminal failure, then persist transaction row

**Context**
M3 follows M2 treasury USDC. Backend Jupiter buy/sell and dev stub route precede vote-gated execute in M4.

Ticket `M3-T6` is wave **3** in `docs/milestones/m3-jupiter.md` under milestone **M3 — Jupiter**.
Subissue of M3-T5.
Blocked until dependencies land: M3-T5.

**Problem**
Subissue `M3-T6` of `M3-T5`: symbols under `apps/backend/internal/jupiter/poll.go`, `apps/backend/internal/postgres/transactions.go` not implemented; parent cannot close.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/jupiter/poll.go`, `apps/backend/internal/postgres/transactions.go`. No drive-by refactors.
2. Subissue of `M3-T5`. Do not add routes or flows owned by parent/sibling subissues.
3. Add `TestJupiterExecute_successRequiresStatusSuccessAndCodeZero` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
4. Add `TestJupiterPoll_transitionsFromPendingToSuccess` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
5. Add `TestJupiterExecute_terminalFailure_doesNotPersistConfirmedRow` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new public route.** Subissue of `M3-T5`. Internal symbols only in **Owns** paths.
**Test fakes (locked names):**
- `fakeJupiterClient`
- `fakeXStocksResolver`
- `fakePrivyTreasurySigner`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/jupiter/poll.go`
- `apps/backend/internal/postgres/transactions.go`

**Out of scope (do not touch)**
- Sell path

**Acceptance Criteria**
- [ ] Test `TestJupiterExecute_successRequiresStatusSuccessAndCodeZero` exists, uses AAA comments, and passes.
- [ ] Test `TestJupiterPoll_transitionsFromPendingToSuccess` exists, uses AAA comments, and passes.
- [ ] Test `TestJupiterExecute_terminalFailure_doesNotPersistConfirmedRow` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/jupiter/poll.go`, `apps/backend/internal/postgres/transactions.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestJupiterExecute_successRequiresStatusSuccessAndCodeZero` exists, uses AAA comments, and passes.
- [ ] Test `TestJupiterPoll_transitionsFromPendingToSuccess` exists, uses AAA comments, and passes.
- [ ] Test `TestJupiterExecute_terminalFailure_doesNotPersistConfirmedRow` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/jupiter/poll.go`, `apps/backend/internal/postgres/transactions.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M3-T5 — **do not start while open**
- **Wave:** 3
- **Parent:** M3-T5
#### M3-T7: Make buy execute idempotent on transaction signature or execute request id

**Context**
M3 follows M2 treasury USDC. Backend Jupiter buy/sell and dev stub route precede vote-gated execute in M4.

Ticket `M3-T7` is wave **3** in `docs/milestones/m3-jupiter.md` under milestone **M3 — Jupiter**.
Subissue of M3-T5.
Blocked until dependencies land: M3-T6.

**Problem**
Subissue `M3-T7` of `M3-T5`: symbols under `apps/backend/internal/postgres/transactions.go` not implemented; parent cannot close.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/postgres/transactions.go`. No drive-by refactors.
2. Subissue of `M3-T5`. Do not add routes or flows owned by parent/sibling subissues.
3. Add `TestDevExecuteBuy_duplicateTxSignature_doesNotDoubleRecordOrCostBasis` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
4. Add `TestDevExecuteBuy_duplicateExecuteRequestId_isIdempotent` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
5. Add `TestProperty_confirmedBuyExactlyOneTransactionRowPerSignature` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new public route.** Subissue of `M3-T5`. Internal symbols only in **Owns** paths.
**Test fakes (locked names):**
- `fakeJupiterClient`
- `fakeXStocksResolver`
- `fakePrivyTreasurySigner`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/postgres/transactions.go`

**Out of scope (do not touch)**
- Sell idempotency (M3-T10)

**Acceptance Criteria**
- [ ] Test `TestDevExecuteBuy_duplicateTxSignature_doesNotDoubleRecordOrCostBasis` exists, uses AAA comments, and passes.
- [ ] Test `TestDevExecuteBuy_duplicateExecuteRequestId_isIdempotent` exists, uses AAA comments, and passes.
- [ ] Test `TestProperty_confirmedBuyExactlyOneTransactionRowPerSignature` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/postgres/transactions.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestDevExecuteBuy_duplicateTxSignature_doesNotDoubleRecordOrCostBasis` exists, uses AAA comments, and passes.
- [ ] Test `TestDevExecuteBuy_duplicateExecuteRequestId_isIdempotent` exists, uses AAA comments, and passes.
- [ ] Test `TestProperty_confirmedBuyExactlyOneTransactionRowPerSignature` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/postgres/transactions.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M3-T6 — **do not start while open**
- **Wave:** 3
- **Parent:** M3-T5
#### M3-T8: Persist cost basis columns from fill price and output amount on confirmed buy transaction

**Context**
M3 follows M2 treasury USDC. Backend Jupiter buy/sell and dev stub route precede vote-gated execute in M4.

Ticket `M3-T8` is wave **3** in `docs/milestones/m3-jupiter.md` under milestone **M3 — Jupiter**.
Subissue of M3-T5.
Blocked until dependencies land: M3-T6.

**Problem**
Subissue `M3-T8` of `M3-T5`: symbols under `apps/backend/internal/postgres/transactions.go` not implemented; parent cannot close.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/postgres/transactions.go`. No drive-by refactors.
2. Subissue of `M3-T5`. Do not add routes or flows owned by parent/sibling subissues.
3. Add `TestDevExecuteBuy_happyPath_insertsOneTransactionRowWithCostBasis` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
4. Add `TestProperty_costBasisAmountMatchesFillOutput` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new public route.** Subissue of `M3-T5`. Internal symbols only in **Owns** paths.
**Test fakes (locked names):**
- `fakeJupiterClient`
- `fakeXStocksResolver`
- `fakePrivyTreasurySigner`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/postgres/transactions.go`

**Out of scope (do not touch)**
- Live Pyth marks

**Acceptance Criteria**
- [ ] Test `TestDevExecuteBuy_happyPath_insertsOneTransactionRowWithCostBasis` exists, uses AAA comments, and passes.
- [ ] Test `TestProperty_costBasisAmountMatchesFillOutput` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/postgres/transactions.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestDevExecuteBuy_happyPath_insertsOneTransactionRowWithCostBasis` exists, uses AAA comments, and passes.
- [ ] Test `TestProperty_costBasisAmountMatchesFillOutput` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/postgres/transactions.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M3-T6 — **do not start while open**
- **Wave:** 3
- **Parent:** M3-T5
#### M3-T9: Implement sell quote and execute from treasury xStock mint to USDC outputMint

**Context**
M3 follows M2 treasury USDC. Backend Jupiter buy/sell and dev stub route precede vote-gated execute in M4.

Ticket `M3-T9` is wave **3** in `docs/milestones/m3-jupiter.md` under milestone **M3 — Jupiter**.
Parent ticket; subissues: M3-T10.
Blocked until dependencies land: M3-T3.

**Problem**
Parent `M3-T9` orchestration in `apps/backend/internal/jupiter/sell.go`, `apps/backend/internal/app/swap.go` missing; subissues M3-T10 not wired together.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/jupiter/sell.go`, `apps/backend/internal/app/swap.go`. No drive-by refactors.
2. Parent glue: wire subissues M3-T10 into `apps/backend/internal/jupiter/sell.go` after each subissue lands.
3. Satisfy milestone gate: `just test backend` and/or `just test mobile` exit 0 with no new failing tests.

**API / data contract**
**Parent orchestration.** Public contract is union of subissue routes/symbols: M3-T10.
**Test fakes (locked names):**
- `fakeJupiterClient`
- `fakeXStocksResolver`
- `fakePrivyTreasurySigner`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/jupiter/sell.go`
- `apps/backend/internal/app/swap.go`

**Out of scope (do not touch)**
- Redeem payout to member wallet

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/jupiter/sell.go`, `apps/backend/internal/app/swap.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] All subissues (M3-T10) closed before parent closes.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/jupiter/sell.go`, `apps/backend/internal/app/swap.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] All subissues (M3-T10) closed before parent closes.
- [ ] Subissues closed: M3-T10.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M3-T3 — **do not start while open**
- **Wave:** 3
- **Subissue:** M3-T10
#### M3-T10: Poll sell execute confirmation and persist USDC proceeds with idempotent signature upsert

**Context**
M3 follows M2 treasury USDC. Backend Jupiter buy/sell and dev stub route precede vote-gated execute in M4.

Ticket `M3-T10` is wave **3** in `docs/milestones/m3-jupiter.md` under milestone **M3 — Jupiter**.
Subissue of M3-T9.
Blocked until dependencies land: M3-T9.

**Problem**
Subissue `M3-T10` of `M3-T9`: symbols under `apps/backend/internal/jupiter/poll.go`, `apps/backend/internal/postgres/transactions.go` not implemented; parent cannot close.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/jupiter/poll.go`, `apps/backend/internal/postgres/transactions.go`. No drive-by refactors.
2. Subissue of `M3-T9`. Do not add routes or flows owned by parent/sibling subissues.
3. Add `TestSellToUSDC_happyPath_reducesXStockAndIncreasesTreasuryUsdc` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
4. Add `TestSellToUSDC_duplicateSignature_doesNotDoubleApply` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new public route.** Subissue of `M3-T9`. Internal symbols only in **Owns** paths.
**Test fakes (locked names):**
- `fakeJupiterClient`
- `fakeXStocksResolver`
- `fakePrivyTreasurySigner`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/jupiter/poll.go`
- `apps/backend/internal/postgres/transactions.go`

**Out of scope (do not touch)**
- Member payout

**Acceptance Criteria**
- [ ] Test `TestSellToUSDC_happyPath_reducesXStockAndIncreasesTreasuryUsdc` exists, uses AAA comments, and passes.
- [ ] Test `TestSellToUSDC_duplicateSignature_doesNotDoubleApply` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/jupiter/poll.go`, `apps/backend/internal/postgres/transactions.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestSellToUSDC_happyPath_reducesXStockAndIncreasesTreasuryUsdc` exists, uses AAA comments, and passes.
- [ ] Test `TestSellToUSDC_duplicateSignature_doesNotDoubleApply` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/jupiter/poll.go`, `apps/backend/internal/postgres/transactions.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M3-T9 — **do not start while open**
- **Wave:** 3
- **Parent:** M3-T9
#### M3-T11: Add temporary dev-only POST execute-buy stub with group id and symbol, no vote gate

**Context**
M3 follows M2 treasury USDC. Backend Jupiter buy/sell and dev stub route precede vote-gated execute in M4.

Ticket `M3-T11` is wave **4** in `docs/milestones/m3-jupiter.md` under milestone **M3 — Jupiter**.
Blocked until dependencies land: M3-T5.

**Problem**
Behavior for `Add temporary dev-only POST execute-buy stub with group id and symbol, no vote gate` is not implemented under `apps/backend/internal/httpapi/dev_buy.go`, `apps/backend/internal/app/start_buy.go`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/httpapi/dev_buy.go`, `apps/backend/internal/app/start_buy.go`. No drive-by refactors.
2. Add `TestStartBuy_devRouteAllowed_whenDevFlagSet` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
3. Add `TestPOST_devBuy_withoutDevFlag_returns404` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `fakeJupiterClient`
- `fakeXStocksResolver`
- `fakePrivyTreasurySigner`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/httpapi/dev_buy.go`
- `apps/backend/internal/app/start_buy.go`

**Out of scope (do not touch)**
- Production vote gate

**Acceptance Criteria**
- [ ] Test `TestStartBuy_devRouteAllowed_whenDevFlagSet` exists, uses AAA comments, and passes.
- [ ] Test `TestPOST_devBuy_withoutDevFlag_returns404` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/httpapi/dev_buy.go`, `apps/backend/internal/app/start_buy.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestStartBuy_devRouteAllowed_whenDevFlagSet` exists, uses AAA comments, and passes.
- [ ] Test `TestPOST_devBuy_withoutDevFlag_returns404` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/httpapi/dev_buy.go`, `apps/backend/internal/app/start_buy.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M3-T5 — **do not start while open**
- **Wave:** 4
#### M3-T12: Mark dev stub for deletion when M4 vote pass becomes execute hook

**Context**
M3 follows M2 treasury USDC. Backend Jupiter buy/sell and dev stub route precede vote-gated execute in M4.

Ticket `M3-T12` is wave **4** in `docs/milestones/m3-jupiter.md` under milestone **M3 — Jupiter**.
Blocked until dependencies land: M3-T11.

**Problem**
Behavior for `Mark dev stub for deletion when M4 vote pass becomes execute hook` is not implemented under `apps/backend/internal/httpapi/dev_buy.go`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/httpapi/dev_buy.go`. No drive-by refactors.
2. Satisfy milestone gate: `just test backend` and/or `just test mobile` exit 0 with no new failing tests.

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `fakeJupiterClient`
- `fakeXStocksResolver`
- `fakePrivyTreasurySigner`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/httpapi/dev_buy.go`

**Out of scope (do not touch)**
- Implementing M4 delete (tracked in M4-T19)

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/httpapi/dev_buy.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/httpapi/dev_buy.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M3-T11 — **do not start while open**
- **Wave:** 4
#### M3-T13: Add GET endpoints for transaction status, treasury token balances, and cost basis by symbol

**Context**
M3 follows M2 treasury USDC. Backend Jupiter buy/sell and dev stub route precede vote-gated execute in M4.

Ticket `M3-T13` is wave **4** in `docs/milestones/m3-jupiter.md` under milestone **M3 — Jupiter**.
Blocked until dependencies land: M3-T5, M3-T9.

**Problem**
Behavior for `Add GET endpoints for transaction status, treasury token balances, and cost basis by symbol` is not implemented under `apps/backend/internal/httpapi/transactions.go`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/httpapi/transactions.go`. No drive-by refactors.
2. Add `TestGET_transactionStatus_returnsConfirmedFill` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
3. Add `TestGET_treasuryTokenBalances_reflectsPostBuyHoldings` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
4. Add `TestGET_costBasisBySymbol_returnsFillDerivedBasis` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `fakeJupiterClient`
- `fakeXStocksResolver`
- `fakePrivyTreasurySigner`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/httpapi/transactions.go`

**Out of scope (do not touch)**
- Mobile catalog search

**Acceptance Criteria**
- [ ] Test `TestGET_transactionStatus_returnsConfirmedFill` exists, uses AAA comments, and passes.
- [ ] Test `TestGET_treasuryTokenBalances_reflectsPostBuyHoldings` exists, uses AAA comments, and passes.
- [ ] Test `TestGET_costBasisBySymbol_returnsFillDerivedBasis` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/httpapi/transactions.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestGET_transactionStatus_returnsConfirmedFill` exists, uses AAA comments, and passes.
- [ ] Test `TestGET_treasuryTokenBalances_reflectsPostBuyHoldings` exists, uses AAA comments, and passes.
- [ ] Test `TestGET_costBasisBySymbol_returnsFillDerivedBasis` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/httpapi/transactions.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M3-T5 — **do not start while open**
- M3-T9 — **do not start while open**
- **Wave:** 4
#### M3-T14: Add structured logs for quote, execute submit, poll transitions, and refusal

**Context**
M3 follows M2 treasury USDC. Backend Jupiter buy/sell and dev stub route precede vote-gated execute in M4.

Ticket `M3-T14` is wave **1** in `docs/milestones/m3-jupiter.md` under milestone **M3 — Jupiter**.

**Problem**
Behavior for `Add structured logs for quote, execute submit, poll transitions, and refusal` is not implemented under `apps/backend/internal/jupiter/`, `apps/backend/internal/app/`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/jupiter/`, `apps/backend/internal/app/`. No drive-by refactors.
2. Add structured log lines at attempt/confirm/credit (or quote/execute/poll) transitions using existing logger; fields: `group_id`, `user_id`, `deposit_id`/`tx_signature`/`symbol` as applicable.
3. Satisfy milestone gate: `just test backend` and/or `just test mobile` exit 0 with no new failing tests.

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `fakeJupiterClient`
- `fakeXStocksResolver`
- `fakePrivyTreasurySigner`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/jupiter/`
- `apps/backend/internal/app/`

**Out of scope (do not touch)**
- WebSocket client

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/jupiter/`, `apps/backend/internal/app/`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/jupiter/`, `apps/backend/internal/app/`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- None
- **Wave:** 1
#### M3-T15: Add thin mobile debug control calling POST /v1/dev/groups/{id}/buy for stub AAPLx buy

**Context**
M3 follows M2 treasury USDC. Backend Jupiter buy/sell and dev stub route precede vote-gated execute in M4.

Ticket `M3-T15` is wave **4** in `docs/milestones/m3-jupiter.md` under milestone **M3 — Jupiter**.
Blocked until dependencies land: M3-T11.

**Problem**
`POST /v1/dev/groups/{id}/buy` and handler code under `apps/mobile/Features/Debug/` do not exist (or fail locked tests).

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Features/Debug/`. No drive-by refactors.
2. Register `POST /v1/dev/groups/{id}/buy` in route table (`apps/backend/cmd/api/` or `internal/httpapi/`).
3. Implement handler in `apps/mobile/Features/Debug/` calling `internal/app` method; return JSON status codes from **API / data contract** below.
4. Build SwiftUI screen in **Owns**; call `MonacoAPIClient` only (no xStocks/Jupiter/Pyth/Solana RPC).
5. Compile gate: `just build mobile` and `just test mobile` exit 0.

**API / data contract**
**Route:** `POST /v1/dev/groups/{id}/buy`
**Auth:** Privy bearer token in `Authorization` header (same as M1 session middleware).
**Happy:** 200 with JSON body defined in milestone doc for this route.
**Failure codes:** 401 missing/invalid auth; 400 invalid body; 403 non-member; 404 unknown resource. 404 when `DEV_BUY_ENABLED` is false.
**Idempotency:** follow milestone doc keys (`tx_signature`, `execute_request_id`, `privy_user_id`, etc.).
**Test fakes (locked names):**
- `fakeJupiterClient`
- `fakeXStocksResolver`
- `fakePrivyTreasurySigner`
- `integrationApp(t)`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/Features/Debug/`
- Route: `POST /v1/dev/groups/{id}/buy`

**Out of scope (do not touch)**
- Swift calls to Jupiter or xStocks

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Debug/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Debug/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M3-T11 — **do not start while open**
- **Wave:** 4
#### M3-T16: Add unit tests for xStocks mint parsing from sample AAPLx and TSLAx payloads

**Context**
M3 follows M2 treasury USDC. Backend Jupiter buy/sell and dev stub route precede vote-gated execute in M4.

Ticket `M3-T16` is wave **2** in `docs/milestones/m3-jupiter.md` under milestone **M3 — Jupiter**.
Blocked until dependencies land: M3-T2.

**Problem**
Locked test names for `M3-T16` are absent or not wired into `just test` recipes; `apps/backend/internal/xstocks/*_test.go` incomplete.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/xstocks/*_test.go`. No drive-by refactors.
2. Add `TestXStocksResolver_AAPLx_returnsSolanaMintFromDeployments` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
3. Add `TestXStocksResolver_TSLAx_returnsSolanaMintFromDeployments` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
4. Add `TestXStocksResolver_missingSolanaDeployment_returnsError` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new API.** Tests and/or `Justfile` wiring only.
**Test fakes (locked names):**
- `fakeJupiterClient`
- `fakeXStocksResolver`
- `fakePrivyTreasurySigner`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/xstocks/*_test.go`

**Out of scope (do not touch)**
- Mainnet HTTP

**Acceptance Criteria**
- [ ] Test `TestXStocksResolver_AAPLx_returnsSolanaMintFromDeployments` exists, uses AAA comments, and passes.
- [ ] Test `TestXStocksResolver_TSLAx_returnsSolanaMintFromDeployments` exists, uses AAA comments, and passes.
- [ ] Test `TestXStocksResolver_missingSolanaDeployment_returnsError` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/xstocks/*_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestXStocksResolver_AAPLx_returnsSolanaMintFromDeployments` exists, uses AAA comments, and passes.
- [ ] Test `TestXStocksResolver_TSLAx_returnsSolanaMintFromDeployments` exists, uses AAA comments, and passes.
- [ ] Test `TestXStocksResolver_missingSolanaDeployment_returnsError` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/xstocks/*_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M3-T2 — **do not start while open**
- **Wave:** 2
#### M3-T17: Add unit tests for Jupiter quote parsing, including no-route refusal

**Context**
M3 follows M2 treasury USDC. Backend Jupiter buy/sell and dev stub route precede vote-gated execute in M4.

Ticket `M3-T17` is wave **5** in `docs/milestones/m3-jupiter.md` under milestone **M3 — Jupiter**.
Blocked until dependencies land: M3-T4.

**Problem**
Locked test names for `M3-T17` are absent or not wired into `just test` recipes; `apps/backend/internal/jupiter/*_test.go` incomplete.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/jupiter/*_test.go`. No drive-by refactors.
2. Add `TestJupiterQuoteBuy_validRoute_returnsQuoteWithUsdcInputMint` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
3. Add `TestJupiterQuoteBuy_noRoute_returnsRoutableFalse` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new API.** Tests and/or `Justfile` wiring only.
**Test fakes (locked names):**
- `fakeJupiterClient`
- `fakeXStocksResolver`
- `fakePrivyTreasurySigner`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/jupiter/*_test.go`

**Out of scope (do not touch)**
- Execute poll tests

**Acceptance Criteria**
- [ ] Test `TestJupiterQuoteBuy_validRoute_returnsQuoteWithUsdcInputMint` exists, uses AAA comments, and passes.
- [ ] Test `TestJupiterQuoteBuy_noRoute_returnsRoutableFalse` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/jupiter/*_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestJupiterQuoteBuy_validRoute_returnsQuoteWithUsdcInputMint` exists, uses AAA comments, and passes.
- [ ] Test `TestJupiterQuoteBuy_noRoute_returnsRoutableFalse` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/jupiter/*_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M3-T4 — **do not start while open**
- **Wave:** 5
#### M3-T18: Add unit tests that execute success requires status Success and code 0

**Context**
M3 follows M2 treasury USDC. Backend Jupiter buy/sell and dev stub route precede vote-gated execute in M4.

Ticket `M3-T18` is wave **5** in `docs/milestones/m3-jupiter.md` under milestone **M3 — Jupiter**.
Blocked until dependencies land: M3-T6.

**Problem**
Locked test names for `M3-T18` are absent or not wired into `just test` recipes; `apps/backend/internal/jupiter/*_test.go` incomplete.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/jupiter/*_test.go`. No drive-by refactors.
2. Add `TestJupiterExecute_successRequiresStatusSuccessAndCodeZero` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new API.** Tests and/or `Justfile` wiring only.
**Test fakes (locked names):**
- `fakeJupiterClient`
- `fakeXStocksResolver`
- `fakePrivyTreasurySigner`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/jupiter/*_test.go`

**Out of scope (do not touch)**
- Integration buy path

**Acceptance Criteria**
- [ ] Test `TestJupiterExecute_successRequiresStatusSuccessAndCodeZero` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/jupiter/*_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestJupiterExecute_successRequiresStatusSuccessAndCodeZero` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/jupiter/*_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M3-T6 — **do not start while open**
- **Wave:** 5
#### M3-T19: Add unit tests for execute failure and other terminal non-success states

**Context**
M3 follows M2 treasury USDC. Backend Jupiter buy/sell and dev stub route precede vote-gated execute in M4.

Ticket `M3-T19` is wave **5** in `docs/milestones/m3-jupiter.md` under milestone **M3 — Jupiter**.
Blocked until dependencies land: M3-T6.

**Problem**
Locked test names for `M3-T19` are absent or not wired into `just test` recipes; `apps/backend/internal/jupiter/*_test.go` incomplete.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/jupiter/*_test.go`. No drive-by refactors.
2. Add `TestJupiterExecute_terminalFailure_doesNotPersistConfirmedRow` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
3. Add `TestJupiterPoll_timeoutOrNonZeroCode_marksTransactionFailed` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new API.** Tests and/or `Justfile` wiring only.
**Test fakes (locked names):**
- `fakeJupiterClient`
- `fakeXStocksResolver`
- `fakePrivyTreasurySigner`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/jupiter/*_test.go`

**Out of scope (do not touch)**
- Failed execute retry policy (needs-decision M4-T40)

**Acceptance Criteria**
- [ ] Test `TestJupiterExecute_terminalFailure_doesNotPersistConfirmedRow` exists, uses AAA comments, and passes.
- [ ] Test `TestJupiterPoll_timeoutOrNonZeroCode_marksTransactionFailed` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/jupiter/*_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestJupiterExecute_terminalFailure_doesNotPersistConfirmedRow` exists, uses AAA comments, and passes.
- [ ] Test `TestJupiterPoll_timeoutOrNonZeroCode_marksTransactionFailed` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/jupiter/*_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M3-T6 — **do not start while open**
- **Wave:** 5
#### M3-T20: Add integration test that duplicate buy signature does not double-record transaction or cost basis

**Context**
M3 follows M2 treasury USDC. Backend Jupiter buy/sell and dev stub route precede vote-gated execute in M4.

Ticket `M3-T20` is wave **5** in `docs/milestones/m3-jupiter.md` under milestone **M3 — Jupiter**.
Blocked until dependencies land: M3-T7.

**Problem**
Locked test names for `M3-T20` are absent or not wired into `just test` recipes; `apps/backend/internal/app/swap_integration_test.go` incomplete.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/app/swap_integration_test.go`. No drive-by refactors.
2. Add `TestDevExecuteBuy_duplicateTxSignature_doesNotDoubleRecordOrCostBasis` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new API.** Tests and/or `Justfile` wiring only.
**Test fakes (locked names):**
- `fakeJupiterClient`
- `fakeXStocksResolver`
- `fakePrivyTreasurySigner`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/app/swap_integration_test.go`

**Out of scope (do not touch)**
- Sell integration

**Acceptance Criteria**
- [ ] Test `TestDevExecuteBuy_duplicateTxSignature_doesNotDoubleRecordOrCostBasis` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/swap_integration_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestDevExecuteBuy_duplicateTxSignature_doesNotDoubleRecordOrCostBasis` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/swap_integration_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M3-T7 — **do not start while open**
- **Wave:** 5
#### M3-T21: Add integration test that sell reduces xStock balance and increases treasury USDC

**Context**
M3 follows M2 treasury USDC. Backend Jupiter buy/sell and dev stub route precede vote-gated execute in M4.

Ticket `M3-T21` is wave **5** in `docs/milestones/m3-jupiter.md` under milestone **M3 — Jupiter**.
Blocked until dependencies land: M3-T10.

**Problem**
Locked test names for `M3-T21` are absent or not wired into `just test` recipes; `apps/backend/internal/app/swap_integration_test.go` incomplete.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/app/swap_integration_test.go`. No drive-by refactors.
2. Add `TestSellToUSDC_happyPath_reducesXStockAndIncreasesTreasuryUsdc` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new API.** Tests and/or `Justfile` wiring only.
**Test fakes (locked names):**
- `fakeJupiterClient`
- `fakeXStocksResolver`
- `fakePrivyTreasurySigner`
- `integrationApp(t)`

### Scope
- `apps/backend/internal/app/swap_integration_test.go`

**Out of scope (do not touch)**
- Redeem payout

**Acceptance Criteria**
- [ ] Test `TestSellToUSDC_happyPath_reducesXStockAndIncreasesTreasuryUsdc` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/swap_integration_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestSellToUSDC_happyPath_reducesXStockAndIncreasesTreasuryUsdc` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/swap_integration_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M3-T10 — **do not start while open**
- **Wave:** 5
#### M3-T22: Wire just test backend to cover Jupiter quote, execute, and sell tests

**Context**
M3 follows M2 treasury USDC. Backend Jupiter buy/sell and dev stub route precede vote-gated execute in M4.

Ticket `M3-T22` is wave **5** in `docs/milestones/m3-jupiter.md` under milestone **M3 — Jupiter**.
Blocked until dependencies land: M3-T16, M3-T17, M3-T18, M3-T19, M3-T20, M3-T21.

**Problem**
Locked test names for `M3-T22` are absent or not wired into `just test` recipes; `Justfile` incomplete.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `Justfile`. No drive-by refactors.
2. Update root `Justfile` recipes so locked tests run under `just test backend` and/or `just test mobile`. Exit 0 required.
3. Satisfy milestone gate: `just test backend` and/or `just test mobile` exit 0 with no new failing tests.

**API / data contract**
**No new API.** Tests and/or `Justfile` wiring only.
**Test fakes (locked names):**
- `fakeJupiterClient`
- `fakeXStocksResolver`
- `fakePrivyTreasurySigner`
- `integrationApp(t)`

### Scope
- `Justfile`

**Out of scope (do not touch)**
- just test mobile beyond M3-T15

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
- M3-T16 — **do not start while open**
- M3-T17 — **do not start while open**
- M3-T18 — **do not start while open**
- M3-T19 — **do not start while open**
- M3-T20 — **do not start while open**
- M3-T21 — **do not start while open**
- **Wave:** 5

## Automated verification

**Commands.** `just test backend`.

### Test layers

| Layer | Runs under | What it proves |
|-------|------------|----------------|
| **Go unit** | `just test backend` | xStocks mint parsing, Jupiter quote/execute/poll parsing, `StartBuy` dev gate, no-route refusal — **fake Jupiter** and **fake xStocks** HTTP, no network. |
| **Go integration** | `just test backend` | Dev buy route and sell path against local Postgres with fake Jupiter + fake Privy treasury sign; idempotency on `tx_signature` and `execute_request_id`. |
| **Boundary** | `just test mobile` (M3-T15) | Swift debug control calls only `POST /v1/dev/groups/{id}/buy`; no Jupiter/xStocks URLs in mobile test bundle (lint or unit assertion on API client base URL). |

No Jupiter WebSocket client in the suite. Tests assert literal mint addresses (`AAPLx`, `TSLAx`), `status: Success`, `code: 0`, and transaction row counts.

### Test map

**M3-T2 — xStocks mint resolver (unit)** — M3-T16

- `TestXStocksResolver_AAPLx_returnsSolanaMintFromDeployments`
- `TestXStocksResolver_TSLAx_returnsSolanaMintFromDeployments`
- `TestXStocksResolver_missingSolanaDeployment_returnsError`
- `TestXStocksResolver_malformedPayload_returnsError`

**M3-T3 / M3-T4 — Jupiter quote (unit)** — M3-T17

- `TestJupiterQuoteBuy_validRoute_returnsQuoteWithUsdcInputMint`
- `TestJupiterQuoteBuy_noRoute_returnsRoutableFalse` (M3-T4)
- `TestJupiterQuoteBuy_httpError_propagatesAsRefusal`

**M3-T5 / M3-T6 / M3-T18 / M3-T19 — execute and poll (unit)**

- `TestJupiterExecute_successRequiresStatusSuccessAndCodeZero` (M3-T18)
- `TestJupiterExecute_terminalFailure_doesNotPersistConfirmedRow` (M3-T19)
- `TestJupiterPoll_transitionsFromPendingToSuccess`
- `TestJupiterPoll_timeoutOrNonZeroCode_marksTransactionFailed`

**M3-T7 / M3-T8 — persistence (integration)**

- `TestDevExecuteBuy_happyPath_insertsOneTransactionRowWithCostBasis` (M3-T8)
- `TestDevExecuteBuy_duplicateTxSignature_doesNotDoubleRecordOrCostBasis` (M3-T20)
- `TestDevExecuteBuy_duplicateExecuteRequestId_isIdempotent` (M3-T7)

**M3-T9 / M3-T10 — sell path (integration)** — M3-T21

- `TestSellToUSDC_happyPath_reducesXStockAndIncreasesTreasuryUsdc`
- `TestSellToUSDC_duplicateSignature_doesNotDoubleApply`

**M3-T11 / M3-T12 — dev stub gate (unit + integration)**

- `TestStartBuy_devRouteAllowed_whenDevFlagSet`
- `TestStartBuy_devRouteBlocked_whenDevFlagUnset`
- `TestPOST_devBuy_withoutDevFlag_returns404`

**M3-T13 — read APIs (integration)**

- `TestGET_transactionStatus_returnsConfirmedFill`
- `TestGET_treasuryTokenBalances_reflectsPostBuyHoldings`
- `TestGET_costBasisBySymbol_returnsFillDerivedBasis`

**Product constraints encoded in tests**

- Backend-only Jupiter and xStocks; Swift never calls those hosts.
- USDC input mint `EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v`.
- No vote gate in M3 (dev stub only).

### Fixtures / fakes

- `fakeXStocksServer` — serves fixture JSON for `AAPLx` and `TSLAx` with Solana `deployments`.
- `fakeJupiterClient` — `QuoteBuy`, `ExecuteBuy`, `SellToUSDC`, configurable poll responses.
- `fixtureJupiterSuccessResponse`, `fixtureJupiterNoRoute`, `fixtureJupiterFailureResponse`.
- `buildTransaction(overrides)`, `buildTreasuryWithUsdc(overrides)`.
- `fakePrivyClient.SignTreasuryTransaction` — returns deterministic signatures.

**Testability note.** Jupiter and xStocks clients must be interfaces injected into `internal/app` so quote/refusal tests do not hit mainnet.

### Property / invariant tests

- `TestProperty_confirmedBuyExactlyOneTransactionRowPerSignature` — replay random valid signatures; row count stays 1.
- `TestProperty_costBasisAmountMatchesFillOutput` — for generated fill payloads, persisted `cost_basis_amount` matches parsed output.

## Manual verification

Mainnet treasury balance, explorer swap confirmation, and log tailing only.

1. Confirm the group treasury holds swept mainnet USDC from M2.
2. Trigger the temporary stub buy for `AAPLx` with a small USDC notional.
3. Watch logs for quote, Privy treasury sign, execute POST, and poll until Success code 0.
4. Open the explorer for the swap signature. Confirm the xStock balance rose on the treasury.
5. Sell a fraction of treasury `AAPLx` back to USDC on the backend sell path.
6. Confirm poll reaches Success code 0, xStock fell, and treasury USDC rose in explorer or Privy.

## Out of scope

Vote lifecycle, proposal UI, member voting, redeem payout to an external address, leaderboards, P and L percent boards, Pyth marks, chart history, Jupiter WebSocket, Privy production webhooks.

## Open decisions

- Failed `/execute` after a passed vote stays unlocked. Do not pick retry, refund, or manual reconcile in M3. M4 records the decision ticket.
- Who may propose a buy is an M4 UI and API rule. The M3 stub is dev-only.

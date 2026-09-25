# M4. Domain logic

**Goal.** Replace M1 to M3 stubs with real group rules, vote lifecycle, marked pot NAV, redeem, P and L, and both leaderboards. Postgres and the Go API are the source of truth. Jupiter runs when a proposal passes. Votes are not on-chain.

**Depends on.** M1 wallets, M2 deposit credit on positions, M3 Jupiter buy and sell plus the stub execute hook.

**Owns.** `apps/backend/internal/app` full commands, `packages/domain` math, remaining Postgres migrations, `StartBuy` for passed votes only, `RedeemJob` executor, deletion of M3 stub buy path.

## Structure

```text
packages/domain/
  nav.go          NavMarked, ComputePotNAV
  votes.go        TallyProposal, ProposalStatus
  redeem.go       ComputeRedeemSlice, RedeemJobStatus
  pnl.go          PercentReturn, board rank helpers
apps/backend/
  internal/app/
    governance.go   ProposeBuy, Vote, join
    redeem.go       Redeem → RedeemJob state machine
    views.go        GroupView, HomeView
    start_buy.go    StartBuy: passed vote only (replaces dev stub gate)
  internal/pyth/    Pyth price client: USDC-only pot + marked pot, after-hours flag
  internal/postgres/
    proposals, votes, nav_snapshots, redeem_jobs, withdrawals writes
```



## Flow

**Group rules**

1. `POST /v1/groups` with join policy, voter set, threshold, expiry. Creator gets `group_members` row.
2. `POST /v1/groups/{id}/join` with optional password.

**Buy via vote**

1. `GET /v1/groups/{id}/assets?query=` for catalog search. Mobile consumes this endpoint only.
2. `POST /v1/groups/{id}/quotes` returns `routable` so propose UI can disable before submit.
3. `POST /v1/groups/{id}/proposals`: backend resolves mint, quote must exist, insert open proposal.
4. `POST /v1/proposals/{id}/votes`: persist vote, `TallyProposal` → passed/failed/expired.
5. On pass only: `StartBuy` allows execute → same Jupiter path as M3 → `transactions` with `proposal_id` set → NAV snapshot.

**Marked NAV and boards**

1. Pyth price client `MarkedPot` feeds `ComputePotNAV(NavMarked)`. Cost basis on `transactions`, live price from Pyth.
2. `GroupView`: pot, viewer equity, in-group board ranked by percent return.
3. `HomeView`: group board + people board. 

**Redeem (debit-first)**

1. `POST /v1/groups/{id}/redeems` with share amount or dollar target, plus payout proof.
2. Verify payout proof. Postgres txn: row-lock `positions`, debit `share_units`, insert `redeem_jobs` status `debited`.
3. If group treasury holds xStock, Jupiter client `SellToUSDC` for redeemed slice → status `selling`.
4. Privy client `PayUSDC` to proven address → status `paying`.
5. Insert `withdrawals` with `tx_signature`, increment `positions.amount_withdrawn`, status `settled`, NAV snapshot.
6. Crash after step 10 resumes from `redeem_jobs.status`. No second debit.

Open decisions (M4-T39–T41): ticket only. No proposer rule, execute-retry, or dissolve logic in code.

## Data models

Keep rules in `packages/domain`, not scattered handler ifs. `packages/domain` stays Go-only. Mobile reads domain results over HTTP with hand-written `Codable` types. Swift never calls xStocks, Jupiter, Pyth, or Solana RPC. Catalog search, quotes, Pyth prices, and after-hours flags are backend endpoints only.

- **groups** gains join policy, voter set, threshold, expiry duration.
- **group_members.** One user may sit in many groups.
- **proposals** and **votes.** Status is open, passed, failed, or expired.
- **deposits**, **positions**, **withdrawals**, **transactions.** Already defined in M2 and M3. M4 writes withdrawal rows on redeem payout and debits position share units first.
- **positions** hold share units and net USDC in columns (`amount_deposited`, `amount_withdrawn`). M4 pot NAV uses USDC plus xStock units times Pyth price.
- **nav_snapshots.** Written on deposit, transaction confirm, and withdrawal payout so boards replay from history.
- **net USDC in.** Per member per group from position columns: deposits credited minus withdrawals paid. Sum for the people board.

**Claim units on deposit.** M2 credits `share_units` and `amount_deposited` 1:1 with swept USDC while the pot is USDC only. Once the treasury holds marked xStock, M4 mints `share_units` from current pot NAV so a later deposit does not absorb unrealized gain (README Alex/Blair example). That ratio is internal ledger math only. User copy says claim units or omits price. Never say share price or NAV to users.

Formulas from [`docs/product.md`](../product.md).

- USDC-only pot: `share_units` and `amount_deposited` increase by swept USDC (M2 path).
- Marked pot: `share_units credited = USDC swept in × total share_units / pot NAV`
- `your equity = (your share_units / total share_units) × pot NAV`
- `percent return = equity / net USDC in − 1`
- Skip a board row when net USDC in is 0.
- Rank by percent return, never by dollars.

Do not invent answers for the three README open decisions. Record tickets only.

## Parallelization

Milestone gate: complete **Wave 5** before M5.

| Wave | Tracks (run in parallel within the wave) | Gate for next wave |
|------|------------------------------------------|--------------------|
| **1** | **Schema** M4-T1 · **Domain types** M4-T2 | M3 |
| **2** | **Deposit credit** M4-T3 · **Pot NAV** M4-T5 · **Pyth** M4-T38 · **Group rules** M4-T7–T10 · **Catalog** M4-T11, M4-T12 | Wave 1 |
| **3** | **Proposals** M4-T13 · **Vote tally** M4-T14 · **Leaderboards** M4-T23–T29, M4-T37 | Wave 2 |
| **4** | **Execute on pass** M4-T19 · **Redeem** M4-T30 | M4-T13, M4-T14 |
| **5** | **Tests** M4-T4, T6, T18, T22, T29, T36 · **Open decisions** M4-T39–T41 | Waves 3–4 |

Tracks inside Wave 2 are fully parallel once schema lands. Leaderboard math (T23–T29) can start once pot NAV (T5) exists; it does not wait on votes. Redeem (T30) and execute-on-pass (T19) are parallel in Wave 4 but each has internal sequential subissues.

## Tickets

1. M4-T1 Add remaining schema for group settings, members, proposals, votes, NAV snapshots, and payout proofs. Do not recreate M1 wallet tables or M2 deposit tables.
2. M4-T2 Add domain types for join policy, voter set, threshold, expiry, proposal status, and withdrawal status.  
   Depends on: M4-T1
3. **Parent** M4-T3 Mint claim units at marked pot NAV on deposit. Keep M2 1:1 USDC credit when treasury holds no marked xStock.  
   Depends on: M4-T2, M4-T5
   - └ **Subissue of M4-T3** M4-T4 Add tests for USDC-only deposit credit, post-buy marked-pot minting, and the README Alex/Blair worked example
4. **Parent** M4-T5 Implement pot NAV as treasury USDC plus each xStock marked, with transaction cost basis for holdings.  
   Depends on: M4-T2
   - └ **Subissue of M4-T5** M4-T6 Add tests for pot NAV with USDC only, mixed pot, and empty shares
5. M4-T7 Replace thin group create with persisted join policy, voter set, threshold, and expiry.  
   Depends on: M4-T2
6. M4-T8 Implement join for open groups and for password groups.  
   Depends on: M4-T7
7. M4-T9 Implement named voter subset with minimum size 1 and every-member voter set mode.  
   Depends on: M4-T7
8. M4-T10 Enforce one user in many groups on membership rows.  
   Depends on: M4-T7
9. M4-T11 Add `GET /v1/groups/{id}/assets?query=` with backend xStocks mint resolution for mobile catalog search.  
   Depends on: M4-T2
10. M4-T12 Add `POST /v1/groups/{id}/quotes` and refuse proposal create when backend cannot quote USDC to the output mint.  
    Depends on: M4-T11, M3-T3
11. M4-T13 Implement buy proposal create with symbol, USDC amount, proposer, and expiry deadline.  
    Depends on: M4-T9, M4-T12
12. **Parent** M4-T14 Implement yes/no votes limited to current voter set members and tally to passed, failed, or expired.  
    Depends on: M4-T13
    - └ **Subissue of M4-T14** M4-T15 Implement unanimous tally among the voter set
    - └ **Subissue of M4-T14** M4-T16 Implement majority tally among the voter set
    - └ **Subissue of M4-T14** M4-T17 Expire open proposals at deadline with failed status and no swap
    - └ **Subissue of M4-T14** M4-T18 Add tests for unanimous pass, majority pass, majority fail, and expiry fail
13. **Parent** M4-T19 On pass, build the Jupiter v2 transaction, sign the treasury, POST execute, confirm Success code 0. Delete the M3 stub route.  
    Depends on: M4-T14, M3-T5
    - └ **Subissue of M4-T19** M4-T20 Record treasury holdings and write a NAV snapshot on confirmed buy transaction
    - └ **Subissue of M4-T19** M4-T21 Add an idempotent execute guard keyed on proposal id and Jupiter tx signature
    - └ **Subissue of M4-T19** M4-T22 Add tests for idempotent deposit credit, buy execute, and withdrawal payout
14. M4-T23 Implement member equity and percent return per group from positions.  
    Depends on: M4-T5
15. M4-T24 Implement net USDC in per member per group from position columns.  
    Depends on: M4-T23
16. M4-T25 Skip leaderboard rows when net USDC in equals zero.  
    Depends on: M4-T24
17. M4-T26 Implement the in-group member board ranked by percent return.  
    Depends on: M4-T25
18. M4-T27 Implement the app-wide group board ranked by pot percent return.  
    Depends on: M4-T5
19. M4-T28 Implement the app-wide people board ranked by summed cross-group percent return.  
    Depends on: M4-T24
20. M4-T29 Add tests for percent ranking, zero-net skip, and full exit drop from the in-group board.  
    Depends on: M4-T26, M4-T27, M4-T28
21. **Parent** M4-T30 Implement partial redeem with share amount or dollar target and a dust minimum.  
    Depends on: M4-T5, M3-T9
    - └ **Subissue of M4-T30** M4-T31 Debit position share units first in a row-locked Postgres transaction
    - └ **Subissue of M4-T30** M4-T32 Sell the redeemed slice of each xStock to USDC on Jupiter when the treasury holds stock
    - └ **Subissue of M4-T30** M4-T33 Pay USDC only to a payout address the user proved they own with a signed message. Persist a withdrawal row
    - └ **Subissue of M4-T30** M4-T34 Reject redeem when the payout pubkey fails ownership proof
    - └ **Subissue of M4-T30** M4-T35 Write a NAV snapshot and recompute boards after confirmed withdrawal payout
    - └ **Subissue of M4-T30** M4-T36 Add tests for debit-first ordering, slice USDC versus deposit refund, and board recompute
22. M4-T37 Persist NAV snapshots on deposit, transaction confirm, and withdrawal payout.  
    Depends on: M4-T5
23. M4-T38 Wire an after-hours flag when the Pyth equity mark is frozen.  
    Depends on: M4-T5
24. M4-T39 Open decision ticket. Who may propose a buy. Any member, voter set only, or creator only. Do not pick.
25. M4-T40 Open decision ticket. Failed Jupiter execute after a passed vote. Do not pick.
26. M4-T41 Open decision ticket. Creator leave and group dissolve. Do not pick.



## Ticket details

#### M4-T1: Add remaining schema for group settings, members, proposals, votes, NAV snapshots, and payout proofs

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T1` is wave **1** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.

**Problem**
Behavior for `Add remaining schema for group settings, members, proposals, votes, NAV snapshots, and payout proofs` is not implemented under `supabase/migrations/`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `supabase/migrations/`. No drive-by refactors.
2. Satisfy milestone gate: `just test backend` and/or `just test mobile` exit 0 with no new failing tests.

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `supabase/migrations/`

**Out of scope (do not touch)**
- Recreating M1 wallet or M2 deposit tables

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
#### M4-T2: Add domain types for join policy, voter set, threshold, expiry, proposal status, and withdrawal status

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T2` is wave **1** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Blocked until dependencies land: M4-T1.

**Problem**
Behavior for `Add domain types for join policy, voter set, threshold, expiry, proposal status, and withdrawal status` is not implemented under `packages/domain/`, `apps/backend/internal/app/`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `packages/domain/`, `apps/backend/internal/app/`. No drive-by refactors.
2. Satisfy milestone gate: `just test backend` and/or `just test mobile` exit 0 with no new failing tests.

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `packages/domain/`
- `apps/backend/internal/app/`

**Out of scope (do not touch)**
- HTTP handlers
- Swift types

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `packages/domain/`, `apps/backend/internal/app/`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `packages/domain/`, `apps/backend/internal/app/`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T1 — **do not start while open**
- **Wave:** 1
#### M4-T3: Mint claim units at marked pot NAV on deposit. Keep M2 1:1 USDC credit when treasury holds no marked xStock

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T3` is wave **2** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Parent ticket; subissues: M4-T4.
Blocked until dependencies land: M4-T2, M4-T5.

**Problem**
Parent `M4-T3` orchestration in `apps/backend/internal/app/deposit.go`, `packages/domain/nav.go` missing; subissues M4-T4 not wired together.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/app/deposit.go`, `packages/domain/nav.go`. No drive-by refactors.
2. Parent glue: wire subissues M4-T4 into `apps/backend/internal/app/deposit.go` after each subissue lands.
3. Add `TestSharesForDeposit_usdcOnlyPot_creditsOneToOneWithSweptUsdc` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
4. Add `TestSharesForDeposit_markedPot_mintsSharesFromPotNav` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**Parent orchestration.** Public contract is union of subissue routes/symbols: M4-T4.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `apps/backend/internal/app/deposit.go`
- `packages/domain/nav.go`

**Out of scope (do not touch)**
- Mobile share math

**Acceptance Criteria**
- [ ] Test `TestSharesForDeposit_usdcOnlyPot_creditsOneToOneWithSweptUsdc` exists, uses AAA comments, and passes.
- [ ] Test `TestSharesForDeposit_markedPot_mintsSharesFromPotNav` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/deposit.go`, `packages/domain/nav.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] All subissues (M4-T4) closed before parent closes.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestSharesForDeposit_usdcOnlyPot_creditsOneToOneWithSweptUsdc` exists, uses AAA comments, and passes.
- [ ] Test `TestSharesForDeposit_markedPot_mintsSharesFromPotNav` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/deposit.go`, `packages/domain/nav.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] All subissues (M4-T4) closed before parent closes.
- [ ] Subissues closed: M4-T4.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T2 — **do not start while open**
- M4-T5 — **do not start while open**
- **Wave:** 2
- **Subissue:** M4-T4
#### M4-T4: Add tests for USDC-only deposit credit, post-buy marked-pot minting, and README Alex/Blair worked example

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T4` is wave **5** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Subissue of M4-T3.
Blocked until dependencies land: M4-T3.

**Problem**
Locked test names for `M4-T4` are absent or not wired into `just test` recipes; `packages/domain/*_test.go`, `apps/backend/internal/app/deposit_test.go` incomplete.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `packages/domain/*_test.go`, `apps/backend/internal/app/deposit_test.go`. No drive-by refactors.
2. Subissue of `M4-T3`. Do not add routes or flows owned by parent/sibling subissues.
3. Add `TestDepositCredit_readmeAlexBlairWorkedExample_matchesLiterals` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
4. Add `TestObserveSweep_postBuyDeposit_doesNotLetNewMemberCaptureUnrealizedGain` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new public route.** Subissue of `M4-T3`. Internal symbols only in **Owns** paths.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `packages/domain/*_test.go`
- `apps/backend/internal/app/deposit_test.go`

**Out of scope (do not touch)**
- Manual demo script

**Acceptance Criteria**
- [ ] Test `TestDepositCredit_readmeAlexBlairWorkedExample_matchesLiterals` exists, uses AAA comments, and passes.
- [ ] Test `TestObserveSweep_postBuyDeposit_doesNotLetNewMemberCaptureUnrealizedGain` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `packages/domain/*_test.go`, `apps/backend/internal/app/deposit_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestDepositCredit_readmeAlexBlairWorkedExample_matchesLiterals` exists, uses AAA comments, and passes.
- [ ] Test `TestObserveSweep_postBuyDeposit_doesNotLetNewMemberCaptureUnrealizedGain` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `packages/domain/*_test.go`, `apps/backend/internal/app/deposit_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T3 — **do not start while open**
- **Wave:** 5
- **Parent:** M4-T3
#### M4-T5: Implement pot NAV as treasury USDC plus each xStock marked, with transaction cost basis for holdings

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T5` is wave **2** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Parent ticket; subissues: M4-T6.
Blocked until dependencies land: M4-T2.

**Problem**
Parent `M4-T5` orchestration in `packages/domain/nav.go`, `apps/backend/internal/app/views.go` missing; subissues M4-T6 not wired together.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `packages/domain/nav.go`, `apps/backend/internal/app/views.go`. No drive-by refactors.
2. Parent glue: wire subissues M4-T6 into `packages/domain/nav.go` after each subissue lands.
3. Satisfy milestone gate: `just test backend` and/or `just test mobile` exit 0 with no new failing tests.

**API / data contract**
**Parent orchestration.** Public contract is union of subissue routes/symbols: M4-T6.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `packages/domain/nav.go`
- `apps/backend/internal/app/views.go`

**Out of scope (do not touch)**
- Swift NAV math

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `packages/domain/nav.go`, `apps/backend/internal/app/views.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] All subissues (M4-T6) closed before parent closes.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `packages/domain/nav.go`, `apps/backend/internal/app/views.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] All subissues (M4-T6) closed before parent closes.
- [ ] Subissues closed: M4-T6.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T2 — **do not start while open**
- **Wave:** 2
- **Subissue:** M4-T6
#### M4-T6: Add tests for pot NAV with USDC only, mixed pot, and empty shares

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T6` is wave **5** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Subissue of M4-T5.
Blocked until dependencies land: M4-T5.

**Problem**
Locked test names for `M4-T6` are absent or not wired into `just test` recipes; `packages/domain/nav_test.go` incomplete.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `packages/domain/nav_test.go`. No drive-by refactors.
2. Subissue of `M4-T5`. Do not add routes or flows owned by parent/sibling subissues.
3. Add `TestComputePotNAV_usdcOnly_returnsTreasuryUsdc` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
4. Add `TestComputePotNAV_mixedPot_sumsUsdcAndMarkedHoldings` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
5. Add `TestComputePotNAV_emptyShares_firstDepositUsesOneDollarPerShare` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new public route.** Subissue of `M4-T5`. Internal symbols only in **Owns** paths.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `packages/domain/nav_test.go`

**Out of scope (do not touch)**
- Pyth live HTTP in unit tests

**Acceptance Criteria**
- [ ] Test `TestComputePotNAV_usdcOnly_returnsTreasuryUsdc` exists, uses AAA comments, and passes.
- [ ] Test `TestComputePotNAV_mixedPot_sumsUsdcAndMarkedHoldings` exists, uses AAA comments, and passes.
- [ ] Test `TestComputePotNAV_emptyShares_firstDepositUsesOneDollarPerShare` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `packages/domain/nav_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestComputePotNAV_usdcOnly_returnsTreasuryUsdc` exists, uses AAA comments, and passes.
- [ ] Test `TestComputePotNAV_mixedPot_sumsUsdcAndMarkedHoldings` exists, uses AAA comments, and passes.
- [ ] Test `TestComputePotNAV_emptyShares_firstDepositUsesOneDollarPerShare` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `packages/domain/nav_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T5 — **do not start while open**
- **Wave:** 5
- **Parent:** M4-T5
#### M4-T7: Replace thin group create with persisted join policy, voter set, threshold, and expiry

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T7` is wave **2** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Blocked until dependencies land: M4-T2.

**Problem**
Behavior for `Replace thin group create with persisted join policy, voter set, threshold, and expiry` is not implemented under `apps/backend/internal/app/governance.go`, `apps/backend/internal/httpapi/groups.go`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/app/governance.go`, `apps/backend/internal/httpapi/groups.go`. No drive-by refactors.
2. Add `TestPOST_groups_persistsJoinPolicyVoterSetThresholdExpiry` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `apps/backend/internal/app/governance.go`
- `apps/backend/internal/httpapi/groups.go`

**Out of scope (do not touch)**
- Join password flow (M4-T8)

**Acceptance Criteria**
- [ ] Test `TestPOST_groups_persistsJoinPolicyVoterSetThresholdExpiry` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/governance.go`, `apps/backend/internal/httpapi/groups.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestPOST_groups_persistsJoinPolicyVoterSetThresholdExpiry` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/governance.go`, `apps/backend/internal/httpapi/groups.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T2 — **do not start while open**
- **Wave:** 2
#### M4-T8: Implement join for open groups and for password groups

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T8` is wave **2** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Blocked until dependencies land: M4-T7.

**Problem**
Behavior for `Implement join for open groups and for password groups` is not implemented under `apps/backend/internal/app/governance.go`, `apps/backend/internal/httpapi/groups.go`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/app/governance.go`, `apps/backend/internal/httpapi/groups.go`. No drive-by refactors.
2. Add `TestPOST_join_openGroup_addsMemberWithoutPassword` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
3. Add `TestPOST_join_passwordGroup_requiresCorrectPassword` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
4. Add `TestPOST_join_wrongPassword_returns403` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `apps/backend/internal/app/governance.go`
- `apps/backend/internal/httpapi/groups.go`

**Out of scope (do not touch)**
- Dissolve on creator leave (needs-decision M4-T41)

**Acceptance Criteria**
- [ ] Test `TestPOST_join_openGroup_addsMemberWithoutPassword` exists, uses AAA comments, and passes.
- [ ] Test `TestPOST_join_passwordGroup_requiresCorrectPassword` exists, uses AAA comments, and passes.
- [ ] Test `TestPOST_join_wrongPassword_returns403` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/governance.go`, `apps/backend/internal/httpapi/groups.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestPOST_join_openGroup_addsMemberWithoutPassword` exists, uses AAA comments, and passes.
- [ ] Test `TestPOST_join_passwordGroup_requiresCorrectPassword` exists, uses AAA comments, and passes.
- [ ] Test `TestPOST_join_wrongPassword_returns403` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/governance.go`, `apps/backend/internal/httpapi/groups.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T7 — **do not start while open**
- **Wave:** 2
#### M4-T9: Implement named voter subset with minimum size 1 and every-member voter set mode

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T9` is wave **2** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Blocked until dependencies land: M4-T7.

**Problem**
Behavior for `Implement named voter subset with minimum size 1 and every-member voter set mode` is not implemented under `apps/backend/internal/app/governance.go`, `packages/domain/votes.go`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/app/governance.go`, `packages/domain/votes.go`. No drive-by refactors.
2. Add `TestVoterSet_namedSubset_enforcesMinimumSizeOne` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
3. Add `TestVoterSet_everyMemberMode_allowsAllMembersToVote` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `apps/backend/internal/app/governance.go`
- `packages/domain/votes.go`

**Out of scope (do not touch)**
- Who may propose (needs-decision M4-T39)

**Acceptance Criteria**
- [ ] Test `TestVoterSet_namedSubset_enforcesMinimumSizeOne` exists, uses AAA comments, and passes.
- [ ] Test `TestVoterSet_everyMemberMode_allowsAllMembersToVote` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/governance.go`, `packages/domain/votes.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestVoterSet_namedSubset_enforcesMinimumSizeOne` exists, uses AAA comments, and passes.
- [ ] Test `TestVoterSet_everyMemberMode_allowsAllMembersToVote` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/governance.go`, `packages/domain/votes.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T7 — **do not start while open**
- **Wave:** 2
#### M4-T10: Enforce one user in many groups on membership rows

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T10` is wave **2** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Blocked until dependencies land: M4-T7.

**Problem**
Behavior for `Enforce one user in many groups on membership rows` is not implemented under `apps/backend/internal/postgres/members.go`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/postgres/members.go`. No drive-by refactors.
2. Add `TestUser_inManyGroups_hasDistinctPositionsPerGroup` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `apps/backend/internal/postgres/members.go`

**Out of scope (do not touch)**
- Cross-group people board math (M4-T28)

**Acceptance Criteria**
- [ ] Test `TestUser_inManyGroups_hasDistinctPositionsPerGroup` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/postgres/members.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestUser_inManyGroups_hasDistinctPositionsPerGroup` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/postgres/members.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T7 — **do not start while open**
- **Wave:** 2
#### M4-T11: Add GET /v1/groups/{id}/assets?query= with backend xStocks mint resolution for mobile catalog search

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T11` is wave **2** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Blocked until dependencies land: M4-T2.

**Problem**
`GET /v1/groups/{id}/assets?query=` and handler code under `apps/backend/internal/httpapi/catalog.go`, `apps/backend/internal/xstocks/` do not exist (or fail locked tests).

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/httpapi/catalog.go`, `apps/backend/internal/xstocks/`. No drive-by refactors.
2. Register `GET /v1/groups/{id}/assets?query=` in route table (`apps/backend/cmd/api/` or `internal/httpapi/`).
3. Implement handler in `apps/backend/internal/xstocks/` calling `internal/app` method; return JSON status codes from **API / data contract** below.
4. Add `TestGET_assets_search_returnsBackendResolvedCatalog` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**Route:** `GET /v1/groups/{id}/assets?query=`
**Auth:** Privy bearer token in `Authorization` header (same as M1 session middleware).
**Happy:** 200 with JSON body defined in milestone doc for this route.
**Failure codes:** 401 missing/invalid auth; 400 invalid body; 403 non-member; 404 unknown resource.
**Idempotency:** follow milestone doc keys (`tx_signature`, `execute_request_id`, `privy_user_id`, etc.).
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `apps/backend/internal/httpapi/catalog.go`
- `apps/backend/internal/xstocks/`
- Route: `GET /v1/groups/{id}/assets?query=`

**Out of scope (do not touch)**
- Swift xStocks calls

**Acceptance Criteria**
- [ ] Test `TestGET_assets_search_returnsBackendResolvedCatalog` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/httpapi/catalog.go`, `apps/backend/internal/xstocks/`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestGET_assets_search_returnsBackendResolvedCatalog` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/httpapi/catalog.go`, `apps/backend/internal/xstocks/`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T2 — **do not start while open**
- **Wave:** 2
#### M4-T12: Add POST /v1/groups/{id}/quotes and refuse proposal create when backend cannot quote USDC to output mint

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T12` is wave **2** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Blocked until dependencies land: M4-T11, M3-T3.

**Problem**
`POST /v1/groups/{id}/quotes` and handler code under `apps/backend/internal/httpapi/quotes.go` do not exist (or fail locked tests).

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/httpapi/quotes.go`. No drive-by refactors.
2. Register `POST /v1/groups/{id}/quotes` in route table (`apps/backend/cmd/api/` or `internal/httpapi/`).
3. Implement handler in `apps/backend/internal/httpapi/quotes.go` calling `internal/app` method; return JSON status codes from **API / data contract** below.
4. Add `TestPOST_quotes_noRoute_returnsRoutableFalse` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
5. Add `TestPOST_proposals_noRoute_refusesBeforeInsert` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**Route:** `POST /v1/groups/{id}/quotes`
**Auth:** Privy bearer token in `Authorization` header (same as M1 session middleware).
**Happy:** 200 with JSON body defined in milestone doc for this route.
**Failure codes:** 401 missing/invalid auth; 400 invalid body; 403 non-member; 404 unknown resource.
**Idempotency:** follow milestone doc keys (`tx_signature`, `execute_request_id`, `privy_user_id`, etc.).
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `apps/backend/internal/httpapi/quotes.go`
- Route: `POST /v1/groups/{id}/quotes`

**Out of scope (do not touch)**
- Execute on pass

**Acceptance Criteria**
- [ ] Test `TestPOST_quotes_noRoute_returnsRoutableFalse` exists, uses AAA comments, and passes.
- [ ] Test `TestPOST_proposals_noRoute_refusesBeforeInsert` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/httpapi/quotes.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestPOST_quotes_noRoute_returnsRoutableFalse` exists, uses AAA comments, and passes.
- [ ] Test `TestPOST_proposals_noRoute_refusesBeforeInsert` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/httpapi/quotes.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T11 — **do not start while open**
- M3-T3 — **do not start while open**
- **Wave:** 2
#### M4-T13: Implement buy proposal create with symbol, USDC amount, proposer, and expiry deadline

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T13` is wave **3** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Blocked until dependencies land: M4-T9, M4-T12.

**Problem**
Behavior for `Implement buy proposal create with symbol, USDC amount, proposer, and expiry deadline` is not implemented under `apps/backend/internal/app/governance.go`, `apps/backend/internal/postgres/proposals.go`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/app/governance.go`, `apps/backend/internal/postgres/proposals.go`. No drive-by refactors.
2. Add `TestPOST_proposals_happyPath_createsOpenProposalWithExpiry` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `apps/backend/internal/app/governance.go`
- `apps/backend/internal/postgres/proposals.go`

**Out of scope (do not touch)**
- Proposer permission rule until M4-T39 decision

**Acceptance Criteria**
- [ ] Test `TestPOST_proposals_happyPath_createsOpenProposalWithExpiry` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/governance.go`, `apps/backend/internal/postgres/proposals.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestPOST_proposals_happyPath_createsOpenProposalWithExpiry` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/governance.go`, `apps/backend/internal/postgres/proposals.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T9 — **do not start while open**
- M4-T12 — **do not start while open**
- **Wave:** 3
#### M4-T14: Implement yes/no votes limited to current voter set members and tally to passed, failed, or expired

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T14` is wave **3** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Parent ticket; subissues: M4-T15, M4-T16, M4-T17, M4-T18.
Blocked until dependencies land: M4-T13.

**Problem**
Parent `M4-T14` orchestration in `packages/domain/votes.go`, `apps/backend/internal/app/governance.go` missing; subissues M4-T15, M4-T16, M4-T17, M4-T18 not wired together.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `packages/domain/votes.go`, `apps/backend/internal/app/governance.go`. No drive-by refactors.
2. Parent glue: wire subissues M4-T15, M4-T16, M4-T17, M4-T18 into `packages/domain/votes.go` after each subissue lands.
3. Satisfy milestone gate: `just test backend` and/or `just test mobile` exit 0 with no new failing tests.

**API / data contract**
**Parent orchestration.** Public contract is union of subissue routes/symbols: M4-T15, M4-T16, M4-T17, M4-T18.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `packages/domain/votes.go`
- `apps/backend/internal/app/governance.go`

**Out of scope (do not touch)**
- On-chain voting

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `packages/domain/votes.go`, `apps/backend/internal/app/governance.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] All subissues (M4-T15, M4-T16, M4-T17, M4-T18) closed before parent closes.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `packages/domain/votes.go`, `apps/backend/internal/app/governance.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] All subissues (M4-T15, M4-T16, M4-T17, M4-T18) closed before parent closes.
- [ ] Subissues closed: M4-T15, M4-T16, M4-T17, M4-T18.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T13 — **do not start while open**
- **Wave:** 3
- **Subissue:** M4-T15
- **Subissue:** M4-T16
- **Subissue:** M4-T17
- **Subissue:** M4-T18
#### M4-T15: Implement unanimous tally among the voter set

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T15` is wave **3** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Subissue of M4-T14.
Blocked until dependencies land: M4-T14.

**Problem**
Subissue `M4-T15` of `M4-T14`: symbols under `packages/domain/votes.go` not implemented; parent cannot close.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `packages/domain/votes.go`. No drive-by refactors.
2. Subissue of `M4-T14`. Do not add routes or flows owned by parent/sibling subissues.
3. Add `TestTallyProposal_unanimous_allYes_passes` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new public route.** Subissue of `M4-T14`. Internal symbols only in **Owns** paths.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `packages/domain/votes.go`

**Out of scope (do not touch)**
- Majority mode

**Acceptance Criteria**
- [ ] Test `TestTallyProposal_unanimous_allYes_passes` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `packages/domain/votes.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestTallyProposal_unanimous_allYes_passes` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `packages/domain/votes.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T14 — **do not start while open**
- **Wave:** 3
- **Parent:** M4-T14
#### M4-T16: Implement majority tally among the voter set

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T16` is wave **3** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Subissue of M4-T14.
Blocked until dependencies land: M4-T14.

**Problem**
Subissue `M4-T16` of `M4-T14`: symbols under `packages/domain/votes.go` not implemented; parent cannot close.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `packages/domain/votes.go`. No drive-by refactors.
2. Subissue of `M4-T14`. Do not add routes or flows owned by parent/sibling subissues.
3. Add `TestTallyProposal_majority_moreYesThanNo_passes` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
4. Add `TestTallyProposal_majority_moreNoThanYes_fails` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new public route.** Subissue of `M4-T14`. Internal symbols only in **Owns** paths.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `packages/domain/votes.go`

**Out of scope (do not touch)**
- Unanimous mode

**Acceptance Criteria**
- [ ] Test `TestTallyProposal_majority_moreYesThanNo_passes` exists, uses AAA comments, and passes.
- [ ] Test `TestTallyProposal_majority_moreNoThanYes_fails` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `packages/domain/votes.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestTallyProposal_majority_moreYesThanNo_passes` exists, uses AAA comments, and passes.
- [ ] Test `TestTallyProposal_majority_moreNoThanYes_fails` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `packages/domain/votes.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T14 — **do not start while open**
- **Wave:** 3
- **Parent:** M4-T14
#### M4-T17: Expire open proposals at deadline with failed status and no swap

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T17` is wave **3** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Subissue of M4-T14.
Blocked until dependencies land: M4-T14.

**Problem**
Subissue `M4-T17` of `M4-T14`: symbols under `apps/backend/internal/app/governance.go` not implemented; parent cannot close.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/app/governance.go`. No drive-by refactors.
2. Subissue of `M4-T14`. Do not add routes or flows owned by parent/sibling subissues.
3. Add `TestTallyProposal_expiredOpenProposal_failsWithoutSwap` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new public route.** Subissue of `M4-T14`. Internal symbols only in **Owns** paths.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `apps/backend/internal/app/governance.go`

**Out of scope (do not touch)**
- Failed execute retry (needs-decision M4-T40)

**Acceptance Criteria**
- [ ] Test `TestTallyProposal_expiredOpenProposal_failsWithoutSwap` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/governance.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestTallyProposal_expiredOpenProposal_failsWithoutSwap` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/governance.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T14 — **do not start while open**
- **Wave:** 3
- **Parent:** M4-T14
#### M4-T18: Add tests for unanimous pass, majority pass, majority fail, and expiry fail

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T18` is wave **5** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Subissue of M4-T14.
Blocked until dependencies land: M4-T15, M4-T16, M4-T17.

**Problem**
Locked test names for `M4-T18` are absent or not wired into `just test` recipes; `packages/domain/votes_test.go`, `apps/backend/internal/app/governance_test.go` incomplete.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `packages/domain/votes_test.go`, `apps/backend/internal/app/governance_test.go`. No drive-by refactors.
2. Subissue of `M4-T14`. Do not add routes or flows owned by parent/sibling subissues.
3. Add `TestPOST_vote_nonVoterSetMember_returns403` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
4. Add `TestPOST_vote_doubleVoteSameMember_isIdempotentOrRejected` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
5. Add `TestPOST_vote_concurrentDoubleVote_recordsOneBallot` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new public route.** Subissue of `M4-T14`. Internal symbols only in **Owns** paths.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `packages/domain/votes_test.go`
- `apps/backend/internal/app/governance_test.go`

**Out of scope (do not touch)**
- Jupiter execute integration

**Acceptance Criteria**
- [ ] Test `TestPOST_vote_nonVoterSetMember_returns403` exists, uses AAA comments, and passes.
- [ ] Test `TestPOST_vote_doubleVoteSameMember_isIdempotentOrRejected` exists, uses AAA comments, and passes.
- [ ] Test `TestPOST_vote_concurrentDoubleVote_recordsOneBallot` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `packages/domain/votes_test.go`, `apps/backend/internal/app/governance_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestPOST_vote_nonVoterSetMember_returns403` exists, uses AAA comments, and passes.
- [ ] Test `TestPOST_vote_doubleVoteSameMember_isIdempotentOrRejected` exists, uses AAA comments, and passes.
- [ ] Test `TestPOST_vote_concurrentDoubleVote_recordsOneBallot` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `packages/domain/votes_test.go`, `apps/backend/internal/app/governance_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T15 — **do not start while open**
- M4-T16 — **do not start while open**
- M4-T17 — **do not start while open**
- **Wave:** 5
- **Parent:** M4-T14
#### M4-T19: On pass, build Jupiter v2 transaction, sign treasury, POST execute, confirm Success code 0. Delete M3 stub route

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T19` is wave **4** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Parent ticket; subissues: M4-T20, M4-T21, M4-T22.
Blocked until dependencies land: M4-T14, M3-T5.

**Problem**
Parent `M4-T19` orchestration in `apps/backend/internal/app/start_buy.go`, `apps/backend/internal/httpapi/dev_buy.go` missing; subissues M4-T20, M4-T21, M4-T22 not wired together.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/app/start_buy.go`, `apps/backend/internal/httpapi/dev_buy.go`. No drive-by refactors.
2. Parent glue: wire subissues M4-T20, M4-T21, M4-T22 into `apps/backend/internal/app/start_buy.go` after each subissue lands.
3. Add `TestExecuteOnPass_onlyAfterTallyPassed_callsJupiter` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
4. Add `TestDevBuyRoute_deleted_returns404` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**Parent orchestration.** Public contract is union of subissue routes/symbols: M4-T20, M4-T21, M4-T22.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `apps/backend/internal/app/start_buy.go`
- `apps/backend/internal/httpapi/dev_buy.go`

**Out of scope (do not touch)**
- Failed execute retry policy (needs-decision M4-T40)

**Acceptance Criteria**
- [ ] Test `TestExecuteOnPass_onlyAfterTallyPassed_callsJupiter` exists, uses AAA comments, and passes.
- [ ] Test `TestDevBuyRoute_deleted_returns404` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/start_buy.go`, `apps/backend/internal/httpapi/dev_buy.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] All subissues (M4-T20, M4-T21, M4-T22) closed before parent closes.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestExecuteOnPass_onlyAfterTallyPassed_callsJupiter` exists, uses AAA comments, and passes.
- [ ] Test `TestDevBuyRoute_deleted_returns404` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/start_buy.go`, `apps/backend/internal/httpapi/dev_buy.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] All subissues (M4-T20, M4-T21, M4-T22) closed before parent closes.
- [ ] Subissues closed: M4-T20, M4-T21, M4-T22.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T14 — **do not start while open**
- M3-T5 — **do not start while open**
- **Wave:** 4
- **Subissue:** M4-T20
- **Subissue:** M4-T21
- **Subissue:** M4-T22
#### M4-T20: Record treasury holdings and write NAV snapshot on confirmed buy transaction

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T20` is wave **4** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Subissue of M4-T19.
Blocked until dependencies land: M4-T19.

**Problem**
Subissue `M4-T20` of `M4-T19`: symbols under `apps/backend/internal/postgres/nav_snapshots.go`, `apps/backend/internal/app/views.go` not implemented; parent cannot close.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/postgres/nav_snapshots.go`, `apps/backend/internal/app/views.go`. No drive-by refactors.
2. Subissue of `M4-T19`. Do not add routes or flows owned by parent/sibling subissues.
3. Add `TestExecuteOnPass_writesNavSnapshotOnConfirm` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new public route.** Subissue of `M4-T19`. Internal symbols only in **Owns** paths.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `apps/backend/internal/postgres/nav_snapshots.go`
- `apps/backend/internal/app/views.go`

**Out of scope (do not touch)**
- Withdrawal snapshots (M4-T37)

**Acceptance Criteria**
- [ ] Test `TestExecuteOnPass_writesNavSnapshotOnConfirm` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/postgres/nav_snapshots.go`, `apps/backend/internal/app/views.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestExecuteOnPass_writesNavSnapshotOnConfirm` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/postgres/nav_snapshots.go`, `apps/backend/internal/app/views.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T19 — **do not start while open**
- **Wave:** 4
- **Parent:** M4-T19
#### M4-T21: Add idempotent execute guard keyed on proposal id and Jupiter tx signature

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T21` is wave **4** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Subissue of M4-T19.
Blocked until dependencies land: M4-T19.

**Problem**
Subissue `M4-T21` of `M4-T19`: symbols under `apps/backend/internal/app/start_buy.go`, `apps/backend/internal/postgres/transactions.go` not implemented; parent cannot close.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/app/start_buy.go`, `apps/backend/internal/postgres/transactions.go`. No drive-by refactors.
2. Subissue of `M4-T19`. Do not add routes or flows owned by parent/sibling subissues.
3. Add `TestExecuteOnPass_duplicateProposalAndSignature_executesOnce` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new public route.** Subissue of `M4-T19`. Internal symbols only in **Owns** paths.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `apps/backend/internal/app/start_buy.go`
- `apps/backend/internal/postgres/transactions.go`

**Out of scope (do not touch)**
- Manual reconcile UX

**Acceptance Criteria**
- [ ] Test `TestExecuteOnPass_duplicateProposalAndSignature_executesOnce` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/start_buy.go`, `apps/backend/internal/postgres/transactions.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestExecuteOnPass_duplicateProposalAndSignature_executesOnce` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/start_buy.go`, `apps/backend/internal/postgres/transactions.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T19 — **do not start while open**
- **Wave:** 4
- **Parent:** M4-T19
#### M4-T22: Add tests for idempotent deposit credit, buy execute, and withdrawal payout

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T22` is wave **5** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Subissue of M4-T19.
Blocked until dependencies land: M4-T19, M4-T30.

**Problem**
Locked test names for `M4-T22` are absent or not wired into `just test` recipes; `apps/backend/internal/app/*_integration_test.go` incomplete.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/app/*_integration_test.go`. No drive-by refactors.
2. Subissue of `M4-T19`. Do not add routes or flows owned by parent/sibling subissues.
3. Add `TestIdempotency_depositSweepBuyWithdrawal_replaySignatureOnceEach` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new public route.** Subissue of `M4-T19`. Internal symbols only in **Owns** paths.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `apps/backend/internal/app/*_integration_test.go`

**Out of scope (do not touch)**
- Mobile tests

**Acceptance Criteria**
- [ ] Test `TestIdempotency_depositSweepBuyWithdrawal_replaySignatureOnceEach` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/*_integration_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestIdempotency_depositSweepBuyWithdrawal_replaySignatureOnceEach` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/*_integration_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T19 — **do not start while open**
- M4-T30 — **do not start while open**
- **Wave:** 5
- **Parent:** M4-T19
#### M4-T23: Implement member equity and percent return per group from positions

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T23` is wave **3** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Blocked until dependencies land: M4-T5.

**Problem**
Behavior for `Implement member equity and percent return per group from positions` is not implemented under `packages/domain/pnl.go`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `packages/domain/pnl.go`. No drive-by refactors.
2. Add `TestMemberEquity_matchesShareFractionTimesPotNav` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
3. Add `TestPercentReturn_equityOverNetInMinusOne` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `packages/domain/pnl.go`

**Out of scope (do not touch)**
- Swift formatters (M5-T23)

**Acceptance Criteria**
- [ ] Test `TestMemberEquity_matchesShareFractionTimesPotNav` exists, uses AAA comments, and passes.
- [ ] Test `TestPercentReturn_equityOverNetInMinusOne` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `packages/domain/pnl.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestMemberEquity_matchesShareFractionTimesPotNav` exists, uses AAA comments, and passes.
- [ ] Test `TestPercentReturn_equityOverNetInMinusOne` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `packages/domain/pnl.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T5 — **do not start while open**
- **Wave:** 3
#### M4-T24: Implement net USDC in per member per group from position columns

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T24` is wave **3** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Blocked until dependencies land: M4-T23.

**Problem**
Behavior for `Implement net USDC in per member per group from position columns` is not implemented under `packages/domain/pnl.go`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `packages/domain/pnl.go`. No drive-by refactors.
2. Add `TestPercentReturn_zeroNetIn_returnsNil` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `packages/domain/pnl.go`

**Out of scope (do not touch)**
- People board aggregation

**Acceptance Criteria**
- [ ] Test `TestPercentReturn_zeroNetIn_returnsNil` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `packages/domain/pnl.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestPercentReturn_zeroNetIn_returnsNil` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `packages/domain/pnl.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T23 — **do not start while open**
- **Wave:** 3
#### M4-T25: Skip leaderboard rows when net USDC in equals zero

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T25` is wave **3** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Blocked until dependencies land: M4-T24.

**Problem**
Behavior for `Skip leaderboard rows when net USDC in equals zero` is not implemented under `packages/domain/pnl.go`, `apps/backend/internal/app/views.go`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `packages/domain/pnl.go`, `apps/backend/internal/app/views.go`. No drive-by refactors.
2. Add `TestBoards_skipRowsWhenNetUsdcInZero` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `packages/domain/pnl.go`
- `apps/backend/internal/app/views.go`

**Out of scope (do not touch)**
- Dollar-ranked boards

**Acceptance Criteria**
- [ ] Test `TestBoards_skipRowsWhenNetUsdcInZero` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `packages/domain/pnl.go`, `apps/backend/internal/app/views.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestBoards_skipRowsWhenNetUsdcInZero` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `packages/domain/pnl.go`, `apps/backend/internal/app/views.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T24 — **do not start while open**
- **Wave:** 3
#### M4-T26: Implement in-group member board ranked by percent return

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T26` is wave **3** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Blocked until dependencies land: M4-T25.

**Problem**
Behavior for `Implement in-group member board ranked by percent return` is not implemented under `apps/backend/internal/app/views.go`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/app/views.go`. No drive-by refactors.
2. Add `TestInGroupBoard_ranksByPercentReturnNotDollars` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `apps/backend/internal/app/views.go`

**Out of scope (do not touch)**
- Mobile board UI (M5)

**Acceptance Criteria**
- [ ] Test `TestInGroupBoard_ranksByPercentReturnNotDollars` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/views.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestInGroupBoard_ranksByPercentReturnNotDollars` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/views.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T25 — **do not start while open**
- **Wave:** 3
#### M4-T27: Implement app-wide group board ranked by pot percent return

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T27` is wave **3** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Blocked until dependencies land: M4-T5.

**Problem**
Behavior for `Implement app-wide group board ranked by pot percent return` is not implemented under `apps/backend/internal/app/views.go`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/app/views.go`. No drive-by refactors.
2. Add `TestGroupBoard_ranksPotsByPercentReturn` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `apps/backend/internal/app/views.go`

**Out of scope (do not touch)**
- Swift home UI

**Acceptance Criteria**
- [ ] Test `TestGroupBoard_ranksPotsByPercentReturn` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/views.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestGroupBoard_ranksPotsByPercentReturn` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/views.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T5 — **do not start while open**
- **Wave:** 3
#### M4-T28: Implement app-wide people board ranked by summed cross-group percent return

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T28` is wave **3** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Blocked until dependencies land: M4-T24.

**Problem**
Behavior for `Implement app-wide people board ranked by summed cross-group percent return` is not implemented under `apps/backend/internal/app/views.go`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/app/views.go`. No drive-by refactors.
2. Add `TestPeopleBoard_aggregatesCrossGroupNetInAndEquity` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `apps/backend/internal/app/views.go`

**Out of scope (do not touch)**
- Profile UI (M5-T16)

**Acceptance Criteria**
- [ ] Test `TestPeopleBoard_aggregatesCrossGroupNetInAndEquity` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/views.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestPeopleBoard_aggregatesCrossGroupNetInAndEquity` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/views.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T24 — **do not start while open**
- **Wave:** 3
#### M4-T29: Add tests for percent ranking, zero-net skip, and full exit drop from in-group board

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T29` is wave **5** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Blocked until dependencies land: M4-T26, M4-T27, M4-T28.

**Problem**
Locked test names for `M4-T29` are absent or not wired into `just test` recipes; `packages/domain/pnl_test.go` incomplete.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `packages/domain/pnl_test.go`. No drive-by refactors.
2. Add `TestInGroupBoard_fullExit_dropsMemberFromBoard` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new API.** Tests and/or `Justfile` wiring only.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `packages/domain/pnl_test.go`

**Out of scope (do not touch)**
- Snapshot UI tests

**Acceptance Criteria**
- [ ] Test `TestInGroupBoard_fullExit_dropsMemberFromBoard` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `packages/domain/pnl_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestInGroupBoard_fullExit_dropsMemberFromBoard` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `packages/domain/pnl_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T26 — **do not start while open**
- M4-T27 — **do not start while open**
- M4-T28 — **do not start while open**
- **Wave:** 5
#### M4-T30: Implement partial redeem with share amount or dollar target and dust minimum

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T30` is wave **4** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Parent ticket; subissues: M4-T31, M4-T32, M4-T33, M4-T34, M4-T35, M4-T36.
Blocked until dependencies land: M4-T5, M3-T9.

**Problem**
Parent `M4-T30` orchestration in `apps/backend/internal/app/redeem.go`, `packages/domain/redeem.go` missing; subissues M4-T31, M4-T32, M4-T33, M4-T34, M4-T35, M4-T36 not wired together.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/app/redeem.go`, `packages/domain/redeem.go`. No drive-by refactors.
2. Parent glue: wire subissues M4-T31, M4-T32, M4-T33, M4-T34, M4-T35, M4-T36 into `apps/backend/internal/app/redeem.go` after each subissue lands.
3. Satisfy milestone gate: `just test backend` and/or `just test mobile` exit 0 with no new failing tests.

**API / data contract**
**Parent orchestration.** Public contract is union of subissue routes/symbols: M4-T31, M4-T32, M4-T33, M4-T34, M4-T35, M4-T36.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `apps/backend/internal/app/redeem.go`
- `packages/domain/redeem.go`

**Out of scope (do not touch)**
- Creator dissolve (needs-decision M4-T41)

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/redeem.go`, `packages/domain/redeem.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] All subissues (M4-T31, M4-T32, M4-T33, M4-T34, M4-T35, M4-T36) closed before parent closes.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/redeem.go`, `packages/domain/redeem.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] All subissues (M4-T31, M4-T32, M4-T33, M4-T34, M4-T35, M4-T36) closed before parent closes.
- [ ] Subissues closed: M4-T31, M4-T32, M4-T33, M4-T34, M4-T35, M4-T36.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T5 — **do not start while open**
- M3-T9 — **do not start while open**
- **Wave:** 4
- **Subissue:** M4-T31
- **Subissue:** M4-T32
- **Subissue:** M4-T33
- **Subissue:** M4-T34
- **Subissue:** M4-T35
- **Subissue:** M4-T36
#### M4-T31: Debit position share units first in row-locked Postgres transaction

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T31` is wave **4** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Subissue of M4-T30.
Blocked until dependencies land: M4-T30.

**Problem**
Subissue `M4-T31` of `M4-T30`: symbols under `apps/backend/internal/app/redeem.go`, `apps/backend/internal/postgres/positions.go` not implemented; parent cannot close.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/app/redeem.go`, `apps/backend/internal/postgres/positions.go`. No drive-by refactors.
2. Subissue of `M4-T30`. Do not add routes or flows owned by parent/sibling subissues.
3. Add `TestRedeem_partialByShareAmount_debitsUnitsFirst` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
4. Add `TestRedeemJob_crashAfterDebit_resumesWithoutSecondDebit` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new public route.** Subissue of `M4-T30`. Internal symbols only in **Owns** paths.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `apps/backend/internal/app/redeem.go`
- `apps/backend/internal/postgres/positions.go`

**Out of scope (do not touch)**
- Jupiter sell slice

**Acceptance Criteria**
- [ ] Test `TestRedeem_partialByShareAmount_debitsUnitsFirst` exists, uses AAA comments, and passes.
- [ ] Test `TestRedeemJob_crashAfterDebit_resumesWithoutSecondDebit` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/redeem.go`, `apps/backend/internal/postgres/positions.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestRedeem_partialByShareAmount_debitsUnitsFirst` exists, uses AAA comments, and passes.
- [ ] Test `TestRedeemJob_crashAfterDebit_resumesWithoutSecondDebit` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/redeem.go`, `apps/backend/internal/postgres/positions.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T30 — **do not start while open**
- **Wave:** 4
- **Parent:** M4-T30
#### M4-T32: Sell redeemed slice of each xStock to USDC on Jupiter when treasury holds stock

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T32` is wave **4** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Subissue of M4-T30.
Blocked until dependencies land: M4-T31.

**Problem**
Subissue `M4-T32` of `M4-T30`: symbols under `apps/backend/internal/app/redeem.go`, `apps/backend/internal/jupiter/sell.go` not implemented; parent cannot close.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/app/redeem.go`, `apps/backend/internal/jupiter/sell.go`. No drive-by refactors.
2. Subissue of `M4-T30`. Do not add routes or flows owned by parent/sibling subissues.
3. Add `TestRedeem_withXStockInTreasury_sellsSliceBeforePayout` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new public route.** Subissue of `M4-T30`. Internal symbols only in **Owns** paths.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `apps/backend/internal/app/redeem.go`
- `apps/backend/internal/jupiter/sell.go`

**Out of scope (do not touch)**
- Pay USDC to external wallet

**Acceptance Criteria**
- [ ] Test `TestRedeem_withXStockInTreasury_sellsSliceBeforePayout` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/redeem.go`, `apps/backend/internal/jupiter/sell.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestRedeem_withXStockInTreasury_sellsSliceBeforePayout` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/redeem.go`, `apps/backend/internal/jupiter/sell.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T31 — **do not start while open**
- **Wave:** 4
- **Parent:** M4-T30
#### M4-T33: Pay USDC only to payout address user proved with signed message. Persist withdrawal row

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T33` is wave **4** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Subissue of M4-T30.
Blocked until dependencies land: M4-T32.

**Problem**
Subissue `M4-T33` of `M4-T30`: symbols under `apps/backend/internal/privy/payout.go`, `apps/backend/internal/postgres/withdrawals.go` not implemented; parent cannot close.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/privy/payout.go`, `apps/backend/internal/postgres/withdrawals.go`. No drive-by refactors.
2. Subissue of `M4-T30`. Do not add routes or flows owned by parent/sibling subissues.
3. Add `TestRedeem_validProof_paysUsdcOnlyToProvenAddress` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
4. Add `TestRedeem_afterPayout_incrementsAmountWithdrawnAndWritesNavSnapshot` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new public route.** Subissue of `M4-T30`. Internal symbols only in **Owns** paths.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `apps/backend/internal/privy/payout.go`
- `apps/backend/internal/postgres/withdrawals.go`

**Out of scope (do not touch)**
- Dissolve flow

**Acceptance Criteria**
- [ ] Test `TestRedeem_validProof_paysUsdcOnlyToProvenAddress` exists, uses AAA comments, and passes.
- [ ] Test `TestRedeem_afterPayout_incrementsAmountWithdrawnAndWritesNavSnapshot` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/privy/payout.go`, `apps/backend/internal/postgres/withdrawals.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestRedeem_validProof_paysUsdcOnlyToProvenAddress` exists, uses AAA comments, and passes.
- [ ] Test `TestRedeem_afterPayout_incrementsAmountWithdrawnAndWritesNavSnapshot` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/privy/payout.go`, `apps/backend/internal/postgres/withdrawals.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T32 — **do not start while open**
- **Wave:** 4
- **Parent:** M4-T30
#### M4-T34: Reject redeem when payout pubkey fails ownership proof

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T34` is wave **4** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Subissue of M4-T30.
Blocked until dependencies land: M4-T30.

**Problem**
Subissue `M4-T34` of `M4-T30`: symbols under `apps/backend/internal/app/redeem.go` not implemented; parent cannot close.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/app/redeem.go`. No drive-by refactors.
2. Subissue of `M4-T30`. Do not add routes or flows owned by parent/sibling subissues.
3. Add `TestRedeem_invalidPayoutProof_rejected` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new public route.** Subissue of `M4-T30`. Internal symbols only in **Owns** paths.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `apps/backend/internal/app/redeem.go`

**Out of scope (do not touch)**
- Retry UX for failed proof

**Acceptance Criteria**
- [ ] Test `TestRedeem_invalidPayoutProof_rejected` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/redeem.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestRedeem_invalidPayoutProof_rejected` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/redeem.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T30 — **do not start while open**
- **Wave:** 4
- **Parent:** M4-T30
#### M4-T35: Write NAV snapshot and recompute boards after confirmed withdrawal payout

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T35` is wave **4** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Subissue of M4-T30.
Blocked until dependencies land: M4-T33.

**Problem**
Subissue `M4-T35` of `M4-T30`: symbols under `apps/backend/internal/postgres/nav_snapshots.go`, `apps/backend/internal/app/views.go` not implemented; parent cannot close.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/postgres/nav_snapshots.go`, `apps/backend/internal/app/views.go`. No drive-by refactors.
2. Subissue of `M4-T30`. Do not add routes or flows owned by parent/sibling subissues.
3. Add `TestRedeem_afterPayout_incrementsAmountWithdrawnAndWritesNavSnapshot` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new public route.** Subissue of `M4-T30`. Internal symbols only in **Owns** paths.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `apps/backend/internal/postgres/nav_snapshots.go`
- `apps/backend/internal/app/views.go`

**Out of scope (do not touch)**
- Mobile board refresh (M5-T19)

**Acceptance Criteria**
- [ ] Test `TestRedeem_afterPayout_incrementsAmountWithdrawnAndWritesNavSnapshot` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/postgres/nav_snapshots.go`, `apps/backend/internal/app/views.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestRedeem_afterPayout_incrementsAmountWithdrawnAndWritesNavSnapshot` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/postgres/nav_snapshots.go`, `apps/backend/internal/app/views.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T33 — **do not start while open**
- **Wave:** 4
- **Parent:** M4-T30
#### M4-T36: Add tests for debit-first ordering, slice USDC versus deposit refund, and board recompute

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T36` is wave **5** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Subissue of M4-T30.
Blocked until dependencies land: M4-T31, M4-T35.

**Problem**
Locked test names for `M4-T36` are absent or not wired into `just test` recipes; `apps/backend/internal/app/redeem_test.go` incomplete.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/app/redeem_test.go`. No drive-by refactors.
2. Subissue of `M4-T30`. Do not add routes or flows owned by parent/sibling subissues.
3. Add `TestRedeem_payoutEqualsRedeemedSliceNotDepositRefund` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
4. Add `TestRedeem_concurrentDoubleRedeem_debitsOnce` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new public route.** Subissue of `M4-T30`. Internal symbols only in **Owns** paths.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `apps/backend/internal/app/redeem_test.go`

**Out of scope (do not touch)**
- Mainnet manual redeem

**Acceptance Criteria**
- [ ] Test `TestRedeem_payoutEqualsRedeemedSliceNotDepositRefund` exists, uses AAA comments, and passes.
- [ ] Test `TestRedeem_concurrentDoubleRedeem_debitsOnce` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/redeem_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestRedeem_payoutEqualsRedeemedSliceNotDepositRefund` exists, uses AAA comments, and passes.
- [ ] Test `TestRedeem_concurrentDoubleRedeem_debitsOnce` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/redeem_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T31 — **do not start while open**
- M4-T35 — **do not start while open**
- **Wave:** 5
- **Parent:** M4-T30
#### M4-T37: Persist NAV snapshots on deposit, transaction confirm, and withdrawal payout

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T37` is wave **2** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Blocked until dependencies land: M4-T5.

**Problem**
Behavior for `Persist NAV snapshots on deposit, transaction confirm, and withdrawal payout` is not implemented under `apps/backend/internal/postgres/nav_snapshots.go`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/postgres/nav_snapshots.go`. No drive-by refactors.
2. Add `TestNavSnapshot_writtenOnDepositConfirmTransactionConfirmWithdrawalPayout` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `apps/backend/internal/postgres/nav_snapshots.go`

**Out of scope (do not touch)**
- Chart history

**Acceptance Criteria**
- [ ] Test `TestNavSnapshot_writtenOnDepositConfirmTransactionConfirmWithdrawalPayout` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/postgres/nav_snapshots.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestNavSnapshot_writtenOnDepositConfirmTransactionConfirmWithdrawalPayout` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/postgres/nav_snapshots.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T5 — **do not start while open**
- **Wave:** 2
#### M4-T38: Wire after-hours flag when Pyth equity mark is frozen

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T38` is wave **2** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.
Blocked until dependencies land: M4-T5.

**Problem**
Behavior for `Wire after-hours flag when Pyth equity mark is frozen` is not implemented under `apps/backend/internal/pyth/`, `apps/backend/internal/app/views.go`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/pyth/`, `apps/backend/internal/app/views.go`. No drive-by refactors.
2. Add `TestMarkedPot_afterHoursFlag_surfacesOnGroupView` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `apps/backend/internal/pyth/`
- `apps/backend/internal/app/views.go`

**Out of scope (do not touch)**
- Swift after-hours label (M5-T20)

**Acceptance Criteria**
- [ ] Test `TestMarkedPot_afterHoursFlag_surfacesOnGroupView` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/pyth/`, `apps/backend/internal/app/views.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestMarkedPot_afterHoursFlag_surfacesOnGroupView` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/pyth/`, `apps/backend/internal/app/views.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M4-T5 — **do not start while open**
- **Wave:** 2
#### M4-T39: Open decision ticket: who may propose a buy

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T39` is wave **5** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.

**Problem**
Product policy for `Open decision ticket: who may propose a buy` is not locked. Milestone doc lists options; no `docs/` decision note or implementation exists yet.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `docs/`. No drive-by refactors.
2. Create/update `docs/` decision note listing options from milestone **Open decisions**. Do **not** implement API/UI behavior. Mark ticket blocked until product picks.

**API / data contract**
**No API.** Decision ticket only. Record chosen option in `docs/` when product decides. Do not ship handler/UI behavior until decision ticket is closed.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `docs/`

**Out of scope (do not touch)**
- Implementation
- API behavior
- UI buttons
- Custom on-chain programs
- On-chain voting
- Android/web
- Privy production webhooks

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `docs/`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `docs/`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- None
- **Wave:** 5
#### M4-T40: Open decision ticket: failed Jupiter execute after passed vote

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T40` is wave **5** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.

**Problem**
Product policy for `Open decision ticket: failed Jupiter execute after passed vote` is not locked. Milestone doc lists options; no `docs/` decision note or implementation exists yet.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `docs/`. No drive-by refactors.
2. Create/update `docs/` decision note listing options from milestone **Open decisions**. Do **not** implement API/UI behavior. Mark ticket blocked until product picks.

**API / data contract**
**No API.** Decision ticket only. Record chosen option in `docs/` when product decides. Do not ship handler/UI behavior until decision ticket is closed.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `docs/`

**Out of scope (do not touch)**
- Implementation
- Automatic retry
- Refund flows
- Custom on-chain programs
- On-chain voting
- Android/web
- Privy production webhooks

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `docs/`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `docs/`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- None
- **Wave:** 5
#### M4-T41: Open decision ticket: creator leave and group dissolve

**Context**
M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.

Ticket `M4-T41` is wave **5** in `docs/milestones/m4-domain.md` under milestone **M4 — Domain**.

**Problem**
Product policy for `Open decision ticket: creator leave and group dissolve` is not locked. Milestone doc lists options; no `docs/` decision note or implementation exists yet.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `docs/`. No drive-by refactors.
2. Create/update `docs/` decision note listing options from milestone **Open decisions**. Do **not** implement API/UI behavior. Mark ticket blocked until product picks.

**API / data contract**
**No API.** Decision ticket only. Record chosen option in `docs/` when product decides. Do not ship handler/UI behavior until decision ticket is closed.
**Test fakes (locked names):**
- `fakePythClient`
- `fakeJupiterClient`
- `fakePrivyClient`
- `integrationApp(t)`
- domain factories in `packages/domain` tests

### Scope
- `docs/`

**Out of scope (do not touch)**
- Implementation
- Dissolve API
- Treasury wind-down automation
- Custom on-chain programs
- On-chain voting
- Android/web
- Privy production webhooks

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `docs/`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `docs/`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- None
- **Wave:** 5

## Automated verification

**Commands.** `just test backend` (includes `packages/domain` unit tests and `apps/backend` integration tests).

### Test layers

| Layer | Runs under | What it proves |
|-------|------------|----------------|
| **`packages/domain` unit** | `just test backend` | Pure math: NAV, share minting on deposit, vote tally, percent return, redeem slice, board rank — no I/O, no clock. |
| **Go unit** | `just test backend` | `app` orchestration with fake Pyth, fake Jupiter, fake Privy — governance, redeem state machine, view assembly. |
| **Go integration** | `just test backend` | HTTP → Postgres lifecycle for join, propose, vote, pass-execute, redeem, boards; fake external clients, real local Postgres. |
| **Concurrency** | `just test backend` | Double-vote, double-redeem, concurrent execute on same proposal/signature. |

Vote and tally tests do not call real Jupiter. Pass-execute integration uses `fakeJupiterClient`. Domain tests use README worked numbers as literals (Alex/Blair). AAA comments on every test.

### Test map

**M4-T3 / M4-T4 — deposit credit at NAV (domain + integration)**

- `TestSharesForDeposit_usdcOnlyPot_creditsOneToOneWithSweptUsdc` (M2 path preserved)
- `TestSharesForDeposit_markedPot_mintsSharesFromPotNav` (M4 minting)
- `TestDepositCredit_readmeAlexBlairWorkedExample_matchesLiterals` (M4-T4 — steps 1–6 from README)
- `TestObserveSweep_postBuyDeposit_doesNotLetNewMemberCaptureUnrealizedGain`

**M4-T5 / M4-T6 — pot NAV (domain)**

- `TestComputePotNAV_usdcOnly_returnsTreasuryUsdc`
- `TestComputePotNAV_mixedPot_sumsUsdcAndMarkedHoldings`
- `TestComputePotNAV_emptyShares_firstDepositUsesOneDollarPerShare`
- `TestComputePotNAV_zeroTotalShares_returnsErrorOrBootstrapPrice`

**M4-T7 / M4-T8 — group rules (integration)**

- `TestPOST_groups_persistsJoinPolicyVoterSetThresholdExpiry`
- `TestPOST_join_openGroup_addsMemberWithoutPassword`
- `TestPOST_join_passwordGroup_requiresCorrectPassword`
- `TestPOST_join_wrongPassword_returns403`

**M4-T9 / M4-T10 — membership (integration)**

- `TestVoterSet_namedSubset_enforcesMinimumSizeOne`
- `TestVoterSet_everyMemberMode_allowsAllMembersToVote`
- `TestUser_inManyGroups_hasDistinctPositionsPerGroup`

**M4-T11 / M4-T12 / M4-T13 — catalog and proposals (integration)**

- `TestGET_assets_search_returnsBackendResolvedCatalog` (mobile never calls xStocks)
- `TestPOST_quotes_noRoute_returnsRoutableFalse`
- `TestPOST_proposals_noRoute_refusesBeforeInsert`
- `TestPOST_proposals_happyPath_createsOpenProposalWithExpiry`

**M4-T14 / M4-T15 / M4-T16 / M4-T17 / M4-T18 — votes (domain + integration)**

- `TestTallyProposal_unanimous_allYes_passes` (M4-T15)
- `TestTallyProposal_majority_moreYesThanNo_passes` (M4-T16)
- `TestTallyProposal_majority_moreNoThanYes_fails`
- `TestTallyProposal_expiredOpenProposal_failsWithoutSwap` (M4-T17)
- `TestPOST_vote_nonVoterSetMember_returns403`
- `TestPOST_vote_doubleVoteSameMember_isIdempotentOrRejected` (idempotency)
- `TestPOST_vote_concurrentDoubleVote_recordsOneBallot` (race)

**M4-T19 / M4-T20 / M4-T21 — execute on pass (integration)**

- `TestExecuteOnPass_onlyAfterTallyPassed_callsJupiter`
- `TestExecuteOnPass_setsProposalIdOnTransactionRow`
- `TestExecuteOnPass_writesNavSnapshotOnConfirm`
- `TestExecuteOnPass_duplicateProposalAndSignature_executesOnce` (M4-T21)
- `TestDevBuyRoute_deleted_returns404` (M3 stub removed)

**M4-T23 / M4-T24 / M4-T25 / M4-T26 / M4-T27 / M4-T28 / M4-T29 — P&L and boards (domain + integration)**

- `TestPercentReturn_equityOverNetInMinusOne`
- `TestPercentReturn_zeroNetIn_returnsNil` (skip row)
- `TestMemberEquity_matchesShareFractionTimesPotNav`
- `TestInGroupBoard_ranksByPercentReturnNotDollars` (M4-T26)
- `TestGroupBoard_ranksPotsByPercentReturn` (M4-T27)
- `TestPeopleBoard_aggregatesCrossGroupNetInAndEquity` (M4-T28)
- `TestInGroupBoard_fullExit_dropsMemberFromBoard` (M4-T29)
- `TestBoards_skipRowsWhenNetUsdcInZero` (M4-T25)

**M4-T30–M4-T36 — redeem (unit + integration)**

- `TestRedeem_partialByShareAmount_debitsUnitsFirst` (M4-T31, row-lock in integration)
- `TestRedeem_payoutEqualsRedeemedSliceNotDepositRefund` (M4-T36)
- `TestRedeem_withXStockInTreasury_sellsSliceBeforePayout` (M4-T32)
- `TestRedeem_invalidPayoutProof_rejected` (M4-T34)
- `TestRedeem_validProof_paysUsdcOnlyToProvenAddress` (M4-T33)
- `TestRedeem_afterPayout_incrementsAmountWithdrawnAndWritesNavSnapshot` (M4-T35)
- `TestRedeemJob_crashAfterDebit_resumesWithoutSecondDebit` (state machine)
- `TestRedeem_concurrentDoubleRedeem_debitsOnce` (race)

**M4-T37 / M4-T38 — snapshots and after-hours (integration)**

- `TestNavSnapshot_writtenOnDepositConfirmTransactionConfirmWithdrawalPayout`
- `TestMarkedPot_afterHoursFlag_surfacesOnGroupView`

**M4-T22 — idempotency suite (integration)**

- `TestIdempotency_depositSweepBuyWithdrawal_replaySignatureOnceEach`

**Product constraints encoded in tests**

- Swift consumes `GET /v1/groups/{id}/assets` only — no xStocks/Jupiter/Pyth/Solana RPC from mobile.
- Rank by percent return, never dollars.
- Redeem debits `share_units` before outbound USDC.
- Open decisions (M4-T39–T41) have no automated behavior until product locks them.

### Fixtures / fakes

- `fakePythClient` — `USDCOnlyPot`, `MarkedPot` with configurable marks and `afterHours`.
- `fakeJupiterClient`, `fakePrivyClient` — from M3; add `VerifyPayoutProof`, `PayUSDC`.
- `fakeClock` — proposal expiry and vote deadlines.
- `buildGroupWithRules(overrides)`, `buildProposal(overrides)`, `buildPosition(overrides)`, `buildMarkedHolding(overrides)`.
- `readmeAlexBlairScenario()` — factory for the worked example inputs.
- `integrationApp(t)` — real Postgres, all external clients faked.

**Testability note.** Vote tally and NAV math belong in `packages/domain`; handlers should not duplicate formulas. Redeem resume requires injectable job store and explicit `RedeemJobStatus` transitions testable without mainnet.

### Property / invariant tests

In `packages/domain` with `testing/quick` or `rapid`:

- `TestProperty_sharesForDeposit_neverExceedsDepositedValueAtCurrentNav` — new shares × nav per share ≤ deposited USDC (fairness).
- `TestProperty_memberEquity_sumsToPotNavWhenAllSharesHeld` — Σ equity == pot NAV for partition of total shares.
- `TestProperty_percentReturn_monotonicWithEquityAtFixedNetIn` — higher equity ⇒ higher or equal % return.
- `TestProperty_redeemSlice_proportionalToSharesRedeemed` — redeem fraction × pot NAV bounds payout.
- `TestProperty_tallyProposal_deterministic` — same votes ⇒ same status.
- `TestProperty_boardRanking_percentReturnOrder` — shuffled members; board order matches sorted % return.

## Manual verification

Two-account demo script on slim sim or devices. Automated tests cover math, idempotency, and board ordering; manual proves Privy/Jupiter/mainnet UX.

1. Create a group via mobile with password join, named voter subset, majority threshold, and short expiry.
2. Join from a second account with the password. Confirm two names on the in-group member board.
3. Both deposit mainnet USDC. Confirm sweep UX and boards show 0% until a mark moves.
4. Propose `AAPLx`, pass the vote. Confirm swap success in explorer and proposal UI chips.
5. On the group screen, confirm pot composition, both slices, dollar P&L, and in-group percent board order.
6. On app home, confirm group board and people board rows.
7. Partial redeem with signed payout proof. Confirm USDC arrives at the payout wallet in explorer.
8. Let a second proposal expire without pass. Confirm UI shows expired and no new swap in explorer.



## Out of scope

Custom on-chain vault or share-token program. On-chain voting. Meteora DBC, DAMM, Clawpump. Privy production webhooks. Android or web. Copy-trading. Backed issuer mint or redeem APIs. App Store listing, full KYC or AML, securities licensing.

## Open decisions

Who may propose. Failed execute after pass. Creator leave and dissolve. M4-T39 through M4-T41 record them only.

# Propose Sell Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add one mobile Propose entry for governed treasury buys and sells, with sell proposals held-asset constrained, Jupiter-routable, vote-gated, idempotently executed, and visible in proposal and activity history.

**Architecture:** Extend the existing Luna `internal/app` spine: HTTP handlers only decode/authenticate, call one app command, and encode; `GovernanceService` owns proposal validation; `ExecuteOnPassService` dispatches by proposal kind; and the existing `SwapService.SellToUSDC` → `jupiter.QuoteSell`/`SellToUSDC` → Logan `transactions` path performs passed sells. New code must follow this repository's existing design and must not invent parallel services, persistence layers, tables, or clients. `packages/domain` remains pure Go, and Swift consumes hand-written HTTP DTOs only.

**Tech Stack:** Go 1.23+, `net/http`, PostgreSQL migrations, Jupiter Swap API v2, xStocks metadata resolver, Privy treasury signing, Swift 6/SwiftUI on iOS 18+, XCTest via `packages/mobile-core`.

**Spec:** GitHub issue [#146](https://github.com/lognorman20/monaco/issues/146), with `docs/product.md`, `docs/architect/synthesis.md`, `docs/architect/sketch.md`, `docs/index.md`, `README.md`, and `AGENTS.md` as canonical repository constraints.

## Locked Decisions

1. **Sell amount is exact token atomics.** Persist `proposals.token_amount bigint`; expose JSON `tokenAmount`; use Go `int64` and Swift `Int64`; and interpret the value as the xStock SPL token's 8-decimal atomic quantity (`100_000_000` atomics = one share). Confirmed sell `transactions.amount` is those atomics. The holding ceiling is `ListNetTokenHoldingsByGroup.Amount`, which is `SUM(buy.cost_basis_amount) - SUM(sell.amount)` — buy fill output tokens minus confirmed sell input tokens — **not** buy `transactions.amount` (that column is USDC on buys). Do not store a second USDC sell notional. User-entered decimal shares convert once to atomics; Jupiter USDC output is quote/display/proceeds only.
2. **Extend `POST /v1/groups/{id}/quotes` with `kind`.** Omitted or empty `kind` means `buy`, preserving `{ "symbol", "usdc" }`. Sell requests are `{ "kind": "sell", "symbol": "AAPLx", "tokenAmount": 50000000 }`. This keeps one proposal quote gate and follows the existing `quotes.go` route rather than adding a parallel sell-quotes endpoint.
3. **Quotes become member-gated; create stays proposer-gated.** `CreateProposal` requires member + `domain.MemberMayVote`; `QuoteProposal` checks `IsGroupMember` only. Sell work must not collapse those. **Correction (verified in code):** `httpapi.QuoteHandlers.authorizeGroupMember` (`apps/backend/internal/httpapi/quotes.go`) is misnamed — it rejects with `app.ErrNotGroupMember` when `group.CreatorUserID != user.ID`, so **only the group creator can quote today**. This plan intentionally widens that helper to real membership (`Store.IsGroupMember`), keeping the existing group-exists and treasury-exists lookups and the same `not_group_member` 403 mapping. No current test asserts creator-only quoting (`quotes_test.go`, `quotes_price_test.go` have no 403/creator cases), so the widening is safe and is a prerequisite for `TestPOST_quotes_nonVoterMemberCanQuoteBuy`.
4. **Create/quote gates are price-only (no taker).** Buy create already calls `StartBuy` without `Taker`; the treasury taker is for execute (`OrderBuy` / `SellToUSDC`). Sell `QuoteSell` at quote/create must omit `Taker` the same way. Do not pass `Taker: treasury.SolanaAddress` at proposal create. Issue #146's "taker=treasury" wording referred to execute, not the create quote gate.
   **Blocker this implies (verified in code):** `jupiter.ParseSellQuoteResponse` (`apps/backend/internal/jupiter/sell.go:72`) currently calls `isSellRoutable`, which **unconditionally requires a non-empty `transaction`** field. Jupiter `/order` omits `transaction` when no `taker` is supplied, so a taker-less `QuoteSell` would always return `ErrNoRoute` against live Jupiter and every sell create would 400 `quote_not_routable` — while `jupiter.NewFakeClient` ignores `Taker` and would keep all tests green. The buy path already solved this: `ParseBuyQuoteResponse(body, requireTransaction bool)` is called with `params.Taker != ""` (`quote.go:149`). Sell must mirror it exactly (Task 3, Step 3a). This is the only change to `internal/jupiter`.
5. **Buy poller retry is unchanged; sell auto-execute is one-shot.** `ListPassedProposalsPendingExecute` today excludes a proposal only after a **confirmed buy**. Leave that buy predicate alone so failed/pending buys still get polled. For `kind='sell'`, exclude the proposal once **any** linked `action='sell'` row exists (`pending`, `failed`, or `confirmed`). Persist a terminal `failed` sell row only on sell `ExecuteOnPass` errors. Do not insert failed buy rows on buy execute errors. Manual `POST /v1/transactions/{id}/retry` remains the user retry for both kinds.
6. **Exact holdings travel with the group view.** Add optional `tokenAmount` (a decimal atomics string) to non-USDC pot rows so mobile constrains sell input using server atomics rather than reverse-converting display `units`. `units` remains display-only. **Field-name correction:** the net atomics value is `postgres.TokenHoldingRow.Amount`, but by the time pot rows are built in `potRowsFromPythInput` it is carried as `pyth.MarkedHolding.Units int64` (`pyth.MarkedHolding` has no `Amount` field; `pyth.CostBasis.Amount` is the buy-fill token amount, **not** the net holding). Emit `strconv.FormatInt(holding.Units, 10)` there.
7. **No inventory reservation.** Open or passed sell proposals do not reduce `ListNetTokenHoldingsByGroup`. Two full-size sells may both create; the second execute fails against remaining confirmed inventory and persists a failed sell. That is the chosen rule — do not add a reservation table or pending-sell deduction at create.
8. **`CreateProposal` USDC check is buy-only.** Today's `if in.UsdcMicros <= 0` runs before any kind dispatch and would reject every sell. Move it into the buy branch. Sell branch requires `TokenAmount > 0` and must not require `UsdcMicros`.

## Global Constraints

- **Existing design in every task:** follow the repository's current Luna app/HTTP/Postgres/Jupiter/Swift patterns; do not invent parallel layers, tables, clients, or a second execution path.
- **Spine:** Luna `internal/app` owns commands. HTTP handlers decode, authenticate, call ONE app method, encode JSON. Do not add a new service layer (no Composer ledger/service split).
- `packages/domain` is pure Go math/types; no I/O. Swift never imports Go. Mobile talks HTTP + Privy Swift only (never Jupiter/xStocks/Pyth/Solana RPC for product flows).
- **Persistence:** Logan tables only — deposits, positions, withdrawals, transactions. Signature/idempotency on row unique keys (`tx_signature`, `execute_request_id`). Do NOT create `sweep_tx_log`, `share_ledger`, `jupiter_orders`, `fills`, or `cost_basis` tables.
- Sells already exist for redeem: `SwapService.SellToUSDC` + `jupiter.QuoteSell`/`SellToUSDC` + `postgres.ConfirmSellTransaction` with `transactions.action='sell'`. Reuse that path for passed sell proposals. Do not duplicate Jupiter execute.
- Buys remain `CreateProposal` → vote → `ProposalExecutePoller` → `ExecuteOnPass` → `SwapService` buy. Sells mirror this and dispatch by proposal kind. `StartBuy` stays the buy gate; do not delete M4 vote-is-the-only-execute-path.
- Local DB is Docker Compose Postgres (`monaco`, port `54322`). New schema is `supabase/migrations/000009_proposal_sells.sql`. Never use hosted Supabase for tests.
- `just test backend` uses stubs/fakes only and never live Jupiter. Focused Go tests use the owning module: `go test -C packages/domain …` and `(cd apps/backend && go test ./internal/…)`. Never `go test ./packages/domain` from the repo root (`packages/domain` is its own module). `just test mobile` is host `swift test` in `packages/mobile-core` with no simulator. `just build mobile` is the iOS compile gate. Gold-sim tap-through is mandatory in Done when.
- **DB-backed commands need the dotenvx wrapper.** `scripts/apply-migrations.sh` delegates to `go run ./cmd/migrate`, which `log.Fatal`s when `DATABASE_URL` is unset, and `postgres.OpenTestDB` tests **skip** (not fail) without `DATABASE_URL`, so an unwrapped focused run can report a false PASS. Prefix every focused Postgres/app test and every migration apply in this plan with `./scripts/with-dotenv-local.sh` (or run inside `just test backend`), e.g. `./scripts/with-dotenv-local.sh scripts/apply-migrations.sh` and `./scripts/with-dotenv-local.sh bash -c 'cd apps/backend && go test ./internal/postgres -run …'`. `docker compose up -d --wait` must already have run.
- Product copy uses human stock names/symbols, never raw mints or “xStock” branding; never hyphenates wallet addresses; uses toasts over banners; and follows `MainFlowCopyManifest`. Existing cabal/group copy must not regress.
- Apps remain `apps/backend` and `apps/mobile`, never `apps/api` or `apps/ios`.
- Reuse existing vote eligibility, threshold, tally, expiry, join-request, and leave-policy behavior unchanged. A pending join requester is not a member and cannot quote/propose. Any current member may quote either kind. Only an eligible voter may create a proposal. A passed proposal remains a group command even if its proposer later leaves.
- Redeem continues using the same `SwapService.SellToUSDC` cash-out path. Proposal-specific linkage is optional input, so redeem sells retain `proposal_id IS NULL`.
- Do not add a hard-coded Jupiter minimum. Require `tokenAmount > 0`; treat `jupiter.ErrBelowMinimumSize`/`ErrNoRoute` as an unroutable quote and tell the user to increase the amount.
- Buy compatibility is mandatory: old clients may omit `kind`, existing rows migrate to `kind='buy'`, and buy JSON continues accepting numeric `usdc`.
- No new member-board math or position mutations: treasury trades alter holdings/NAV, while share units, net USDC in, join/leave rules, and member-board membership stay unchanged.

---

## File Structure Map

### Create

- `supabase/migrations/000009_proposal_sells.sql` — add proposal kind/token amount and cross-kind integrity checks.
- `apps/backend/internal/postgres/proposals_test.go` — persistence and migration-shape coverage for buy/sell proposal rows.
- `apps/backend/internal/app/proposal_sell.go` — focused sell quote/holding validation methods on existing `GovernanceService`; no new service.
- `apps/mobile/Monaco/Features/Proposals/ProposeChooserView.swift` — one Buy/Sell chooser reached from Group Detail.
- `apps/mobile/Monaco/Features/Proposals/ProposeSellView.swift` — held-symbol selection, decimal-share input, exact-atomics ceiling, quote, and submit flow.

### Modify

- `packages/domain/votes.go` — `ProposalKind`, parser, and kind/token fields on `Proposal`.
- `packages/domain/types_test.go` — kind parser tests.
- `packages/domain/factories_test.go` — buy default fixture fields.
- `apps/backend/internal/postgres/proposals.go` — nullable buy notional, sell amount, kind-aware insert/get/list/poller scans.
- `apps/backend/internal/postgres/transactions.go` — action-aware proposal lookup, proposal-linked failed sells, weighted remaining cost basis.
- `apps/backend/internal/postgres/nav_snapshots.go` — use the canonical 8-decimal xStock atomic scale.
- `apps/backend/internal/jupiter/sell.go` — add `requireTransaction` to `ParseSellQuoteResponse`/`isSellRoutable` so taker-less sell quotes are price-only, mirroring `ParseBuyQuoteResponse`.
- `apps/backend/internal/jupiter/quote_test.go` — price-only vs executable sell-quote parse coverage (only in-repo caller of `ParseSellQuoteResponse` is `sell.go:58`).
- `apps/backend/internal/app/governance.go` — kind dispatch, shared proposal authorization/expiry persistence, and the updated `InsertProposalTx` params-struct call site (line ~643 is the only caller of the old positional signature).
- `apps/backend/internal/app/governance_types.go` — proposal kind aliases/constants.
- `apps/backend/internal/app/swap.go` — reusable sell quote command and optional proposal linkage on sell pending rows.
- `apps/backend/internal/app/start_buy.go` — kind-dispatched `ExecuteOnPass`, action-aware idempotency/linking, terminal failed transaction recording.
- `apps/backend/internal/app/marked_pot.go` — canonical xStock scale and exact `TokenAmount` on pot rows.
- `apps/backend/internal/app/group_view_test.go` — update existing pot/NAV expectations after 8-decimal scale.
- `apps/backend/internal/app/proposal_queries.go` — kind-aware list/detail and action-aware execution status.
- `apps/backend/internal/app/group_activity.go` — sell proposal pending rows and sold-symbol/token/proceeds activity fields.
- `apps/backend/internal/app/transaction_retry.go` — action-aware idempotency and sell proposal linkage on retry.
- `apps/backend/internal/app/governance_test.go` — sell create ceiling, route, membership, expiry/threshold reuse.
- `apps/backend/internal/app/start_buy_test.go` — sell execute, idempotency, NAV/holding, and redeem regression tests.
- `apps/backend/internal/app/group_activity_test.go` — sell proposal pending/confirmed/failed copy data.
- `apps/backend/internal/app/transaction_retry_test.go` — proposal-linked sell retry behavior.
- `apps/backend/internal/app/marked_pot_test.go` — 8-decimal units and exact pot token amount.
- `apps/backend/internal/httpapi/proposals.go` — backward-compatible create contract and kind-aware list/detail responses/errors.
- `apps/backend/internal/httpapi/quotes.go` — unified buy/sell quote request/response and error mapping.
- `apps/backend/internal/httpapi/groups.go` — exact token/proceeds activity JSON and pot `tokenAmount`.
- `apps/backend/internal/httpapi/transactions.go` — clarify sell amount/proceeds response while retaining existing fields.
- `apps/backend/internal/httpapi/proposals_test.go` — HTTP create/list/detail sell contracts and buy compatibility.
- `apps/backend/internal/httpapi/quotes_test.go` — unified quote-side tests.
- `apps/backend/internal/httpapi/home_test.go` — group-view and activity sell amount JSON contracts.
- `apps/backend/internal/httpapi/transactions_test.go` — sell proceeds detail and retry HTTP contracts.
- `apps/backend/internal/worker/proposal_execute_poller.go` — pass full kind/amount and dispatch through `ExecuteOnPass`.
- `apps/backend/internal/worker/proposal_execute_poller_test.go` — buy/sell dispatch and terminal failure behavior.
- `apps/backend/cmd/api/main.go` — wire existing `SwapService` into governance/quotes and document the extended route.
- `packages/mobile-core/Sources/MonacoCore/ProposalDTO.swift` — proposal kind and optional side-specific amounts.
- `packages/mobile-core/Sources/MonacoCore/BuyQuoteDTO.swift` — unified quote DTO and sell submit gate.
- `packages/mobile-core/Sources/MonacoCore/GroupViewDTO.swift` — exact optional `tokenAmount` for held stocks.
- `packages/mobile-core/Sources/MonacoCore/MonacoAPIClient.swift` — kind-aware quote/create calls.
- `packages/mobile-core/Sources/MonacoCore/MainFlowCopyAudit.swift` — replace “Propose buy” entry copy with “Propose”, add buy/sell sub-flow copy.
- `packages/mobile-core/Tests/MonacoCoreTests/ProposalDTOTests.swift` — buy-default and sell decode tests.
- `packages/mobile-core/Tests/MonacoCoreTests/ProposalsAPITests.swift` — exact buy/sell request bodies and quote responses.
- `packages/mobile-core/Tests/MonacoCoreTests/GroupViewDTOTests.swift` — exact held token amount decode.
- `packages/mobile-core/Tests/MonacoCoreTests/MainFlowCopyAuditTests.swift` — unified Propose copy audit.
- `apps/mobile/Monaco/API/DTOs/ProposalDTO.swift` — app-side kind/optional amount fields.
- `apps/mobile/Monaco/API/DTOs/BuyQuoteDTO.swift` — app-side unified quote response.
- `apps/mobile/Monaco/API/DTOs/GroupViewDTO.swift` — app-side exact pot `tokenAmount`.
- `apps/mobile/Monaco/API/DTOs/GroupActivityDTO.swift` — optional sell token/proceeds fields.
- `apps/mobile/Monaco/API/DTOs/TransactionDetailDTO.swift` — explicit optional `proceedsUsdcMicros`.
- `apps/mobile/Monaco/API/MonacoAPIClient.swift` — authenticated buy/sell quote/create requests.
- `apps/mobile/Monaco/Features/Groups/GroupDetailView.swift` — replace Propose buy with one Propose link.
- `apps/mobile/Monaco/Features/Groups/ProposalHistorySection.swift` — distinct Buy/Sell rows and amount copy.
- `apps/mobile/Monaco/Features/Groups/GroupActivitySection.swift` — sold stock/token amount/proceeds copy and sell pending fallback.
- `apps/mobile/Monaco/Features/Groups/TransactionDetailView.swift` — 8-decimal token amount and sell proceeds labels.
- `apps/mobile/Monaco/Features/Groups/ActivityDetailDestination.swift` — allow pending sell proposal IDs to fall back to proposal detail.
- `apps/mobile/Monaco/Features/Proposals/ProposeQuoteDetailView.swift` — explicit buy kind in API calls; buy behavior otherwise unchanged.
- `apps/mobile/Monaco/Features/Proposals/ProposalDetailView.swift` — kind-aware title, amount, execution copy.
- `apps/mobile/Monaco/Features/Proposals/ProposalStatusChip.swift` — side + status label/accessibility identifier.

The Xcode project uses a file-system-synchronized `Monaco` group, so creating the two Swift files does not require editing `apps/mobile/Monaco.xcodeproj/project.pbxproj`.

---

### Task 1: Lock Domain and SQL Proposal Contract

**Files:**
- Create: `supabase/migrations/000009_proposal_sells.sql`
- Modify: `packages/domain/votes.go`
- Modify: `packages/domain/types_test.go`
- Modify: `packages/domain/factories_test.go`

**Interfaces:**
- Consumes: existing `domain.ProposalStatus`, proposal table from migration `000005`.
- Produces:
  - `type ProposalKind string`
  - `const ProposalKindBuy ProposalKind = "buy"` and `ProposalKindSell = "sell"`
  - `func ParseProposalKind(raw string) (ProposalKind, error)`
  - `domain.Proposal{Kind ProposalKind, UsdcMicros int64, TokenAmount int64}`
  - SQL columns `proposals.kind text NOT NULL DEFAULT 'buy'`, `proposals.token_amount bigint NULL`, nullable `usdc_micros`.

**Existing-design constraint:** Extend the pure domain type and the existing additive migration sequence only; add no I/O to `packages/domain` and no new persistence table.

- [ ] **Step 1: Write failing domain tests**

```go
func TestParseProposalKind_validValues(t *testing.T) {
	for _, raw := range []string{"buy", "sell"} {
		got, err := ParseProposalKind(raw)
		if err != nil || string(got) != raw {
			t.Fatalf("ParseProposalKind(%q) = %q, %v", raw, got, err)
		}
	}
}

func TestParseProposalKind_rejectsUnknown(t *testing.T) {
	if _, err := ParseProposalKind("redeem"); err == nil {
		t.Fatal("expected invalid proposal kind")
	}
}
```

- [ ] **Step 2: Run the focused tests and verify failure**

Run: `go test -C packages/domain -run 'TestParseProposalKind|TestBuildProposal'`

Expected: FAIL because `ProposalKind` and `ParseProposalKind` do not exist.

- [ ] **Step 3: Add the pure domain type**

```go
type ProposalKind string

const (
	ProposalKindBuy  ProposalKind = "buy"
	ProposalKindSell ProposalKind = "sell"
)

func ParseProposalKind(raw string) (ProposalKind, error) {
	switch ProposalKind(raw) {
	case ProposalKindBuy, ProposalKindSell:
		return ProposalKind(raw), nil
	default:
		return "", fmt.Errorf("invalid proposal kind: %q", raw)
	}
}
```

Add `Kind` and `TokenAmount` to `Proposal`, and set `Kind: ProposalKindBuy` in `buildProposal`.

- [ ] **Step 4: Add the additive migration with cross-kind integrity**

```sql
-- Sell quantities are exact 8-decimal xStock SPL atomics:
-- 100000000 token_amount = one whole share.
ALTER TABLE proposals
  ADD COLUMN IF NOT EXISTS kind text NOT NULL DEFAULT 'buy',
  ADD COLUMN IF NOT EXISTS token_amount bigint,
  ALTER COLUMN usdc_micros DROP NOT NULL;

ALTER TABLE proposals DROP CONSTRAINT IF EXISTS proposals_kind_check;
ALTER TABLE proposals
  ADD CONSTRAINT proposals_kind_check CHECK (kind IN ('buy', 'sell'));

ALTER TABLE proposals DROP CONSTRAINT IF EXISTS proposals_amount_by_kind_check;
ALTER TABLE proposals
  ADD CONSTRAINT proposals_amount_by_kind_check CHECK (
    (kind = 'buy' AND usdc_micros IS NOT NULL AND usdc_micros > 0 AND token_amount IS NULL)
    OR
    (kind = 'sell' AND token_amount IS NOT NULL AND token_amount > 0 AND usdc_micros IS NULL)
  );
```

Leave migration `000005`'s inline `CHECK (usdc_micros > 0)` in place: PostgreSQL treats a CHECK evaluating to `NULL` as satisfied, so a sell row with `usdc_micros IS NULL` passes it. Do not drop or rewrite that shared constraint. Every existing row is a buy with `usdc_micros > 0` and `token_amount IS NULL`, so `proposals_amount_by_kind_check` validates against current data without a backfill. `ADD COLUMN kind text NOT NULL DEFAULT 'buy'` backfills existing rows to `'buy'`. Migrations are directory-scanned by `postgres.Apply`, so no Go file needs editing to register `000008`.

- [ ] **Step 5: Apply and inspect the migration locally**

Run: `docker compose up -d --wait && ./scripts/with-dotenv-local.sh scripts/apply-migrations.sh`

Run:

```bash
docker compose exec -T postgres psql -U monaco -d monaco -c \
  "\d+ proposals"
```

Expected: `kind` defaults to `buy`, `token_amount` and `usdc_micros` are nullable, and `proposals_amount_by_kind_check` is present. The script applies only local Docker Postgres.

- [ ] **Step 6: Document migration rollback behavior in the implementation commit**

Application rollback is safe without dropping columns because old code reads existing buy columns and every old/new buy row defaults to `kind='buy'`. Before the migration is shared, local schema rollback is: revert `000009_proposal_sells.sql`, then run `just reset db` to recreate local `monaco` and `monaco_test`; never edit an already-shared migration or target hosted Supabase.

Note: `scripts/ensure-test-database.sh` (run by `just test backend`) must also see `000008`, so run the full `just test backend` gate at least once before relying on focused `internal/postgres` runs.

- [ ] **Step 7: Re-run domain tests**

Run: `go test -C packages/domain -run 'TestParseProposalKind|TestBuildProposal'`

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add supabase/migrations/000009_proposal_sells.sql packages/domain/votes.go packages/domain/types_test.go packages/domain/factories_test.go
git commit -m "feat: add buy and sell proposal contract"
```

### Task 2: Persist Kind-Aware Proposals

**Files:**
- Create: `apps/backend/internal/postgres/proposals_test.go`
- Modify: `apps/backend/internal/postgres/proposals.go`

**Interfaces:**
- Consumes: Task 1 SQL/domain contract.
- Produces:
  - `type InsertProposalParams struct { GroupID, ProposerID, Symbol string; Kind domain.ProposalKind; UsdcMicros, TokenAmount int64; ExpiresAt time.Time }`
  - `func (s *Store) InsertProposalTx(ctx context.Context, tx *sql.Tx, params InsertProposalParams) (ProposalRow, error)`
  - `ProposalRow.Kind domain.ProposalKind`, `UsdcMicros int64`, `TokenAmount int64`.
  - Kind-aware `ListPassedProposalsPendingExecute`: buy rows still excluded only after a confirmed buy; sell rows excluded once any linked `action='sell'` transaction exists.

This replaces the current positional signature `InsertProposalTx(ctx, tx, groupID, proposerID, symbol string, usdcMicros int64, expiresAt time.Time)`. `apps/backend/internal/app/governance.go` (line ~643) is the **only** in-repo caller and must be updated in the same commit or the backend will not compile; add it to this task's `git add`.

**Existing-design constraint:** Keep all persistence in the existing Postgres store and `proposals`/`transactions` tables; do not add repositories, ledgers, or order tables.

- [ ] **Step 1: Write failing persistence tests**

```go
func TestInsertProposalTx_roundTripsBuyAndSellAmounts(t *testing.T) {
	// Seed one group/member with existing test helpers, then insert:
	// buy: KindBuy, UsdcMicros=2_000_000, TokenAmount=0
	// sell: KindSell, UsdcMicros=0, TokenAmount=50_000_000
	// Assert GetProposalByID preserves kind and only the side-specific amount.
}

func TestInsertProposalTx_rejectsAmountForWrongKind(t *testing.T) {
	// Insert sell with UsdcMicros > 0 and assert proposals_amount_by_kind_check rejects it.
}

func TestListPassedProposalsPendingExecute_failedBuyStillListed(t *testing.T) {
	// Passed buy + linked failed buy row must still be returned for the poller.
}

func TestListPassedProposalsPendingExecute_failedSellNotListed(t *testing.T) {
	// Passed sell + linked failed sell row must not be returned.
}
```

- [ ] **Step 2: Run and verify failure**

Run: `(cd apps/backend && go test ./internal/postgres -run 'TestInsertProposalTx_|TestListPassedProposalsPendingExecute_')`

Expected: FAIL because kind-aware fields and insert params do not exist.

- [ ] **Step 3: Replace positional proposal scans with one exact column list**

Use this list in insert/get/list/poller queries:

```sql
id, group_id, proposer_id, symbol, kind, usdc_micros, token_amount,
status, expires_at, created_at
```

Scan nullable amounts through `sql.NullInt64`, assigning zero to the non-applicable Go field. Parse `kind` with `domain.ParseProposalKind`.

- [ ] **Step 4: Implement kind-aware insert validation**

```go
switch params.Kind {
case domain.ProposalKindBuy:
	if params.UsdcMicros <= 0 || params.TokenAmount != 0 {
		return ProposalRow{}, fmt.Errorf("buy proposal requires positive usdc_micros only")
	}
case domain.ProposalKindSell:
	if params.TokenAmount <= 0 || params.UsdcMicros != 0 {
		return ProposalRow{}, fmt.Errorf("sell proposal requires positive token_amount only")
	}
default:
	return ProposalRow{}, fmt.Errorf("invalid proposal kind")
}
```

Pass SQL `NULL` for the unused amount.

- [ ] **Step 5: Keep buy poller selection; make sell auto-execute one-shot**

Do **not** change `ListPassedProposalsAwaitingExecuteByGroupID` (UI still treats any linked row as no longer awaiting). Change only `ListPassedProposalsPendingExecute` (the poller):

```sql
WHERE p.status = 'passed'
  AND (
    (
      p.kind = 'buy'
      AND NOT EXISTS (
        SELECT 1 FROM transactions t
        WHERE t.proposal_id = p.id
          AND t.action = 'buy'
          AND t.status = 'confirmed'
      )
    )
    OR
    (
      p.kind = 'sell'
      AND NOT EXISTS (
        SELECT 1 FROM transactions t
        WHERE t.proposal_id = p.id
          AND t.action = 'sell'
      )
    )
  )
```

Add tests: a passed buy with a linked `failed` buy is still listed; a passed sell with a linked `failed` sell is not. Manual retry remains Task 6.

- [ ] **Step 6: Run persistence tests**

Run: `(cd apps/backend && go test ./internal/postgres -run 'TestInsertProposalTx_|TestListPassedProposalsPendingExecute_')`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add apps/backend/internal/postgres/proposals.go apps/backend/internal/postgres/proposals_test.go apps/backend/internal/app/governance.go
git commit -m "feat: persist buy and sell proposals"
```

### Task 3: Add Sell Quote and Create Validation in Luna App

**Files:**
- Create: `apps/backend/internal/app/proposal_sell.go`
- Modify: `apps/backend/internal/jupiter/sell.go`
- Modify: `apps/backend/internal/jupiter/quote_test.go`
- Modify: `apps/backend/internal/app/governance.go`
- Modify: `apps/backend/internal/app/governance_types.go`
- Modify: `apps/backend/internal/app/swap.go`
- Modify: `apps/backend/internal/app/governance_test.go`

**Interfaces:**
- Consumes: existing `BuyService.StartBuy`, `SwapService`, `xstocks.Resolver` already held by `BuyService`, `postgres.ListNetTokenHoldingsByGroup`, Privy treasury.
- Produces:
  - `type QuoteProposalInput struct { GroupID, UserID, Symbol string; Kind ProposalKind; UsdcMicros, TokenAmount int64 }`
  - `type QuoteProposalResult struct { Kind ProposalKind; Symbol string; UsdcMicros, TokenAmount int64; Routable bool; OutputAmount string; OutputUsdcMicros string; PriceUsdcMicros string }`
  - `func (g *GovernanceService) QuoteProposal(ctx context.Context, in QuoteProposalInput) (QuoteProposalResult, error)`
  - `func (g *GovernanceService) SetSwapService(swap *SwapService)`
  - `CreateProposalInput.Kind`, `.TokenAmount`.
  - `var ErrExceedsTreasuryHolding = errors.New("exceeds treasury holding")`.

**Existing-design constraint:** Add methods to the current `GovernanceService`/`SwapService`; do not create a sell service, a second xStocks/Jupiter client, or a parallel command layer.

- [ ] **Step 1: Write failing sell-create tests**

Add real tests:

```go
func TestCreateProposal_sellHeldAmount_createsOpenProposal(t *testing.T)
func TestCreateProposal_sellExceedsHolding_rejected(t *testing.T)
func TestCreateProposal_sellUnroutableQuote_rejected(t *testing.T)
func TestCreateProposal_sellBelowJupiterMinimum_rejectedAsUnroutable(t *testing.T)
func TestCreateProposal_sellPendingJoinRequester_rejected(t *testing.T)
func TestCreateProposal_sellReusesVoteExpiryAndThreshold(t *testing.T)
func TestCreateProposal_sellDoesNotHitBuyUsdcPositiveGuard(t *testing.T)
func TestQuoteProposal_nonEligibleVoterMember_canQuote(t *testing.T)
```

Seed a confirmed buy through `ConfirmBuyTransaction` so holding inventory is derived from the existing `transactions` table; register the xStocks fake mint, fake sell quote, fake Privy treasury, and no live network clients.

- [ ] **Step 2: Run and verify failure**

Run: `(cd apps/backend && go test ./internal/app -run 'TestCreateProposal_sell')`

Expected: FAIL because sell proposal input and validation do not exist.

- [ ] **Step 3a: Make taker-less sell quotes price-only in `internal/jupiter`**

Without this, every real sell create returns `quote_not_routable` while fakes stay green (see Locked Decision 4). Mirror the buy signature exactly:

```go
// sell.go
quote, err := ParseSellQuoteResponse(body, params.Taker != "")

// ParseSellQuoteResponse parses Jupiter v2 sell order JSON.
// requireTransaction mirrors ParseBuyQuoteResponse: price-only quotes omit
// transaction; executable quotes must include a buildable unsigned transaction.
func ParseSellQuoteResponse(body []byte, requireTransaction bool) (SellQuote, error)

func isSellRoutable(raw orderResponse, requireTransaction bool) bool {
	// ...unchanged error checks...
	if requireTransaction && strings.TrimSpace(raw.Transaction) == "" {
		return false
	}
	// ...unchanged outAmount check...
}
```

`sell.go:58` is the only in-repo caller. Add `quote_test.go` cases matching the existing buy pair: a sell order body with no `transaction` parses routable when `requireTransaction=false` and returns `ErrNoRoute` when `true`. Do not change `SellToUSDC` execute, which always passes the treasury taker.

- [ ] **Step 3: Add reusable sell quote behavior to existing `SwapService`**

```go
type SellQuoteRequest struct {
	GroupID, UserID, Symbol string
	TokenAmount             int64
	Taker                   string // empty at quote/create; treasury address only at execute
}

type SellQuoteResult struct {
	InputMint string
	Quote     jupiter.SellQuote
}

func (s *SwapService) QuoteSell(ctx context.Context, req SellQuoteRequest) (SellQuoteResult, error)
```

Resolve the mint through `s.buy.ResolveOutputMint`, call the existing `s.jupiter.QuoteSell` with `Amount: req.TokenAmount` and `Taker: req.Taker` (empty string at quote/create), and map `jupiter.ErrNoRoute` plus `jupiter.ErrBelowMinimumSize` to `ErrQuoteNotRoutable`. This method only quotes; it never signs or executes.

- [ ] **Step 4: Implement `GovernanceService.QuoteProposal` on the existing service**

Follow the existing design in `proposal_sell.go`; do not introduce a second governance/sell layer. `QuoteProposal` validates **current group membership only** (`IsGroupMember`), matching `QuoteHandler.authorizeGroupMember`. It must **not** call `MemberMayVote` / `ErrNotEligibleProposer`. For sell:

1. Resolve input mint via `BuyService.ResolveOutputMint`.
2. Find that mint in `ListNetTokenHoldingsByGroup` (confirmed `buy.cost_basis_amount` minus confirmed `sell.amount`).
3. Reject zero or `TokenAmount > holding.Amount` with `ErrExceedsTreasuryHolding`. Do not subtract open/passed sell proposals from the ceiling.
4. Call `g.swap.QuoteSell` with `Taker: ""` (price-only, same as buy `StartBuy` at create). Do not load the treasury for a taker at this gate.
5. Return Jupiter `OutAmount` as `OutputUsdcMicros`.

For buy, call the existing `StartBuy` **without** `Taker` and preserve its price-only behavior.

- [ ] **Step 5: Dispatch `CreateProposal` by kind without changing governance policy**

Normalize empty kind to buy. **Delete or relocate** the current top-of-function `if in.UsdcMicros <= 0` so it cannot run for sells. Shared checks first: IDs, symbol, `IsGroupMember`, `GetGroupRules`, `MemberMayVote` (eligible proposer), then:

```go
switch kind {
case ProposalKindBuy:
	if in.UsdcMicros <= 0 || in.TokenAmount != 0 {
		return Proposal{}, fmt.Errorf("usdc must be positive")
	}
	// Existing treasury total + price-only StartBuy (no Taker).
case ProposalKindSell:
	if in.TokenAmount <= 0 || in.UsdcMicros != 0 {
		return Proposal{}, fmt.Errorf("token amount must be positive")
	}
	// Exact confirmed holding + price-only QuoteSell (no Taker).
default:
	return Proposal{}, fmt.Errorf("invalid proposal kind")
}
```

Insert one `postgres.InsertProposalParams`. Vote tally, threshold, expiry, member board, positions, join requests, and leave behavior remain unchanged. Open sell proposals do not reserve inventory.

- [ ] **Step 6: Run app tests**

Run: `(cd apps/backend && go test ./internal/app -run 'TestCreateProposal_(sell|exceeds|jupiter)|TestTallyProposal')`

Expected: PASS, including existing buy tests.

- [ ] **Step 7: Commit**

```bash
git add apps/backend/internal/jupiter/sell.go apps/backend/internal/jupiter/quote_test.go apps/backend/internal/app/proposal_sell.go apps/backend/internal/app/governance.go apps/backend/internal/app/governance_types.go apps/backend/internal/app/swap.go apps/backend/internal/app/governance_test.go
git commit -m "feat: validate governed treasury sells"
```

### Task 4: Extend Quote and Proposal HTTP Contracts

**Files:**
- Modify: `apps/backend/internal/httpapi/quotes.go`
- Modify: `apps/backend/internal/httpapi/proposals.go`
- Modify: `apps/backend/internal/httpapi/quotes_test.go`
- Modify: `apps/backend/internal/httpapi/proposals_test.go`
- Modify: `apps/backend/cmd/api/main.go`

**Interfaces:**
- Consumes: Task 3 `GovernanceService.QuoteProposal` and `CreateProposal`.
- Produces:
  - Quote request: `kind?: "buy"|"sell"`, `symbol: string`, `usdc?: int64`, `tokenAmount?: int64`.
  - Sell quote response: `kind`, `symbol`, `tokenAmount` string, `routable`, `outputUsdcMicros` string.
  - Proposal list/detail: `kind`, optional `usdcMicros`, optional `tokenAmount`.
  - Error codes/messages:
    - `invalid_kind`: `kind must be buy or sell`
    - `invalid_token_amount`: `tokenAmount must be positive`
    - `exceeds_treasury_holding`: `amount exceeds treasury holding`
    - existing `quote_not_routable`: `quote not routable`.

**Existing-design constraint:** Extend the existing handlers and route; each handler decodes/authenticates, calls one Luna app method, and encodes without direct Postgres/Jupiter orchestration.

- [ ] **Step 1: Write failing HTTP tests**

```go
func TestPOST_quotes_sellReturnsTokenAmountAndUSDCOutput(t *testing.T)
func TestPOST_quotes_nonVoterMemberCanQuoteBuy(t *testing.T)
func TestPOST_proposals_sellHappyPathReturnsProposalID(t *testing.T)
func TestPOST_proposals_sellExceedsHoldingReturns400(t *testing.T)
func TestPOST_proposals_omittedKindDefaultsBuy(t *testing.T)
func TestGET_proposalDetail_sellReturnsKindAndTokenAmount(t *testing.T)
func TestGET_groupProposals_buyAndSellUseSideSpecificAmounts(t *testing.T)
```

Assert exact bodies, including that legacy buy response omits `tokenAmount` and sell response omits `usdcMicros`.

- [ ] **Step 2: Run and verify failure**

Run: `(cd apps/backend && go test ./internal/httpapi -run 'Test(POST_(quotes|proposals)|GET_(proposalDetail|groupProposals))')`

Expected: new sell tests FAIL.

- [ ] **Step 3: Parse the unified quote request**

```go
type quoteRequest struct {
	Kind        string `json:"kind"`
	Symbol      string `json:"symbol"`
	USDC        *int64 `json:"usdc"`
	TokenAmount *int64 `json:"tokenAmount"`
}
```

Default empty kind to buy. Enforce exactly one side-specific positive amount. Authenticate with `QuoteHandlers.authorizeGroupMember`, then call `Governance.QuoteProposal`. Do not require eligible proposer on this route. Keep the path `POST /v1/groups/{id}/quotes`.

**Required fix in the same step (see Locked Decision 3):** `authorizeGroupMember` in `quotes.go` currently returns `app.ErrNotGroupMember` unless `group.CreatorUserID == user.ID`, i.e. it is creator-only despite its name. Replace that single check with `h.Store.IsGroupMember(ctx, groupID, user.ID)`:

```go
member, err := h.Store.IsGroupMember(ctx, groupID, user.ID)
if err != nil {
	return "", err
}
if !member {
	return "", app.ErrNotGroupMember
}
```

Keep the surrounding `GetGroupByID` (404 on missing group) and `GetTreasuryByGroupID` lookups and the existing `writeQuoteError` mapping unchanged, so `not_group_member` still returns 403. This is what makes `TestPOST_quotes_nonVoterMemberCanQuoteBuy` meaningful; without it, buy quotes for non-creator members stay broken and the sell quote route inherits the same defect.

- [ ] **Step 4: Parse the backward-compatible proposal request**

Use the same optional fields. For omitted kind, default buy. Return the exact 400 codes/messages above before calling the app command for malformed bodies; map `ErrExceedsTreasuryHolding` and `ErrQuoteNotRoutable` in `writeProposalCreateError`.

- [ ] **Step 5: Encode kind-aware list/detail**

Use pointers with `omitempty`:

```go
Kind        string  `json:"kind"`
UsdcMicros *string `json:"usdcMicros,omitempty"`
TokenAmount *string `json:"tokenAmount,omitempty"`
```

Always emit `kind`; emit only the applicable amount. Existing clients continue decoding buy fields.

- [ ] **Step 6: Wire existing objects in `main.go`**

Call `governance.SetSwapService(swap)`, set `QuoteHandlers.Governance = governance`, retain the single `POST /v1/groups/{id}/quotes` route in `apiRoutes`, and do not construct another Jupiter/xStocks/Privy client.

- [ ] **Step 7: Run HTTP tests**

Run: `(cd apps/backend && go test ./internal/httpapi -run 'Test(POST_(quotes|proposals)|GET_(proposalDetail|groupProposals))')`

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add apps/backend/internal/httpapi/quotes.go apps/backend/internal/httpapi/proposals.go apps/backend/internal/httpapi/quotes_test.go apps/backend/internal/httpapi/proposals_test.go apps/backend/cmd/api/main.go
git commit -m "feat: expose buy and sell proposal APIs"
```

### Task 5: Dispatch Passed Sells Through Existing Swap Path

**Files:**
- Modify: `apps/backend/internal/postgres/transactions.go`
- Modify: `apps/backend/internal/app/swap.go`
- Modify: `apps/backend/internal/app/start_buy.go`
- Modify: `apps/backend/internal/app/start_buy_test.go`

**Interfaces:**
- Consumes: `Proposal.Kind`, `Proposal.TokenAmount`, existing `SwapService.SellToUSDC`.
- Produces:
  - `SellToUSDCRequest.ProposalID string`.
  - `func (s *Store) GetLatestTransactionByProposalAndAction(ctx context.Context, proposalID, action string) (TransactionRow, bool, error)`.
  - `func (s *Store) GetConfirmedTransactionByProposalAndAction(ctx context.Context, proposalID, action string) (TransactionRow, bool, error)`.
  - `func (s *Store) InsertFailedProposalTransaction(ctx context.Context, params InsertPendingTransactionParams) (TransactionRow, error)`.
  - Kind-dispatched `ExecuteOnPass(ctx context.Context, proposal Proposal)`.

**Existing-design constraint:** Reuse `ExecuteOnPassService`, `SwapService.SellToUSDC`, Jupiter execute/poll, and Logan transactions; do not add a sibling executor or duplicate sell execution.

- [ ] **Step 1: Write failing execute tests**

```go
func TestExecuteOnPass_sellCallsExistingSellPathAndLinksProposal(t *testing.T)
func TestExecuteOnPass_sellDuplicateProposalExecutesOnce(t *testing.T)
func TestExecuteOnPass_sellDecreasesHoldingAndWritesNavSnapshot(t *testing.T)
func TestExecuteOnPass_sellFailurePersistsTerminalLinkedTransaction(t *testing.T)
func TestExecuteOnPass_buyRemainsVoteOnly(t *testing.T)
func TestRedeem_sellPathStillLeavesProposalIDNull(t *testing.T)
```

Use `jupiter.NewFakeClient`, `RegisterSellQuote`, `RegisterExecutePoll`, fake Privy signer, and confirmed-buy inventory seeded so `ListNetTokenHoldingsByGroup` is positive (`ConfirmBuyTransaction` with `CostBasisAmount` = token atomics). Assert confirmed action is `sell`, input mint is the catalog mint, output mint is USDC, sell `transactions.amount == proposal.TokenAmount`, and `proposal_id` is set. Do not assert sell amount against buy `transactions.amount`.

- [ ] **Step 2: Run and verify failure**

Run: `(cd apps/backend && go test ./internal/app -run 'TestExecuteOnPass_(sell|buy)|TestRedeem_sellPath')`

Expected: sell tests FAIL.

- [ ] **Step 3: Preserve proposal linkage inside the sell path**

Pass `ProposalID` into `InsertPendingTransaction`:

```go
InsertPendingTransactionParams{
	GroupID: req.GroupID, ProposalID: req.ProposalID,
	Action: postgres.TransactionActionSell,
	InputMint: req.InputMint, OutputMint: jupiter.USDCMint,
	Amount: req.Amount, ExecuteRequestID: quote.RequestID,
}
```

Redeem does not set `ProposalID`, preserving its existing cash-out behavior.

- [ ] **Step 4: Generalize proposal transaction lookup by action**

Replace buy-only lookup internals with action-parameterized SQL and keep thin buy wrappers only where existing callers need them. Reject actions other than `buy`/`sell` before querying.

- [ ] **Step 5: Dispatch `ExecuteOnPass`**

```go
switch proposal.Kind {
case ProposalKindBuy:
	return s.executeBuyOnPass(ctx, proposal)
case ProposalKindSell:
	return s.executeSellOnPass(ctx, proposal)
default:
	return ExecuteOnPassResult{}, fmt.Errorf("invalid proposal kind")
}
```

Dispatch must be the first thing `ExecuteOnPass` does. Today's body validates `proposal.UsdcMicros <= 0` (`start_buy.go:163`) and logs `proposal.UsdcMicros` via `logExecuteOnPassStart` **before** any kind check, exactly like the `CreateProposal` guard in Locked Decision 8. Move both the guard and the amount log field into `executeBuyOnPass`; the sell branch logs `token_amount`. Leaving either at the top makes every passed sell fail execute forever.

The sell branch validates passed status/IDs/symbol/token amount, checks a confirmed sell for `(proposal_id, action='sell')`, resolves the mint through the existing buy resolver, calls `SwapService.SellToUSDC` (execute path **does** use the treasury taker, as buy `OrderBuy` does), and links defensively with the existing `SetTransactionProposalID`. If remaining confirmed holding is below `TokenAmount` because another sell already filled, persist a failed sell and return the execution error.

- [ ] **Step 6: Persist terminal execution failure for sells only**

On **sell** `ExecuteOnPass` errors (before or after Jupiter order creation), first reuse an existing linked pending/failed **sell** row; otherwise insert one `failed` transaction with the proposal ID, `action='sell'`, mints, and proposal token amount. Return the original execution error. The sell predicate in `ListPassedProposalsPendingExecute` then excludes it until explicit retry.

`InsertFailedProposalTransaction` reuses `InsertPendingTransactionParams` but must **not** copy `InsertPendingTransaction`'s `execute_request_id is required` guard: pre-order failures (holding shortfall, mint resolution, quote refusal) have no request id, and `transactions.execute_request_id` is `UNIQUE`, so reusing an id already written by `SellToUSDC` would raise a constraint violation. Insert `NULL` when the id is empty, and require positive `Amount` (`transactions.amount` has `CHECK (amount > 0)`). Do not try to attach the proposal afterwards with `SetTransactionProposalID` — that statement only matches `status = 'confirmed'` rows.

Reuse detail: for failures after `InsertPendingTransaction`, `SwapService.markSwapFailed` already flips the proposal-linked pending row to `failed` via `FailTransactionByExecuteRequestID`, preserving `proposal_id`. That is the row the reuse lookup must find, so only one failed row per attempt exists.

Do **not** insert a failed buy row when buy `ExecuteOnPass` errors. Buy poller behavior stays “retry until a confirmed buy exists.”

- [ ] **Step 7: Verify holdings and NAV**

Do not write a new holdings table. After a confirmed sell, `ListNetTokenHoldingsByGroup` is `SUM(buy.cost_basis_amount) - SUM(sell.amount)` for that mint. The existing `SellToUSDC` created branch writes one `transaction_confirm` NAV snapshot. Assert both effects; avoid adding a duplicate snapshot.

- [ ] **Step 8: Run execute/redeem regression tests**

Run: `(cd apps/backend && go test ./internal/app -run 'TestExecuteOnPass_(sell|buy)|TestRedeem_sellPath')`

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add apps/backend/internal/postgres/transactions.go apps/backend/internal/app/swap.go apps/backend/internal/app/start_buy.go apps/backend/internal/app/start_buy_test.go
git commit -m "feat: execute passed sell proposals"
```

### Task 6: Make Polling, Detail, Retry, and Activity Kind-Aware

**Files:**
- Modify: `apps/backend/internal/worker/proposal_execute_poller.go`
- Modify: `apps/backend/internal/worker/proposal_execute_poller_test.go`
- Modify: `apps/backend/internal/app/proposal_queries.go`
- Modify: `apps/backend/internal/app/group_activity.go`
- Modify: `apps/backend/internal/app/transaction_retry.go`
- Modify: `apps/backend/internal/app/group_activity_test.go`
- Modify: `apps/backend/internal/app/transaction_retry_test.go`
- Modify: `apps/backend/internal/httpapi/groups.go`
- Modify: `apps/backend/internal/httpapi/transactions.go`
- Modify: `apps/backend/internal/httpapi/home_test.go`
- Modify: `apps/backend/internal/httpapi/transactions_test.go`

**Interfaces:**
- Consumes: Tasks 2/5 kind-aware rows and action-aware transaction lookup.
- Produces:
  - `ProposalListItem.Kind`, `.TokenAmount`.
  - `ProposalDetailResult.Kind`, `.TokenAmount`.
  - `GroupActivityItem.TokenAmount int64`, `.ProceedsUsdcMicros int64`.
  - Activity JSON optional string fields `tokenAmount`, `proceedsUsdcMicros`.
  - Transaction detail optional numeric `proceedsUsdcMicros`.

**Existing-design constraint:** Extend current worker, read projections, retry route, and transaction/activity DTOs; no new worker, feed, retry service, or activity table.

- [ ] **Step 1: Write failing worker/query/activity/retry tests**

```go
func TestProposalExecutePoller_executesPassedSellProposal(t *testing.T)
func TestProposalExecutePoller_failedSellIsNotAutomaticallyRetried(t *testing.T)
func TestProposalExecutePoller_failedBuyIsStillAutomaticallyRetried(t *testing.T)
func TestGetProposalDetail_sellUsesSellExecutionRow(t *testing.T)
func TestListGroupActivity_sellProposalShowsStockAmountAndProceeds(t *testing.T)
func TestRetryFailedSwap_proposalSellKeepsProposalID(t *testing.T)
```

- [ ] **Step 2: Run and verify failure**

Run: `(cd apps/backend && go test ./internal/worker ./internal/app -run 'Test(ProposalExecutePoller|GetProposalDetail_sell|ListGroupActivity_sell|RetryFailedSwap_proposalSell)')`

Expected: FAIL for missing kind-aware behavior.

- [ ] **Step 3: Pass complete proposal rows from the poller**

Populate `Kind`, `UsdcMicros`, and `TokenAmount`; keep one call to `ExecuteOnPass`. Log the applicable amount under `usdc_micros` or `token_amount`, not a misleading shared USDC label.

- [ ] **Step 4: Read execution status by proposal kind**

Map buy → `TransactionActionBuy`, sell → `TransactionActionSell`, then call `GetLatestTransactionByProposalAndAction`. A linked failed row yields `execution.state="failed"` and `failureReason="swap failed"`; a passed row without a transaction is pending.

- [ ] **Step 5: Emit correct activity semantics**

For an awaiting sell proposal:

```go
GroupActivityItem{
	ID: proposal.ID, Kind: "sell", Status: "pending",
	Symbol: proposal.Symbol, TokenAmount: proposal.TokenAmount,
	CreatedAt: proposal.CreatedAt,
}
```

For sell transactions, keep the sold stock symbol, set `TokenAmount=tx.Amount`, and set `ProceedsUsdcMicros=tx.CostBasisAmount.Int64` only when confirmed. Preserve legacy `amountMicros` for deposits/buys; encode the new sell fields so mobile never treats token atomics as USDC.

This is a deliberate change to **existing** confirmed-sell rows: `activityItemFromTransaction` today rewrites a confirmed sell to `Symbol = "USDC"` with `AmountMicros = tx.CostBasisAmount.Int64`. Redeem sells flow through the same branch, so their activity copy changes too. Update the existing `group_activity_test.go` and `httpapi/home_test.go` expectations for redeem sells, not just the new proposal-sell cases. Also replace the hard-coded `Kind: postgres.TransactionActionBuy` used for awaiting-execute proposals with the proposal's kind.

- [ ] **Step 6: Keep manual retry action-aware**

For linked sells, call `GetConfirmedTransactionByProposalAndAction(..., "sell")`, pass `ProposalID` into `SellToUSDCRequest`, and return the already-confirmed sell idempotently. Buy retry remains unchanged.

- [ ] **Step 7: Clarify transaction response**

Continue returning `amountMicros` for compatibility, but add `proceedsUsdcMicros` on confirmed sells. The field is sourced from existing `cost_basis_amount`; do not add a column/table.

- [ ] **Step 8: Run focused tests**

Run: `(cd apps/backend && go test ./internal/worker ./internal/app ./internal/httpapi -run 'Test(ProposalExecutePoller|GetProposalDetail|ListGroupActivity|RetryFailedSwap|GET_groupActivity)')`

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add apps/backend/internal/worker/proposal_execute_poller.go apps/backend/internal/worker/proposal_execute_poller_test.go apps/backend/internal/app/proposal_queries.go apps/backend/internal/app/group_activity.go apps/backend/internal/app/transaction_retry.go apps/backend/internal/app/group_activity_test.go apps/backend/internal/app/transaction_retry_test.go apps/backend/internal/httpapi/groups.go apps/backend/internal/httpapi/transactions.go
git commit -m "feat: surface sell proposal execution state"
```

### Task 7: Correct Atomic Units and Remaining Cost Basis

**Files:**
- Modify: `apps/backend/internal/app/marked_pot.go`
- Modify: `apps/backend/internal/app/marked_pot_test.go`
- Modify: `apps/backend/internal/app/group_view_test.go`
- Modify: `apps/backend/internal/postgres/nav_snapshots.go`
- Modify: `apps/backend/internal/postgres/transactions.go`
- Modify: `apps/backend/internal/app/start_buy_test.go`

**Interfaces:**
- Consumes: `jupiter.XStockAtomicScale = 100_000_000`, Logan transactions.
- Produces:
  - `GroupViewPotRow.TokenAmount` as `strconv.FormatInt(holding.Units, 10)` in `potRowsFromPythInput` — raw atomics, distinct from the display `Units` string on the response row. `pyth.MarkedHolding` has **no** `Amount` field, so `holding.Amount` there is a compile error; and `pyth.CostBasis.Amount` is the buy-fill token amount, not the net holding. The net holding originates as `postgres.TokenHoldingRow.Amount` and reaches `pyth.MarkedHolding.Units` through `costBasisForHoldings` → `pyth.CostBasis.Units`.
  - Remaining position cost basis from existing confirmed buy/sell rows.

**Existing-design constraint:** Correct valuation through existing transaction-derived holdings and NAV snapshots only; never add `cost_basis`, fills, or holdings tables. This task changes displayed pot units and mark-per-unit because today's helpers use `tokenAtomicScale = 1_000_000` while Jupiter fills are 8-decimal. Treat it as a first-class regression: update existing `group_view` / marked-pot / NAV tests, not only the new ones below.

- [ ] **Step 1: Write failing precision and cost-basis tests**

```go
func TestTokenAtomicsToDecimalUnits_usesEightDecimals(t *testing.T)
func TestGroupPotView_stockRowIncludesExactTokenAmount(t *testing.T)
func TestGetFillDerivedCostBasis_afterPartialSellReturnsRemainingBasis(t *testing.T)
func TestExecuteOnPass_sellDoesNotChangeMemberShareUnits(t *testing.T)
```

Example: buy `100_000_000` atomics for `10_000_000` micro-USDC, sell `25_000_000`; remaining holding is `75_000_000`, remaining basis is `7_500_000`, display units are `0.75`, and member share units are unchanged.

- [ ] **Step 2: Run and verify failure**

Run: `(cd apps/backend && go test ./internal/app ./internal/postgres -run 'Test(TokenAtomics|GroupPotView|GetFillDerived|ExecuteOnPass_sellDoesNot|GroupView)')`

Expected: FAIL because current NAV helpers use a 6-decimal token scale and latest-buy basis.

- [ ] **Step 3: Use one canonical scale**

Replace local `tokenAtomicScale int64 = 1_000_000` with `jupiter.XStockAtomicScale` in both `apps/backend/internal/app/marked_pot.go:17` and `apps/backend/internal/postgres/nav_snapshots.go:16` (the helper pair is duplicated across the two packages; both `tokenAtomicsToDecimalUnits` and `costBasisMarkPerUnitMicros` use it). Keep USDC at 6 decimals.

Both copies of `tokenAtomicsToDecimalUnits` also format with `r.FloatString(6)`; that **must become `FloatString(8)`** or dividing by `1e8` silently truncates the last two decimals (1 atomic renders as `"0"`). `domain.ParseShareUnits` / `multiplyDecimalByMicros` use `big.Rat`, so 8 decimals are safe downstream.

Emit `TokenAmount: strconv.FormatInt(holding.Units, 10)` for stock pot rows and omit/empty for USDC. Re-run existing `TestGroupView` / marked-pot cases and update expected `units` / mark strings that assumed 6-decimal tokens.

- [ ] **Step 4: Derive remaining cost basis from Logan rows**

Update `GetFillDerivedCostBasisByOutputMint` and its `…Tx` variant to aggregate. **The mint column differs by action** — buys hold the stock in `output_mint`, sells hold it in `input_mint` (`output_mint` is USDC) — so a single `WHERE output_mint = $2` predicate cannot see sells. Match `ListNetTokenHoldingsByGroup`'s shape:

```sql
WITH buys AS (
  SELECT COALESCE(SUM(cost_basis_price), 0)  AS usdc,
         COALESCE(SUM(cost_basis_amount), 0) AS tokens
  FROM transactions
  WHERE group_id = $1 AND output_mint = $2
    AND action = 'buy' AND status = 'confirmed'
),
sells AS (
  SELECT COALESCE(SUM(amount), 0) AS tokens
  FROM transactions
  WHERE group_id = $1 AND input_mint = $2
    AND action = 'sell' AND status = 'confirmed'
)
SELECT buys.usdc, buys.tokens, sells.tokens FROM buys, sells
```

Then in Go: `remaining_tokens = buys.tokens - sells.tokens`, and `remaining_basis = buys.usdc * remaining_tokens / buys.tokens` (guard `buys.tokens > 0` before dividing). Return `(remaining_basis, remaining_tokens, found)` and not found when remaining tokens are `<= 0`. Note `cost_basis_price` on **buy** rows is USDC spent while on **sell** rows it holds proceeds, which is why the action filters above are mandatory. This preserves average-cost basis using only `transactions`; confirmed sell proceeds remain in sell rows and are not treated as stock cost basis.

- [ ] **Step 5: Run focused tests**

Run: `(cd apps/backend && go test ./internal/app ./internal/postgres -run 'Test(TokenAtomics|GroupPotView|GetFillDerived|ExecuteOnPass_sellDoesNot|GroupView)')`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add apps/backend/internal/app/marked_pot.go apps/backend/internal/app/marked_pot_test.go apps/backend/internal/app/group_view_test.go apps/backend/internal/postgres/nav_snapshots.go apps/backend/internal/postgres/transactions.go apps/backend/internal/app/start_buy_test.go
git commit -m "fix: preserve sell units and remaining basis"
```

### Task 8: Extend Host-Tested Swift HTTP Contracts

**Files:**
- Modify: `packages/mobile-core/Sources/MonacoCore/ProposalDTO.swift`
- Modify: `packages/mobile-core/Sources/MonacoCore/BuyQuoteDTO.swift`
- Modify: `packages/mobile-core/Sources/MonacoCore/GroupViewDTO.swift`
- Modify: `packages/mobile-core/Sources/MonacoCore/MonacoAPIClient.swift`
- Modify: `packages/mobile-core/Sources/MonacoCore/MainFlowCopyAudit.swift`
- Modify: `packages/mobile-core/Tests/MonacoCoreTests/ProposalDTOTests.swift`
- Modify: `packages/mobile-core/Tests/MonacoCoreTests/ProposalsAPITests.swift`
- Modify: `packages/mobile-core/Tests/MonacoCoreTests/GroupViewDTOTests.swift`
- Modify: `packages/mobile-core/Tests/MonacoCoreTests/MainFlowCopyAuditTests.swift`

**Interfaces:**
- Consumes: Task 4 JSON.
- Produces:
  - `public enum ProposalKindDTO: String, Codable, Sendable { case buy, sell }`
  - `ProposalDTO.kind: ProposalKindDTO?`, optional `usdcMicros`, optional `tokenAmount`, computed `resolvedKind` defaulting to `.buy`.
  - `BuyQuoteDTO` extended in place with side-specific optional amounts — **keep the existing type name.** `BuyQuoteDTO` is referenced by `apps/mobile` (`ProposeQuoteDetailView`, `ProposeBuyView`) and by `ProposalsAPITests`; introducing a differently named `ProposalQuoteDTO` would either be dead code or force a rename ripple this plan does not budget for. Its `usdcMicros` and the `ProposalDTO.usdcMicros` both become `String?` (they are non-optional today), which is source-breaking at every call site that reads them — fix those in Task 9/10, not with force-unwraps.
  - `postQuote(groupId:symbol:kind:usdc:tokenAmount:)`.
  - `createProposal(groupId:symbol:kind:usdc:tokenAmount:)`.
  - `PotRowDTO.tokenAmount: String?`.

**Existing-design constraint:** Preserve hand-written Swift HTTP DTOs and host tests; Swift must not import Go or call Jupiter, xStocks, Pyth, or Solana RPC.

- [ ] **Step 1: Write failing host Swift tests**

```swift
func testProposalDTO_missingKindDefaultsToBuy() throws
func testProposalDTO_decodesSellTokenAmount() throws
func testAPIClient_postSellQuote_sendsKindAndTokenAmountOnly() async throws
func testAPIClient_postSellProposal_sendsKindAndTokenAmountOnly() async throws
func testGroupViewDTO_decodesExactStockTokenAmount() throws
func testMainFlowCopyManifest_containsUnifiedProposeAndSellCopy()
```

- [ ] **Step 2: Run and verify failure**

Run: `swift test --package-path packages/mobile-core --filter 'ProposalDTOTests|ProposalsAPITests|GroupViewDTOTests|MainFlowCopyAuditTests'`

Expected: FAIL because sell fields/methods are absent.

- [ ] **Step 3: Implement backward-compatible DTOs**

Use optional wire kind and:

```swift
public var resolvedKind: ProposalKindDTO { kind ?? .buy }
```

Keep old buy fixtures decodable. Side-specific amounts are optional strings in responses. Add exact `tokenAmount` to pot rows.

- [ ] **Step 4: Implement one kind-aware request encoder**

```swift
private struct ProposalTradeRequestDTO: Encodable {
    let kind: ProposalKindDTO
    let symbol: String
    let usdc: Int64?
    let tokenAmount: Int64?
}
```

Public convenience methods may retain the existing buy signatures and forward with `.buy`; add explicit sell calls that forward `.sell`. Assert JSON omits nil fields.

- [ ] **Step 5: Update copy manifest**

Include `"Propose"`, `"Buy"`, `"Sell"`, `"Propose buy"`, `"Propose sell"`, and `"No stocks to sell yet."`; keep forbidden-term audit green and use cabal copy.

- [ ] **Step 6: Run host Swift tests**

Run: `swift test --package-path packages/mobile-core --filter 'ProposalDTOTests|ProposalsAPITests|GroupViewDTOTests|MainFlowCopyAuditTests'`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add packages/mobile-core
git commit -m "feat: add mobile sell proposal contracts"
```

### Task 9: Build One Propose Entry and Held-Only Sell Flow

**Files:**
- Create: `apps/mobile/Monaco/Features/Proposals/ProposeChooserView.swift`
- Create: `apps/mobile/Monaco/Features/Proposals/ProposeSellView.swift`
- Modify: `apps/mobile/Monaco/API/DTOs/ProposalDTO.swift`
- Modify: `apps/mobile/Monaco/API/DTOs/BuyQuoteDTO.swift`
- Modify: `apps/mobile/Monaco/API/DTOs/GroupViewDTO.swift`
- Modify: `apps/mobile/Monaco/API/MonacoAPIClient.swift`
- Modify: `apps/mobile/Monaco/Features/Groups/GroupDetailView.swift`
- Modify: `apps/mobile/Monaco/Features/Proposals/ProposeQuoteDetailView.swift`

**Interfaces:**
- Consumes: Task 8-equivalent app DTOs and Task 4 API.
- Produces:
  - `ProposeChooserView(auth:groupId:groupView:)`.
  - `ProposeSellView(auth:groupId:holdings:)`.
  - Accessibility IDs: `group-action-propose`, `propose-kind-buy`, `propose-kind-sell`, `proposal-sell-{symbol}`, `proposal-sell-amount`, `proposal-sell-quote`, `proposal-sell-submit`.

**Existing-design constraint:** Extend the current Group Detail navigation and Monaco API client; do not add a mobile data/service layer or a direct chain/catalog client.

- [ ] **Step 1: Mirror the tested DTO/API contract in the iOS target**

Add app-side `ProposalKind`, optional proposal amount fields, unified quote fields, and `PotRowDTO.tokenAmount`. Keep existing buy overloads forwarding `kind: .buy`; add sell overloads with exact `Int64 tokenAmount`.

- [ ] **Step 2: Replace the Group Detail action**

```swift
NavigationLink {
    ProposeChooserView(auth: auth, groupId: groupId, groupView: view)
} label: {
    Label("Propose", systemImage: "arrow.left.arrow.right.circle")
}
.accessibilityIdentifier("group-action-propose")
```

There must be exactly one proposal action in Group Detail.

- [ ] **Step 3: Add the chooser**

Buy navigates to the unchanged full-catalog `ProposeBuyView`. Sell navigates to `ProposeSellView` with:

```swift
let heldStocks = groupView.pot.filter {
    $0.symbol.uppercased() != "USDC"
      && (Int64($0.tokenAmount ?? "0") ?? 0) > 0
}
```

When empty, disable Sell and show `"No stocks to sell yet."`; Buy stays enabled.

- [ ] **Step 4: Add exact sell amount parsing**

Display the server `units`, but use `tokenAmount` as the ceiling. Parse decimal shares with POSIX `Decimal`, multiply by `100_000_000`, reject more than 8 fractional digits, round down so input never exceeds the selected holding, and require `1...holdingAtomics`.

Show:

```swift
"You can sell up to \(holding.units) shares of \(displaySymbol)."
```

Do not show mints or “xStock”.

- [ ] **Step 5: Quote and submit**

Call the extended quote route with `.sell` and navigate to a quote section showing estimated USDC output. Disable submit for unroutable, below-minimum, zero, or over-holding input. Submit `{kind:"sell",symbol,tokenAmount}` and show success via `MonacoToast("Proposal submitted", isSuccess: true)`.

For no route/dust use `"No sell route for this amount. Try a larger amount."`; for a stale holding 400 use `"That amount is no longer available to sell."`

- [ ] **Step 6: Keep buy explicit**

Update `ProposeQuoteDetailView` to send `kind: .buy`; preserve full catalog search, treasury ceiling, quote details, and toast behavior.

- [ ] **Step 7: Compile the app**

Run: `just build mobile`

Expected: BUILD SUCCEEDED.

- [ ] **Step 8: Commit**

```bash
git add apps/mobile/Monaco/API apps/mobile/Monaco/Features/Groups/GroupDetailView.swift apps/mobile/Monaco/Features/Proposals
git commit -m "feat: add unified buy and sell proposal flow"
```

### Task 10: Render Sell Proposals and Activity Distinctly

**Files:**
- Modify: `apps/mobile/Monaco/API/DTOs/GroupActivityDTO.swift`
- Modify: `apps/mobile/Monaco/API/DTOs/TransactionDetailDTO.swift`
- Modify: `apps/mobile/Monaco/Features/Groups/ProposalHistorySection.swift`
- Modify: `apps/mobile/Monaco/Features/Groups/GroupActivitySection.swift`
- Modify: `apps/mobile/Monaco/Features/Groups/TransactionDetailView.swift`
- Modify: `apps/mobile/Monaco/Features/Groups/ActivityDetailDestination.swift`
- Modify: `apps/mobile/Monaco/Features/Proposals/ProposalDetailView.swift`
- Modify: `apps/mobile/Monaco/Features/Proposals/ProposalStatusChip.swift`

**Interfaces:**
- Consumes: Task 6 activity/detail JSON and Task 9 DTOs.
- Produces: side-aware proposal/history/detail/status rendering and exact sell activity copy.

**Existing-design constraint:** Reuse current proposal/activity/detail views, formatters, status chips, and toast system; do not create a parallel history feed or banner confirmation.

- [ ] **Step 1: Decode explicit sell activity values**

Add optional `tokenAmount: String?` and `proceedsUsdcMicros: String?` to activity; add optional numeric proceeds to transaction detail. Missing fields remain decodable for old activity.

- [ ] **Step 2: Render proposal rows distinctly**

History title: `"Buy \(symbol)"` or `"Sell \(symbol)"`.

Amount subtitle:
- buy: `"$5.00 proposed"`
- sell: `"0.5000 shares proposed"`

Use 8-decimal atomics, strip “x” branding through `AssetSymbolFormatter`, and pass kind into `ProposalStatusChip`.

- [ ] **Step 3: Render proposal detail distinctly**

Use `"Cabal buy proposal"` with USDC for buy and `"Cabal sell proposal"` with token shares for sell. Vote threshold, expiry, yes/no controls, and execution states remain shared. For failed execution show the existing failure reason and let activity expose Retry.

- [ ] **Step 4: Make the status chip side-aware**

Render labels such as `"Buy · Open"` and `"Sell · Passed"` with accessibility identifier:

```swift
"proposal-status-\(kind.rawValue)-\(status.lowercased())"
```

Status colors remain open/passed/failed/expired; side distinction is textual, not color-only.

- [ ] **Step 5: Render sell activity correctly**

Pending/failed: `"Sell AAPL"` plus exact token shares. Confirmed: `"Sold AAPL"` plus exact token shares and a secondary `"Received $…"` from `proceedsUsdcMicros`. Treat pending sell proposal IDs like pending buys in `ActivityDetailDestination`, falling back to proposal detail when no transaction row exists.

- [ ] **Step 6: Correct transaction detail units**

For sell, format `amountMicros` as 8-decimal token atomics despite the legacy field name; label confirmed `costBasisAmount`/`proceedsUsdcMicros` as `"USDC received"`, never `"Cost basis"`. Buy display remains unchanged.

- [ ] **Step 7: Run mobile gates**

Run: `just test mobile`

Expected: PASS; this is host `swift test` only.

Run: `just build mobile`

Expected: BUILD SUCCEEDED.

- [ ] **Step 8: Commit**

```bash
git add apps/mobile/Monaco/API/DTOs apps/mobile/Monaco/Features/Groups apps/mobile/Monaco/Features/Proposals
git commit -m "feat: distinguish sell proposals and activity"
```

### Task 11: Full Regression and Mandatory Manual Verification

**Files:**
- Verify all files listed in the File Structure Map; no additional production files are expected.

**Interfaces:**
- Consumes: Tasks 1–10.
- Produces: verified issue #146 acceptance and regression evidence.

**Existing-design constraint:** Verify the implemented repository design as-is; do not add alternate test recipes, simulator scripts, or external live-client test harnesses.

- [ ] **Step 1: Run the complete backend gate**

Run: `just test backend`

Expected: PASS using local Docker Compose Postgres and fake/stub Jupiter/Privy clients; no live Jupiter calls.

- [ ] **Step 2: Run the complete host mobile gate**

Run: `just test mobile`

Expected: PASS without booting a simulator.

- [ ] **Step 3: Run the iOS compile gate**

Run: `just build mobile`

Expected: BUILD SUCCEEDED on the simulator resolved by repository scripts.

- [ ] **Step 4: Verify migration idempotency**

Run: `./scripts/with-dotenv-local.sh scripts/apply-migrations.sh`

Run it a second time.

Expected: first run applies `000009_proposal_sells.sql` if needed; second run reports it already applied/skipped. Confirm local compose project `monaco`, container `monaco-postgres`, host port `54322`.

- [ ] **Step 5: Perform mandatory gold-sim setup**

Resolve only through:

```bash
SIMULATOR_ID="$(./scripts/gold-sim-udid.sh)"
```

Use the repository gold-sim QA workflow with that ID. Never commit a UDID, never target by device name, and never run `simctl erase`.

- [ ] **Step 6: Perform the full gold-sim buy-then-sell path**

1. Sign in with an existing Privy SMS or email OTP test account.
2. Create/join a cabal and deposit small mainnet Solana USDC; wait for member-wallet sweep confirmation.
3. Propose → Buy, search the full catalog, quote a small buy, vote it passed, and wait for confirmed activity.
4. Confirm the pot shows the purchased human-readable stock symbol and a positive exact holding.
5. Propose → Sell; verify only held stocks appear; enter no more than the displayed holding; obtain a routable quote; create the proposal.
6. Vote the sell passed; wait for one confirmed `sell` activity row.
7. Confirm activity names the sold symbol/quantity, the pot row decreases or disappears, treasury USDC increases, and proposal detail reports confirmed execution.
8. Propose → Buy again and confirm the full catalog still loads.

- [ ] **Step 7: Verify failure and retry behavior on gold sim or deterministic local fake scenario**

Force one fake passed-sell execute failure in backend tests and confirm one failed linked activity row, no automatic second attempt, and `POST /v1/transactions/{id}/retry` creates/returns a linked confirmed sell when the fake becomes routable.

- [ ] **Step 8: Verify unaffected product boundaries**

Confirm:
- redeem still sells a member slice to USDC and leaves `proposal_id` null;
- member share units and board membership do not change from a treasury sell;
- pending join requesters cannot propose;
- leave blocking still depends on shares/redeems/votes, not sell kind;
- copy contains no raw mint, wallet/gas language, “xStock”, or regressed “group/club” wording;
- confirmations use toasts.

- [ ] **Step 9: Inspect repository status and commit only intended work**

```bash
git status --short
git diff --check
```

Expected: no whitespace errors and only files from the map changed.

```bash
git add docs/superpowers/plans/2026-09-18-propose-sell.md
git commit -m "docs: record propose sell implementation plan"
```

Implementation workers may omit the plan-only commit if the plan was already committed before execution.

## Done When

- All issue #146 acceptance criteria are demonstrated.
- `just test backend`, `just test mobile`, and `just build mobile` pass.
- The mandatory gold-sim deposit → passed buy → passed sell flow passes, including activity, lower holding, higher USDC, and unchanged Buy catalog behavior.
- Sell creation is bounded by exact **confirmed** inventory (`ListNetTokenHoldingsByGroup`) and a **price-only** Jupiter sell quote (no taker). Execute uses the treasury taker.
- Passed sells use the existing `SwapService.SellToUSDC` path and persist `transactions.action='sell'`.
- Failed **sell** automatic execution is terminal until explicit authenticated retry. Failed **buy** execute still auto-retries until a confirmed buy exists.
- Redeem, votes/expiry/threshold, join/leave, NAV/P&L, member board, and legacy buy clients remain correct.

## Self-Review

- **Spec coverage:** Complete. Tasks 1–10 map all nine “Implement exactly” bullets; Task 11 includes every verification and gold-sim step.
- **Review locks (2026-09-18):** buy `UsdcMicros <= 0` moved into the buy branch; poller buy predicate unchanged / sell one-shot on any linked sell row; quote/create are price-only (no taker); `QuoteProposal` is member-only; holdings ceiling uses `buy.cost_basis_amount - sell.amount`; no inventory reservation; domain tests via `go test -C packages/domain`.
- **Second code cross-check (2026-09-18, Opus review):** verified against source and corrected — (1) `jupiter.ParseSellQuoteResponse` needs a `requireTransaction` flag or taker-less sell quotes always return `ErrNoRoute` in production while fakes pass (Locked Decision 4, Task 3 Step 3a); (2) `httpapi.QuoteHandlers.authorizeGroupMember` is creator-only today, not member-only, so quoting is intentionally widened to `IsGroupMember` (Locked Decision 3, Task 4 Step 3); (3) pot atomics are `pyth.MarkedHolding.Units`, not `holding.Amount`, which does not exist (Locked Decision 6, Task 7); (4) `tokenAtomicsToDecimalUnits` must move from `FloatString(6)` to `FloatString(8)` in both `marked_pot.go` and `nav_snapshots.go`; (5) remaining-cost-basis SQL must key buys on `output_mint` and sells on `input_mint`; (6) `InsertProposalTx`'s positional signature has exactly one caller (`governance.go`) that must change in the same commit; (7) `ExecuteOnPass`'s existing `UsdcMicros <= 0` guard and start log must move into the buy branch; (8) the proposal-linked failed-sell insert must allow a NULL `execute_request_id` (that column is `UNIQUE`) and cannot use `SetTransactionProposalID`, which is confirmed-only; (9) migration `000005`'s inline `CHECK (usdc_micros > 0)` stays because NULL satisfies a CHECK; (10) `apply-migrations.sh` and DB-backed Go tests need `scripts/with-dotenv-local.sh` — without `DATABASE_URL` the tests **skip** and report a false PASS; (11) Swift keeps the `BuyQuoteDTO` name and makes `usdcMicros` optional; (12) confirmed-sell activity semantics change affects redeem rows, so existing activity tests must be updated.
- **Ticket gaps closed:** nullable buy notional with a cross-kind SQL constraint; exact 8-decimal atomics in Group View (with existing pot/NAV test updates); sell-only terminal failed execution; sell-aware manual retry linkage; remaining cost basis after partial sale; correct sold-symbol/token/proceeds activity fields; pending join/leave/member-board invariants; redeem regression coverage; non-voter member quote regression test.
- **Placeholder scan:** Clean; every code-changing task has concrete files, signatures, named tests, commands, expected outcomes, and implementation snippets.
- **Type/signature consistency:** `token_amount` ↔ Go `TokenAmount int64` ↔ JSON `tokenAmount` ↔ Swift `Int64`; `kind` is `buy|sell` with omitted kind defaulting to buy; quote/create/list/detail/activity contracts use the same names; confirmed sell `transactions.amount` remains exact token atomics (matching buy `cost_basis_amount` units) and proceeds remain existing `cost_basis_amount` on sell rows plus response alias `proceedsUsdcMicros`.

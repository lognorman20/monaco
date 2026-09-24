# Tessera. Pre-IPO tokens in the same catalog

**Goal.** A cabal can search, propose, vote, buy, hold, value, and sell Tessera pre-IPO tokens (T-SpaceX, T-OpenAI, T-Kalshi) exactly the way it does xStocks today. Same Stocks tab, same search box, same propose sheet, same vote, same Jupiter execution from the group treasury, same holdings row, same P&L and leaderboard math, same sell and cash-out rails. Visible additions: a **Pre-IPO** chip, a sector line, an "around the clock" note instead of after-hours, a per-token private-market reference with its premium, a nudge on the propose sheet when that premium is wide, and a short disclosure on the asset screen.

**Branch.** `solana/tessera-pre-ipo`, cut from `archive/main-before-dynamic` (Privy + Solana + Jupiter). Tessera is Solana-only, so this lane does not touch the Dynamic/Base line on `main`.

**Depends on.** M5 complete on the Solana line. Mainnet treasury with a few USDC for verification. `JUPITER_API_KEY` recommended (Price v3 rate limit).

**Owns.** `apps/backend/internal/tessera/` (new), `apps/backend/internal/catalog/` (new composite), decimals plumbing across `app/`, `pricechain/`, `httpapi/`, `jupiter/`, one migration, `packages/mobile-core` formatters and DTOs, the Stocks tab / asset detail / holdings rows in `apps/mobile`, `docs/product.md` and `docs/api.md` updates.

## What Tessera is, in the terms this codebase already uses

Verified against `https://docs.tessera.pe` and live probes on 2026-09-22.

| Fact | Value | Why it matters here |
| --- | --- | --- |
| Chain / standard | Solana mainnet, **Token-2022** | Same token program family as xStocks. `privy.ListSPLTokenBalances` already scans Token-2022. |
| Decimals | **9** (xStocks are 8) | Every `jupiter.XStockAtomicScale` / `XStockDecimals` use is wrong by 10× for these mints. This is the biggest code change. |
| Transfer fee | **0.2 % (20 bps)** withheld from the sender on every transfer. No transfer hook. Tessera's docs say the recipient is not charged an extra fee; Token-2022 still takes the fee out of the transferred amount, so the receiver can land ~20 bps light. Jupiter's quote may already net that. | Do not assume the direction. TS-T6 measures `received` vs `quoted` on the first fill. TS-T14 adds the 20 bps to the redeem buffer only if that measurement shows Jupiter did not already net it. |
| Catalog API | `GET https://rest-api.tessera.pe/v1/public/token-details` → `[{id,name,symbol,code,sector,mint,markPrice,holders,markValuation}]`. Also `GET /v1/public/tokens` → `[{token,latest_supply,name,symbol,uri}]`; the `uri` JSON has `image` (SVG logo) and `description`. No per-symbol route, no price history. | Public, no key. Three rows today. Cache it; keep a static fallback of the three known mints. |
| Symbols | API `symbol`/`id`/`name` = `T-SpaceX`; API `code` = on-chain symbol `tSpaceX` | We key on the on-chain symbol (`tSpaceX`) so `symbol` stays the single asset identity end to end (propose, quote, agent intents). Display name strips `T-` → **SpaceX**. |
| Mints | tSpaceX `TSPXcLV76s6V2zDiZQ18kBfcbnjaE2ZzNT3ga2Pd99v` · tKalshi `TKLSidmLVt3cqGaaodG8tyRzoANfQwoh67AccjmubeZ` · tOpenAI `oPAiAikWTaFj9RYoRFD35ccfwhnMcB3ThgBZRHSkjTZ` | Test fixtures and static fallback. |
| Execution | Trades on Meteora DLMM; Jupiter routes USDC→T-token and back. Probe on 2026-09-22: $25 buy quotes at ~0.4 % price impact for all three; 1-token sells route. | The existing Jupiter Swap v2 `/order` + `/execute` path works unchanged. |
| Live price | Jupiter Price v3 returns `usdPrice`, `liquidity` ($120k–$520k), `priceChange24h`, `decimals: 9` for all three mints, plus a conditional `stockData` block. `stockData.price` is the private-market mark **for that mint's unit**; `stockData.mcap` is the company value. On 2026-09-22 the Tessera SpaceX unit marked $774 and the PreStocks SpaceX unit marked $155, with the same $2.03T company value (PreStocks scales the unit). | Price chain tier 2 already fits. Liquidity clears the $10k floor. Never share one reference price across two mints of the same company. |
| Pyth | No feed. `pyth.EquityQuerySymbol("tSpaceX")` would query `Equity.US.TSPACE/USD`, fail, and open an entitlement breaker. | Pre-IPO holdings must skip the Pyth tier, not fail through it. |
| Market hours | 24/7 | Never `afterHours: true`. Copy says "around the clock". |
| Reference mark | Tessera's API `markPrice` is the **launch-round** number and does not move (tSpaceX $423 / $800B while the DEX printed $562 and `stockData.price` for that mint was $774 on 2026-09-22). | NAV, P&L, and share minting use the **DEX price**. The reference shown next to it is that mint's `stockData.price`, dated. Tessera `markPrice` is stored only as `issuer_stale` and never produces a premium. See **Pricing and catalog conventions**. |
| Legal shape | "Loan participation right", pays out only on a liquidity event during an announced redemption window. Not a share. | Copy says **tokens**, not shares. One disclosure card. Redemption windows are out of scope. |

## Pricing and catalog conventions

Taken from how Jupiter (Stocks screener, stock page, token page), Phantom (asset grouping), PreStocks (Token Price / Mark Price / Premium %), and brokerages (Fidelity CEF snapshot, IBKR NAV columns, Zerodha iNAV order-ticket nudge) handle the same two problems. Measured on 2026-09-22: same-company pre-IPO tokens traded from −50 % to +27 % against their private-secondary mark (tKalshi −50 %, PreStocks OPENAI +27 %) while AAPLx sat at −0.1 %. The gap is the product, not an error.

**Price vs reference**

1. **Headline price is always the executable DEX price** (Jupiter Price v3 `usdPrice`, or the live quote in the propose sheet). It is what the pot can realise and what NAV, P&L, boards, and share minting use. Never the mark.
2. **Reference price is per mint. Company value is per company.** `stockData.price` is the mark for that token's unit (Tessera tSpaceX $774 vs PreStocks SPACEX $155 on 2026-09-22, same $2.03T `mcap`). Source order for the price: Jupiter `stockData.price` when `updatedAt` is under 48 h → otherwise no reference. Tessera `markPrice` is kept on the catalog row as `ReferenceSource = "issuer_stale"` and is not shown and not used in the premium. Copy on the detail row: "Private-market reference $774" plus a short chip "27% below", plus "Company value $2.0T · updated 2h ago". For stocks, show the reference row only when the gap crosses the nudge threshold below.
3. **Premium is one signed percentage** `dexPrice / stockData.price − 1`, in basis points. Detail chip and holdings subtitle (pre-IPO only) use the short form "27% below" or "27% above". Never "discount to NAV". Catalog, detail, and pot recompute this at read time. A proposal does not: see rule 4.
4. **One nudge sentence, stored on the proposal.** When `|premium| ≥ 10 %` (pre-IPO) or `≥ 1 %` (stock), the propose amount step shows the non-blocking line "Trading 27% below its private-market reference. That gap can widen or close." (or "above"). `CreateProposal` writes `proposals.premium_bps` once from the server's own computation. The proposal card reads that column and does not recompute. `NULL` means no nudge.
5. **Stale reference is hidden, not shown wrong.** Older than 48 h, missing `stockData`, or `issuer_stale`: the detail row reads "Reference unavailable", `premiumBps` is null, and there is no nudge.

**Combined catalogs**

6. **The company is the search unit; the token is the trade unit.** `CatalogAsset` gains `UnderlyingID` (lowercase company slug: `spacex`, `openai`, `kalshi`, `aapl`). Search groups by `UnderlyingID`; one row per company in the Stocks tab and search results, exactly like Jupiter's default Stocks view and Phantom's ticker grouping.
7. **Default variant per company** is chosen server-side by: routable, then Jupiter Price v3 `liquidity` descending, then a fixed issuer order (`xstocks` first among stocks, `tessera` first among pre-IPO). Price v3 does not return 24h volume, and this lane does not fire a quote per mint to rank. The composite caches the chosen default per `UnderlyingID` for 30 minutes on its own key. The routability cache is per mint and is not that key.
8. **Other variants stay reachable.** The detail screen lists every variant with issuer, liquidity, and **that mint's** price. Tapping one makes it the asset for that proposal only. Proposals, transactions, and holdings stay keyed by mint. Two SpaceX tokens are two holdings rows, because their units are not the same size.
9. **Never rank by best ask.** A better ask on a thin pool is not a better fill. Liquidity, then issuer order. The propose sheet still quotes the real amount before the vote.
10. **No cross-issuer netting.** Different issuers are different legal claims and different token units. Holdings, sells, and cost basis never merge across mints.

With one issuer per company, grouping changes nothing a user can see. A second issuer (PreStocks already has SpaceX and Kalshi) is not a data-only change: its unit scale differs, so the reference and the displayed price stay on the mint that was chosen.

## Structure

```text
apps/backend/
  internal/tessera/                 NEW  Tessera public API client, fixtures, static fallback
    client.go                            HTTPCatalog: token-details + tokens (logo uri), 60s cache, 24h last-good
    assets.go                            → []xstocks.CatalogAsset with Kind=pre_ipo, Decimals=9, TransferFeeBps=20
    fake.go                              NewFakeCatalog for tests
  internal/catalog/                 NEW  composite over sources
    searcher.go                          Search / LookupByMint across xstocks ∪ tessera; kind filter; ranking
    resolver.go                          ResolveSolanaMint: xstocks HTTP resolver first, tessera by symbol/name
  internal/xstocks/catalog.go            CatalogAsset gains Kind, Source, Decimals, Sector, LogoURL,
                                         TransferFeeBps, UnderlyingID, Issuer, reference fields, Holders
  internal/jupiter/types.go              XStockDecimals/XStockAtomicScale kept for xStocks; add AtomicScale(decimals)
  internal/app/                          per-holding decimals in marked_pot, swap, quotes, symbols
  internal/pricechain/chain.go           Kind-aware: pre_ipo skips Pyth, never after-hours
  internal/httpapi/assets.go, catalog.go, quotes.go, proposals.go, transactions.go
                                         new JSON fields; ?kind= filter
supabase/migrations/000025_token_decimals.sql
                                         transactions.token_decimals, proposals.token_decimals (default 8),
                                         proposals.premium_bps (nullable, written once)
packages/mobile-core/Sources/MonacoCore/
  MarketAssetDTO.swift, CatalogDTO.swift  kind, source, tokenDecimals, sector, referenceMark…
  DisplayFormatters.swift                 AssetKind-aware name/ticker/quantity formatters
  ProposalFeedFormatters.swift            ProposalShareFormatter takes decimals
apps/mobile/Monaco/
  Features/Assets/AssetsTabView.swift     Pre-IPO section + chip
  Features/Assets/AssetDetailView.swift   sector, reference value, around-the-clock, disclosure card
  Features/Proposals/…                    chip in picker; token wording for pre_ipo
  Features/Groups/PotSectionView.swift    "tokens" label, no after-hours for pre_ipo
docs/product.md, docs/api.md               catalog is multi-source
```

## Flow

Buy, unchanged except where marked **new**.

1. Mobile `GET /v1/assets?query=spacex` → `catalog.Searcher.Search` fans out to xStocks (as today) and Tessera (**new**, in-memory over the cached 3 rows). Tessera matches on on-chain symbol (`tspacex`), API name (`t-spacex`), underlying (`spacex`), and sector. Results carry `kind`, `tokenDecimals`, `sector`.
2. Mobile shows the row with a **Pre-IPO** chip. Tapping opens the same asset detail; price and 24h change come from Jupiter Price v3 as today. **New:** sector line, "Trades around the clock", "Private-market reference" from that mint's `stockData.price` with a "27% below" chip, disclosure card. Chart stays "Price history is not available yet." until TS-T15 lands.
3. Propose sends `{ symbol: "tSpaceX", usdcMicros }` as today. `POST /v1/groups/{id}/quotes` → `StartBuy` → `catalog.Resolver.ResolveSolanaMint("tSpaceX")` (**new** composite) → Jupiter `QuoteBuy`. Price per token uses the asset's decimals (**new**). The quote response includes `premiumBps` for the nudge. `CreateProposal` stores that premium on the proposal row.
4. Vote and pass as today. `ExecuteOnPass` → `SwapService.DevExecuteBuy` builds the Jupiter order with `OutputDecimals` from the catalog row (**new**), slippage 100 bps for `pre_ipo` (**new**; 50 bps stays for stocks).
5. On `Success`/`code 0`, **new:** read the treasury's post-transaction token balance delta for the output mint (`getTransaction` → `postTokenBalances − preTokenBalances` for the treasury owner) and persist that as `cost_basis_amount`; persist `token_decimals = 9`. Log `quoted_out`, `received_out`, `fee_bps_observed`. Falls back to Jupiter `OutputAmountResult` if the RPC lookup fails, with a warning.
6. Holdings: `ListNetTokenHoldingsByGroup` returns decimals per mint (**new**). `marked_pot` values `units × mark / 10^decimals` (**new**). `pricechain` marks `pre_ipo` from Jupiter Price directly, never Pyth, `afterHours=false` (**new**). Cost-basis fallback divides by the right scale (**new**).
7. Sell (proposal sell, agent sell, cash-out): amounts are atomics as today, capped by the ledger holding. Redeem shortfall sizing adds the asset's `TransferFeeBps` to the 100 bps buffer (**new**). Sell confirmation persists actual USDC proceeds as today.
8. Agents: same `symbol` contract; `tSpaceX` resolves through the composite. `GET /v1/groups/{id}/assets?kind=pre_ipo` lets an agent discover them (**new** filter, same auth).

## Data models

**`CatalogAsset`** (Go, `internal/xstocks/catalog.go`; keep the type where it is to avoid churn, rename later):

```go
type AssetKind string   // "stock" | "pre_ipo"
type AssetSource string // "xstocks" | "tessera"

type CatalogAsset struct {
    Symbol, Name, SolanaMint string
    Routable bool
    Kind AssetKind; Source AssetSource
    Decimals int                    // 8 xStocks, 9 Tessera
    TransferFeeBps int              // 0 xStocks, 20 Tessera
    Sector string                   // Tessera only
    LogoURL string                  // Tessera token uri.image
    UnderlyingID string             // company slug: "spacex", "aapl"; groups variants
    Issuer string                   // "xstocks" | "tessera" (display name of the variant)
    ReferenceMarkUsdcMicros *int64  // Jupiter stockData.price; Tessera markPrice fallback
    ReferenceValuationUsd  *int64   // Jupiter stockData.mcap; Tessera markValuation fallback
    ReferenceUpdatedAt     *time.Time
    ReferenceSource        string   // "jupiter" | "issuer_stale" | ""
    Holders *int
}
```

Catalog, detail, and pot `premiumBps` are computed at read time as `usdPrice / stockData.price − 1` and are null when the reference is stale or missing. `proposals.premium_bps` is the exception: written once at propose, never recomputed.

Zero values (`Kind==""`, `Decimals==0`) are treated as `stock`/8 by a single `asset.Normalize()` so untouched xStocks code paths keep working during the migration.

**Migration `000025_token_decimals.sql`:** `transactions.token_decimals` and `proposals.token_decimals` are `smallint NOT NULL DEFAULT 8` with `CHECK (token_decimals BETWEEN 0 AND 12)`. `proposals.premium_bps` is `integer NULL`. Existing rows are xStocks, so decimals 8 and a null premium are correct. `ListNetTokenHoldingsByGroup` selects `max(token_decimals)` per mint.

**HTTP contract additions** (all additive; Swift `Codable` gets optionals with defaults):

| Route | New fields |
| --- | --- |
| `GET /v1/assets`, `/v1/assets/popular`, `/v1/assets/{symbol}`, `/v1/groups/{id}/assets` | `kind` (`"stock"`\|`"pre_ipo"`), `source`, `issuer`, `underlyingId`, `tokenDecimals`, `sector?`, `logoUrl?`, `alwaysOpen` (true for pre_ipo), `referenceMarkUsdcMicros?`, `referenceValuationUsd?`, `referenceUpdatedAt?`, `premiumBps?`, `holders?`. List routes return one row per `underlyingId` (the default variant) plus `variantCount`; `GET /v1/assets/{symbol}` adds `variants[]` with `{symbol, issuer, solanaMint, priceUsdcMicros, liquidityUsd, routable}` |
| `GET /v1/assets?kind=`, `GET /v1/groups/{id}/assets?kind=` | filter; omitted = all |
| `POST /v1/groups/{id}/quotes` response | `tokenDecimals`, `kind`, `premiumBps?` (live, for the nudge) |
| proposal rows | `tokenDecimals`, `kind`, `premiumBps?` (the stored column; null means no nudge) |
| pot rows, transaction rows | `tokenDecimals`, `kind`; pot rows also `premiumBps?` recomputed at read time, and `afterHours=false` for pre_ipo |

`priceUsdcMicros` on all rows keeps meaning **USDC micros per one whole token** regardless of decimals.

**Env:** `TESSERA_API_BASE_URL` (default `https://rest-api.tessera.pe`), `TESSERA_ENABLED` (default `true`). Telemetry upstream label `tessera`.

## Decisions locked here

- **DEX price is the mark.** NAV, share minting, redeem sizing, P&L, and boards use Jupiter Price v3. The reference next to it is that mint's `stockData.price`. Tessera `markPrice` never produces a premium. The nudge sentence is "Trading {n}% below its private-market reference. That gap can widen or close." and is stored on the proposal. See **Pricing and catalog conventions**.
- **One row per company, one default variant, all variants reachable.** Default: routable, then liquidity, then issuer order, cached 30 minutes per company. Holdings and ledger stay per mint. A second issuer is a unit-scale problem, not a data-only add.
- **`symbol` = on-chain symbol** (`tSpaceX`). `T-SpaceX` and `spacex` are accepted as search/resolve aliases, never emitted as the identity.
- **Decimals live on the row that recorded the fill** (`transactions.token_decimals`), not only in the catalog, so valuation never depends on a third-party API being up.
- **Fill amount is what landed, not what was quoted.** Reconcile from chain post-balances; Jupiter's number is the fallback.
- **Slippage per kind:** stocks 50 bps (unchanged), pre-IPO 100 bps.
- **Words:** user copy says **Pre-IPO** and **tokens**. The reference row says "Private-market reference", not "Tessera". The only Tessera string in the UI is the Terms link label "Terms at tessera.pe". Never "T-Token", "Token-2022", or "transfer fee". `MainFlowCopyAudit` bans remain (`mint`, `route`, `quote`, `treasury`, `units`, `xstock`). `ProductBoundaryScanner` is not extended to ban `tessera.pe`; that host is a Safari link, not an API call.
- **Out of this lane:** Tessera redemption windows, referral program, Tessera auctions, charts before TS-T15, multi-chain.

## Parallelization

Milestone gate: complete **Wave 4** and the manual mainnet check before merging to the Solana integration branch.

| Wave | Tracks (parallel within the wave) | Gate |
| --- | --- | --- |
| **1** | **Schema** TS-T1 ∥ **Catalog model + composite** TS-T3, then **Tessera client** TS-T2 | TS-T2 waits for TS-T3's `CatalogAsset` fields |
| **2** | **Decimals** TS-T4 ∥ **Price chain** TS-T5 ∥ **Swap fill** TS-T6, then **HTTP contract** TS-T7 | Wave 1. TS-T7 waits for TS-T4 so they do not edit `quotes.go` together |
| **3** | **Wiring + env** TS-T8 · **mobile-core DTOs/formatters** TS-T9 · **Stocks tab** TS-T10 · **Asset detail** TS-T11 · **Holdings/activity wording** TS-T12 · **Copy audit** TS-T13 | TS-T7 for mobile tickets; TS-T4–T6 for TS-T8 |
| **4** | **Redeem buffer** TS-T14 · **Docs** TS-T16 · **Sweep tool** TS-T17 | Wave 3 |
| **5** (optional) | **Price sampler + chart** TS-T15 | Wave 4 |

Backend and mobile split across agents. TS-T7 fixes the JSON contract first so mobile can start against fixtures.

## Tickets

1. TS-T1 Add `token_decimals` to `transactions` and `proposals`, and nullable `proposals.premium_bps`.
2. TS-T2 Implement the Tessera public API catalog client with cache and static fallback.
   Depends on: TS-T3
3. TS-T3 Extend `CatalogAsset` and add the composite catalog searcher and mint resolver.
4. TS-T4 Replace global xStock decimals with per-asset decimals in valuation and quote math.
   Depends on: TS-T1, TS-T3
5. TS-T5 Make the price chain kind-aware: pre-IPO skips Pyth and is never after-hours.
   Depends on: TS-T3
6. TS-T6 Buy/sell with per-asset decimals, per-kind slippage, and on-chain fill reconciliation.
   Depends on: TS-T1, TS-T3
7. TS-T7 Add `kind`, `tokenDecimals`, reference, and `premiumBps` to the JSON; parse Jupiter `stockData`; add `?kind=` filter.
   Depends on: TS-T3, TS-T4
8. TS-T8 Wire the composite catalog, Tessera client, env, and telemetry in `cmd/api/main.go`.
   Depends on: TS-T4, TS-T5, TS-T6, TS-T7
9. TS-T9 mobile-core: decode the new fields; make name, ticker, and quantity formatters kind- and decimals-aware.
   Depends on: TS-T7
10. TS-T10 Stocks tab: Pre-IPO section, row chip, search results with chip.
    Depends on: TS-T9
11. TS-T11 Asset detail: sector, reference value, around-the-clock note, disclosure card, chart empty copy.
    Depends on: TS-T9
12. TS-T12 Holdings, propose picker, sell sheet, activity, and transaction detail: "tokens" wording and decimals from the row.
    Depends on: TS-T9
13. TS-T13 Add the new strings to `MainFlowCopyAudit` manifest and keep the audit green.
    Depends on: TS-T10, TS-T11, TS-T12
14. TS-T14 Redeem shortfall sizing adds the holding's transfer fee to the sell buffer.
    Depends on: TS-T6
15. TS-T15 (optional) Sample Jupiter prices for pre-IPO mints and serve 1D/1W/1M charts from samples.
    Depends on: TS-T8
16. TS-T16 Update `docs/product.md`, `docs/api.md`, `README` env table for the multi-source catalog.
    Depends on: TS-T8
17. TS-T17 Sweep tool: sell Token-2022 fee tokens with the asset's decimals and fee-aware minimum.
    Depends on: TS-T6

## Ticket details

#### TS-T1: Add `token_decimals` to `transactions` and `proposals`

**Context**
Tessera pre-IPO tokens have 9 decimals; xStocks have 8. Valuation, quotes, and sell sizing currently assume 8 everywhere. Persisting decimals on the row that recorded a fill keeps NAV correct even when the catalog API is down.

**Problem**
`transactions` and `proposals` store token atomics (`cost_basis_amount`, `amount`, `token_amount`) with no decimals column. `supabase/migrations/000009_proposal_sells.sql` documents "100000000 token_amount = one whole share".

**Proposal**
Implement exactly:
1. Add `supabase/migrations/000025_token_decimals.sql` (latest today is `000024_idempotency_keys.sql`) with:
   `ALTER TABLE transactions ADD COLUMN token_decimals smallint NOT NULL DEFAULT 8 CHECK (token_decimals BETWEEN 0 AND 12);`
   `ALTER TABLE proposals ADD COLUMN token_decimals smallint NOT NULL DEFAULT 8 CHECK (token_decimals BETWEEN 0 AND 12);`
   `ALTER TABLE proposals ADD COLUMN premium_bps integer NULL;`
   Update the comment in `000009` via a `COMMENT ON COLUMN proposals.token_amount IS 'token atomics; scale is 10^token_decimals'` in the new file (do not edit old migrations).
2. `apps/backend/internal/postgres/transactions.go`: add `TokenDecimals int` to `InsertPendingTransactionParams`, `Transaction`, and the net-holdings row; `ListNetTokenHoldingsByGroup` selects `max(token_decimals)` per mint. Default 8 when the param is 0.
3. `apps/backend/internal/postgres/proposals.go`: add `TokenDecimals` and `PremiumBps *int` to insert/select for buy and sell proposals. Decimals default 8. `PremiumBps` stays null until TS-T7 writes it at propose time. Do not update it later.

### Scope
- `supabase/migrations/`
- `apps/backend/internal/postgres/transactions.go`, `proposals.go`

**Out of scope**: any caller change (TS-T4/T6), mobile.

**Acceptance Criteria**
- [ ] Migration applies on a fresh DB and on a DB with existing rows; existing rows read `token_decimals = 8` and `premium_bps` null.
- [ ] `TestListNetTokenHoldingsByGroup_returnsDecimalsPerMint` inserts a 9-decimal buy and asserts the holding row reports 9.
- [ ] `just test backend` exits 0.

**Verification**
- `just test backend`
- `psql "$DATABASE_URL" -c '\d transactions'` shows `token_decimals smallint not null default 8`.

**Done when** every AC box is checked and no file outside Scope changed. **Wave:** 1

#### TS-T2: Implement the Tessera public API catalog client

**Context**
Tessera exposes a public, keyless REST API listing its tokens with mint, sector, mark price, holders, and valuation. Monaco needs those rows in the catalog next to xStocks, resilient to that API being slow or down.

**Problem**
No client exists. `internal/xstocks` is the only asset source and is hardwired to `api.xstocks.fi`.

**Proposal**
Implement exactly, in `apps/backend/internal/tessera/`:
1. `client.go`: `HTTPCatalog` with `baseURL` (default `https://rest-api.tessera.pe`), `*http.Client` wrapped by `telemetry.InstrumentClient(telemetry.UpstreamTessera, …)` (add the `UpstreamTessera = "tessera"` label in `internal/telemetry/transport.go`), 15 s timeout, `User-Agent: monaco-backend/1`.
2. `List(ctx) ([]xstocks.CatalogAsset, error)`: `GET /v1/public/token-details` and `GET /v1/public/tokens`; join by mint; for each row build `CatalogAsset{Symbol: code, Name: name, SolanaMint: mint, Kind: pre_ipo, Source: tessera, Issuer: "tessera", UnderlyingID: slug(name minus "T-"), Decimals: 9, TransferFeeBps: 20, Sector, ReferenceMarkUsdcMicros: round(markPrice*1e6), ReferenceValuationUsd: markValuation, ReferenceSource: "issuer_stale", Holders, LogoURL}`. `ReferenceSource` stays `issuer_stale`. TS-T7 replaces the reference from Jupiter `stockData` at response time and does not promote this mark. `LogoURL` comes from fetching each `uri` once and reading `image`; cache per mint for 24 h; failure leaves it empty.
3. Cache: successful `List` results are reused for 60 s; on error, return the last good result if it is younger than 24 h, else the static fallback table in `assets.go` (the three known mints, symbols, names, decimals, fee bps, no prices) and log `slog.Warn` once per 5 min.
4. `fake.go`: `NewFakeCatalog(rows...)` for tests. `testdata/token-details.json` and `testdata/tokens.json` fixtures captured from the live API.
5. Refuse rows whose `mint` is not base58 32–44 chars or whose `code` is empty.

### Scope
- `apps/backend/internal/tessera/` (new)
- `apps/backend/internal/telemetry/transport.go` (one constant)

**Out of scope**: search ranking, resolver, wiring.

**Acceptance Criteria**
- [ ] `TestTesseraList_parsesTokenDetailsFixture` returns three assets with `Kind == pre_ipo`, `Decimals == 9`, `TransferFeeBps == 20`, correct mints, `Symbol == "tSpaceX"` style codes.
- [ ] `TestTesseraList_cachesFor60s` makes one upstream call across two `List` calls (httptest counter).
- [ ] `TestTesseraList_upstream500_returnsLastGoodThenStaticFallback` covers both fallbacks.
- [ ] `TestTesseraList_rejectsMalformedMint` drops the bad row and keeps the rest.
- [ ] No test hits the network (`httptest` only).

**Verification**
- `go test ./internal/tessera/... -count=1` and `just test backend`.

**Done when** every AC box is checked. **Wave:** 1. **Depends on:** TS-T3 (the `CatalogAsset` fields). Do not start this ticket against the old struct.

#### TS-T3: Extend `CatalogAsset` and add the composite catalog searcher and resolver

**Context**
Search, popular, detail, mint→symbol lookup, and symbol→mint resolution all go through `xstocks.CatalogSearcher` / `xstocks.Resolver`. Tessera must join those without every handler learning about two sources.

**Problem**
`xstocks.CatalogAsset` has no kind/decimals; `HTTPCatalogSearcher.Search` appends `x` to bare tickers and rejects hyphenated queries (`catalog.go:271–297`); `HTTPResolver.ResolveSolanaMint` only knows `api.xstocks.fi`; `app.SymbolResolver.LookupByMint` is xStocks-only (`app/symbols.go`).

**Proposal**
Implement exactly:
1. `internal/xstocks/catalog.go`: add the fields from **Data models** to `CatalogAsset`; add `func (a CatalogAsset) Normalize() CatalogAsset` (empty kind → `stock`, 0 decimals → 8, empty source → `xstocks`) and `func (a CatalogAsset) AtomicScale() int64`. Set `Kind: stock, Source: xstocks, Decimals: 8` wherever xStocks rows are built (`searchBySymbol`, `searchPaginatedList`, `ensureMintIndex`, `catalogAssetsFromListResponse`). Add `AssetKind`/`AssetSource` constants.
2. `internal/jupiter/types.go`: add `func AtomicScale(decimals int) int64` (10^decimals, panics outside 0–12). Keep `XStockDecimals`/`XStockAtomicScale` for xStocks-only call sites.
3. New `internal/catalog/searcher.go`: `type Source interface { List(ctx) ([]xstocks.CatalogAsset, error) }`; `Composite{xstocks xstocks.CatalogSearcher; tessera Source; prober xstocks.RoutabilityProber}` implementing `xstocks.CatalogSearcher` plus `SearchKind(ctx, query, kind, limit, offset)`.
   - `Search`: run xStocks search as today; run Tessera match in memory over `List()`: needle matches lowercase `Symbol`, `Name`, `Name` with `T-` prefix stripped, or `Sector`. Probe routability through the same prober. Merge: if the needle exactly equals a Tessera symbol or stripped name, Tessera rows go first; otherwise routable pinned xStocks, then routable Tessera, then the rest by symbol. Paginate the merged slice with the existing `offset/limit` semantics and `HasMore`.
   - Empty query: xStocks page as today followed by all Tessera rows (so the Stocks tab list and `?kind=pre_ipo` both work).
   - **Group by `UnderlyingID`**: after merge, collapse rows sharing an `UnderlyingID` into one default variant (rule 7: routable, then liquidity descending, then issuer order) and cache that choice for 30 minutes per `UnderlyingID`. Do not call Jupiter quotes and do not sort by volume. Attach the rest as `Variants`. xStocks rows get `UnderlyingID` = lowercase ticker minus trailing `x`, `Issuer: "xstocks"`. `SearchVariants(ctx, underlyingID)` returns every mint, each with its own price. `LookupByMint` never substitutes the company default.
   - `LookupByMint`: xStocks first, then Tessera. Always returns the exact mint's row, never the company default.
   - A Tessera `List` error never fails the search; log once and return xStocks-only.
4. New `internal/catalog/resolver.go`: `Resolver{xstocks xstocks.Resolver; tessera Source}` implementing `xstocks.Resolver`. Order: exact case-insensitive Tessera match on `Symbol` or `Name` (`tspacex`, `t-spacex`) → mint; else xStocks HTTP resolver as today. Return `xstocks.ErrNotFound` when neither knows the symbol.
5. `app/symbols.go`: accept the composite (it already only needs `LookupByMint`); remove nothing else.

### Scope
- `apps/backend/internal/xstocks/catalog.go`, `mint_catalog.go`
- `apps/backend/internal/jupiter/types.go`
- `apps/backend/internal/catalog/` (new)

**Out of scope**: handlers, wiring, pricing.

**Acceptance Criteria**
- [ ] `TestCompositeSearch_spacexQuery_returnsTesseraFirst`, `TestCompositeSearch_aaplQuery_returnsXStockFirst`, `TestCompositeSearch_emptyQuery_appendsPreIpoRows`, `TestCompositeSearch_tesseraDown_returnsXStocksOnly` pass with fakes.
- [ ] `TestCompositeSearchKind_preIpo_filtersToTessera` returns only `Kind == pre_ipo`.
- [ ] `TestCompositeResolver_tSpaceX_and_T_SpaceX_resolveSameMint`; `TestCompositeResolver_unknown_returnsErrNotFound`.
- [ ] `TestCatalogAssetNormalize_zeroValues_meanStock8`.
- [ ] Existing `internal/xstocks` tests pass unchanged.

**Verification**
- `just test backend`

**Done when** every AC box is checked. **Wave:** 1. TS-T2 starts after this ticket, not before.

#### TS-T4: Replace global xStock decimals with per-asset decimals

**Context**
With 9-decimal assets in the pot, every `XStockAtomicScale` use mis-values by 10×: displayed units, cost-basis marks, holding value, quoted price per token, spread, and the 1-token sell probe.

**Problem**
Hardcoded scale at `app/marked_pot.go:25,38,264`, `pricechain/chain.go:395`, `httpapi/quotes.go:241`, `httpapi/assets.go:348,357`, `faker/seeder.go:562–596`; `pyth.CostBasis` / `pyth.MarkedHolding` carry no decimals.

**Proposal**
Implement exactly:
1. `internal/pyth` types: add `Decimals int` to `CostBasis` and `MarkedHolding`; `MarkedHolding` also gains `Kind xstocks.AssetKind`.
2. `app/marked_pot.go`: `tokenAtomicsToDecimalUnits(atomics, decimals)`, `costBasisMarkPerUnitMicros(total, atomics, decimals)`; holding value uses `jupiter.AtomicScale(holding.Decimals)`. Holdings are built from `ListNetTokenHoldingsByGroup` rows, which now carry decimals (TS-T1). Pot row JSON gets `tokenDecimals` (handled in TS-T7; here only the struct field).
3. `pricechain/chain.go:395`: same signature change; delete the duplicate helper in favour of one exported `pyth.CostBasisMarkPerUnitMicros`.
4. `httpapi/quotes.go`: `quotePriceUsdcMicros(usdc, outAtomics, decimals)` using the resolved asset's decimals. Response JSON fields are TS-T7.
5. Do not edit `httpapi/assets.go` in this ticket. The sell-probe amount and spread decimals move with the JSON work in TS-T7, so the two tickets do not share a file.
6. `faker/seeder.go`: use the club's asset decimals (all xStocks → 8; no behaviour change).
7. `internal/app/start_buy.go`: `ResolveOutputMint` returns `(mint string, asset xstocks.CatalogAsset, err)` or add `ResolveAsset`; callers thread decimals to TS-T6.

### Scope
- `apps/backend/internal/pyth/` (types only), `app/marked_pot.go`, `app/start_buy.go`, `pricechain/chain.go`, `httpapi/quotes.go` (the price helper only), `faker/seeder.go`

**Out of scope**: `httpapi/assets.go` (TS-T7), swap execution (TS-T6), JSON field names (TS-T7).

**Acceptance Criteria**
- [ ] `TestMarkedPot_nineDecimalHolding_valuesOnceNotTenTimes`: 1_000_000_000 atomics at mark $500 → $500, not $5,000.
- [ ] `TestCostBasisMark_nineDecimals` and existing 8-decimal cases both pass.
- [ ] `TestQuotePrice_nineDecimalOutAmount` returns the right USDC micros per token.
- [ ] `grep -rn "XStockAtomicScale\|XStockDecimals" apps/backend/internal --include=*.go | grep -v _test | grep -v jupiter/types.go` returns `swap.go` (TS-T6) and `httpapi/assets.go` (TS-T7), or nothing once those land.

**Verification**
- `just test backend`

**Done when** every AC box is checked. **Wave:** 2

#### TS-T5: Kind-aware price chain

**Context**
Pre-IPO holdings have no Pyth feed and trade around the clock. The chain must mark them from Jupiter Price v3 without first failing through Pyth, and must never flag them after-hours.

**Problem**
`pricechain.resolveMarketMark` always tries `pythMark` first (`chain.go:243–252`); `EquityQuerySymbol` fabricates `Equity.US.TSPACE/USD`; a denial opens a 10-minute entitlement breaker and logs a warning per symbol. `checkJupiterPrice` and `checkAgainstCostBasis` are fine but use the 8-decimal helper (fixed in TS-T4).

**Proposal**
Implement exactly:
1. `pricechain.Chain.markHolding`: if `holding.Kind == pre_ipo`, call `jupiterMark` only; set `afterHours = false`; source `MarkSourceJupiter`.
2. `MarkedPot`'s `AfterHours` aggregate ignores pre_ipo holdings (`pyth.PotAfterHours` gets the kind).
3. `ChartSeries`: return `AssetChartSeries{EmptyReason: "price history unavailable"}` immediately for pre_ipo symbols (no Pyth probe). Handler passes kind (TS-T7 wires it; here accept an optional `pyth.ChartQuery{Symbol, Kind}` overload or a `ChartSeriesForKind`).
4. Keep `MinLiquidityUsd` 10_000 and `MaxDeviationBps` 2_500 unchanged; document in code that Tessera pools measured $120k–$520k on 2026-09-22.

### Scope
- `apps/backend/internal/pricechain/chain.go`, `apps/backend/internal/pyth/` (types/helpers only)

**Out of scope**: sampler/charts (TS-T15).

**Acceptance Criteria**
- [ ] `TestChain_preIpoHolding_skipsPythAndMarksFromJupiter`: fake Pyth records zero calls; mark equals Jupiter price; `AfterHours == false`.
- [ ] `TestChain_preIpoHolding_jupiterDown_fallsToCostBasisWithNineDecimals`.
- [ ] `TestChain_stockHolding_behaviourUnchanged` (existing tests pass).
- [ ] `TestPotAfterHours_ignoresPreIpo`.

**Verification**
- `just test backend`

**Done when** every AC box is checked. **Wave:** 2

#### TS-T6: Buy/sell with per-asset decimals, per-kind slippage, and fill reconciliation

**Context**
Tessera tokens charge a 0.2 % transfer fee on transfers. Jupiter's quote may already net that fee, so a buy can land at the quoted amount or about 20 bps under it. The ledger records what landed. TS-T14 uses the measurement from this ticket before adding a second buffer.

**Problem**
`app/swap.go:191,300` hardcode `jupiter.XStockDecimals`; `jupiter/quote.go:24` uses one `defaultSlippageBps = 50` for every order; `ConfirmBuyTransaction` persists `fill.OutputAmount` straight from Jupiter `/execute` with no chain check; `InsertPendingTransaction` has no decimals.

**Proposal**
Implement exactly:
1. `swapprovider.Request` gains `Kind xstocks.AssetKind` and `TransferFeeBps int`. `app/swap.go` fills `InputDecimals/OutputDecimals/Kind/TransferFeeBps` from the resolved asset (TS-T4's `ResolveAsset`) for buys; for sells, from the holding's mint via `LookupByMint` (fallback: `transactions.token_decimals` for that mint).
2. `jupiter` swap provider: `slippageBps = 50` for `stock`, `100` for `pre_ipo`. Constant `PreIPOSlippageBps = 100` in `jupiter/quote.go`.
3. `InsertPendingTransaction` / `ConfirmBuyTransaction` / `ConfirmSellTransaction` pass `TokenDecimals`.
4. New `internal/privy` (or `internal/solana`) helper `TokenBalanceDelta(ctx, signature, owner, mint) (int64, error)`: `getTransaction` with `jsonParsed`, `maxSupportedTransactionVersion: 0`; find `postTokenBalances` and `preTokenBalances` entries where `owner == treasury && mint == mint`; return post − pre. Uses the existing `postSolanaRPCWithRetry`.
5. On confirmed buy: `received := TokenBalanceDelta(...)`; if `err == nil && received > 0`, persist `cost_basis_amount = received`, else persist Jupiter's `OutputAmountResult` and `slog.Warn("fill reconciliation unavailable", …)`. Log `quoted_out`, `received_out`, `fee_bps_observed = (quoted−received)*10_000/quoted`. Alert (`telemetry.Alert`, warning) when `fee_bps_observed > TransferFeeBps + slippage`. The first mainnet fill decides TS-T14: if `fee_bps_observed` is about 0, Jupiter already netted the fee; if it is about 20, it did not.
6. On confirmed sell: persist actual USDC proceeds as today (already from fill); additionally log `sent_in` vs `fill.InputAmount`.
7. `app/agent_intent.go`: sell path passes decimals via the same lookup; no other change.

### Scope
- `apps/backend/internal/app/swap.go`, `start_buy.go`, `agent_intent.go` (decimals only)
- `apps/backend/internal/swapprovider/provider.go`
- `apps/backend/internal/jupiter/quote.go`, `swap_provider.go`
- `apps/backend/internal/privy/tokens.go` (new helper)
- `apps/backend/internal/postgres/transactions.go` (pass-through)

**Out of scope**: redeem sizing (TS-T14), Flash provider beyond passing decimals.

**Acceptance Criteria**
- [ ] `TestDevExecuteBuy_preIpo_usesNineDecimalsAnd100bps` asserts the fake provider saw `OutputDecimals == 9`, `slippageBps == 100`; a stock buy still sees 8 and 50.
- [ ] `TestConfirmBuy_persistsChainDeltaOverQuote`: fake RPC returns 99_800_000_000 vs quoted 100_000_000_000 → row stores 99_800_000_000 and log has `fee_bps_observed=20`.
- [ ] `TestConfirmBuy_rpcUnavailable_fallsBackToJupiterOutput`.
- [ ] `TestSellToUSDC_preIpo_usesNineDecimals`.
- [ ] Existing swap integration tests pass.

**Verification**
- `just test backend`
- Manual (see **Manual verification** step 3–6).

**Done when** every AC box is checked. **Wave:** 2

#### TS-T7: HTTP contract: kind, decimals, Tessera fields, `?kind=` filter

**Context**
Mobile needs to know an asset's kind and decimals to render chips, section the Stocks tab, format quantities, and hide after-hours. Agents need to discover pre-IPO assets.

**Problem**
`marketAssetResponse`, `catalogAssetResponse`, quote, proposal, pot row, and transaction JSON carry no `kind` or `tokenDecimals`; `/v1/assets` has no kind filter; `assetDetailResponse` has nowhere for sector or reference value.

**Proposal**
Implement exactly the table in **Data models → HTTP contract additions**:
1. `httpapi/assets.go` and `catalog.go`: add fields; parse `kind` query (`stock`|`pre_ipo`|empty; anything else → 400 `invalid catalog kind`); call `SearchKind`. `alwaysOpen = kind == pre_ipo`. `liquidity.label` stays `"Via Jupiter"`. `MidSpreadBps(..., asset.Decimals)` and sell probe `Amount: jupiter.AtomicScale(asset.Decimals)` land here, not in TS-T4.
2. `jupiter.TokenPrice` gains `StockData *struct{ Price float64; Mcap float64; UpdatedAt time.Time }` parsed from Price v3 `stockData` (absent on most tokens). `enrichAssets` and `buildAssetDetail` set the reference fields from `stockData` when `UpdatedAt` is under 48 h and `ReferenceSource = "jupiter"`. They leave `issuer_stale` in place otherwise and set `premiumBps` only for source `jupiter`, using that mint's `stockData.price`, never a shared company price. `TestAssetDetail_sellProbe_usesOneWholeToken` asserts probe amount `1e9` for pre_ipo and `1e8` for a stock.
3. `GetAssetChartHandler`: pass kind to the chain (TS-T5).
4. `httpapi/quotes.go`: add `tokenDecimals`, `kind`, `premiumBps` on the response struct. Do not rewrite `quotePriceUsdcMicros` (TS-T4 owns that helper).
5. `CreateProposal` computes `premiumBps` the same way and writes `proposals.premium_bps` once. Later reads return the column. Pot rows recompute `premiumBps` at read time. `httpapi/proposals.go`, the group view, and `httpapi/transactions.go` add `tokenDecimals` and `kind`. Kind comes from `LookupByMint`; when unknown, `stock`.
6. `lookupAsset` in `assets.go`: match on symbol case-insensitively and on Tessera name aliases (`GET /v1/assets/tSpaceX` and `/T-SpaceX` both 200; the response `symbol` is always `tSpaceX`).
7. Update `docs/api.md` route tables for these fields (short; TS-T16 does prose).

### Scope
- `apps/backend/internal/httpapi/assets.go`, `catalog.go`, `quotes.go`, `proposals.go`, `transactions.go`, group view handler
- `docs/api.md` (tables only)

**Out of scope**: mobile.

**Acceptance Criteria**
- [ ] `TestGET_assets_kindPreIpo_returnsOnlyTessera`; `TestGET_assets_kindInvalid_returns400`.
- [ ] `TestGET_asset_tSpaceX_hasKindDecimalsSectorReferenceMarkAlwaysOpen`.
- [ ] `TestGET_asset_T_SpaceX_alias_returnsCanonicalSymbol`.
- [ ] `TestPOST_quotes_preIpo_returnsTokenDecimals9`.
- [ ] `TestCreateProposal_storesPremiumBpsOnce`: a second read after the DEX price moves still returns the stored bps.
- [ ] `TestAssetDetail_sellProbe_usesOneWholeToken`.
- [ ] Group view pot row for a pre_ipo holding has `afterHours == false`, `tokenDecimals == 9`, `kind == "pre_ipo"`.
- [ ] JSON for stock assets is a superset of today's (no renamed or removed keys).

**Verification**
- `just test backend`
- `curl -s -H "Authorization: Bearer $TOKEN" 'localhost:8080/v1/assets?kind=pre_ipo' | jq '.assets[] | {symbol,kind,tokenDecimals,sector}'`

**Done when** every AC box is checked. **Wave:** 2, after TS-T4.

#### TS-T8: Wire composite catalog, Tessera client, env, telemetry

**Context**
Everything above is inert until `cmd/api/main.go` builds the composite and injects it where `xstocks.NewHTTPCatalogSearcher()` and `xstocks.NewHTTPResolver()` go today (`main.go:227–253`).

**Problem**
Handlers, `BuyService`, `SymbolResolver`, `SwapService`, and `AgentIntentService` receive xStocks-only implementations.

**Proposal**
Implement exactly:
1. `internal/config`: `TesseraAPIBaseURL` (`TESSERA_API_BASE_URL`, default `https://rest-api.tessera.pe`), `TesseraEnabled` (`TESSERA_ENABLED`, default true). Add both to `.env.example` with comments.
2. `main.go`: build `tessera.NewHTTPCatalog(cfg.TesseraAPIBaseURL)` when enabled (nil source otherwise); `catalog.NewComposite(xstocksSearcher, tesseraSource, catalogRoutability)`; `catalog.NewResolver(xstocksResolver, tesseraSource)`; pass the composite to `CatalogHandlers`, `AssetsHandlers`, `app.NewSymbolResolver`, `app.NewBuyService`. Log `slog.Info("catalog sources ready", "xstocks", true, "tessera", enabled)`.
3. Startup does **not** call Tessera; first request warms the cache.
4. `docs/ops-observability.md`: add the `tessera` upstream label and the `fill reconciliation unavailable` warning.

### Scope
- `apps/backend/cmd/api/main.go`, `apps/backend/internal/config/`, `.env.example`, `docs/ops-observability.md`

**Acceptance Criteria**
- [ ] `TestConfig_tesseraDefaults` (enabled, default URL) and `TestConfig_tesseraDisabled`.
- [ ] `just run backend` boots with and without `TESSERA_ENABLED`; `GET /v1/assets?kind=pre_ipo` returns three rows when enabled and zero when disabled.
- [ ] `just test backend` exits 0.

**Verification**
- `just test backend`; `just run backend` then the curl above.

**Done when** every AC box is checked. **Wave:** 3

#### TS-T9: mobile-core DTOs and kind-aware formatters

**Context**
Swift decodes hand-written `Codable` mirrors of the Go JSON. New fields must decode with safe defaults so an older backend still works, and formatters must stop assuming "strip trailing x", "8 decimals", and "shares".

**Problem**
`MarketAssetDTO`, `CatalogAssetDTO`, quote/proposal/pot/transaction DTOs lack `kind`/`tokenDecimals`; `ProposalShareFormatter.decimals = 8` (`ProposalFeedFormatters.swift:23–25`); `AssetSymbolFormatter.display` strips a trailing lowercase `x` (`DisplayFormatters.swift:57–66`); `AssetDisplayNames` is a static equity table; share labels hardcode "shares" (`DisplayFormatters.swift:274–285`).

**Proposal**
Implement exactly in `packages/mobile-core` (mirror the app copies in `apps/mobile/Monaco/API/DTOs` where duplicated):
1. `public enum AssetKind: String, Codable { case stock, preIpo = "pre_ipo" }` with `init(from:)` defaulting unknown → `.stock`.
2. Add optional `kind`, `source`, `issuer`, `underlyingId`, `tokenDecimals`, `sector`, `logoUrl`, `alwaysOpen`, `referenceMarkUsdcMicros`, `referenceValuationUsd`, `referenceUpdatedAt`, `premiumBps`, `holders`, `variantCount` to `MarketAssetDTO` and `AssetDetailDTO`; the same price fields that apply to `CatalogAssetDTO`; `variants` on `AssetDetailDTO`. Add `kind`, `tokenDecimals`, `premiumBps` to quote, proposal, and pot-row DTOs, and `kind`/`tokenDecimals` to transaction DTOs. Computed `resolvedKind` (`kind ?? .stock`) and `resolvedDecimals` (`tokenDecimals ?? 8`).
3. `AssetSymbolFormatter.display(symbol, kind:)`: for `.preIpo` return the symbol untouched; keep today's behaviour for `.stock`.
4. `AssetDisplayName` / `CatalogAssetNameFormatter`: for `.preIpo` strip a leading `T-` from the API name (`T-SpaceX` → `SpaceX`); never consult `AssetDisplayNames`.
5. `TokenQuantityFormatter.format(atomics:decimals:kind:)` replaces the fixed-8 paths; label `shares`/`share` for `.stock`, `tokens`/`token` for `.preIpo`. `ProposalShareFormatter` delegates to it with the row's decimals.
6. `UsdAmountFormatter` unchanged.

### Scope
- `packages/mobile-core/Sources/MonacoCore/` (DTOs, `DisplayFormatters.swift`, `ProposalFeedFormatters.swift`), `apps/mobile/Monaco/API/DTOs/`

**Acceptance Criteria**
- [ ] `testMarketAssetDTO_decodesWithoutKind_defaultsToStock8`; `testMarketAssetDTO_decodesPreIpoFields`.
- [ ] `testAssetSymbolFormatter_preIpo_keepsTSpaceX`; `testAssetSymbolFormatter_stock_stripsAAPLx` still passes.
- [ ] `testDisplayName_preIpo_stripsTPrefix` → `SpaceX`.
- [ ] `testTokenQuantity_nineDecimals_preIpo_labelsTokens`: 1_500_000_000 → `1.5 tokens`; 1_000_000_000 → `1 token`.
- [ ] `just test mobile` exits 0.

**Verification**
- `just test mobile`

**Done when** every AC box is checked. **Wave:** 3

#### TS-T10: Stocks tab: Pre-IPO section and chip

**Context**
The Stocks tab shows one Popular section and a search list (`AssetsTabView.swift:48–50, 114–128, 160–177`). Pre-IPO assets need to be discoverable without a new screen.

**Problem**
No section for pre-IPO rows; rows have no kind indicator; search results mix kinds with no visual cue.

**Proposal**
Implement exactly:
1. `AssetsTabView`: when the search box is empty, fetch `GET /v1/assets?kind=pre_ipo&limit=10` alongside popular; render `MonacoSectionHeader("Pre-IPO")` with those rows **below** Popular. Loading and error states reuse the Popular ones. Empty pre-IPO result hides the section.
2. `assetRow`: trailing `MonacoChip("Pre-IPO")` (existing primitive, `MonacoPrimitives.swift:87`) when `resolvedKind == .preIpo`; title from the kind-aware display name (TS-T9); subtitle keeps price and 24h.
3. `StockMark` letter tile: use the first letter of the display name for `.preIpo` (so `S` for SpaceX, not `t`). No remote logo download in this ticket.
4. Search list: same row, chip included; no re-sorting on the client.
5. `ProposeStockRow` (`ProposeBuyView.swift:256–277`): same chip.

### Scope
- `apps/mobile/Monaco/Features/Assets/AssetsTabView.swift`, `Design/MonacoMarks.swift`, `Features/Proposals/ProposeBuyView.swift`

**Acceptance Criteria**
- [ ] With the backend returning three pre_ipo rows, the Stocks tab shows a Pre-IPO section with SpaceX, OpenAI, Kalshi rows, each with the chip and a price.
- [ ] Searching `spacex` shows the SpaceX row first with the chip; searching `AAPL` shows Apple first without a chip.
- [ ] `just build mobile` exits 0; `just test mobile` exits 0.

**Verification**
- `just build mobile`; gold-sim tap-through (see **Manual verification** step 1).

**Done when** every AC box is checked. **Wave:** 3

#### TS-T11: Asset detail for pre-IPO

**Context**
The detail screen shows price, 24h, chart, and Buy/Sell (`AssetDetailView.swift`). Pre-IPO assets need sector, the private-market reference for this token, and that it trades around the clock.

**Problem**
No sector line, no reference value, no disclosure, and the chart empty state is hardcoded (`131–136`) with `emptyReason` ignored (`184`).

**Proposal**
Implement exactly, only when `detail.resolvedKind == .preIpo`:
1. Under the hero price: `sector` as a caption; "Trades around the clock" caption.
2. A `MonacoRow` "Private-market reference" with the dollar reference, a short chip "27% below" or "27% above" (`MonacoTheme.warning` when `|premiumBps| ≥ 1000`, otherwise secondary text), and a caption "Company value $2.0T · updated 2h ago". When `premiumBps` is nil the row reads "Reference unavailable" and shows no percentage. The chip is the short form. The long sentence lives only on the propose sheet and the proposal card.
3. A collapsed `MonacoCard` "About pre-IPO tokens" with this exact copy: "Pre-IPO tokens track a private company before it lists. They pay out if the company goes public or is sold. You can sell anytime here. A 0.2% fee applies when buying and when selling." and a link row "Terms at tessera.pe" opening `https://terms.tessera.pe` in Safari. That string is allowed. Do not add `tessera.pe` to `ProductBoundaryScanner.forbiddenHostFragments`.
4. Chart empty state: keep the existing card; copy for pre_ipo is "Price history builds up over time." (replaces "not available yet" only for pre_ipo).
5. When `variants.count > 1`, an "Also available from" list: one row per variant with issuer, liquidity, and that mint's price. Tapping sets that variant for the Buy button on this screen only.
6. Buy/Sell buttons unchanged.

### Scope
- `apps/mobile/Monaco/Features/Assets/AssetDetailView.swift`

**Acceptance Criteria**
- [ ] SpaceX detail shows sector "Aerospace", "Trades around the clock", "Private-market reference" with a "27% below" chip from the fixture's `premiumBps`, the About card (fee on buying and selling), and Buy/Sell. It does not show "$423" or the words "Tessera reference".
- [ ] Apple detail is pixel-identical to before (no new rows).
- [ ] `just build mobile` exits 0.

**Verification**
- `just build mobile`; gold-sim screenshot of both screens.

**Done when** every AC box is checked. **Wave:** 3

#### TS-T12: Holdings, propose, sell, activity, transaction detail wording and decimals

**Context**
Everywhere a quantity is shown, the app divides by 1e8 and says "shares". A 9-decimal pre-IPO holding would display 10× too many "shares".

**Problem**
`PotSectionView.swift:64,83–87` (always "shares", after-hours badge), `GroupActivitySection.swift:257–274` and `TransactionDetailView.swift:137,169–170` (`/ 100_000_000`), `ProposeService.swift:212–215` (`ProposeMath` 8 decimals), `ProposeSellView` labels, `ProposalFeedCopy` "Shares".

**Proposal**
Implement exactly:
1. Replace every fixed `100_000_000` / `decimals = 8` in the files above with `TokenQuantityFormatter` using the row's `resolvedDecimals` and `resolvedKind` (TS-T9).
2. `PotSectionView`: subtitle uses tokens/shares by kind; the After hours badge is not rendered for `.preIpo` (server already sends false; guard client-side too).
3. `ProposeSellView` and `ProposeReviewView`: quantity labels by kind; math by decimals.
3b. `ProposeAmountView`: when `premiumBps` is non-nil and `|premiumBps| ≥ 1000` (pre_ipo) or `≥ 100` (stock), show one non-blocking line: "Trading 27% below its private-market reference. That gap can widen or close." Swap in "above" when the bps are positive. Fill the percent from `abs(premiumBps) / 100`. The proposal card shows that same sentence from `proposal.premiumBps`, the stored column, not a live recompute.
4. `GroupActivitySection` titles keep "Bought/Sold {name}"; quantity suffix by kind.
5. `TransactionDetailView`: "Shares" row label becomes "Tokens" for pre_ipo; per-token price uses decimals.

### Scope
- `apps/mobile/Monaco/Features/Groups/PotSectionView.swift`, `GroupActivitySection.swift`, `TransactionDetailView.swift`, `Features/Proposals/ProposeService.swift`, `ProposeSellView.swift`, `ProposeReviewView.swift`, `ProposalFeedCopy.swift`

**Acceptance Criteria**
- [ ] A pot row with `tokenAmount 1_500_000_000, tokenDecimals 9, kind pre_ipo` renders "1.5 tokens"; an AAPLx row with `150_000_000, 8` renders "1.5 shares".
- [ ] Sell sheet for SpaceX shows tokens and computes dollars from the 9-decimal amount.
- [ ] `just test mobile` (formatter tests) and `just build mobile` exit 0.

**Verification**
- `just test mobile`; `just build mobile`; gold-sim check of a seeded pre_ipo holding (faker adds one SpaceX buy to one fake club in this ticket).

**Done when** every AC box is checked. **Wave:** 3

#### TS-T13: Copy audit manifest

**Context**
`MainFlowCopyAudit` fails the mobile test target if main-flow strings contain plumbing words (`mint`, `route`, `quote`, `treasury`, `units`, `xstock`, …).

**Problem**
New strings from TS-T10–T12 are not in the audited manifest; any slip ("units", "quote") would ship unaudited.

**Proposal**
Add every new user-facing string from TS-T10–T12 to the `MainFlowCopyAudit` manifest: "Pre-IPO", "Trades around the clock", "Private-market reference", "Reference unavailable", "27% below", "Trading 27% below its private-market reference. That gap can widen or close.", "Company value", "About pre-IPO tokens", the disclosure sentence including "when buying and when selling", "Terms at tessera.pe", "Also available from", "Price history builds up over time.", "tokens", "token", "Tokens". Fix any string that fails. Do not add "Tessera reference value".

### Scope
- `packages/mobile-core/Sources/MonacoCore/MainFlowCopyAudit.swift` and its tests

**Acceptance Criteria**
- [ ] `testMainFlowCopyAudit_preIpoStrings_pass` covers all strings above.
- [ ] `just test mobile` exits 0.

**Verification**
- `just test mobile`

**Done when** every AC box is checked. **Wave:** 3

#### TS-T14: Fee-aware redeem shortfall sizing

**Context**
Cash-out sells slices of holdings to cover the payout, with a 100 bps buffer (`jupiter/sell.go:119–149`). A 20 bps transfer fee stacks on top of slippage and can leave the payout short by a few cents, which then fails or leaves dust.

**Problem**
`RedeemShortfallSellAmount` knows nothing about transfer fees. Adding 20 bps on top of a quote that already nets the fee would oversell.

**Proposal**
Implement exactly: `RedeemShortfallSellAmount(holdingAtomics, shortfallUsdc, stockValueUsdc int64)` (`jupiter/sell.go:127`) gains a fourth parameter `bufferBps int64`. Callers in `app/redeem.go` / `redeem_recovery.go` pass `RedeemSellSlippageBufferBps` (100) plus `holding.TransferFeeBps` only when the TS-T6 mainnet measurement logged `fee_bps_observed` near 20. If that log was near 0, pass 100 and do not add the fee. Until that measurement exists, pass 100. The function already scales by holding value, so it is decimals-agnostic. Keep the post-sell balance check.

### Scope
- `apps/backend/internal/jupiter/sell.go`, `apps/backend/internal/app/redeem.go`, `redeem_recovery.go`

**Acceptance Criteria**
- [ ] `TestRedeemShortfallSellAmount_feeAlreadyInQuote_staysAt100Bps` and `TestRedeemShortfallSellAmount_feeNotInQuote_uses120Bps`.
- [ ] `TestRedeemShortfallSellAmount_nineDecimalHolding_roundsUpAtomics` uses a 1e9-scale holding.
- [ ] Existing redeem integration tests pass.

**Verification**
- `just test backend`

**Done when** every AC box is checked. **Wave:** 4

#### TS-T15 (optional): Price sampler and chart for pre-IPO

**Context**
Pyth has no history for these mints, so the detail chart is empty. Jupiter Price v3 gives a current price; sampling it builds a usable 1D/1W/1M series in a few days.

**Problem**
`ChartSeries` has no non-Pyth source; there is no price history table.

**Proposal**
Implement exactly:
1. Migration `asset_price_samples(mint text, price_usdc_micros bigint, liquidity_usd numeric, sampled_at timestamptz, PRIMARY KEY (mint, sampled_at))`.
2. A background sampler in `cmd/api` (same pattern as the proposal execute poller): every 5 min, for every pre_ipo mint in the catalog plus every mint held by any group, fetch Jupiter Price v3 once (batched) and insert a sample. Skip inserts when Jupiter is on breaker.
3. `pricechain.ChartSeries` for pre_ipo reads samples: 1D = last 24 h at 5 min, 1W = last 7 d bucketed to 1 h, 1M = last 30 d bucketed to 6 h. Fewer than 2 points → `EmptyReason: "price history builds up over time"`.
4. Retention: delete samples older than 45 days nightly.

### Scope
- `supabase/migrations/`, `apps/backend/internal/pricechain/`, `apps/backend/internal/postgres/price_samples.go` (new), `cmd/api/main.go`

**Acceptance Criteria**
- [ ] `TestChartSeries_preIpo_bucketsSamplesByRange` for the three ranges.
- [ ] `TestSampler_skipsWhenJupiterBreakerOpen`.
- [ ] After 30 minutes of `just run backend`, `GET /v1/assets/tSpaceX/chart?range=1D` returns ≥ 6 points.

**Verification**
- `just test backend`; run backend 30 min; curl the chart.

**Done when** every AC box is checked. **Wave:** 5

#### TS-T16: Docs

**Context**
`docs/product.md` and `docs/api.md` describe a single-source xStocks catalog.

**Problem**
Product brief, constants section, and API reference do not mention pre-IPO assets, decimals, or Tessera env.

**Proposal**
Implement exactly:
1. `docs/product.md`: in **How it works** step 4 and **Votes and buys** item 1, say "from the catalog (tokenized US stocks via xStocks, and pre-IPO tokens via Tessera)". Add a **Pre-IPO tokens** subsection with the fact table from this file (decimals, fee, DEX price is the mark, 24/7, copy rules). Add the three mints under **Constants**.
2. `docs/api.md`: prose for the new fields and `?kind=`; note `priceUsdcMicros` is per whole token regardless of decimals.
3. `README.md` env table: `TESSERA_API_BASE_URL`, `TESSERA_ENABLED`.
4. `docs/index.md`: link this file under **Milestone files** as "Tessera. Pre-IPO tokens".

### Scope
- `docs/product.md`, `docs/api.md`, `docs/index.md`, `README.md`

**Acceptance Criteria**
- [ ] `rg -n "pre-IPO|Tessera" docs/product.md docs/api.md README.md` shows all four edits.
- [ ] No mention of "T-Token", "Token-2022", or "transfer fee" in user-facing copy sections (dev sections may keep them).

**Verification**
- Manual read-through.

**Done when** every AC box is checked. **Wave:** 4

#### TS-T17: Sweep tool handles pre-IPO tokens

**Context**
`./scripts/sweep-wallets.sh` sells non-USDC SPL balances to USDC then sweeps USDC (`docs/ops-sweep-wallets.md`). QA cabals will hold tSpaceX dust.

**Problem**
`cmd/sweep-member-to-address/sweep_run.go` sizes sells with 8-decimal assumptions and has no fee-aware minimum, so a 9-decimal fee token can be mis-sized or fail on dust.

**Proposal**
Implement exactly: resolve decimals per mint via the composite catalog (`LookupByMint`), fall back to Jupiter Price v3 `decimals`, else 8; skip balances whose USDC value at the Jupiter price is under $0.50 (log `skip dust`); pass `Kind` so slippage is 100 bps for pre_ipo. Update `docs/ops-sweep-wallets.md` with one line.

### Scope
- `apps/backend/cmd/sweep-member-to-address/`, `docs/ops-sweep-wallets.md`

**Acceptance Criteria**
- [ ] `TestSweepRun_preIpoBalance_usesNineDecimalsAndSkipsDust`.
- [ ] `--dry-run` against a wallet holding tSpaceX logs the planned sell with the right whole-token amount.

**Verification**
- `just test backend`; `./scripts/sweep-wallets.sh --destination <addr> --source <treasury> --dry-run`.

**Done when** every AC box is checked. **Wave:** 4

## Automated verification

**Commands.** `just test backend`, `just test mobile`, `just build mobile`.

### Test layers

| Layer | Runs under | What it proves |
| --- | --- | --- |
| **Go unit** | `just test backend` | Tessera fixture parsing, cache and fallbacks; composite search ranking and kind filter; composite resolver aliases; decimals math (marks, quotes, probes, redeem buffer); price chain skips Pyth for pre_ipo; per-kind slippage; fill reconciliation preference. **Fake Tessera, fake Jupiter, fake Pyth, fake RPC** — no network. |
| **Go integration** | `just test backend` | Migration; `ListNetTokenHoldingsByGroup` decimals; a full propose→pass→buy→pot→sell cycle for a 9-decimal fake asset against local Postgres; JSON contract for every touched route. |
| **Swift host** | `just test mobile` | DTO decoding with and without new fields; kind-aware name/ticker/quantity formatters; copy audit. |
| **Boundary** | `just test mobile` | `ProductBoundaryScanner` keeps its current host list (`jup.ag`, `xstocks.fi`, Pyth, Solana RPC). Do not add `tessera.pe`. The Terms row may contain that host as a Safari link. Product feature sources still make no Tessera, Jupiter, or xStocks HTTP calls. |

### Test map

**TS-T1** `TestListNetTokenHoldingsByGroup_returnsDecimalsPerMint`
**TS-T2** `TestTesseraList_parsesTokenDetailsFixture` · `TestTesseraList_cachesFor60s` · `TestTesseraList_upstream500_returnsLastGoodThenStaticFallback` · `TestTesseraList_rejectsMalformedMint`
**TS-T3** `TestCompositeSearch_spacexQuery_returnsTesseraFirst` · `TestCompositeSearch_aaplQuery_returnsXStockFirst` · `TestCompositeSearch_emptyQuery_appendsPreIpoRows` · `TestCompositeSearch_tesseraDown_returnsXStocksOnly` · `TestCompositeSearchKind_preIpo_filtersToTessera` · `TestCompositeResolver_tSpaceX_and_T_SpaceX_resolveSameMint` · `TestCompositeResolver_unknown_returnsErrNotFound` · `TestCatalogAssetNormalize_zeroValues_meanStock8`
**TS-T4** `TestMarkedPot_nineDecimalHolding_valuesOnceNotTenTimes` · `TestCostBasisMark_nineDecimals` · `TestQuotePrice_nineDecimalOutAmount`
**TS-T5** `TestChain_preIpoHolding_skipsPythAndMarksFromJupiter` · `TestChain_preIpoHolding_jupiterDown_fallsToCostBasisWithNineDecimals` · `TestPotAfterHours_ignoresPreIpo`
**TS-T6** `TestDevExecuteBuy_preIpo_usesNineDecimalsAnd100bps` · `TestConfirmBuy_persistsChainDeltaOverQuote` · `TestConfirmBuy_rpcUnavailable_fallsBackToJupiterOutput` · `TestSellToUSDC_preIpo_usesNineDecimals`
**TS-T7** `TestGET_assets_kindPreIpo_returnsOnlyTessera` · `TestGET_assets_kindInvalid_returns400` · `TestGET_asset_tSpaceX_hasKindDecimalsSectorReferenceMarkAlwaysOpen` · `TestGET_asset_T_SpaceX_alias_returnsCanonicalSymbol` · `TestPOST_quotes_preIpo_returnsTokenDecimals9` · `TestCreateProposal_storesPremiumBpsOnce` · `TestAssetDetail_sellProbe_usesOneWholeToken`
**TS-T8** `TestConfig_tesseraDefaults` · `TestConfig_tesseraDisabled`
**TS-T9** `testMarketAssetDTO_decodesWithoutKind_defaultsToStock8` · `testMarketAssetDTO_decodesPreIpoFields` · `testAssetSymbolFormatter_preIpo_keepsTSpaceX` · `testDisplayName_preIpo_stripsTPrefix` · `testTokenQuantity_nineDecimals_preIpo_labelsTokens`
**TS-T13** `testMainFlowCopyAudit_preIpoStrings_pass`
**TS-T14** `TestRedeemShortfallSellAmount_feeAlreadyInQuote_staysAt100Bps` · `TestRedeemShortfallSellAmount_feeNotInQuote_uses120Bps` · `TestRedeemShortfallSellAmount_nineDecimalHolding_roundsUpAtomics`
**TS-T15** `TestChartSeries_preIpo_bucketsSamplesByRange` · `TestSampler_skipsWhenJupiterBreakerOpen`
**TS-T17** `TestSweepRun_preIpoBalance_usesNineDecimalsAndSkipsDust`

### Fixtures / fakes

- `internal/tessera/testdata/token-details.json`, `tokens.json` — captured 2026-09-22 (three rows).
- `tessera.NewFakeCatalog(rows...)`; `xstocks.NewFakeCatalogSearcher` (existing); `catalog.NewComposite` over both.
- `fakeJupiterClient` gains a `LastRequest()` accessor exposing `slippageBps` and decimals.
- `fakeSolanaRPC.SetTokenBalanceDelta(sig, owner, mint, delta)` for TS-T6.
- `fixtureJupiterPriceV3PreIpo` — the three-mint Price v3 body (liquidity, decimals 9, priceChange24h).

### Property / invariant tests

- `TestProperty_potValueInvariantToDecimals`: for random `(atomics8, mark)` and the equivalent `(atomics8×10, mark)` at 9 decimals, pot value is identical.
- `TestProperty_fillReconciliation_neverExceedsQuote`: recorded `cost_basis_amount ≤ quoted outAmount` for any fake delta ≤ quote.

## Manual verification

Mainnet, small money, one QA cabal. Gold sim per `.cursor/skills/ios-simslim-fast-qa/SKILL.md`.

1. **Discover.** Stocks tab shows a Pre-IPO section with SpaceX, OpenAI, Kalshi, chips, and live prices. Search `spacex` → SpaceX first. Search `AAPL` → Apple first, no chip.
2. **Detail.** SpaceX detail: price, 24h, sector "Aerospace", "Trades around the clock", "Private-market reference $7xx · 27% below · updated Nh ago", About card, Terms link opens Safari. Apple detail unchanged except no reference row (gap under 1 %).
2b. **Nudge.** Propose $5 of SpaceX: the line "Trading 27% below its private-market reference. That gap can widen or close." appears above the amount. Propose $5 of Apple: no line. The SpaceX proposal card shows that same sentence, and it does not change if the DEX price moves before the vote closes.
3. **Buy.** Propose $5 of SpaceX in the QA cabal; pass the vote; watch logs for `resolve_mint` → Tessera, `slippage_bps=100`, execute Success code 0, then `quoted_out`, `received_out`, `fee_bps_observed`. Record whether that last number is near 0 or near 20; TS-T14 follows it. Solscan: treasury received tSpaceX.
4. **Hold.** Cabal screen Holdings row "SpaceX · 0.00xx tokens · $price", no After hours badge; pot total moved by ≈ $5 minus impact and fee; member P&L updates. Home boards still render.
5. **Sell.** Propose a sell of half the SpaceX holding; pass; USDC back in the treasury; holdings row halves. Then cash out a small amount from a member: payout lands, no shortfall error.
6. **Sweep.** `./scripts/sweep-wallets.sh --source <treasury> --dry-run` lists the tSpaceX remainder with the right whole-token amount; live run returns USDC to the Phantom agent wallet.
7. **Degrade.** Set `TESSERA_API_BASE_URL` to an unreachable host, restart: Stocks tab still shows xStocks; the Pre-IPO section shows the static three rows with prices (Jupiter) but no sector; existing SpaceX holding still values from Jupiter. Set `TESSERA_ENABLED=false`: section gone, holding still valued.

## Out of scope

Tessera redemption windows and claims, Tessera referral codes and fee sharing, Tessera Alpha Vault auctions, Meteora-direct routing, remote logo download on iOS, agent-side discovery UI beyond `?kind=`, moving `CatalogAsset` out of `internal/xstocks`, jurisdiction gating, the Dynamic/Base line.

## Risks

- **Single live price tier.** Stocks fall back Pyth → Jupiter; pre-IPO has only Jupiter. `potMarksLiveOnly` (`app/pot_valuation.go:27–29`) refuses cost-basis marks for deposit share minting and redeem pricing, so while Jupiter's breaker is open any cabal holding a pre-IPO token cannot fund or cash out (it can still view). Accepted for this lane; the existing `price_source_down` alert already pages on that breaker. If it bites, TS-T15's sampler gives a second tier (last sample ≤ 15 min old) for a follow-up.
- **Thin pools.** $120k–$520k liquidity on DLMM; a cabal buy of a few thousand dollars moves price. Propose-amount ceiling stays the pot total; no per-asset cap is added here.
- **Fee on every transfer**, not just sells: any future treasury→member token transfer (none exists today) would also pay 20 bps.

## Open decisions

- **Jurisdiction and eligibility copy.** Tessera's terms exclude some jurisdictions. Whether Monaco surfaces anything beyond the Terms link is a product/legal call; not picked here.
- **Nudge thresholds.** 10 % (pre-IPO) and 1 % (stock) are starting points taken from Zerodha's 0.25 % iNAV nudge scaled to the observed −50 %…+27 % pre-IPO range. Revisit after a week of samples from TS-T15.
- **Sampler cost.** TS-T15 adds a Jupiter Price call every 5 minutes; fine unauthenticated for three mints, revisit if the catalog grows.

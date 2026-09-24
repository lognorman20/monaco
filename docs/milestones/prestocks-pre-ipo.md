# PreStocks. Second pre-IPO issuer, best price per company

**Goal.** A cabal can search, propose, vote, buy, hold, value, and sell PreStocks tokens (SPACEX, OPENAI, KALSHI, ANTHROPIC, ANDURIL, NEURALINK, FIGUREAI, POLYMARKET) through the same path as xStocks and Tessera. When two issuers sell exposure to the same company, the backend picks the one that gives the most company exposure per USDC at that moment, and every row that shows the asset says which issuer it came from.

**Branch.** `solana/prestocks-pre-ipo`, cut from `solana/tessera-pre-ipo` after the sweep-tool and docs commit lands. Solana only. Does not touch the Dynamic/Base line on `main`.

**Depends on.** Tessera lane merged (composite catalog, per-asset decimals, `variants[]`, `premium_bps`). Mainnet treasury with a few USDC. `SOLANA_RPC_URL` that answers `getAccountInfo` (mint extensions are read live). `JUPITER_API_KEY` recommended.

**Owns.** `apps/backend/internal/prestocks/` (new), `apps/backend/internal/solana/mintinfo/` (new), best-price ranking in `internal/catalog/`, unit-scale plumbing in `app/` and `httpapi/`, the quote-time variant pick, mobile issuer labels and comparison line, docs.

## Bounty constraint, read first

The PreStocks bounty text says: *projects that integrate any non-PreStocks pre-IPO tokens will be ineligible for this bounty.* The Tessera lane already integrates Tessera tokens. Building both plus a comparison makes the combined product ineligible for the PreStocks prize as written.

This plan keeps both issuers behind existing switches so one codebase can run two profiles:

| Profile | Env | What the app shows |
| --- | --- | --- |
| Product | `TESSERA_ENABLED=true`, `PRESTOCKS_ENABLED=true` (defaults) | Both issuers, one row per company, best-price default, issuer label everywhere. |
| PreStocks bounty | `TESSERA_ENABLED=false` | PreStocks only. No Tessera rows, names, or Terms link. Comparison line never renders because every company has one variant. |

Which profile to submit is a product call, listed under **Open decisions**. The code is the same either way. PS-T13 verifies the bounty profile leaks nothing from Tessera.

## What PreStocks is, in the terms this codebase already uses

Verified against `https://prestocks.com/api/prestocks`, `https://prestocks.com/products`, Jupiter Price v3, Jupiter quotes, and mainnet `getAccountInfo` on 2026-09-23.

| Fact | Value | Why it matters here |
| --- | --- | --- |
| Chain / standard | Solana mainnet, **Token-2022** | Same family as xStocks and Tessera. `privy.ListSPLTokenBalances` already scans it. |
| Decimals | **9** | Same as Tessera. The per-asset decimals work from TS-T1/T4 carries over. |
| Transfer fee | **1 % (100 bps)** since epoch 1039 (before that 50 bps). Current epoch 1041. Fee config authority can change it again. | Five times Tessera's 20 bps. Round trip costs about 2 % before impact. Read the fee from the mint, epoch-aware; never hardcode 100. |
| Scaled UI amount | **`scaledUiAmountConfig` extension.** SPACEX multiplier **5** since 2026-06-10. OPENAI **1.4861347** since 2026-07-17. Others 1. Multipliers can change again (split-like events). | Jupiter `usdPrice`, `stockData.price`, and PreStocks `tokenPrice`/`markPrice` are **per scaled unit**. Jupiter swap `outAmount` and token balances are **raw atomics**. Measured: $10 USDC → 0.016688 raw SPACEX = $599 per raw token = $119.8 per scaled unit (Jupiter `usdPrice` $116.74, plus fee and impact). Valuing raw atomics against the scaled price undervalues SpaceX 5×. This is the biggest code change. |
| Other extensions | `permanentDelegate`, `pausableConfig` (`paused: false` today), `transferHook` with null program, confidential transfer configs, `defaultAccountState: initialized` | The issuer can pause transfers or move tokens. Paused must read as not routable. Disclosure copy needs one sentence. |
| Catalog API | `GET https://prestocks.com/api/prestocks` → `[{name, symbol, description, image, external_url, contract_address, markPrice, markValuation, tokenPrice, impliedValuation, supply}]`. Public, no key. 8 rows. No timestamps, no per-symbol route, no history. URL slugs match Tessera underlyings: `spacex`, `openai`, `kalshi`. | 60 s cache, 24 h last-good, static fallback of the 8 mints. `tokenPrice` tracks Jupiter `usdPrice` and `markPrice` tracks `stockData.price`, but neither has a timestamp. Do not copy them onto `referenceMark` or into the premium. Reference stays Jupiter `stockData` only, same as Tessera. |
| Symbols | On-chain and API symbol are the same bare word: `SPACEX`, `OPENAI`, `KALSHI`, `ANTHROPIC`, `ANDURIL`, `NEURALINK`, `FIGUREAI`, `POLYMARKET`. Name is `SpaceX PreStocks`. | Identity is the on-chain symbol. Display name strips the ` PreStocks` suffix → **SpaceX**. No collision with xStocks (`…x`) or Tessera (`t…`). |
| Mints | SPACEX `PreANxuXjsy2pvisWWMNB6YaJNzr7681wJJr2rHsfTh` · OPENAI `PreweJYECqtQwBtpxHL171nL2K6umo692gTm7Q3rpgF` · KALSHI `PreLWGkkeqG1s4HEfFZSy9moCrJ7btsHuUtfcCeoRua` · ANTHROPIC `Pren1FvFX6J3E4kXhJuCiAD5aDmGEb7qJRncwA8Lkhw` · ANDURIL `PresTj4Yc2bAR197Er7wz4UUKSfqt6FryBEdAriBoQB` · NEURALINK `PrekqLJvJ3qVdXmBGDiexvwUTF4rLFDa6HWS4HJbw9S` · FIGUREAI `PreZad18qfPtbxNpMtMuAuX2zVpvkEU8DnJx56faCWd` · POLYMARKET `Pre8AREmFPtoJFT8mQSXQLh56cwJmM7CFDRuoGBZiUP` | Fixtures and static fallback. |
| Execution | Jupiter routes USDC ↔ every mint. A $10 SPACEX quote on lite-api swap v1 returned 16,688,071 raw atomics ($599 per raw token, about $120 per scaled unit versus `usdPrice` $116.74). Re-measure on the app's Swap v2 `/order` in PS-T6 before treating the gap as impact. | Existing `/order` + `/execute` path. The v1 number only pins the direction: prices are per scaled unit, fills are raw. |
| Live price | Jupiter Price v3: `usdPrice`, `liquidity` $98k (KALSHI) to $907k (OPENAI), `decimals: 9`, `stockData` present for all 8 with `price` per scaled unit and company `mcap`. | Same tier-2 path as Tessera. `stockData` is what makes cross-issuer comparison possible. |
| Pyth | No feed. | Same skip as Tessera: `Kind == pre_ipo` never touches Pyth. |
| Market hours | 24/7 | `afterHours` never true. |
| Legal shape | "Backed 1:1 by SPV exposure that tracks the price of the underlying private company." | Copy says **tokens**. Same disclosure card as Tessera with the issuer name swapped. |
| Overlap with Tessera | SpaceX, OpenAI, Kalshi exist on both. Five PreStocks names have no Tessera twin. | Three companies get a comparison. Five behave like single-issuer Tessera rows. |

### Measured head-to-head, 2026-09-23

Per-token prices are not comparable across issuers. SPACEX is a fifth of a tSpaceX unit, and the two `stockData.price` values differ by that same factor (746.61 / 149.32 = 5.00). Compare **dollars paid per dollar of reference exposure**: `costRatio = usdPrice / stockData.price`. Lower is cheaper. Do not also divide by the transfer fee until a fill proves Jupiter did not already net it (`jupiterNetsTransferFee`, PS-T6). Applying the fee on the list and not on the live quote would pick different issuers on a close race.

| Company | Tessera `usdPrice / stockData.price` | PreStocks | Cheaper today |
| --- | --- | --- | --- |
| SpaceX | 562.19 / 746.61 = 0.753 | 116.74 / 149.32 = 0.782 | Tessera by 3.7 % |
| OpenAI | 1038.33 / 1024.33 = 1.014 | 1343.67 / 1024.33 = 1.312 | Tessera by 23 % |
| Kalshi | 446.19 / 883.30 = 0.505 | 866.00 / 883.30 = 0.980 | Tessera by 49 % |

Showing $117 next to $562 on one row would read as PreStocks being cheaper. Variant rows lead with the exposure gap, not the per-token price. The fee columns (0.2 % vs 1 %) are labels. They enter the ratio only after `jupiterNetsTransferFee` is set false, and then on both the list and the quote together. Entry only: the sell fee is not in the score.

Both mints of a company carry the same `stockData.mcap` from Jupiter (SpaceX 1.958 T on both), which is the sanity check that the two references describe the same company. The Kalshi gap sits on a $190k pool; a real fill of a few thousand dollars would move it. That is why the list rank uses Price v3 and the propose step re-checks with live quotes for the actual amount.

## Pricing and catalog conventions

Rules 1 to 6, 8, and 10 from `tessera-pre-ipo.md` stand. Rules 7 and 9 are replaced. One Tessera copy rule is relaxed.

7. **Default variant per company is the best price per dollar of exposure.** For every routable, unpaused variant with a fresh reference (`stockData.updatedAt` under 48 h): `costRatio = usdPrice / stockData.price`, and when `jupiterNetsTransferFee` is false also divide by `(1 − transferFeeBps / 10000)`. Lowest wins. Fetch Price v3 only for underlyings that have more than one variant, not for every stock in the search. If the two lowest sit within 25 bps of each other, the one with more Price v3 `liquidity` wins. Cached 5 minutes per `UnderlyingID`. Drop the cache entry when its multiplier's effective timestamp passes, so a split is not served for the rest of the TTL. When any candidate lacks a fresh reference, or the candidates' `mcap` differ by more than 2 %, fall back to the old rule (routable, liquidity, issuer order) and mark the comparison `unavailable`. In that case no variant gets `bestPrice: true`.
9. **The propose step re-picks with live quotes for the amount, and the proposal stores the server's pick.** `selectBestVariant` is buy-only. A sell, a pinned variant, and an agent intent quote the requested symbol and never switch issuer. The server quotes every eligible variant for that company (cap 8; more than 8 returns `basis: unavailable` and quotes the requested symbol) price-only for the same `usdcMicros`, using one shared function with rule 7. Exposure from a quote is `outAmount × multiplier / 10^decimals × stockData.price`, times `(1 − fee)` only when `jupiterNetsTransferFee` is false. Highest exposure wins. `CreateProposal` runs the same pick again and persists that symbol; execute buys the stored symbol and does not pick again. The client shows whatever symbol the proposal response returns. If only one variant is eligible, `priceComparison.basis` is `single` and mobile renders nothing extra.
11. **Quantities are scaled units. Storage is raw atomics.** Everything persisted (`cost_basis_amount`, `token_amount`, sells) stays raw. Display and valuation multiply by the multiplier. Dollar entry and typed-token entry divide by it before converting to raw atomics, so a SpaceX sell of the amount on screen does not move 5× the holding. "Sell all" keeps using the raw ceiling. The multiplier is a rational parsed from the mint's decimal string (5 = 5/1, 1.4861347 = 14861347/10000000), not a float64. Absent extension means 1. A parsed 0 or negative is unresolved, not 1. A future multiplier change rescales the displayed quantity and leaves value unchanged.
12. **Issuer is visible whenever it matters.** Rows for a company with more than one variant, and every pre-IPO holdings, proposal, activity, and transaction row, carry a short issuer label: "via PreStocks", "via Tessera". This supersedes the Tessera-lane rule that the only Tessera string in the UI is the Terms link. `MainFlowCopyAudit` allows the two issuer names as labels; the bans on `mint`, `route`, `quote`, `treasury`, `units`, `xstock` stay.
13. **Paused means not for sale here.** A mint whose `pausableConfig.paused` is true is not routable, is not a default candidate, and a quote for it returns the existing not-routable shape with `reason: "issuer_paused"`. Existing holdings still value from Jupiter.

## Structure

```text
apps/backend/
  internal/solana/mintinfo/         NEW  getAccountInfo jsonParsed → decimals, transfer fee (epoch-aware),
    reader.go                            scaled UI multiplier (timestamp-aware), paused, permanent delegate
    cache.go                             10 min cache, 24 h last-good, static fallback table
    fake.go, testdata/*.json             fixtures for SPACEX, OPENAI, KALSHI, tSpaceX
  internal/prestocks/               NEW  PreStocks public API client
    client.go                            60 s cache, 24 h last-good, then static fallback
    assets.go                            → []xstocks.CatalogAsset (Kind pre_ipo, Source prestocks, Issuer prestocks)
    fake.go, testdata/prestocks.json     captured 2026-09-23
  internal/catalog/
    searcher.go                          []Source instead of one tessera field; issuer flags; PreStocks name matching
    best_price.go                        rule 7 ranking, 5 min cache, Comparison struct
    resolver.go                          SPACEX / spacex / "SpaceX PreStocks" aliases
  internal/xstocks/catalog.go            CatalogAsset gains UiAmountMultiplier, Paused, IssuerName
  internal/app/
    start_buy.go, quotes                 selectBestVariant path, price-only quotes per variant
    marked_pot.go, pot_valuation.go      × uiMultiplier
    swap.go                              fee from mintinfo, paused refusal
    redeem.go                            buffer wiring per measurement
  internal/httpapi/
    asset_catalog_json.go                issuerName, uiAmountMultiplier, paused, comparison on variants
    quotes.go                            selectBestVariant, provider, priceComparison
    proposals.go, transactions.go, groups view   issuerName on rows
  internal/config/config.go              PRESTOCKS_API_BASE_URL, PRESTOCKS_ENABLED
  cmd/api/main.go                        wiring; RPC client for mintinfo
  cmd/sweep-member-to-address/           whole-token display × multiplier
packages/mobile-core/Sources/MonacoCore/
  MarketAssetDTO.swift, CatalogDTO.swift issuerName, uiAmountMultiplier, paused
  BuyQuoteDTO.swift                      provider, priceComparison
  DisplayFormatters.swift                quantity × multiplier; IssuerLabel
apps/mobile/Monaco/Features/
  Assets/                                issuer chip when variantCount > 1; "Also available from" with Best price tag
  Proposals/                             selectBestVariant, comparison line, "via PreStocks" on cards
  Groups/PotSectionView.swift            issuer label on pre-IPO rows
docs/product.md, docs/api.md, README.md, docs/index.md
```

## Flow

Buy, unchanged except where marked **new**.

1. `GET /v1/assets?query=spacex` → composite fans out to xStocks, Tessera, and PreStocks (**new** source). Rows for `spacex` collapse to one. The default is the best-price variant per rule 7 (**new**). The row carries `issuer`, `issuerName`, `variantCount: 2`.
2. Detail shows the default variant's price, the reference row, and "Also available from" with both variants. Each row leads with the exposure gap, then fee and liquidity, and a **Best price** tag only when the comparison is available (**new**). Tapping a variant pins it for this screen's Buy.
3. Propose sends `{ symbol, usdcMicros, selectBestVariant: true }` unless a variant was pinned (**new**). Server quotes every eligible variant price-only, picks the highest reference exposure, returns `provider` and `priceComparison` (**new**). `CreateProposal` runs the pick again and stores that symbol. Mobile shows the comparison line and the symbol from the proposal response. Execute does not pick again.
4. Vote and pass as today. `DevExecuteBuy` uses decimals 9, slippage 100 bps, `TransferFeeBps` from `mintinfo` (**new**), refuses a paused mint (**new**).
5. Confirm persists the RPC balance delta as today. `fee_bps_observed` is logged; for PreStocks it should read near 100 if Jupiter does not net the fee.
6. Holdings: value = `raw × uiMultiplier / 10^9 × usdPrice` (**new**). Quantity shows scaled units (**new**). Two SpaceX holdings are two rows, "SpaceX via Tessera" and "SpaceX via PreStocks".
7. Sell and cash-out stay in raw atomics. Typed amounts and dollar amounts are converted with the multiplier, so the number on screen is the number that moves (**new**). Redeem buffer includes the fee only when `jupiterNetsTransferFee` is false.
8. Agents: `symbol` contract unchanged; `SPACEX` resolves through the composite. `?kind=pre_ipo` lists both issuers.

## Data models

`CatalogAsset` additions:

```go
UiAmountMultiplier *big.Rat // nil = unresolved; 1 = no scale; 5/1 for SPACEX. Parsed from the decimal string. JSON emits the original decimal string.
Paused             bool    // pausableConfig.paused
IssuerName         string  // "xStocks" | "Tessera" | "PreStocks" (display)
```

`AssetSource` gains `prestocks`. `Issuer` gains `prestocks`.

`mintinfo.Info`:

```go
type Info struct {
    Mint            string
    Decimals        int
    TransferFeeBps  int       // newerTransferFee if currentEpoch >= its epoch, else older
    UiMultiplier    *big.Rat  // nil when the decimal string is missing, 0, or negative; 1 when the extension is absent
    Paused          bool
    PermanentDelegate bool
    FetchedAt       time.Time
}
```

`catalog.Comparison`:

```go
type Comparison struct {
    Basis        string              // "price_v3" | "live_quote" | "single" | "unavailable"
    ChosenSymbol string
    Candidates   []ComparisonCandidate // every eligible variant, including the chosen one
}
type ComparisonCandidate struct {
    Symbol, Issuer, IssuerName, SolanaMint string
    CostRatioBps int64   // price_v3 basis: costRatio × 10000
    ExposureUsdcMicros int64 // live_quote basis
    DeltaBps     int64   // vs chosen; 0 for chosen; positive = worse
    Reason       string  // "" | "issuer_paused" | "no_route" | "reference_unavailable"
}
```

No migration. Proposals and transactions already store mint and decimals; issuer is derived from mint through the composite with the static fallback.

**HTTP contract additions** (all additive; Swift optionals with defaults):

| Route | New fields |
| --- | --- |
| asset rows and detail | `issuerName`, `uiAmountMultiplier`, `paused`. `variants[]` items gain `issuerName`, `transferFeeBps`, `costRatioBps?`, `bestPrice: bool` |
| `POST /v1/groups/{id}/quotes` request | `selectBestVariant?: bool` (buy only) |
| `POST /v1/groups/{id}/quotes` response | `provider: {issuer, issuerName}`, `priceComparison: {basis, candidates[]}`, `uiAmountMultiplier`; `symbol` may differ from the request when `selectBestVariant` picked another variant |
| proposal rows, pot rows, activity, transaction detail | `issuer`, `issuerName`, `uiAmountMultiplier` for pre-IPO rows |
| not-routable quote | `reason: "issuer_paused"` when applicable |

`priceUsdcMicros` keeps meaning USDC micros per one **scaled** unit, which is the unit wallets and Jupiter show.

**Env:** `PRESTOCKS_API_BASE_URL` (default `https://prestocks.com`), `PRESTOCKS_ENABLED` (default `true`). Telemetry upstream `prestocks`. `mintinfo` reads through `SOLANA_RPC_URL`.

## Decisions locked here

- **Best price means most reference exposure per USDC.** Never per-token price. Fee is outside the ratio while `jupiterNetsTransferFee` is true, and inside both the list and the quote together when it is false. List rank uses Price v3 with a 25 bps liquidity tiebreak; propose uses live quotes for the amount. Comparison degrades to the old rule, never to a wrong number. The vote buys the symbol stored at propose time.
- **Provider is explicit.** The quote returns `provider`; mobile prints "Best price via {issuerName}" and the runner-up's delta. Proposal cards, holdings, activity, and transaction detail say "via {issuerName}" for pre-IPO rows.
- **Scaled units are the display and valuation unit; raw atomics are storage.** Multiplier is read live from the mint with a cached last-good and a static fallback that matches the fixtures. A pre-IPO holding whose multiplier cannot be resolved by any tier is valued at multiplier 1 and logged at error level with the mint; `potMarksLiveOnly` treats that holding as not live.
- **Fee is read from the mint, epoch-aware.** Static fallbacks: Tessera 20, PreStocks 100.
- **Paused mints do not trade here.**
- **Bounty profile is `TESSERA_ENABLED=false`.** No new env concept. PS-T13 proves it.
- **Words:** "Pre-IPO", "tokens", "via PreStocks", "via Tessera", "Best price". Never "SPV", "permanent delegate", "scaled", "multiplier", "Token-2022", "transfer fee". The disclosure card says "The issuer can pause transfers." in the PreStocks version.
- **Out of this lane:** PreStocks redemptions, referral or ecosystem hooks, DeFi or collateral uses, charts before the sampler, multi-chain, a per-asset buy cap.

## Parallelization

| Wave | Tracks (parallel within the wave) | Gate |
| --- | --- | --- |
| **1** | **Mint info reader** PS-T1 ∥ **PreStocks client** PS-T2, then **Composite sources + best-price rank** PS-T3 | PS-T3 waits for PS-T1 and PS-T2 |
| **2** | **Unit scale in valuation** PS-T4, then **Quote-time variant pick** PS-T5 ∥ **Fee, pause, redeem** PS-T6 | Wave 1. PS-T5 waits for PS-T4 so they do not edit quote math together |
| **3** | **HTTP contract** PS-T7 · **Wiring + env** PS-T8 · **mobile-core** PS-T9 | Wave 2 for T7/T8; T7 fixtures for T9 |
| **4** | **Stocks tab + detail** PS-T10 ∥ **Propose, cards, holdings** PS-T11 ∥ **Sweep + docs** PS-T12 | PS-T9 |
| **5** | **Bounty profile check** PS-T13 · manual mainnet check | Wave 4 |
| **6** (optional) | **Price sampler + charts** PS-T14 | Wave 5 |

Backend and mobile split across agents. PS-T7 fixes JSON first so mobile builds against fixtures.

## Tickets

1. PS-T1 `mintinfo`: read decimals, epoch-aware transfer fee, timestamp-aware UI multiplier, paused, permanent delegate from the mint account; cache, last-good, static fallback.
2. PS-T2 PreStocks catalog client with cache, last-good, static fallback, fixture.
3. PS-T3 Composite over N sources; PreStocks matching and aliases; best-price default variant with 5 min cache; `Comparison`.
   Depends on: PS-T1, PS-T2
4. PS-T4 Multiply by `uiAmountMultiplier` in pot valuation, cost-basis mark, quote price per unit, pot-row quantity, sell probe size, sweep whole-token display.
   Depends on: PS-T1
5. PS-T5 `selectBestVariant` on buy quotes: price-only quotes per variant, exposure pick, `provider` and `priceComparison`; issuer on proposal, pot, activity, transaction rows.
   Depends on: PS-T3
6. PS-T6 Fee from `mintinfo` into swap requests; paused refusal; redeem buffer wiring after the fee measurement; slippage probe for the 8 mints.
   Depends on: PS-T1
7. PS-T7 HTTP contract fields and `docs/api.md`.
   Depends on: PS-T4, PS-T5
8. PS-T8 Wire `prestocks`, `mintinfo`, env, telemetry, RPC client in `cmd/api/main.go` and the sweep tool.
   Depends on: PS-T4, PS-T5, PS-T6
9. PS-T9 mobile-core DTOs, quantity formatter with multiplier, `IssuerLabel`, copy-audit allowances.
   Depends on: PS-T7
10. PS-T10 Stocks tab and detail: issuer chip when `variantCount > 1`, "Also available from" with Best price tag, fee line, PreStocks about link, pause state.
    Depends on: PS-T9
11. PS-T11 Propose sheet sends `selectBestVariant`, shows the comparison line, uses the returned symbol; proposal cards, holdings, activity, transaction detail show "via {issuerName}".
    Depends on: PS-T9
12. PS-T12 Sweep tool multiplier display; `docs/product.md`, `README`, `docs/index.md`.
    Depends on: PS-T8
13. PS-T13 Bounty profile: run with `TESSERA_ENABLED=false`, prove no Tessera row, name, or link renders; scripted check.
    Depends on: PS-T10, PS-T11
14. PS-T14 (optional) Price sampler and 1D/1W/1M charts for every pre-IPO mint (same design as TS-T15).
    Depends on: PS-T8

## Ticket details

#### PS-T1: `mintinfo` reader

**Context**
PreStocks mints scale their displayed unit (`scaledUiAmountConfig`) and changed their transfer fee once already. Both values must come from the chain, not a constant.

**Problem**
Nothing in the backend reads Token-2022 mint extensions. Tessera's fee and decimals are static.

**Proposal**
Implement exactly:
1. `apps/backend/internal/solana/mintinfo/reader.go`: `type Reader interface { Info(ctx, mint string) (Info, error) }`. `HTTPReader` calls `getAccountInfo` with `{"encoding":"jsonParsed"}` on `SOLANA_RPC_URL`, lazily per mint (not at boot), plus one `getEpochInfo` per cache window. Parse `decimals`, `transferFeeConfig` (choose `newerTransferFee` when `epoch >= newerTransferFee.epoch`, else `olderTransferFee`), `scaledUiAmountConfig` (choose `newMultiplier` when `now >= newMultiplierEffectiveTimestamp` and that timestamp is non-zero, else `multiplier`; parse the decimal string into a `*big.Rat`; absent extension = 1; `0` or negative = nil). `pausableConfig.paused`, presence of `permanentDelegate`. Non-Token-2022 owner returns `Info{Decimals, UiMultiplier: 1}`. An empty `SOLANA_RPC_URL` skips the RPC tier and uses the static table; the API still boots.
2. `cache.go`: 10 min TTL per mint. A cache hit whose stored multiplier is the pre-effective one is a miss once `now >= newMultiplierEffectiveTimestamp`. 24 h last-good on RPC error, then `StaticFallback()` with the multiplier strings `5`, `1.4861347`, and `1` (not float literals): SPACEX (5, 100 bps), OPENAI (1.4861347, 100 bps), the other six PreStocks mints (1, 100 bps), the three Tessera mints (1, 20 bps). A miss on every tier returns `ErrUnknownMint`. Pause fails open (last-good, else not paused). Fee and multiplier fail to the static table, never to 0.
3. `fake.go`: `NewFakeReader(map[string]Info)`.
4. Fixtures: captured `getAccountInfo` bodies for SPACEX, OPENAI, KALSHI, tSpaceX under `testdata/`.

### Scope
- `apps/backend/internal/solana/mintinfo/` (new)

**Acceptance Criteria**
- [ ] `TestReader_spacex_multiplier5_fee100_afterEffective` and `TestReader_spacex_beforeEffective_multiplier1` (clock injected).
- [ ] `TestReader_feeEpoch_picksOlderBeforeNewerEpoch`.
- [ ] `TestReader_tSpaceX_noScaleExtension_multiplier1_fee20`.
- [ ] `TestReader_rpcDown_returnsLastGoodThenStatic`.
- [ ] `go test ./internal/solana/mintinfo/` exits 0. No network.

**Verification**
- `just test backend`

**Done when** every AC box is checked. **Wave:** 1

#### PS-T2: PreStocks catalog client

**Context**
`internal/tessera` shows the shape: HTTP client, cache, last-good, static fallback, fixture.

**Problem**
No PreStocks source exists.

**Proposal**
Implement exactly:
1. `apps/backend/internal/prestocks/client.go`: `NewHTTPCatalogWithClient(baseURL string, httpClient *http.Client) *HTTPCatalog` implementing `catalog.Source`. `GET {base}/api/prestocks`. 60 s cache, 24 h last-good, then `StaticFallback()`. Telemetry upstream `prestocks`.
2. `assets.go`: map each row to `CatalogAsset{Symbol: symbol, Name: name minus the suffix " PreStocks", SolanaMint: contract_address, Kind: pre_ipo, Source: prestocks, Issuer: "prestocks", IssuerName: "PreStocks", Decimals: 9, TransferFeeBps: 100 (overridden by mintinfo at read), UnderlyingID: last path segment of external_url lowercased, LogoURL: image, Sector: ""}`. Leave `ReferenceMark*` empty. `markPrice` and `tokenPrice` are not a reference. Reject rows whose mint is not base58 of length 32–44. Hide an empty sector in the UI (PS-T10); do not invent one.
3. `fake.go`: `NewFakeCatalog(rows...)`.
4. `testdata/prestocks.json`: today's 8-row body.

### Scope
- `apps/backend/internal/prestocks/` (new)

**Acceptance Criteria**
- [ ] `TestPreStocksList_parsesFixture_eightRows_spacexUnderlying`.
- [ ] `TestPreStocksList_nameStripsSuffix` (`SpaceX PreStocks` → `SpaceX`).
- [ ] `TestPreStocksList_cachesFor60s`, `TestPreStocksList_upstream500_lastGoodThenStatic`, `TestPreStocksList_rejectsMalformedMint`.
- [ ] No network in tests.

**Verification**
- `just test backend`

**Done when** every AC box is checked. **Wave:** 1

#### PS-T3: Composite over N sources and best-price default

**Context**
`catalog.Composite` holds one `tessera Source`. Rule 7 is being replaced by best price.

**Problem**
A second supplemental source does not fit; default variant ignores price.

**Proposal**
Implement exactly:
1. `Composite` takes `sources []Source` (each tagged with its `AssetSource`), a `mintinfo.Reader`, and a `jupiter.PriceClient`. Keep `NewComposite(xstocks, tessera, prober)` as a thin wrapper for existing tests; add `NewCompositeWithSources(xstocks, sources, prober, mintinfo, prices)`.
2. On list, enrich every pre-IPO row from `mintinfo`: `Decimals`, `TransferFeeBps`, `UiAmountMultiplier`, `Paused`. `ErrUnknownMint` keeps the catalog row's fee and decimals and sets the multiplier unresolved. A `Paused` row is `Routable: false`.
3. Matching: PreStocks rows match on symbol (`spacex`), stripped name, and underlying (`spacex`, `openai`, `kalshi`, `anthropic`, `anduril`, `neuralink`, `figureai`, `polymarket`). Resolver accepts `SPACEX`, `spacex`, `SpaceX PreStocks`. A bare `spacex` resolves to the company default; `tSpaceX` and `SPACEX` resolve exactly.
4. `best_price.go`: `selectDefaultVariant` becomes rule 7, and only when `len(variants) > 1`. Price v3 is fetched for those mints only. Cache 5 minutes. Expose `Compare(ctx, underlyingID) (Comparison, error)` returning `Basis: "price_v3"` or `"unavailable"`. Empty-query search must keep PreStocks rows when the xStocks page is already full (same bug class as the Tessera empty-query trim).
5. `SearchVariants` returns every variant with `UiAmountMultiplier`, `TransferFeeBps`, `Paused` filled.

### Scope
- `apps/backend/internal/catalog/`, `apps/backend/internal/xstocks/catalog.go` (three new fields)

**Acceptance Criteria**
- [ ] `TestBestPrice_spacex_prefersLowerCostRatio` using today's numbers picks `tSpaceX`.
- [ ] `TestBestPrice_within25bps_prefersLiquidity`.
- [ ] `TestBestPrice_missingReference_fallsBackToLiquidityRule_basisUnavailable`.
- [ ] `TestBestPrice_mcapMismatch_fallsBack`.
- [ ] `TestBestPrice_pausedVariant_excluded_reasonIssuerPaused`.
- [ ] `TestCompositeSearch_spacexQuery_oneRow_variantCount2`.
- [ ] `TestCompositeResolver_SPACEX_and_tSpaceX_distinctMints`.
- [ ] `TestCompositeSearch_tesseraDisabled_noTesseraRows`.
- [ ] `TestCompositeSearch_emptyQuery_fullXStockPage_keepsPreStocks`.

**Verification**
- `just test backend`

**Done when** every AC box is checked. **Wave:** 1

#### PS-T4: Unit scale in valuation and display

**Context**
Storage is raw atomics; prices are per scaled unit.

**Problem**
`marked_pot`, cost-basis marks, `quotePriceUsdcMicros`, pot-row quantity, the one-whole-token sell probe, and the sweep display all treat `raw / 10^decimals` as the priced unit. SpaceX via PreStocks values at one fifth.

**Proposal**
Implement exactly:
1. `pyth.MarkedHolding` and `CostBasis` gain `UiMultiplier *big.Rat` (nil or unset → unresolved, not 1). `marked_pot` value = `units × multiplier × mark / 10^decimals` using that rational, rounded to micros. `CostBasisMarkPerUnitMicros` divides by `units × multiplier`.
2. `quotePriceUsdcMicros(inUSDC, outAtomics, decimals, multiplier)` returns price per scaled unit, so the propose sheet matches the detail price ($117, not $599).
3. Pot rows, transaction detail, and activity carry `uiAmountMultiplier` as a decimal string; the `tokenAmount` field stays raw.
4. Sell routability probe for detail uses one scaled unit: `raw = ceil(10^decimals / multiplier)` in rational arithmetic. OPENAI's 1.4861347 does not divide 1e9 evenly.
5. `cmd/sweep-member-to-address` whole-token display multiplies by the multiplier from the composite row. This ticket owns that file; PS-T12 does not.
6. Multiplier source per holding: composite `LookupByMint`, else `mintinfo` directly. If both miss, log `mint_multiplier_unresolved` and mark the holding not live for `potMarksLiveOnly`. Do not value it at multiplier 1.

### Scope
- `apps/backend/internal/pyth/decimals.go`, `internal/app/marked_pot.go`, `pot_valuation.go`, `quotes*.go`, `internal/httpapi/assets.go` probe, `cmd/sweep-member-to-address/sweep_plan.go`

**Acceptance Criteria**
- [ ] `TestMarkedPot_spacexPreStocks_multiplier5_valuesFiveTimesRaw`.
- [ ] `TestProperty_potValueInvariantToMultiplier`: `(raw, mult, mark)` and `(raw×k, mult/k, mark)` value equal within 1 micro.
- [ ] `TestQuotePrice_scaledUnit` with the measured $10 → 16688071 raw at multiplier 5 gives about $119.8 per unit.
- [ ] `TestSellProbe_scaledUnit_roundsUp`.
- [ ] Existing 8-decimal and Tessera tests unchanged.

**Verification**
- `just test backend`

**Done when** every AC box is checked. **Wave:** 2

#### PS-T5: Quote-time best variant and provider on rows

**Context**
Rule 9. The list default is a 5-minute Price v3 estimate; the propose step must use the real amount.

**Problem**
`POST /quotes` quotes exactly the requested symbol. Rows do not say which issuer a pre-IPO asset came from.

**Proposal**
Implement exactly:
1. `StartBuy` gains `SelectBestVariant bool`. Ignored unless the request is a buy. When true and the resolved asset is pre-IPO with `variantCount > 1`: fetch variants, drop paused and unroutable, request a price-only `QuoteBuy` (no taker) for each (cap 8) with the same `usdcMicros` in parallel. Exposure uses the same function as rule 7. Pick the highest. Then run the normal taker quote for the winner. Build `Comparison{Basis: "live_quote"}`. If any candidate lacks a fresh reference, or there are more than 8 variants, skip the pick, quote the requested symbol, and return `Basis: "unavailable"`.
2. Quote response: `Symbol` is the chosen symbol; add `Provider{Issuer, IssuerName}` and `PriceComparison`.
3. `CreateProposal` with `selectBestVariant: true` runs that pick again and persists the winner. Execute loads the stored symbol and does not pick again. Proposal, pot, activity, and transaction rows: `Issuer`, `IssuerName` derived from mint through the composite with static fallback. xStocks rows return `xstocks` / `xStocks`.
4. Agent intents and sells quote the exact symbol. `selectBestVariant` is rejected on those routes.

### Scope
- `apps/backend/internal/app/start_buy.go`, `quotes*.go`, `proposal_view*.go`, `pot_view*.go`, `transactions*.go`, `internal/httpapi/quotes.go`

**Acceptance Criteria**
- [ ] `TestStartBuy_selectBestVariant_measuredFixture_picksTessera`: SPACEX out 16688071 at 5/1 and reference $149.32 versus tSpaceX out 17711094 at 1/1 and reference $746.61. tSpaceX wins. Inverting the multiplier fails this test.
- [ ] `TestStartBuy_selectBestVariant_singleVariant_basisSingle`.
- [ ] `TestStartBuy_selectBestVariant_referenceMissing_quotesRequestedSymbol_basisUnavailable`.
- [ ] `TestStartBuy_selectBestVariant_false_quotesRequestedSymbol`.
- [ ] `TestStartBuy_selectBestVariant_onSell_ignored`.
- [ ] `TestCreateProposal_selectBestVariant_persistsServerSymbol` even when the body names the other variant.
- [ ] `TestExecute_doesNotRepick`.
- [ ] `TestPotRows_preIpo_carryIssuerName`.
- [ ] Fake Jupiter `LastRequests()` shows at most `variants + 1` order calls per quote.

**Verification**
- `just test backend`

**Done when** every AC box is checked. **Wave:** 2

#### PS-T6: Fee, pause, slippage, redeem

**Context**
PreStocks fee is 1 % and adjustable; mints are pausable.

**Problem**
`swapprovider.Request.TransferFeeBps` comes from the static catalog row. Nothing checks pause. Redeem buffer ignores fee pending the Tessera measurement.

**Proposal**
Implement exactly:
1. `SwapService` fills `TransferFeeBps` from `mintinfo` at execute time; static row value is the fallback.
2. Before building an order for a pre-IPO mint, check `Paused`; if true return `ErrIssuerPaused` and the proposal execution records `failed` with `reason: issuer_paused`. Quotes return the not-routable shape with the same reason.
3. Slippage stays 100 bps for pre-IPO. Add `cmd/probe-preipo-impact` (dev tool, not shipped in the API) that prints Jupiter price impact at $25 and $250 for all 11 pre-IPO mints. Record the output in this file's manual section. If any mint exceeds 1 % at $25, raise that issuer's slippage to 150 bps by constant, not env.
4. One constant `jupiterNetsTransferFee` (default `true`) gates both this buffer and the cost-ratio fee term in rule 7. Set it `false` only when a fill's `fee_bps_observed` is within 20 % of that mint's fee, meaning Jupiter did not already net it. While `true`, redeem stays at 100 bps and the comparison does not divide by `(1 − fee)`. The probe in item 3 uses the app's Swap v2 client, not lite-api.

### Scope
- `apps/backend/internal/app/swap.go`, `redeem.go`, `internal/swapprovider`, `cmd/probe-preipo-impact` (new, dev only)

**Acceptance Criteria**
- [ ] `TestDevExecuteBuy_paused_refusesWithIssuerPaused`.
- [ ] `TestDevExecuteBuy_feeFromMintInfo_overridesStaticRow`.
- [ ] `TestRedeemBuffer_preIpo_followsConstant`.
- [ ] Probe output pasted under **Manual verification**.

**Verification**
- `just test backend`; `go run ./cmd/probe-preipo-impact`

**Done when** every AC box is checked. **Wave:** 2

#### PS-T7: HTTP contract

**Proposal**
Implement exactly the table under **Data models**: `issuerName`, `uiAmountMultiplier`, `paused` on asset rows and detail; `variants[]` items gain `issuerName`, `transferFeeBps`, `costRatioBps`, `bestPrice`; quote request `selectBestVariant`; quote response `provider`, `priceComparison`, `uiAmountMultiplier`; `issuer`/`issuerName`/`uiAmountMultiplier` on proposal, pot, activity, transaction rows; `reason: issuer_paused`. Update `docs/api.md` table rows and the **Catalog kind** paragraph.

### Scope
- `apps/backend/internal/httpapi/`, `docs/api.md`

**Acceptance Criteria**
- [ ] `TestGET_asset_spacex_variants_oneBestPriceFlag` and `TestGET_asset_comparisonUnavailable_noBestPriceFlag`.
- [ ] `TestPOST_quotes_selectBestVariant_returnsProviderAndComparison`.
- [ ] `TestPOST_quotes_pausedMint_notRoutable_reasonIssuerPaused`.
- [ ] `TestGET_groupView_potRows_preIpo_issuerName`.
- [ ] `cmd/api/routes_doc_test.go` passes.

**Verification**
- `just test backend`

**Done when** every AC box is checked. **Wave:** 3

#### PS-T8: Wiring and env

**Proposal**
Implement exactly: `PRESTOCKS_API_BASE_URL` (default `https://prestocks.com`), `PRESTOCKS_ENABLED` (default true) in `config.go` with tests; `cmd/api/main.go` builds `mintinfo.NewHTTPReader(solanaRPC)`, the PreStocks source when enabled, `catalog.NewCompositeWithSources(...)` with both supplemental sources filtered by their flags, and passes the Jupiter price client. Boot log `catalog sources ready xstocks=true tessera=<bool> prestocks=<bool>`. `cmd/sweep-member-to-address` uses the same constructor. `.env.example` and `docs/ops-observability.md` gain the two vars and the `prestocks` upstream label.

### Scope
- `apps/backend/internal/config/`, `cmd/api/main.go`, `cmd/sweep-member-to-address/main.go`, `.env.example`, `docs/ops-observability.md`

**Acceptance Criteria**
- [ ] `TestConfig_prestocksDefaults`, `TestConfig_prestocksDisabled`.
- [ ] `go build ./...` and `just test backend` exit 0.
- [ ] `just run backend` logs the three-source line.

**Done when** every AC box is checked. **Wave:** 3

#### PS-T9: mobile-core

**Proposal**
Implement exactly: optional `issuerName`, `uiAmountMultiplier` (decimal string), `paused` on `MarketAssetDTO`, `AssetDetailDTO`, `CatalogAssetDTO`, `AssetVariantDTO` (plus `transferFeeBps`, `costRatioBps`, `bestPrice`); `provider` and `priceComparison` on `BuyQuoteDTO`; `issuer`, `issuerName`, `uiAmountMultiplier` on proposal, pot, activity, transaction DTOs. `resolvedMultiplier` parses the string and is 1 only when the field is absent. A present `"0"` is an error, not 1. `TokenQuantityFormatter` and `ProposeMath.shares` multiply raw by that rational. `ProposeMath.atomics(fromShares:)` and `atomics(forUsd:)` divide by it; the dollar "all" path still returns the raw ceiling. Extend `BuyQuoteDTO.hash(into:)` with `symbol` already covered plus `provider` so a changed issuer refreshes the sheet. New DTO fields get defaulted memberwise arguments, and every sample initializer (`GroupDetailSampleHarness`, `SampleProposalFeedService`) compiles in this ticket. `IssuerLabel.via(issuerName)` → "via PreStocks". Never `.capitalized` on the issuer slug (`prestocks` would render "Prestocks"). `MainFlowCopyAudit` allows `PreStocks` and `Tessera` as issuer labels. `ProductBoundaryScanner` unchanged; `prestocks.com` is a Safari link only.

**Acceptance Criteria**
- [ ] `testBuyQuoteDTO_decodesProviderAndComparison`, `testBuyQuoteDTO_decodesWithoutThem`.
- [ ] `testTokenQuantity_multiplier5_showsScaledUnits` (16688071 raw, 9 decimals, ×5 → "0.0834 tokens").
- [ ] `testProposeMath_dollarEntry_dividesByMultiplier` so $5 at $116.74 and ×5 is one fifth of the raw atomics the old formula produced, and "all" still returns the raw ceiling.
- [ ] `testIssuerLabel_via`.
- [ ] `testMainFlowCopyAudit_issuerLabels_pass`.
- [ ] `just test mobile` exits 0.

**Done when** every AC box is checked. **Wave:** 3

#### PS-T10: Stocks tab and asset detail

**Proposal**
Implement exactly:
1. Rows with `variantCount > 1` show a small "via {issuerName}" caption under the name. Single-variant pre-IPO rows keep the Pre-IPO chip only.
2. Detail "Also available from" uses `issuerName`, not `issuer.capitalized`. The primary line is the exposure gap ("4% less exposure"), then "Fee 0.2%" or "Fee 1%" and liquidity. The per-token price is a secondary caption. A **Best price** tag only when `bestPrice == true`, and the unavailable state shows no tag. Tapping pins that variant; the Buy button label becomes "Buy via {issuerName}". Omit the sector row when `sector` is empty.
3. Paused variant rows show "Paused by issuer" and are not tappable.
4. PreStocks disclosure card: same layout as Tessera's with "The issuer can pause transfers." and a link row "About on prestocks.com" → `external_url` in Safari.
5. Reference row and premium chip unchanged; they are per variant already.

**Acceptance Criteria**
- [ ] Sample data renders both SpaceX variants with exactly one Best price tag.
- [ ] Pinning a variant changes the Buy label and passes `initialSymbol` through `GroupPickerForProposalView` → `ProposeBuyView` with `selectBestVariant = false`.
- [ ] `just build mobile` exits 0.

**Done when** every AC box is checked. **Wave:** 4

#### PS-T11: Propose, cards, holdings, activity

**Proposal**
Implement exactly:
1. `ProposeService` buy quote sends `selectBestVariant: true` unless a variant was pinned. On response, the sheet updates its `symbol` and display name from the returned quote and shows one line under the price: "Best price via {chosen}. {runnerUp} gets {delta}% less for this amount." When `basis == single` or `unavailable`, no line.
2. Create proposal sends `selectBestVariant` and displays the symbol and issuer from the proposal response, not from the earlier quote, in case the server's second pick moved.
3. Proposal cards, pot rows, activity rows, and transaction detail show "via {issuerName}" for pre-IPO rows. Sell entry uses the PS-T9 `ProposeMath` divisor so a typed token count or a dollar amount converts to raw atomics. Quantities use `resolvedMultiplier`.
4. Toast copy for a paused refusal: "This token is paused by its issuer right now."

**Acceptance Criteria**
- [ ] Host test: the propose view model switches symbol when the quote returns another variant and keeps it when `selectBestVariant` is false.
- [ ] Sample pot with both SpaceX variants shows two rows with different labels.
- [ ] `just test mobile` and `just build mobile` exit 0.

**Done when** every AC box is checked. **Wave:** 4

#### PS-T12: Sweep tool display and docs

**Proposal**
Implement exactly: `docs/product.md` Pre-IPO subsection gains the issuer paragraph, the best-price rule, and the eight PreStocks mints; `README` env table gains the two vars; `docs/index.md` links this file; `docs/ops-sweep-wallets.md` one line that the printed whole-token amount is the wallet amount. Sweep code stays in PS-T4.

**Acceptance Criteria**
- [ ] `rg -n "PreStocks" docs/product.md README.md docs/index.md docs/ops-sweep-wallets.md` shows all four edits.
- [ ] No "SPV", "multiplier", "Token-2022", or "transfer fee" in user-facing sections.

**Done when** every AC box is checked. **Wave:** 4

#### PS-T13: Bounty profile check

**Proposal**
Implement exactly: `TestCatalog_tesseraDisabled_listsEightPreStocksAndNoTessera` (no live API). A Swift host test renders the asset detail for a PreStocks row and asserts the Tessera terms URL is absent, and renders a Tessera row and asserts it is present. Manual gold-sim tap-through under `TESSERA_ENABLED=false` confirms Stocks, detail, propose, and holdings show no Tessera text. Do not gate this on `rg tessera`, which matches the Terms link source.

**Acceptance Criteria**
- [ ] The Go test and the Swift host test pass.
- [ ] Gold-sim notes for the bounty profile recorded under **Manual verification**.

**Done when** every AC box is checked. **Wave:** 5

#### PS-T14 (optional): Price sampler and charts

Same as TS-T15 in `tessera-pre-ipo.md`, sampling every pre-IPO mint from both issuers. Samples store price per scaled unit and the multiplier in effect at sample time so a later multiplier change does not bend the chart.

## Automated verification

**Commands.** `just test backend`, `just test mobile`, `just build mobile`.

| Layer | Proves |
| --- | --- |
| Go unit | `mintinfo` parsing, epoch and timestamp choices, fallbacks; PreStocks fixture, cache, fallbacks; best-price rank, tiebreak, degrade paths; multiplier math; quote-time pick; paused refusal; fee override. Fake RPC, fake Jupiter, fake sources. No network. |
| Go integration | Full propose → pass → buy → pot → sell for a fake 9-decimal, multiplier-5 asset against local Postgres; JSON contract for every touched route. |
| Swift host | DTO decoding with and without the new fields; quantity with multiplier; issuer label; copy audit; propose view model symbol switch. |
| Boundary | `ProductBoundaryScanner` host list unchanged. Product sources make no PreStocks, Tessera, Jupiter, or xStocks HTTP calls. |

Property tests: `TestProperty_potValueInvariantToMultiplier`; `TestProperty_bestPrice_deterministicUnderReorder` (candidate order does not change the pick).

## Manual verification

Mainnet, small money, one QA cabal. Gold sim per `.cursor/skills/ios-simslim-fast-qa/SKILL.md`.

1. **Discover.** Stocks tab Pre-IPO section shows eight companies once each (SpaceX, OpenAI, Kalshi, Anthropic, Anduril, Neuralink, Figure AI, Polymarket). SpaceX, OpenAI, Kalshi rows carry "via Tessera" or "via PreStocks" matching the cheaper issuer at that moment. Search `anduril` → Anduril, no comparison.
2. **Detail.** SpaceX detail lists both variants with prices per unit, fees 0.2 % and 1 %, liquidity, and one Best price tag. Tapping PreStocks changes the Buy label to "Buy via PreStocks".
3. **Propose.** From the default, propose $5 of SpaceX. The sheet prints "Best price via Tessera. PreStocks gets 4.x% less for this amount." (numbers as measured). Pin PreStocks and propose $5: no comparison line, provider PreStocks.
4. **Buy PreStocks.** Pass the PreStocks proposal. Logs: `resolve_mint` → prestocks, `slippage_bps=100`, `transfer_fee_bps=100`, Success code 0, `quoted_out`, `received_out`, `fee_bps_observed`. Record that number. Solscan: treasury holds SPACEX; the wallet-displayed amount is 5× the raw amount.
5. **Hold.** Holdings show "SpaceX via PreStocks · 0.08xx tokens" where the quantity equals the Solscan UI amount, value ≈ $5 minus fee and impact. If a Tessera SpaceX holding also exists, it is a separate row.
6. **Sell.** Propose selling half the PreStocks holding; pass; USDC back; row halves. Cash out a small member amount; no shortfall error. If the redeem log shows a shortfall retry, that is the fee not being netted: set `jupiterNetsTransferFee` false per PS-T6. That also puts the fee into the best-price ratio.
7. **Sweep.** `./scripts/sweep-wallets.sh --source <treasury> --dry-run` prints the SPACEX remainder in scaled units matching Solscan.
8. **Degrade.** Point `PRESTOCKS_API_BASE_URL` at an unreachable host: eight static rows remain with Jupiter prices; comparison still works because it needs Jupiter only. Point `SOLANA_RPC_URL` at an unreachable host: multipliers come from last-good then static; the log shows `mintinfo fallback`.
9. **Bounty profile.** `TESSERA_ENABLED=false`: PS-T13 script passes; tap-through shows no Tessera text.

Record here after the run: impact probe output (PS-T6), `fee_bps_observed` for one PreStocks buy, and the SpaceX comparison numbers on the day.

## Out of scope

PreStocks redemptions and SPV mechanics, referral or ecosystem-page work, DeFi or collateral uses of the tokens, prediction-market or social features from the bounty brief, charts before PS-T14, per-asset buy caps, the Dynamic/Base line.

## Risks

- **Issuer controls.** Permanent delegate and pause exist on every PreStocks mint. A pause strands a holding until the issuer resumes. The disclosure card says so in one sentence. `potMarksLiveOnly` still values a paused holding from Jupiter, so funding and cash-out keep working unless Jupiter stops pricing it.
- **Multiplier changes.** A new `newMultiplier` rescales displayed quantities at its effective timestamp. Value is unchanged. No user notice is planned in this lane; log it at info when `mintinfo` observes a change.
- **Fee changes.** The fee moved from 50 to 100 bps in one epoch. Reading it live covers pricing; the redeem-buffer constant needs a re-measure if the fee changes again.
- **Thin pools.** Kalshi via Tessera sits on $98k–$190k. The live-quote pick at propose time is what protects against choosing a discount that a real fill would erase.
- **Reference availability.** Comparison needs a fresh `stockData` on both mints. When it is missing the app quietly falls back to the liquidity rule; the row does not claim best price.
- **Bounty eligibility.** The product profile is ineligible for the PreStocks bounty as written. Only the bounty profile qualifies.

## Open decisions

- **Which profile to submit.** Product (both issuers, comparison, no PreStocks bounty) or bounty (PreStocks only, Tessera hidden by env). The code is the same. Needs a call before PS-T13.
- **Auto-pick versus suggest.** This plan auto-picks the best variant at propose time and prints the provider. The alternative is quoting the requested symbol and offering a "Switch to PreStocks" button. Auto-pick matches the request; the button is one ticket if preferred.
- **Fee on the propose sheet.** The exposure line does not include the fee until `jupiterNetsTransferFee` is false. Whether to also print "Fee 1%" next to the provider is a copy call. The detail variant row already shows the fee.
- **Terms link for PreStocks.** The API gives a product page per token, not a terms URL. Confirm the terms location on prestocks.com before PS-T10.

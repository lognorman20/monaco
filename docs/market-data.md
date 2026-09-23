# Market data on the asset routes

What `GET /v1/assets`, `/v1/assets/popular`, `/v1/assets/held`, `/v1/assets/{symbol}`
and `/v1/assets/{symbol}/chart` return (and the market figures on a cabal's holdings),
and where every number comes from. `docs/api.md`
is not on `main` yet; when the API reference comes back, this becomes its market
section.

Money is integer USDC micros (1 USDC = 1,000,000). Timestamps are RFC 3339 in UTC.

## Three instruments, three sources

A B20 token (for example `AAPLc`) is a Coinbase-issued ERC-20 on Base that tracks a
US equity. Chainlink's documentation for the Base tokenized-equity feeds
(<https://docs.chain.link/data-feeds/tokenized-equity-feeds/coinbase>) defines the
feed's total-return value as the equity's market price times the token's
**multiplier**, read from Coinbase's on-chain registry. A cash dividend is converted
into shares of the underlying and shows up as a multiplier increase rather than as a
cash payment; a split moves the multiplier the other way. So the token and the share
are different units. The same page says the feeds run 24/5 (pre-market, regular,
post-market and overnight) and hold the last close off-hours with no heartbeat.

| Figure | Source | Unit |
|---|---|---|
| `priceUsdcMicros` (list, popular, detail hero) | Chainlink total-return (TRV) feed on Base, `latestRoundData()` | per token, multiplier included. The same mark pot NAV uses |
| `chart` | Pyth Benchmarks history for `Equity.US.<TICKER>/USD`; Hermes per-sample history as fallback; the token's own Chainlink rounds last | per share (`basis: "underlying"`), or per token for the Chainlink fallback (`basis: "token"`) |
| `stats` | Pyth Benchmarks candles and the latest equity price's confidence interval | per share |
| `change24h` | The latest 1D price against the previous regular-session close, on one series: Pyth Benchmarks when it can answer (`"underlying"` / `"AAPL"`), the token's own Chainlink rounds when it cannot (`"token"` / `"AAPLc"`). Both ends of the ratio always come from the same series | whichever `change24hBasis` names — the stock's day move, or the token's |
| `stockVsToken.token` | Kyber: the mid of a 1 USDC buy probe and a 1-token sell probe | per token |
| `stockVsToken.mark` | the Chainlink TRV mark (equals the hero price), with the round's `updatedAt` as `publishedAt` | per token |
| `stockVsToken.equity` | Pyth Hermes latest price for the underlying | per share |

Pyth publishes no feed for B20 tokens. **Pyth is for charts and display; never NAV.**

## Conventions

- Anything a source could not supply is **omitted, never substituted.** A stats cell
  with no source is absent; a grid with no cell is absent; a card line that could not
  be priced is `status: "unavailable"` with a `reason` (`no_feed`, `not_entitled`,
  `not_configured`, `no_route`, `upstream_error`) and no price.
- A Pyth price whose publish time froze or is more than five minutes old is
  `status: "stale"` with the `publishedAt` it froze at.
- The Chainlink mark is `status: "stale"` when its round is past the feed's 25-hour
  heartbeat, when it has no round time, or when the exchange calendar has no
  session running and the round is no newer than the last session's post-market
  close (weekends and holidays, when the feed holds the last close).
- A Kyber quote has no publish time and no confidence interval, and none is sent.
  The token line carries `probedAt`, when we took the probes; they are shared for up
  to 60 s, so it can be up to a minute older than `asOf`.

## `market`

On every asset response: `session` (`pre_market | open | after_hours | closed`),
`isOpen`, `afterHours`, `nextSession`, `nextTransition`, `asOf`, and `holiday` /
`earlyClose` when they apply. It is computed from the NYSE/Nasdaq calendar
(`internal/marketcal`), so it is one fact about the exchange and lives on the envelope.
The detail repeats it as `marketSession` and `afterHours`.

## Market rows

`GET /v1/assets`, `/v1/assets/popular`, the `asset` of every `/v1/assets/held` row
and the holdings on `GET /v1/groups/{id}/view` carry the same market figures, read by
one type (`httpapi.MarketRowSource`), so the app draws a stock the same way wherever
it lists one.

| Field | Meaning |
|---|---|
| `priceUsdcMicros` | The Chainlink TRV mark, per token. Not on holdings rows, which already carry the cabal's own `markUsd` (the same mark). |
| `change24h`, `change24hBasis`, `change24hBasisSymbol` | The move against the previous regular-session close of whichever series answered: Pyth Benchmarks' underlying (`"underlying"` / `"AAPL"`), or the token's own Chainlink rounds (`"token"` / `"AAPLc"`). The basis is not decoration — a row ships no change without it. |
| `spark` | About two dozen closes of the same Pyth 1D series, oldest first, for the row's sparkline. Omitted when no series could be read in budget or it had fewer than two usable closes: the row then draws no line rather than a flat one. |
| `sparkBasis`, `sparkBasisSymbol` | Which instrument `spark` is about. Set exactly when `spark` is. |
| `logoUrl` | The company icon the issuer publishes in the token's own on-chain metadata: B20 tokens implement ERC-7572 `contractURI()`, which returns an inline `data:application/json` document whose `image` is on `metadata.coinbase.com`. Read with one `eth_call` per token, cached 24 h (a failed read backs off 5 min). Only an `https` URL on that host is sent, because the app loads whatever it is given. Omitted when unreadable; the app then draws a ticker tile. |

`spark` and `change24h` come from one read of one series, so their bases always agree
and the app tints the line by the day move. Which basis that is depends on who
answered: Pyth Benchmarks' history endpoint 404s and our key is crypto-only, so today
it is `token` for every equity. The labels still ship, because the rule the app follows
is "tint by the change only when both figures are the same instrument; otherwise by the
line's own first and last close", and the same agreement decides whether the pill can
show dollars — a share's move must never be priced at the token's price. The tab's
footnote is read off the rows for the same reason.

A row with no series is a row with a price and no line. Past 40 symbols in one
response, a row ships with its symbol only.

## `GET /v1/assets/held`

The Stocks tab's two social sections in one scan of the caller's cabals. Read-only.

```json
{
  "held": [{
    "asset": { "symbol": "AAPLc", "name": "Apple", "priceUsdcMicros": 232050000, "change24h": "0.015000", "spark": [...], ... },
    "cabals": [{ "groupId": "…", "name": "Weekend investors", "units": "0.5", "valueUsd": "1.20", "dollarPnl": "+0.20", "mySliceUsd": "0.60" }],
    "totalValueUsd": "1.20",
    "totalDollarPnl": "+0.20",
    "mySliceUsd": "0.60"
  }],
  "upForVote": [{ "asset": {...}, "openProposals": 2, "cabalNames": ["Semis or bust"], "soonestExpiresAt": "2026-09-22T18:00:00Z" }],
  "market": { ... }
}
```

- `held` is biggest position first; `upForVote` is closing soonest first. Both are
  always arrays.
- `mySliceUsd` is the caller's share units over `SumShareUnitsByGroup` (every unit on
  the cabal's positions) times the position's value: the same share base Home divides
  a whole pot by, so a member's slices add up to what Home says they own.
- Cash is not a holding and is left out.
- Each cabal is valued under its own 3 s budget, four at a time, and at most 25 of the
  caller's cabals are scanned. A cabal that errors or is slow is left out of `held`
  for that response; its open votes are still read.
- A symbol the catalog does not know still ships, as `{symbol, name}` only.

## `GET /v1/assets/{symbol}/chart?range=`

`range` is `1D`, `1W`, `1M`, `3M`, `1Y` or `ALL` (default `1D`). The response:

- `points`: `[{timestamp, priceUsdcMicros, openUsdcMicros?, highUsdcMicros?, lowUsdcMicros?}]`,
  always an array (`[]` when empty, with `emptyReason`). OHLC is present only for
  candle sources.
- `range` echoes the request; `source` is `benchmarks`, `hermes` or `chainlink`;
  `basis` / `basisSymbol` name the instrument (`"underlying"`/`"AAPL"`, or
  `"token"`/`"AAPLc"`).
- `previousCloseUsdcMicros`: the previous regular session's close, the day chart's
  baseline. Omitted when the source does not know one (the Hermes sampler and the
  Chainlink rounds never send it).
- 1D is the last trading session from the exchange calendar, pre-market to
  post-market, not whatever bars arrived after the bell.
- The Chainlink fallback only covers days of rounds, so it serves 1D/1W/1M and is
  empty for 3M/1Y/ALL.

## `GET /v1/assets/{symbol}`

Adds to the existing detail:

- `stats`: `openUsdcMicros`, `highUsdcMicros`, `lowUsdcMicros` (the regular cash
  session, 09:30–16:00 ET or 13:00 on a half day), `previousCloseUsdcMicros`,
  `week52HighUsdcMicros`, `week52LowUsdcMicros`, `confUsdcMicros` (Pyth's confidence
  interval, **only while the equity quote is live** — after the bell the feed
  republishes a frozen print and the grid has no freshness field to say so), and
  `basis`/`basisSymbol`. A hero price above `stats.highUsdcMicros` is two
  instruments, not an error. The candle cells come only from Benchmarks: when
  Benchmarks is down they are omitted, never folded from the sparse Hermes sampler.
  A close-only Benchmarks payload has no open: the other three legs stay unset
  rather than borrowing the close, so such a series draws as a line, not as candles
  whose four prices happen to be equal. The Kyber spread is the token's, so it is
  not in this grid.
- `change24h`, `change24hBasis`, `change24hBasisSymbol` (also on list and popular
  rows).
- `stockVsToken`, present only when both Kyber probes routed: `token` (source
  `dex_kyber`, with `bidUsdcMicros` / `askUsdcMicros` / `probedAt`), `mark` (source
  `chainlink_trv`), `equity` (source `pyth_equity`), `equitySymbol`, `premiumBps`
  (token against mark; absent unless both are priced **and live**, so a stale mark
  has no premium), `spreadBps` and `asOf`. No premium is computed against the equity
  line: it is per share and the mark is per token.
- `liquidity.spreadBps`: `(ask - bid) / mid` of the two Kyber probes, in basis points.
  A 1 USDC probe includes pool fees and price impact, so thin pools read wide.

## Configuration

- `PYTH_API_KEY` gates Hermes only (the equity reference line and the per-sample
  chart fallback). Without it those are `not_configured` / skipped.
- `PYTH_BENCHMARKS_BASE_URL` (optional) overrides `https://benchmarks.pyth.network`.
  Benchmarks is keyless, so charts, stats and the day change work without a key.

## Caching and budgets

- Chart series: 1 min for 1D, 10 min for longer ranges, empty answers at most 2 min,
  keyed on the equity feed. An outage is not cached; a Benchmarks outage (a
  transport error, a non-200, an upstream timeout) opens a 60 s breaker. The
  caller's own deadline or a cancelled request does not, and a per-symbol
  `"s":"error"` is cached as that symbol's empty answer. A Hermes sampler run cut
  short by the deadline is discarded, and one with failed samples is served but
  not cached.
- The previous close is the close of the last bar stamped strictly before the
  previous session's bell (16:00 ET, or 13:00 on a half day) **and no earlier than
  that session's pre-market open**: bars are stamped with their open time, so the
  bar at the bell is post-market. The 1D fetch reaches seven days back so a long
  holiday weekend cannot hide the closing bar, which is exactly why the lower bound
  is needed — without it a hole in the history hands back a close from days ago.
  When no bar falls inside the previous session there is no previous close, and
  `previousCloseUsdcMicros` and `change24h` are both omitted.
- Equity quote: 10 s. Kyber probes: 60 s per token, and a probe that errored is not
  cached.
- The detail's reads run concurrently under a 4 s bound; the list's day series (one
  read per row gives both `change24h` and `spark`) run four at a time inside the
  list's 4 s budget. Market figures on someone else's screen (holdings rows, held and
  up-for-vote rows) get 2 s.
- A spark warmer refreshes the popular symbols' 1D series every 45 s, inside the
  1-minute lifetime, so the popular rows are served from memory. It refreshes rather
  than reads through the cache, and an outage leaves the cached series in place.

## Known follow-ups

- **Day-change fan-out on a cold cache.** The list asks `Charts.DaySeries` once per
  catalog row, so a 25-row page on a cold 1-minute cache is 25 separate Benchmarks
  requests. It is bounded (four at a time, inside the list's 4 s budget) and cached,
  and the spark warmer keeps the popular rows warm, so it is not a correctness
  problem. But Benchmarks has no batch endpoint and the fan-out grows with the
  catalog. Coalescing identical in-flight requests (singleflight) around the chart
  cache is the fix; it was left out of the stocks data change to keep the cache's
  failure semantics — outages uncached, per-symbol empties cached — in one place.
- **Solana-era fixtures in the older backend tests.** `AAPLx`/`MSFTx` symbols and
  base58 mints in EVM address fields still appear across `internal/app` and other
  packages inherited from the migration. `internal/httpapi/assets_test.go` was
  brought in line with `assets_market_test.go`; the rest is base-branch debt.

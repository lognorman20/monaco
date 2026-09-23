# Market data on the asset routes

What `GET /v1/assets`, `/v1/assets/popular`, `/v1/assets/{symbol}`,
`/v1/assets/{symbol}/chart` and `/v1/assets/{symbol}/social` return, and where every
number comes from. `docs/api.md` is not on `main` yet; when the API reference comes
back, this becomes its market section.

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
| `change24h` | Pyth Benchmarks: the latest 1D price against the previous regular-session close. Ships with `change24hBasis: "underlying"` and `change24hBasisSymbol: "AAPL"` | the stock's day move, not the token's |
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
  interval), and `basis`/`basisSymbol`. A hero price above `stats.highUsdcMicros` is
  two instruments, not an error. The candle cells come only from Benchmarks: when
  Benchmarks is down they are omitted, never folded from the sparse Hermes sampler.
  The Kyber spread is the token's, so it is not in this grid.
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

## `GET /v1/assets/{symbol}/social` ●

The one asset route that is not market data: what the **caller's own** cabals are
doing with this stock. Authenticated; the caller's membership list is the only
authorization the queries perform, so a non-member never appears in another cabal's
answer.

- `holdings`: one row per cabal that holds the symbol, biggest position first —
  `units`, `tokenAmount`, `markUsd`, `valueUsd`, `costBasisUsd`, `dollarPnl`,
  `percentReturn` (null with no cost basis), `mySliceUsd`, `mySlicePercent`,
  `afterHours`. The mark is the token's Chainlink TRV mark, one read per token for
  the whole request; a cabal whose token has no usable mark carries the position at
  what it paid.
- `mySliceUsd` divides by `SumShareUnitsByGroup`, the same share base Home and the
  group view use, so one member's slice cannot read differently on two screens.
- `openProposals`: open votes on this symbol in those cabals, with the tally,
  `memberCount`, the caller's own `myVote` (absent when they have not voted) and
  `voters`.
- `activity`: proposals and **confirmed** fills, newest first, capped at 20. A
  sell's dollar figure comes from its recorded proceeds; a sell with none sends
  `usdcMicros: 0` and the row shows its share count rather than reading token
  atomics as dollars.
- `unvaluedGroups` counts the caller's cabals that could not be priced on this pass.
  It is never folded into silence: a short list with no caveat would tell a member
  their other cabals hold nothing.

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
  previous session's bell (16:00 ET, or 13:00 on a half day): bars are stamped
  with their open time, so the bar at the bell is post-market.
- Equity quote: 10 s. Kyber probes: 60 s per token, and a probe that errored is not
  cached.
- The detail's reads run concurrently under a 4 s bound; the list's day changes run
  four at a time inside the list's 4 s budget.

# Market data on the asset routes

What `GET /v1/assets`, `/v1/assets/popular`, `/v1/assets/{symbol}` and
`/v1/assets/{symbol}/chart` return, and where every number comes from. `docs/api.md`
is not on `main` yet; when the API reference comes back, this becomes its market
section.

Money is integer USDC micros (1 USDC = 1,000,000). Timestamps are RFC 3339 in UTC.

## Three instruments, three sources

A B20 token (for example `AAPLc`) is a Coinbase-issued ERC-20 on Base that tracks a
US equity. Coinbase's B20 docs define its price as the equity's price times the
token's **multiplier**; cash dividends are converted into more of the underlying and
show up as a multiplier increase rather than as a cash payment, and splits move the
multiplier the other way. So the token and the share are different units.

| Figure | Source | Unit |
|---|---|---|
| `priceUsdcMicros` (list, popular, detail hero) | Chainlink total-return (TRV) feed on Base, `latestRoundData()` | per token, multiplier included. The same mark pot NAV uses |
| `chart` | Pyth Benchmarks history for `Equity.US.<TICKER>/USD`; Hermes per-sample history as fallback; the token's own Chainlink rounds last | per share (`basis: "underlying"`), or per token for the Chainlink fallback (`basis: "token"`) |
| `stats` | Pyth (candles and the latest equity price's confidence interval) and the Kyber spread | per share |
| `change24h` | Pyth: the latest 1D price against the previous regular-session close | the stock's day move, not the token's |
| `stockVsToken.token` | Kyber: the mid of a 1 USDC buy probe and a 1-token sell probe | per token |
| `stockVsToken.mark` | the Chainlink TRV mark (equals the hero price) | per token |
| `stockVsToken.equity` | Pyth Hermes latest price for the underlying | per share |

Pyth publishes no feed for B20 tokens. **Pyth is for charts and display; never NAV.**

## Conventions

- Anything a source could not supply is **omitted, never substituted.** A stats cell
  with no source is absent; a grid with no cell is absent; a card line that could not
  be priced is `status: "unavailable"` with a `reason` (`no_feed`, `not_entitled`,
  `not_configured`, `no_route`, `upstream_error`) and no price.
- A Pyth price whose publish time froze or is more than five minutes old is
  `status: "stale"` with the `publishedAt` it froze at.
- A Kyber quote has no publish time and no confidence interval, and none is sent.

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
  interval), `spreadBps`, and `basis`/`basisSymbol`. A hero price above
  `stats.highUsdcMicros` is two instruments, not an error.
- `stockVsToken`: `token` (source `dex_kyber`, with `bidUsdcMicros` / `askUsdcMicros`),
  `mark` (source `chainlink_trv`), `equity` (source `pyth_equity`), `equitySymbol`,
  `premiumBps` (token against mark; absent unless both are priced), `spreadBps` and
  `asOf`. No premium is computed against the equity line: it is per share and the mark
  is per token.
- `liquidity.spreadBps`: `(ask - bid) / mid` of the two Kyber probes, in basis points.
  A 1 USDC probe includes pool fees and price impact, so thin pools read wide.

## Configuration

- `PYTH_API_KEY` gates Hermes only (the equity reference line and the per-sample
  chart fallback). Without it those are `not_configured` / skipped.
- `PYTH_BENCHMARKS_BASE_URL` (optional) overrides `https://benchmarks.pyth.network`.
  Benchmarks is keyless, so charts, stats and the day change work without a key.

## Caching and budgets

- Chart series: 1 min for 1D, 10 min for longer ranges, empty answers at most 2 min.
  An outage is not cached; a failed Benchmarks call opens a 60 s breaker.
- Equity quote: 10 s. Kyber probes: 60 s per token, and a probe that errored is not
  cached.
- The detail's reads run concurrently under a 4 s bound; the list's day changes run
  four at a time inside the list's 4 s budget.

# HTTP API

Routes are registered in `apps/backend/cmd/api/main.go`; handlers and their request and
response structs are in `apps/backend/internal/httpapi`. Read the handler for field-level
detail. A test (`cmd/api/routes_doc_test.go`) fails if a route is missing from the table below.

## Conventions

**Auth.** `Authorization: Bearer <Privy access token>` on every `/v1` route. Agents instead send
`X-Monaco-Agent-Key` on the `/v1/agent` routes (the key names the cabal), `POST /v1/groups/{id}/agents/intents`
and `GET /v1/groups/{id}/assets`. `GET /v1/agent/skill.md` is public.
A valid token with no Monaco user yet gets `404 user not found`: call `POST /v1/auth/session` first.

**Errors.** `{ "error": "message", "requestId": "…" }`. Send your own `X-Request-Id` (1–64 of
`A-Z a-z 0-9 - _`) or the API mints one; it is echoed on the response and stamped on log lines.
A blocked `POST /v1/groups/{id}/leave` is `409` with the same shape plus a machine-readable `reason`
(for example `share_units_remaining`, `creator_must_transfer`).

**Money.** USDC is integer micros (1 USDC = 1,000,000). Timestamps are UTC RFC 3339. `priceUsdcMicros` is the price of one whole token, whatever that token's decimals are.

**Catalog kind.** Asset rows carry `kind` (`stock` or `pre_ipo`), `source`, `issuer`, `underlyingId`, `tokenDecimals`, `sector`, `logoUrl`, `alwaysOpen`, reference fields (`referenceMarkUsdcMicros`, `referenceValuationUsd`, `referenceUpdatedAt`), `premiumBps`, `holders`, and `variantCount`. Filter with `?kind=stock` or `?kind=pre_ipo`; any other value is `400`. Quote JSON keeps `kind` as `buy` or `sell`. Buy quotes add `assetKind` and `tokenDecimals`. Detail adds `variants[]` when several issuers share an underlying. Pre-IPO chart responses are empty until a sampler has history.

**Market data.** The asset routes carry a `market` object — `session`
(`pre_market | open | after_hours | closed`), `isOpen`, `afterHours`, `nextSession`,
`nextTransition`, and `holiday` / `earlyClose` when they apply. It is computed from the
NYSE/Nasdaq calendar, so it is one fact about the exchange and lives on the envelope
rather than on each asset. Anything a vendor could not supply is **omitted, never
substituted**: a stats cell with no source is absent, and a price feed that is missing,
unentitled or down comes back as `status: "unavailable"` with a `reason`, not as a
number borrowed from somewhere else. A feed that has stopped publishing is
`status: "stale"` with the `publishedAt` it froze at.

**Two prices per symbol.** An xStock has an underlying equity and a token, and they
do not agree — the premium is what `stockVsToken` is for. `priceUsdcMicros` on the
asset routes is the **token's** on-chain price. Everything folded from price
history — the `stats` grid and the `chart` series — comes from Pyth's history for
the **underlying equity**, and says so in `basis` (`underlying | token`) and
`basisSymbol` (`"AAPL"`). A `priceUsdcMicros` above `stats.highUsdcMicros` is
therefore two instruments, not an error; render the grid and the chart under their
`basisSymbol`.

**`previousCloseUsdcMicros`** is the close of the regular session before the window,
for the dashed day-change baseline. It is **omitted when the source does not know
one**: the sampled fallback (`source: "hermes"`) starts its grid inside the window,
so any value it could offer is a point already in `points`. Draw no baseline rather
than one lying on the curve's first point.

**`stats` open/high/low** are the **regular cash session's** (09:30–16:00 ET, 13:00
on a half day), not the extended session's. The 1D chart still spans pre- and
post-market — the curve and the grid deliberately cover different windows.

**Rate limits.** Per process, non-GET only. Over budget is `429` with `Retry-After`.
"Per user" is the verified Privy user, so refreshing a token does not reset it (agent
callers: per agent key). A bearer token that fails verification is limited per IP only.

| Class | Routes | Per user | Per IP |
| --- | --- | --- | --- |
| `auth` | `POST /v1/auth/session` | burst 10, +1 per 6s | burst 20, +1 per 3s |
| `money` | fund, withdraw-to-balance, withdrawals, retry, agent intents | burst 5, +1 per 10s | burst 20, +1 per 3s |
| `write` | every other non-GET route | burst 20, +1 per 2s | burst 60, +1 per 1s |

Chat, comments, display name and profile photo have their own tighter per-user limits.
Set `TRUST_PROXY_HEADERS=true` only behind a proxy that overwrites `X-Forwarded-For`.

**Idempotency.** Routes marked ● accept an `Idempotency-Key` header (8–128 of
`A-Z a-z 0-9 . _ : -`), scoped per user, kept 24 hours.

- Same key, same request: the stored response is replayed with `Idempotency-Status: replayed`.
- Same key while the first is still running: `409` with `Idempotency-Status: in_progress`.
- Same key, different route or body: `422`.
- A `5xx` releases the key, so the retry runs again.

Agent intents do not read the header. They take an `idempotencyKey` field in the body with
the same replay rules, scoped per agent ([agent trading](agent-trading.md)). Without it, a
resent intent is a new trade.

**Limits.** JSON bodies are capped at 64 KiB (chat and comments 16 KiB, `PATCH /v1/me` 4 KiB,
profile photo 2 MB). Over the cap is `413` on every route; a body that fits but cannot be parsed
is `400`, as is a message or comment over its character limit. The server write timeout is
3 minutes because cash out, withdraw, retry and agent intents confirm a Solana transaction
inside the request; give clients the same patience. Browser origins are refused unless listed in
`CORS_ALLOWED_ORIGINS`.

## Routes

| Route | What it does |
| --- | --- |
| `GET /health` | Dependency status. No auth. See [observability](ops-observability.md). |
| `GET /metrics` | Prometheus scrape. Bearer `METRICS_TOKEN`, or loopback only. |
| `POST /v1/auth/session` | Verify the Privy token, upsert the user, make sure a member wallet exists. |
| `GET /v1/me` | Signed-in profile. |
| `PATCH /v1/me` | Set the display name. |
| `POST /v1/me/profile-photo` | Upload a profile photo (`multipart/form-data`). |
| `GET /v1/me/balance` | Account balance: member-wallet USDC minus in-flight funds and withdrawals. |
| `POST /v1/me/withdrawals` ● | Send USDC from the account balance to an external Solana address. |
| `GET /v1/me/withdrawals/{id}` | One withdrawal. |
| `GET /v1/home` | Group and people boards. |
| `GET /v1/home/dashboard` | Net worth, the caller's cabals, a 1H series, leaderboard and missed proposals in one call. |
| `GET /v1/home/pnl-series` | The caller's equity series. `range` = `1H`, `1D`, `1W`, `1M`. At most one point per second, so `ts` is unique at second precision. |
| `GET /v1/home/missed-proposals` | Proposals the caller missed. |
| `GET /v1/users/{id}/groups` | Cabals the caller shares with another user. |
| `POST /v1/groups` | Create a cabal and its treasury. |
| `GET /v1/groups/search` | Search cabals by name. |
| `GET /v1/groups/leaderboard` | Ranked cabals. |
| `GET /v1/groups/pnl-history` | One P&L series per cabal the caller belongs to. |
| `GET /v1/groups/{id}` | Name and treasury address. |
| `GET /v1/groups/{id}/view` | The cabal screen in one call: pot, holdings, members, agent. |
| `GET /v1/groups/{id}/pnl-history` | P&L series for one cabal. |
| `GET /v1/groups/{id}/activity` | Trade and money activity, agent trades marked. |
| `POST /v1/groups/{id}/picture` | Set or replace the cabal picture (`multipart/form-data`, field `picture`). Creator only. |
| `DELETE /v1/groups/{id}/picture` | Remove the cabal picture, falling back to its initials. Creator only. |
| `POST /v1/groups/{id}/join` | Join, or ask to join when the cabal is by request. |
| `POST /v1/groups/{id}/leave` ● | Leave a cabal. |
| `GET /v1/groups/{id}/join-requests` | Pending join requests. |
| `POST /v1/groups/{id}/join-requests/{requestId}/approve` | Approve a join request. |
| `POST /v1/groups/{id}/join-requests/{requestId}/deny` | Deny a join request. |
| `POST /v1/groups/{id}/fund` ● | Move USDC from the account balance into the cabal treasury. |
| `POST /v1/groups/{id}/deposits` | Deprecated; answers `410`. Use `fund`. |
| `GET /v1/deposits/{id}` | One deposit and its sweep status. |
| `GET /v1/groups/{id}/share-units` | The caller's share units in a cabal. |
| `GET /v1/groups/{id}/treasury/usdc` | Treasury USDC balance. |
| `GET /v1/groups/{id}/treasury/tokens` | Treasury USDC plus token holdings. |
| `GET /v1/groups/{id}/cost-basis/{symbol}` | Fill-derived cost basis for one symbol. |
| `POST /v1/groups/{id}/withdraw-to-balance` ● | Cash out a slice of the cabal to the account balance. |
| `GET /v1/groups/{id}/assets` | Tradable catalog for a cabal. Bearer or agent key. Optional `kind=stock\|pre_ipo`. Rows include `kind`, `source`, `issuer`, `tokenDecimals`, `uiAmountMultiplier`, reference fields, `premiumBps`, and `variantCount`. |
| `GET /v1/assets` | Catalog search with prices and the market session. Optional `kind=stock\|pre_ipo`. |
| `GET /v1/assets/popular` | Popular assets with prices and the market session. |
| `GET /v1/assets/held` | What the caller's cabals own and have open votes on, in one scan. Read-only. |
| `GET /v1/assets/{symbol}` | Asset detail: price, liquidity, market session, the stats grid, the underlying equity against the token (`stockVsToken`), and `variants[]` when two issuers share a company. |
| `GET /v1/assets/{symbol}/chart` | Price history. `range` is `1D`, `1W`, `1M`, `3M`, `1Y` or `ALL` (default `1D`); the response echoes the range, names its `source`, and carries the `previousCloseUsdcMicros` baseline. Pre-IPO charts are empty. |
| `GET /v1/assets/{symbol}/social` | What the caller's own cabals are doing with one stock: `holdings` (units, value, cost basis, P&L and the caller's slice), `openProposals` (tally, the caller's ballot and who voted) and `activity` (proposals and fills). Scoped to the caller's memberships, so a non-member never appears in another cabal's answer. `unvaluedGroups` counts cabals that could not be priced on this pass. |
| `POST /v1/groups/{id}/quotes` | Check that a buy or sell can route, and at what price. `kind` stays `buy` or `sell`. A buy may send `selectBestVariant: true`; the response symbol is the issuer that was chosen. |
| `GET /v1/groups/{id}/proposals` | List proposals. |
| `POST /v1/groups/{id}/proposals` ● | Open a proposal: buy, sell, or add, pause, resume, revoke an agent. |
| `GET /v1/proposals/{id}` | Proposal detail, votes and execution state. |
| `POST /v1/proposals/{id}/votes` | Cast a vote. |
| `GET /v1/proposals/{id}/comments` | Comment thread, oldest first. |
| `POST /v1/proposals/{id}/comments` | Add a comment or reply. |
| `GET /v1/groups/{id}/messages` | A page of cabal chat. |
| `POST /v1/groups/{id}/messages` | Post a chat message. |
| `GET /v1/transactions/{id}` | One swap. `404 transaction not found` for an unknown id and for a club you cannot read alike. |
| `POST /v1/transactions/{id}/retry` ● | Retry a failed swap. Members only; a non-member gets the same `404` as an unknown id. |
| `POST /v1/groups/{id}/agents/intents` | An agent submits a trade. Agent key only. See [agent trading](agent-trading.md). |
| `GET /v1/agent` | The agent's cabal, budget, cash and holdings. Agent key only. |
| `GET /v1/agent/assets` | Tradable stocks with marks. Agent key only. |
| `POST /v1/agent/intents` | An agent submits a trade; the key names the cabal. Agent key only. |
| `GET /v1/agent/intents/{intentId}` | One of the agent's intents with its fill. Agent key only. |
| `GET /v1/agent/skill.md` | Public markdown instructions for agents. |
| `POST /v1/dev/faker` | Seed demo data. Only with `FAKER_ENABLED`, from loopback, on a local database. |

## Market rows

`GET /v1/assets`, `/v1/assets/popular`, `/v1/assets/held` and the holdings on
`GET /v1/groups/{id}` all carry the same row shape, so the app renders a stock the
same way wherever it lists one.

| Field | Meaning |
| --- | --- |
| `priceUsdcMicros` | Current mark for the xStock's mint, from Jupiter. |
| `change24h` | The **token's** 24h move on Solana, as a ratio string. |
| `spark` | About two dozen closes for the row's sparkline. Omitted when no series was cached; the row then draws no line rather than a flat one. |
| `sparkBasis` / `sparkBasisSymbol` | Which instrument `spark` is about — `underlying` (`AAPL` on NASDAQ) or `token`. |
| `changeBasis` / `changeBasisSymbol` | Which instrument `change24h` is about. |
| `logoUrl` | The catalogue's logo for the xStock. Absent when it publishes none; the app falls back to a ticker tile. |

`sparkBasis` and `changeBasis` are not decoration. Pyth serves price history for the
underlying equity while Jupiter prices the token, and the two genuinely diverge — that
divergence is what the stock-vs-token card on the detail screen exists to show. A row
that drew one and tinted it by the other would be asserting they are the same
instrument, so when these two disagree the app tints the drawn line from the drawn
series.

`spark` is always served from cache. A list route never fetches price history: a miss
is a row without a sparkline now and a background warm for the next request, so no
page of rows can ever wait on a vendor.

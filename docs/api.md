# HTTP API

Routes are registered in `apps/backend/cmd/api/main.go`; handlers and their request and
response structs are in `apps/backend/internal/httpapi`. Read the handler for field-level
detail. A test (`cmd/api/routes_doc_test.go`) fails if a route is missing from the table below.

## Conventions

**Auth.** `Authorization: Bearer <Privy access token>` on every `/v1` route. Agents instead send
`X-Monaco-Agent-Key` on the `/v1/agent` routes (the key names the cabal), `POST /v1/groups/{id}/agents/intents`
and `GET /v1/groups/{id}/assets`. `GET /v1/agent/skill.md` and `GET /v1/invites/{code}` are public.
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
| `public` | `GET /v1/invites/{code}` (no session, so any method) | burst 20, +1 per 2s | burst 60, +1 per 1s |

Chat, comments, display name and profile photo have their own tighter per-user limits, and a
proposal can be nudged once an hour whoever sends it.
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
| `GET /v1/me/portfolio` | The caller's slice of every cabal, merged by stock. See [Portfolio and history](#portfolio-and-history). |
| `GET /v1/me/transactions` | The caller's money history, newest first. `type` = `all`, `money_in`, `cash_out`, `buy`, `sell`; `cursor`; `limit` (default 30, max 100). |
| `GET /v1/me/transactions/export.csv` | The same history as a CSV download. Takes `type`. |
| `GET /v1/me/notifications` | The inbox, newest first, with `unreadCount`. `cursor` from the last page's `nextCursor`; `limit` 1–100 (default 30). See [Notifications](#notifications). |
| `POST /v1/me/notifications/read` | Mark read: `{ "ids": [...] }` (up to 200 of the caller's own) or `{ "all": true }`. Answers `{ "unreadCount": n }`. |
| `PUT /v1/me/devices` | Register this install for Apple push: `{ "token", "platform": "ios", "appEnv": "debug\|production" }`. Upsert; a token signed in as someone else moves to the caller. `204`. |
| `DELETE /v1/me/devices/{token}` | Forget the caller's install. Unknown or someone else's token is a no-op. `204`. |
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
| `POST /v1/groups/join-by-code` | `{ "code" }`. The same join through an invite code; see [Invites](#invites). |
| `GET /v1/groups/{id}/invites` | The cabal's live invite `{ "code", "url" }`, made on the spot if it has none. Members only. |
| `POST /v1/groups/{id}/invites` | `201` with a new `{ "code", "url" }`. The old code stops working. Members only. |
| `POST /v1/groups/{id}/invites/revoke` | `204`. The cabal has no live code until a member asks again. Members only. |
| `GET /v1/invites/{code}` | Public preview of the cabal behind a live code. No auth, limited per IP. |
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
| `GET /v1/groups/{id}/assets` | Tradable catalog for a cabal. Bearer or agent key. Optional `kind=stock\|pre_ipo`. Rows include `kind`, `source`, `issuer`, `tokenDecimals`, `uiAmountMultiplier`, reference fields, `premiumBps`, `variantCount`. |
| `GET /v1/assets` | Catalog search with prices and the market session. Optional `kind=stock\|pre_ipo`. Same catalog fields as group assets. |
| `GET /v1/assets/popular` | Popular assets with prices and the market session. |
| `GET /v1/assets/held` | What the caller's cabals own and have open votes on, in one scan. Read-only. |
| `GET /v1/assets/{symbol}` | Asset detail: price, liquidity, market session, the stats grid, the underlying equity against the token (`stockVsToken`), and `variants[]` when several issuers share an `underlyingId` (each names the issuer, fee, and whether it is the best price). |
| `GET /v1/assets/{symbol}/chart` | Price history. `range` is `1D`, `1W`, `1M`, `3M`, `1Y` or `ALL` (default `1D`); the response echoes the range, names its `source`, and carries the `previousCloseUsdcMicros` baseline. |
| `GET /v1/assets/{symbol}/social` | What the caller's own cabals are doing with one stock: `holdings` (units, value, cost basis, P&L and the caller's slice), `openProposals` (tally, the caller's ballot and who voted) and `activity` (proposals and fills). Scoped to the caller's memberships, so a non-member never appears in another cabal's answer. `unvaluedGroups` counts cabals that could not be priced on this pass. |
| `GET /v1/assets/{symbol}/news` | Headlines about one stock: `items[]` (`title`, `url`, `source`, `publishedAt`) newest first, at most 12, plus `asOf`. See [News](#news). |
| `GET /v1/news/market` | The day's market headlines for the Stocks tab, same shape. |
| `POST /v1/groups/{id}/quotes` | Check that a buy or sell can route, and at what price. `kind` stays `buy` or `sell`. A buy may send `selectBestVariant: true`; the response symbol is the issuer that was chosen. Buy responses add `tokenDecimals`, `assetKind` (`stock` or `pre_ipo`), `issuer`, and live `premiumBps` when a fresh Jupiter reference exists. |
| `GET /v1/groups/{id}/proposals` | List proposals. |
| `POST /v1/groups/{id}/proposals` ● | Open a proposal: buy, sell, or add, pause, resume, revoke an agent. |
| `GET /v1/proposals/{id}` | Proposal detail, votes and execution state. |
| `POST /v1/proposals/{id}/votes` | Cast a vote. |
| `POST /v1/proposals/{id}/nudge` | Remind the voters who have not voted. The proposer or a member who voted; once per proposal per hour (`429` + `Retry-After` inside the hour, `403` for a member who has not voted, `409` once the vote closed). Answers `{ "reminded": n, "waitingOn": m }`. |
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

## News

`GET /v1/assets/{symbol}/news` and `GET /v1/news/market` answer
`{ "items": [{ "title", "url", "source", "publishedAt" }], "asOf" }`. Both read keyless
public RSS on the server; the app never calls a feed.

| Subject | Feed |
| --- | --- |
| Listed stock | Yahoo Finance's headline feed for the underlying ticker (`GOOGLx` reads `GOOGL`, the chart's mapping), then a Google News search for `"<name>" stock` if Yahoo fails or is empty. |
| Pre-IPO token | Google News search for the company's name (`"SpaceX"`). Both issuers of one company share a list. |
| Market | Google News search for the day's `"stock market" OR "Wall Street"` coverage, then Yahoo's S&P 500 and Nasdaq feed. |

- `items` is always a list. `publishedAt` is RFC 3339 UTC, or `null` when the feed gave no usable date; those items sort last.
- `url` is an absolute `http(s)` link with tracking parameters removed. A Google News item links through `news.google.com`, which redirects to the publisher.
- `source` is the item's own publisher, else the article's host when it is off the feed's site, else the feed's name.
- Cached per company for 10 minutes (2 when a feed has nothing). When every feed fails, the last list is served with the `asOf` it was read at; with nothing cached the route answers `503`. A failure is not retried for a minute.
- An unknown symbol is `404`, a symbol that is not ticker-shaped is `400`.
## Invites

A cabal has at most one live invite code: eight characters from `23456789ABCDEFGHJKLMNPQRSTUVWXYZ`
(no `0`, `O`, `1` or `I`), shared as `https://trymonaco.xyz/join/<code>`. Codes are case-insensitive
and may carry spaces or dashes (`k7qm-4xpd`). A revoked code is never reissued.

`GET /v1/invites/{code}` answers without a session, for the landing page and a signed-out phone:

```json
{ "code": "K7QM4XPD", "groupId": "…", "name": "Sunday Investors", "memberCount": 9,
  "tint": "pine", "pictureUrl": null, "joinPolicy": "open", "potValueUsd": "1240.50" }
```

`tint` is the app's cabal tint (`pine`, `ochre`, `plum`, `indigo`, `moss`), from the same hash of
the id. `joinPolicy` is `open` or `request`. `potValueUsd` is the Groups tab's pot value (cached for
30 seconds). A malformed, unknown or revoked code is `404 invite not found`, the same for all three.
It is sent with `Cache-Control: no-store`, so a revoked code stops previewing at once. A browser
page reading it needs its origin in `CORS_ALLOWED_ORIGINS`.

`POST /v1/groups/join-by-code` runs `POST /v1/groups/{id}/join` for the code's cabal and answers the
same way: `204` when the member is in (or already was), `202 { "status": "pending", "groupId" }` when
the cabal's admin approves members. Both carry `Location: /v1/groups/{id}`. An unknown or revoked
code is `404 invite not found`; a demo cabal is `403`. The old id route keeps working.

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

## Portfolio and history

`GET /v1/me/portfolio` is what Home's "Your money in cabals" is made of. It values each cabal
with the same pot valuation Home uses, so `totalUsd` is the same figure.

```
{ "totalUsd", "cashUsd", "accountBalanceUsd", "dollarPnl", "percentReturn", "unvaluedCabals",
  "holdings": [{ "symbol", "name", "kind", "logoUrl", "valueUsd", "shareOfTotal", "dollarPnl", "percentReturn",
                 "cabals": [{ "groupId", "name", "tint", "pictureUrl", "valueUsd", "quantity", "dollarPnl" }] }] }
```

| Field | Rule |
| --- | --- |
| slice | The caller's share units over the pot's share base (units held by an unpaid cash out included). |
| holding `valueUsd` | Slice × the cabal's position, at the mark the pot valuation used. Summed across cabals. |
| holding `dollarPnl` | Value less slice × the cabal's fill-derived cost basis (the `cost-basis/{symbol}` figure). |
| `quantity` | Slice × the cabal's tokens, in whole shares or tokens, as a decimal string. |
| `cashUsd` | The caller's slice less their holdings: USDC in the pots. Holdings plus cash equal `totalUsd`. |
| `totalUsd`, `dollarPnl`, `percentReturn` | As Home: slices summed, against net USDC put in. |
| `accountBalanceUsd` | `GET /v1/me/balance`, outside every cabal and not in `totalUsd`. `null` when it could not be read. |
| `shareOfTotal` | Holding value over `totalUsd`, 0..1. Cash's share is `cashUsd / totalUsd`. |
| `tint` | `pine`, `ochre`, `plum`, `indigo` or `moss`: the app's rule (FNV-1a of the lowercased group id, mod 5). |
| `unvaluedCabals` | Cabals that could not be valued this pass. They are left out of every figure rather than shown at zero. |

Holdings are sorted by value, and each holding's cabals too.

`GET /v1/me/transactions` returns `{ "items": [...], "nextCursor" }`; `nextCursor` is `null` on
the last page. Pass it back unchanged. Rows are ordered by (`at`, `id`), newest first.

```
{ "id", "kind", "status", "groupId", "groupName", "symbol", "name", "assetKind", "amountUsd", "quantity", "at", "transactionId" }
```

| `kind` | Source | `id` |
| --- | --- | --- |
| `fund` | Money moved from the account balance into a cabal. | deposit (`GET /v1/deposits/{id}`) |
| `cash_out` | A cash out of a cabal: paid, still running, or a transfer that failed and returned the shares. | withdrawal, redeem job, or payout |
| `withdrawal` | USDC sent from the account balance to an outside address. | platform withdrawal |
| `buy`, `sell` | The cabal's trade, attributed to the caller by slice. | transaction (`GET /v1/transactions/{id}`) |
| `bot_buy`, `bot_sell` | The same, placed by the cabal's agent. | transaction |
| `deposit` | Reserved. USDC arriving in the account balance is read from the chain and not recorded, so no rows yet. | |

`status` is `pending`, `done` or `failed`. `symbol`, `name` and `assetKind` (`stock` or `pre_ipo`) are set on trades only. `money_in` keeps `deposit` and `fund`; `cash_out` keeps
`cash_out` and `withdrawal`; `buy` and `sell` include the agent's trades.

**Trade attribution uses the current slice.** Share units are stored as they are now, not as
they were at each trade, so a trade's `amountUsd` and `quantity` are the caller's slice today
times the cabal's trade. Trades from before the caller's first confirmed fund into that cabal
are left out, and so are the trades of a cabal the caller has fully cashed out of. A sell that
has not filled has no `amountUsd`; a buy that has not filled has no `quantity`.

The CSV has the columns `date,kind,status,cabal,stock,amount_usd,quantity,id` and is sent with
`Content-Disposition: attachment; filename="monaco-history.csv"`. It holds at most 10,000 rows,
newest first; past that `X-Monaco-History-Truncated: true` is set. Cabal names that a spreadsheet
would run as a formula are prefixed with `'`.

## Notifications

Every event below writes one inbox row per recipient and, when `APNS_*` is set, pushes it to
each device the recipient registered. A recipient who set
`users.preferences.notifications.<category>` to `false` gets neither; a missing key is on.
Seeded ghost members never get rows. The push carries `aps.alert` (the row's title and body),
`aps.badge` (the unread count), `aps.sound`, `aps.thread-id` (the cabal) and the routing keys
`groupId`, `proposalId`, `notificationId` and `kind`.

| Kind | Category | When | Who |
| --- | --- | --- | --- |
| `proposal_created` | proposals | A proposal opens | Every voter but the proposer |
| `proposal_expiring` | proposals | An hour before close, in cabals whose vote window is 2h or more | Voters who have not voted, once per proposal |
| `proposal_nudge` | proposals | `POST /v1/proposals/{id}/nudge` | Voters who have not voted, the sender aside |
| `join_request` | proposals | Someone asks to join a by-request cabal | The creator |
| `proposal_passed` | results | A bot vote (add, pause, resume, remove) passes | Every member |
| `proposal_failed` | results | A vote is voted down | Every member |
| `proposal_expired` | results | A vote runs out of time (the poller closes it within a minute) | Every member |
| `trade_bought`, `trade_sold` | results | A voted buy or sell fills | Every member |
| `bot_trade` | results | The cabal's bot fills an intent | Every member |
| `member_joined` | results | Someone joins | The creator |
| `join_approved` | results | The creator lets a requester in | The requester |
| `chat_message` | chat | A chat message | Every member but the author, at most one per cabal per 10 minutes |
| `funds_arrived` | money | USDC from outside reaches the account balance | That member |
| `fund_credited` | money | A fund-to-cabal sweep is credited | That member |
| `cash_out_settled` | money | A cash out settles | That member |

`funds_arrived` compares the member wallet with a running total of money that reached it from
outside Monaco: chain balance plus confirmed sweeps and withdrawals, minus cash-outs paid to it.
The first reading only sets the mark, and the mark never falls, so an in-flight sweep or
withdrawal never reads as an arrival. It is checked on every `GET /v1/me/balance` and, for
members with a push device, every two minutes in the background.

A row: `id`, `kind`, `category`, `title`, `body`, `groupId`, `groupName`, `groupPictureUrl`,
`proposalId`, `transactionId`, `symbol` (the stock of a buy or sell), `readAt` and `createdAt`.
Absent references are `null`.

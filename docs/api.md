# HTTP API

Routes are registered in `apps/backend/cmd/api/main.go`; handlers and their request and
response structs are in `apps/backend/internal/httpapi`. Read the handler for field-level
detail. A test (`cmd/api/routes_doc_test.go`) fails if a route is missing from the table below.

## Conventions

**Auth.** `Authorization: Bearer <Privy access token>` on every `/v1` route. Agents instead send
`X-Monaco-Agent-Key` on `POST /v1/groups/{id}/agents/intents` and `GET /v1/groups/{id}/assets`.
A valid token with no Monaco user yet gets `404 user not found`: call `POST /v1/auth/session` first.

**Errors.** `{ "error": "message", "requestId": "…" }`. Send your own `X-Request-Id` (1–64 of
`A-Z a-z 0-9 - _`) or the API mints one; it is echoed on the response and stamped on log lines.
A blocked `POST /v1/groups/{id}/leave` is `409` with the same shape plus a machine-readable `reason`
(for example `share_units_remaining`, `creator_must_transfer`).

**Money.** USDC is integer micros (1 USDC = 1,000,000). Timestamps are UTC RFC 3339.

**Rate limits.** Per process, non-GET only. Over budget is `429` with `Retry-After`.

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

Agent intents do not accept it. A bot must never resend an intent after a timeout or `5xx`
([agent trading](agent-trading.md)).

**Limits.** JSON bodies are capped at 64 KiB (profile photo 2 MB). The server write timeout is
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
| `GET /v1/home/pnl-series` | The caller's equity series. `range` = `1H`, `1D`, `1W`, `1M`. |
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
| `GET /v1/groups/{id}/assets` | Tradable catalog for a cabal. Bearer or agent key. |
| `GET /v1/assets` | Catalog search with prices. |
| `GET /v1/assets/popular` | Popular assets with prices. |
| `GET /v1/assets/{symbol}` | Asset detail. |
| `GET /v1/assets/{symbol}/chart` | Price history. |
| `POST /v1/groups/{id}/quotes` | Check that a buy or sell can route, and at what price. |
| `GET /v1/groups/{id}/proposals` | List proposals. |
| `POST /v1/groups/{id}/proposals` ● | Open a proposal: buy, sell, or add, pause, resume, revoke an agent. |
| `GET /v1/proposals/{id}` | Proposal detail, votes and execution state. |
| `POST /v1/proposals/{id}/votes` | Cast a vote. |
| `GET /v1/proposals/{id}/comments` | Comment thread, oldest first. |
| `POST /v1/proposals/{id}/comments` | Add a comment or reply. |
| `GET /v1/groups/{id}/messages` | A page of cabal chat. |
| `POST /v1/groups/{id}/messages` | Post a chat message. |
| `GET /v1/transactions/{id}` | One swap. |
| `POST /v1/transactions/{id}/retry` ● | Retry a failed swap. |
| `POST /v1/groups/{id}/agents/intents` | An agent submits a trade. Agent key only. See [agent trading](agent-trading.md). |
| `POST /v1/dev/faker` | Seed demo data. Only with `FAKER_ENABLED`, from loopback, on a local database. |

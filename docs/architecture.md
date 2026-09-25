# Monaco architecture (pre-Dynamic)

This map is the Solana + Privy system on `archive/main-before-dynamic` (tip `6b0b194`, merge of #269, 2026-09-20). It is not the later Base + Dynamic tree on `main`.

Product rules live in [product.md](product.md). Route list lives in [api.md](api.md). Agent trading lives in [agent-trading.md](agent-trading.md). Clone and run live in the [README](../README.md).

## What the system is

Friends form a **cabal** (API and tables say `groups`), pool USDC, and buy tokenized US stocks (**xStocks**) on **Solana mainnet**. Votes and share ownership live in Postgres. Assets live in **Privy server wallets**. Swaps go through **Jupiter Swap API v2**. There is no custom Solana program and no on-chain vault.

User-facing copy says cabal, deposit, and fund. Code says group, share units, and NAV.

```text
SwiftUI (apps/mobile, iOS 18+)
  Privy Swift for OTP and the member wallet
  HTTP only for product data (packages/mobile-core client)
        |
        v
Go API (apps/backend)
  cmd/api          routes, middleware, process boot
  internal/httpapi decode, auth, one app call, JSON
  internal/app     commands and read models
  packages/domain  pure money, NAV, P&L, governance, agent checks
  internal/postgres SQL
        |
        +--> Docker Postgres (local) or Supabase Postgres (hosted)
        +--> Privy (users, member wallets, group treasuries, signatures)
        +--> Jupiter v2 (quotes and swaps) / Flash (optional swap provider)
        +--> xStocks public API (mint metadata)
        +--> Pyth Hermes then Jupiter price (marks)
        +--> Solana RPC (balances, confirmations)
        +--> app relayer keypair (pays SOL fees; not a Privy wallet)
```

Mobile never calls Jupiter, Pyth, xStocks, or Solana RPC for product flows. Those clients are backend-only.

## Processes

| Piece | Where | Role |
| --- | --- | --- |
| API | `apps/backend/cmd/api` | HTTP server. Registers every route. Boot refuses to start if the relayer holds ≤ 0.001 SOL. |
| Migrations | `apps/backend/cmd/migrate`, SQL in `supabase/migrations` | Ordered SQL. Local DB is Compose Postgres (`monaco` on host port `54322`). Tests use sibling `monaco_test`. |
| Workers | `apps/backend/internal/worker` | In-process pollers: deposit sweep, proposal execute, redeem recovery, Solana confirmation. |
| Domain math | `packages/domain` | No I/O. Share minting, pot NAV, member equity, percent return, vote rules, agent intent checks. |
| iOS app | `apps/mobile` (Xcode scheme `Monaco`) | SwiftUI tabs. Privy only for auth and showing the deposit address. |
| Swift unit tests | `packages/mobile-core` | Host `swift test`. Networking and formatting. No simulator. |
| Reference agent | `agents/momentum-bot` | External process. Trades with `X-Monaco-Agent-Key`. Does not hold treasury keys. |
| Demo seed | `cmd/seed-demo`, `cmd/faker-seed`, `POST /v1/dev/faker` | Local Postgres only. |

`just run` starts Postgres, the API, and the simulator. Recipes that need secrets re-exec under `scripts/with-dotenv-local.sh`.

## Request path

Handlers in `internal/httpapi` do not own money rules. They authenticate, decode, call one method on `internal/app`, and encode. That split is the locked shape in [architect/synthesis.md](architect/synthesis.md): one `app` module, clients (Privy, Jupiter, Pyth) private to it, workers call the same commands as HTTP.

Auth on `/v1`:

1. The app gets a Privy access token (SMS or email OTP). OTP itself never hits Monaco.
2. `POST /v1/auth/session` verifies the token, upserts `users`, and ensures one Privy member wallet.
3. Later calls send `Authorization: Bearer <token>`. A valid token with no Monaco user is `404 user not found`.
4. Agents skip the bearer token. They send `X-Monaco-Agent-Key` on intent and cabal-asset routes.

Money POSTs (fund, withdraw, cash out, leave, propose, swap retry) may send `Idempotency-Key`. The key is scoped to the user, kept 24 hours in `idempotency_keys`. Same key and body replays. Same key and different body is 422. In-flight duplicate is 409. 5xx does not store the key. Agent intents use an `idempotencyKey` field in the body instead of the header.

USDC amounts are integer micros (1 USDC = 1_000_000).

## Wallets

| Wallet | Key holder | What it holds |
| --- | --- | --- |
| Member | One Privy wallet per user. Reused on login, never recreated. | Inbound USDC. This is **account balance**. |
| Group treasury | One Privy server wallet per group, created with the group. | USDC and xStock after funding and swaps. |
| Relayer | `RELAYER_PRIVATE_KEY` in `.env.local`. | SOL for fees. Member and treasury wallets do not need SOL on the happy path. |
| Phantom agent wallet | Cursor Phantom MCP. Not in `.env.local`. | QA funding only. Not a product wallet. |

The backend signs sweeps, swaps, and payouts through Privy. Users do not approve individual Solana transactions.

## Money

Account balance is on-chain USDC in the member wallet minus in-flight fund jobs and pending platform withdrawals (`GET /v1/me/balance`). It is not a separate ledger balance.

**Deposit** shows the member address. There is no amount submit. USDC sent to that address (Solana mainnet mint `EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v`) stays there until the user funds a cabal.

**Fund this cabal** is `POST /v1/groups/{id}/fund`. The app creates a pending `deposits` row and the sweep poller moves that exact amount member → treasury. Share units credit only after the sweep confirms, idempotent on `deposits.tx_signature`. `POST /v1/groups/{id}/deposits` is gone (`410`).

**Buy / sell.** A proposal names a catalog symbol and a USDC amount. If Jupiter cannot quote, the API refuses the proposal. Votes live in Postgres (`proposals`, `votes`). On pass, the execute poller builds a Jupiter order, Privy-signs with the treasury, and records a `transactions` row. Tokens stay in the treasury. Partial sells do not remove the member from the group.

**Platform withdraw** (`POST /v1/me/withdrawals`) sends idle member-wallet USDC to an external Solana address. It does not touch share units or the treasury.

**Cabal cash out** (`POST /v1/groups/{id}/withdraw-to-balance`) debits share units first (`redeem_jobs`: Debited → Selling → Paying → Settled). If the slice is in stock, that slice is sold to USDC, then USDC is paid. `redeem_payouts` stores the signed transfer before broadcast so a retry cannot sign a second payout. A failed or expired transfer returns the shares.

Pot value is treasury USDC the ledger accounts for plus holdings at a live mark. On-chain USDC that has not been credited is not a gain. First deposit into an empty pot mints shares at $1. After that, shares minted = amount × total shares / pre-credit NAV, rounded down. The implementation is `valuePot` in `apps/backend/internal/app/pot_valuation.go`, with the arithmetic in `packages/domain` (`ComputePotNAV`, `MemberEquity`, `PercentReturn`).

Marks: Pyth Hermes, then Jupiter price, with a circuit breaker in `internal/pricechain`. Cost basis (Jupiter fill) is for screens and history, not for minting or paying.

Percent return is equity / net USDC in − 1. Net USDC in is deposited minus withdrawn on `positions`. Rows with zero net in stay off the boards. `nav_snapshots` record pot value at deposit, fill, and redeem so charts are not recomputed only from live wallets.

## Governance and social

Set at cabal create: join policy (open or password), voter set (named subset or every member), threshold (majority or unanimous), vote expiry. On-chain voting is out of scope.

Also in the product, still Postgres:

- Join requests (`group_join_requests`)
- Proposal comments
- Group chat (`group_messages`)
- Profile name and photo
- Group agents: proposals can add, pause, resume, or revoke an agent. The agent calls `POST /v1/groups/{id}/agents/intents`. `packages/domain.ValidateIntent` caps allocation and stops an agent from selling stock the members bought.

## Data

Core tables, in migration order:

| Tables | Migration | Holds |
| --- | --- | --- |
| `users`, `member_wallets`, `groups`, `treasuries` | `000002` | Identity and the two Privy addresses. |
| `deposits`, `positions`, `withdrawals` | `000003` | Fund jobs and share claims (`share_units`, `amount_deposited`, `amount_withdrawn`). |
| `transactions` | `000004` | Buy and sell swaps, signatures, cost basis. |
| `group_members`, `group_voters`, `proposals`, `votes`, `nav_snapshots`, `payout_proofs` | `000005` | Membership, governance, history. |
| `redeem_jobs` | `000006` | Cash-out state machine. |
| `group_join_requests` | `000007` | Approval-gated joins. |
| `platform_withdrawals` | `000008` | Member-wallet sends. |
| `group_agents`, `agent_intents` | `000011` | Agentic trading. |
| `proposal_comments`, `group_messages` | `000014`, `000015` | Threads and chat. |
| `redeem_payouts` | `000019` | Exactly-once payout signatures. |
| `idempotency_keys` | `000024` | HTTP money retries. |

`internal/postgres` is the only SQL package. `internal/app` does not embed queries.

## iOS

Five-tab shell under `apps/mobile/Monaco/Features`:

| Tab | Folder | Reads |
| --- | --- | --- |
| Home | `Features/Home` | `/v1/home`, `/v1/home/dashboard` (boards, net worth, series). |
| Cabals | `Features/Groups` | Search, leaderboard, detail via `GET /v1/groups/{id}/view`. |
| Assets | `Features/Assets` | Catalog and charts; propose from a symbol into a cabal. |
| Proposals | `Features/Proposals` | Feed, votes, comments, buy/sell/agent proposals. |
| Profile | `Features/Profile` | Same home and `/v1/me` payloads; name and photo writes. |

Auth is `Features/Auth` (`PrivyAuthService`, `SessionGateView`). Deposit and fund are `Features/Deposit`. Cash out is `Features/Redeem`. DTOs live in `Monaco/API/DTOs`. Shared HTTP behavior that unit tests cover is `packages/mobile-core`.

## Background work

Pollers in `internal/worker` share the API process:

- **Sweep** — pending funds: build the member→treasury transfer, Privy-sign, confirm, then credit shares.
- **Proposal execute** — passed proposals whose swap has not finished.
- **Redeem recovery** — resume a cash-out that crashed after the share debit.
- **Solana RPC status** — confirmation and expiry for in-flight signatures.

Handlers do not both broadcast and credit. Credit runs after confirm, through the same `app` methods.

## External systems

| System | Package | Used for |
| --- | --- | --- |
| Privy | `internal/privy` | Token verify, wallet create, sign and submit. |
| Jupiter | `internal/jupiter` | Quote, swap build, execute. |
| Flash | `internal/flash`, `internal/swapprovider` | Alternate swap path when configured. |
| xStocks | `internal/xstocks` | Symbol → Solana mint. Not an execution rail. |
| Pyth | `internal/pyth` | Equity marks. Charts need an entitled Hermes key. |
| Solana RPC | `internal/solana`, `internal/worker/solana_rpc.go` | Balances and tx status. |

USDC on Ethereum or Base is invisible to this tree. The deposit poller only sees the Solana USDC mint.

## Where to read next

| Question | Start here |
| --- | --- |
| Share price and P&L formulas | [product.md](product.md) “NAV and share units”; `packages/domain/nav.go`, `pnl.go` |
| A route’s JSON | [api.md](api.md), then the matching file in `internal/httpapi` |
| Why `app` is one package | [architect/synthesis.md](architect/synthesis.md) |
| Agent keys and limits | [agent-trading.md](agent-trading.md) |
| Sweep operations | [ops-sweep-wallets.md](ops-sweep-wallets.md) |
| Boot and metrics | [ops-observability.md](ops-observability.md) |

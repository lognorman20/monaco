# Monaco grafted sketch

Phase D. Luna base plus Composer and Grok grafts. Signatures only. Bodies `panic("not implemented")`.

## Module map

```text
monaco/
  apps/backend/
    cmd/api/                    startup, config, routes
    internal/httpapi/           decode, encode, auth middleware
    internal/app/               session, deposit, sweep, swap, redeem, group view
    internal/postgres/          Postgres store
    internal/privy/             Privy client
    internal/jupiter/           Jupiter client (quote, execute, poll)
    internal/xstocks/           mint resolver (xStocks API)
    internal/pyth/              Pyth price client (M4)
    internal/worker/            member USDC poll, sweep confirm → ObserveSweep
    internal/dto/               private HTTP DTOs
  packages/domain/              pure math, no I/O
    money.go                    USDCMicros, ShareUnits
    nav.go                      NavMode, NavInput, PotNAV, ComputePotNAV
    shares.go                   SharesForDeposit, MemberEquity
    pnl.go                      PercentReturn, board rank
    votes.go                    TallyProposal
    redeem.go                   RedeemSlice, RedeemJobStatus
  apps/mobile/
    API/MonacoAPIClient.swift   one method per user action
    API/DTOs/                   hand-written Codable
    Features/                   Auth, Groups, Deposit, Proposals, Home, Redeem, Settings
  supabase/migrations/          additive per milestone
```

Dependency rule: `httpapi` calls one `app` method per route. `app` calls the Privy client, Jupiter client, Postgres store, and (M4) Pyth price client. `app` and tests import `packages/domain`. Nothing in `domain` imports `apps/backend`.

## packages/domain

```go
package domain

type USDCMicros int64
type ShareUnits string // decimal string; invariant > 0 when crediting

type NavMode int
const (
    NavUSDCOnly NavMode // USDC-only pot: deposit credits share_units 1:1 with swept USDC (M2)
    NavMarked   NavMode // M4: USDC + Σ(units × mark); mint claim units on deposit
)

type NavInput struct {
    Mode         NavMode
    TreasuryUsdc USDCMicros
    TotalShares  ShareUnits
    Holdings     []MarkedHolding // ignored unless NavMarked
}

type MarkedHolding struct {
    Symbol     string
    Units      string
    MarkUsdc   USDCMicros
    CostBasis  USDCMicros
    AfterHours bool
}

type PotNAV struct {
    TotalUsdc    USDCMicros
    PerShareUsdc USDCMicros
}

func ComputePotNAV(in NavInput) (PotNAV, error)
func SharesForDeposit(deposited USDCMicros, nav PotNAV) (ShareUnits, error)
func MemberEquity(memberShares, totalShares ShareUnits, nav PotNAV) (USDCMicros, error)
func PercentReturn(equity, netIn USDCMicros) (*float64, error) // nil when netIn == 0
func TallyProposal(in VoteTallyInput) (ProposalStatus, error)
func ComputeRedeemSlice(in RedeemSliceInput) (RedeemSlice, error)

type RedeemJobStatus string
const (
    RedeemDebited  RedeemJobStatus = "debited"
    RedeemSelling  RedeemJobStatus = "selling"
    RedeemPaying   RedeemJobStatus = "paying"
    RedeemSettled  RedeemJobStatus = "settled"
)
```

## App (handler entrypoints)

Handlers decode JSON, check auth, call one function on `app`, encode JSON. **Group treasury** means the Privy server wallet per group. This interface is the Go orchestration layer, not that wallet.

```go
type App interface {
    Session(ctx context.Context, token PrivyToken) (SessionResult, error)
    CreateGroup(ctx context.Context, actor UserID, cmd CreateGroup) (GroupView, error)
    JoinGroup(ctx context.Context, actor UserID, cmd JoinGroup) (GroupView, error)
    CreateDeposit(ctx context.Context, cmd DepositRequest) (DepositStatus, error)
    ObserveSweep(ctx context.Context, fact ObservedSweep) (DepositStatus, error)
    SearchAssets(ctx context.Context, query AssetQuery) ([]Asset, error)
    DevExecuteBuy(ctx context.Context, cmd DevBuyCommand) (TransactionView, error) // M3 only; deleted M4
    ProposeBuy(ctx context.Context, cmd BuyProposalCommand) (ProposalView, error)
    Vote(ctx context.Context, cmd VoteCommand) (ProposalView, error)
    Redeem(ctx context.Context, cmd RedeemCommand) (RedeemJobView, error)
    GroupView(ctx context.Context, viewer UserID, group GroupID) (GroupView, error)
    HomeView(ctx context.Context, viewer UserID) (HomeView, error)
}
```

## What `app` calls

```go
// Postgres store (internal/postgres)
type Store interface {
    InTx(ctx context.Context, fn func(TxStore) error) error
}
type TxStore interface {
    UpsertSession(User) (User, error)
    CreateGroup(Group, TreasuryRef) (Group, error)
    CreateDeposit(Deposit) (Deposit, error)
    ApplyConfirmedSweep(ObservedSweep, ShareCredit) (Deposit, error) // idempotent on tx_signature
    CreateTransaction(Transaction) (Transaction, error)              // idempotent on tx_signature
    CreateWithdrawal(Withdrawal) (Withdrawal, error)               // idempotent on tx_signature
    UpdateRedeemJob(RedeemJob) (RedeemJob, error)
    CastVote(Vote) (ProposalViewData, error)
}

// Privy client (internal/privy)
type PrivyClient interface {
    VerifySession(PrivyToken) (PrivyIdentity, error)
    EnsureMemberWallet(UserID) (WalletRef, error)
    EnsureTreasury(GroupID) (TreasuryRef, error)
    VerifyPayoutProof(UserID, PayoutProof) (PayoutProof, error)
    SubmitSweep(Deposit, WalletRef, TreasuryRef) (TxSignature, error)
    PayUSDC(TreasuryRef, SolanaAddress, USDCMicros) (TxSignature, error)
}

// Jupiter client (internal/jupiter)
type JupiterClient interface {
    Search(AssetQuery) ([]Asset, error)
    QuoteBuy(TreasuryRef, Asset, USDCMicros) (BuyQuote, error)
    ExecuteBuy(TreasuryRef, BuyQuote, idempotencyKey string) (Fill, error)
    SellToUSDC(TreasuryRef, HoldingSlice) (Fill, error)
}

// Pyth price client (internal/pyth, M4)
type PythClient interface {
    USDCOnlyPot(TreasuryRef) (NavInput, error)
    MarkedPot(TreasuryRef, []CostBasis) (NavInput, error)
}

// Who may start a buy. M3: dev stub HTTP route only. M4: passed vote only. Stub deleted with route.
type StartBuy interface {
    Allow(ctx context.Context, reason BuyReason) error
}
```

Proposal expiry uses `time.Now`. Tests may inject a fake clock when they need fixed expiry times. No `Clock` type in the architecture.

## HTTP routes

```text
GET  /health
POST /v1/auth/session
GET  /v1/me
POST /v1/groups
GET  /v1/groups/{id}
POST /v1/groups/{id}/join                          M4
POST /v1/groups/{id}/deposits                      M2
GET  /v1/deposits/{id}                             M2
GET  /v1/groups/{id}/assets?query=                 M4 catalog search
POST /v1/groups/{id}/quotes                        M4 routable check before propose
POST /v1/dev/groups/{id}/buy                       M3 only; DELETE M4
POST /v1/groups/{id}/proposals                     M4
POST /v1/proposals/{id}/votes                      M4
POST /v1/groups/{id}/redeems                       M4
GET  /v1/home                                      M4
```

Auth: `Authorization: Bearer <privy-access-token>` on `/v1/*`.

Localhost: API listen port chosen in M0 (sketch uses `8080`, mobile `Config.apiBaseURL`).

## Postgres tables

**M1**


| Table            | Key columns                                                                   |
| ---------------- | ----------------------------------------------------------------------------- |
| `users`          | `id`, `privy_user_id` UNIQUE, `display_name`, `created_at`                    |
| `member_wallets` | `id`, `user_id` UNIQUE FK, `privy_wallet_id`, `solana_address`, `created_at`  |
| `groups`         | `id`, `name`, `creator_user_id` FK, `created_at`                              |
| `treasuries`     | `id`, `group_id` UNIQUE FK, `privy_wallet_id`, `solana_address`, `created_at` |


**M2**


| Table         | Key columns                                                                                                      |
| ------------- | ---------------------------------------------------------------------------------------------------------------- |
| `deposits`    | `id`, `user_id`, `group_id`, `amount`, `from_address`, `status`, `tx_signature` UNIQUE, `created_at`             |
| `positions`   | `user_id`, `group_id` PK, `share_units`, `amount_deposited`, `amount_withdrawn` (default 0)                      |
| `withdrawals` | `id`, `user_id`, `group_id`, `amount`, `to_address`, `status`, `tx_signature` UNIQUE, `created_at` (schema only) |


**M3**


| Table          | Key columns                                                                                                                                                                                                                              |
| -------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `transactions` | `id`, `group_id`, `proposal_id` NULL, `action` buy/sell, `amount`, `input_mint`, `output_mint`, `status`, `tx_signature` UNIQUE, `execute_request_id` UNIQUE NULL, `cost_basis_price`, `cost_basis_amount`, `created_at`, `confirmed_at` |


**M4** (alter, do not recreate M1/M2)


| Table / change  | Key columns                                                                                                                             |
| --------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| `groups` +      | `join_mode`, `join_password_hash`, `voter_set_mode`, `threshold`, `vote_expiry_seconds`                                                 |
| `group_members` | `group_id`, `user_id` PK, `joined_at`                                                                                                   |
| `group_voters`  | `group_id`, `user_id` when named subset                                                                                                 |
| `proposals`     | `id`, `group_id`, `proposer_id`, `symbol`, `usdc_micros`, `status`, `expires_at`, `fill_tx_signature` NULL                              |
| `votes`         | `proposal_id`, `voter_id` PK, `choice`, `cast_at`                                                                                       |
| `nav_snapshots` | `id`, `group_id`, `pot_nav_micros`, `nav_per_share_micros`, `total_shares`, `reason`, `created_at`                                      |
| `redeem_jobs`   | `id`, `group_id`, `user_id`, `share_units`, `slice_usdc`, `payout_address`, `status` debited/selling/paying/settled, `withdrawal_id` FK |
| `payout_proofs` | audit of verified payout messages                                                                                                       |


Relayer fee payer: config only, not a table. Startup fails if missing from M1.

## Per-milestone flow

### M0

Health only. Compose Postgres. Empty `packages/domain` module path. No product tables.

### M1

1. Mobile Privy login → `POST /v1/auth/session`.
2. `app.Session`: Privy client verifies token, Postgres upserts `users`, ensures `member_wallets`.
3. `CreateGroup`: insert thin `groups`, Privy client provisions group treasury, insert `treasuries`.
4. `GET /v1/me`, `GET /v1/groups/{id}` return addresses (debug until M5).

### M2

1. `CreateDeposit` inserts `deposits` row (pending).
2. Worker polls member USDC, Privy client signs sweep (relayer pays SOL).
3. On confirm, worker calls `ObserveSweep`.
4. Postgres transaction: if `deposits.tx_signature` already set for this sig, return prior credit.
5. Upsert `positions`: increment `share_units` and `amount_deposited` by swept USDC (1:1 in M2).
6. Member-wallet-only balance: no sweep, no credit.

### M3

1. Only the dev buy route may call `StartBuy` (dev build flag).
2. `POST /v1/dev/groups/{id}/buy` → Jupiter client quotes, group treasury signs via Privy, Jupiter execute, poll Success code 0.
3. Insert `transactions` idempotent on `tx_signature` or `execute_request_id`.
4. Sell path: xStock → USDC through Jupiter client, same poll rule. Prepares M4 redeem liquidation.

### M4

1. Full group create with rules. `JoinGroup` adds `group_members`.
2. `ProposeBuy`: quote must exist. `Undecided` proposer rule returns error until M4-T39 closes.
3. `Vote` → `TallyProposal`. On pass, only a passed vote may call `StartBuy` → Jupiter `ExecuteBuy` (dev route deleted).
4. Pyth price client `MarkedPot` → `NavMarked` for portfolio and redeem slice.
5. `Redeem`: Privy client verifies proof → debit `positions` in Postgres txn → `redeem_jobs` status `debited` → sell slice via Jupiter client → `selling` → pay USDC via Privy client → `paying` → insert `withdrawals`, `settled` → NAV snapshot.
6. `GroupView`, `HomeView`: boards rank by `PercentReturn`, skip net USDC in == 0.

### M5

Swift `MonacoAPIClient` maps routes 1:1. Screens render server numbers. No share math in Swift. Addresses under Settings, Advanced only.

## Idempotency keys


| Operation        | Key                                                 |
| ---------------- | --------------------------------------------------- |
| Sweep credit     | `deposits.tx_signature`                             |
| Jupiter buy/sell | `transactions.tx_signature` or `execute_request_id` |
| Redeem payout    | `withdrawals.tx_signature`                          |
| Redeem debit     | `redeem_jobs.id` (resume from status)               |
| Wallet provision | `users.privy_user_id`                               |


## Red-flag screen


| Flag                   | Check                                                 |
| ---------------------- | ----------------------------------------------------- |
| Shallow module         | Handlers call one `app` method per operation.         |
| Information leakage    | Jupiter/Privy structs stay in their client packages.    |
| Temporal decomposition | No validate/persist packages. One txn per transition. |
| Pass-through           | No handler → postgres direct.                         |


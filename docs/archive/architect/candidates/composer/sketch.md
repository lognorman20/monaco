# Monaco architecture sketch (composer candidate)

Signatures only. Bodies `panic("not implemented")` or `// TODO`. Types encode invariants.

---

## Module map

```
monaco/
├── packages/domain/                 # Go module — pure math, no I/O
│   ├── money.go                     # USDCMicros, ShareUnits, rounding
│   ├── nav.go                       # NavUSDCOnly, NavMarked, PotNAV
│   ├── shares.go                    # SharesForDeposit, equity
│   ├── pnl.go                       # NetUsdcIn, PercentReturn, rank
│   ├── votes.go                     # Tally, threshold, expiry
│   └── redeem.go                    # SliceUsdc, stock sell fractions
│
├── apps/backend/
│   ├── main.go                      # wire deps, register routes, M3 dev routes flag
│   └── internal/
│       ├── config/                  # DATABASE_URL, Privy, relayer keypair (fail startup if missing M1)
│       ├── middleware/              # auth session, request id
│       ├── handler/                   # HTTP parse/encode only
│       │   ├── auth.go
│       │   ├── me.go
│       │   ├── groups.go
│       │   ├── deposits.go          # M2+
│       │   ├── swap.go              # M3 order status GETs
│       │   ├── dev/stub_buy.go      # M3 only — DELETE in M4
│       │   ├── proposals.go         # M4
│       │   ├── votes.go             # M4
│       │   ├── portfolio.go         # M4 group pot + boards
│       │   └── redeem.go            # M4
│       ├── service/                 # orchestration; calls ledger + adapters
│       │   ├── auth.go
│       │   ├── group.go
│       │   ├── deposit.go           # intent + poller tick
│       │   ├── buy_on_pass.go       # M4 — replaces stub
│       │   └── redeem.go
│       ├── ledger/                  # sole writer: shares, net_usdc_in, nav_snapshots
│       │   ├── ledger.go
│       │   └── types.go
│       ├── governance/              # proposals, votes, tally, expiry job
│       │   └── governance.go
│       ├── treasury/                # Privy sign, RPC balance reads
│       │   └── treasury.go
│       ├── swap/                    # Jupiter quote/execute/poll; xStock sell
│       │   └── swap.go
│       ├── privy/                   # auth verify, wallet create, sign tx
│       ├── jupiter/                 # wire client — private
│       ├── xstocks/                 # mint resolver — private
│       ├── pyth/                    # equity marks — private
│       └── db/                      # SQL repos — private to above
│
├── apps/mobile/Monaco/
│   ├── API/
│   │   ├── MonacoAPIClient.swift
│   │   └── DTOs/                    # hand-written Codable mirrors Go JSON
│   ├── Features/                    # M1–M5 screens per milestone
│   │   ├── Auth/
│   │   ├── Groups/
│   │   ├── Deposit/
│   │   ├── DevSwap/                 # M3 debug — remove UI M5
│   │   ├── Proposals/
│   │   ├── Portfolio/
│   │   ├── Home/
│   │   ├── Redeem/
│   │   └── Settings/
│   └── Display/                     # percent/P&L formatters (M5 tests)
│
└── supabase/migrations/             # one migration per milestone tranche
```

**Dependency rule:** `handler` → `service` → (`ledger` | `governance` | `swap` | `treasury`) → `db` / adapters. `service` and `ledger` import `packages/domain`. Nothing in `packages/domain` imports `apps/backend`.

---

## packages/domain

```go
// money.go
package domain

type USDCMicros int64   // invariant: >= 0
type ShareUnits string   // decimal string; invariant: > 0 when crediting/debiting

func (u USDCMicros) IsZero() bool
func ParseShareUnits(s string) (ShareUnits, error)

// nav.go
type NavMode int
const (
    NavUSDCOnly NavMode // M2: treasury USDC / total shares; empty → $1/share
    NavMarked   NavMode // M4: USDC + Σ(token units × mark)
)

type NavInput struct {
    Mode         NavMode
    TreasuryUsdc USDCMicros
    TotalShares  ShareUnits // zero ⇒ first-deposit price $1
    Positions    []MarkedPosition // ignored unless NavMarked
}

type MarkedPosition struct {
    Symbol     string
    Units      string  // decimal
    MarkUsdc   USDCMicros // per-unit from Pyth or fill
    CostBasis  USDCMicros // Jupiter fill — display only
    AfterHours bool
}

type PotNAV struct {
    TotalUsdc    USDCMicros
    PerShareUsdc USDCMicros // PotNAV / total shares; empty pot uses 1_000_000 ($1)
}

func ComputePotNAV(in NavInput) (PotNAV, error)

// shares.go
func SharesForDeposit(deposited USDCMicros, nav PotNAV) (ShareUnits, error)
// invariant: shares = deposited / nav.PerShareUsdc

func MemberEquity(memberShares, totalShares ShareUnits, nav PotNAV) (USDCMicros, error)
// invariant: (member/total) × pot

// pnl.go
type NetUsdcIn USDCMicros // sweeps in − redeem payouts; per member per group

func PercentReturn(equity USDCMicros, netIn NetUsdcIn) (*float64, error)
// nil when netIn == 0 (skip board row)

type LeaderboardRow struct {
    SubjectID   string
    DisplayName string
    Equity      USDCMicros
    NetIn       NetUsdcIn
    PctReturn   float64
    DollarPnL   USDCMicros
}

func RankByPercentReturn(rows []LeaderboardRow) []LeaderboardRow

// votes.go
type VoteThreshold string
const (
    ThresholdUnanimous VoteThreshold = "unanimous"
    ThresholdMajority  VoteThreshold = "majority"
)

type VoteChoice string
const (
    VoteYes VoteChoice = "yes"
    VoteNo  VoteChoice = "no"
)

type ProposalStatus string
const (
    ProposalOpen     ProposalStatus = "open"
    ProposalPassed   ProposalStatus = "passed"
    ProposalFailed   ProposalStatus = "failed"
    ProposalExpired  ProposalStatus = "expired"
)

type VoterSetMode string
const (
    VoterSetAllMembers VoterSetMode = "all_members"
    VoterSetNamed      VoterSetMode = "named_subset"
)

type VoteTallyInput struct {
    Threshold   VoteThreshold
    VoterIDs    []string // resolved voter set at proposal time
    Cast        map[string]VoteChoice
    ExpiresAt   time.Time
    Now         time.Time
}

func TallyProposal(in VoteTallyInput) (ProposalStatus, error)

// redeem.go
type RedeemSliceInput struct {
    SharesRedeemed ShareUnits
    MemberShares   ShareUnits
    TotalShares    ShareUnits
    Nav            PotNAV
    Positions      []MarkedPosition
}

type RedeemSlice struct {
    UsdcOwed       USDCMicros
    StockSellFrac  float64 // sharesRedeemed/totalShares applied per position
}

func ComputeRedeemSlice(in RedeemSliceInput) (RedeemSlice, error)
```

---

## apps/backend — core types

```go
// internal/ledger/types.go
package ledger

type TxSignature string // Solana sig — idempotency key for sweeps/fills/payouts

type SnapshotReason string
const (
    SnapshotDeposit SnapshotReason = "deposit"
    SnapshotFill    SnapshotReason = "fill"
    SnapshotRedeem  SnapshotReason = "redeem"
)

type SweepCredit struct {
    GroupID    uuid.UUID
    UserID     uuid.UUID
    Signature  TxSignature
    UsdcMicros domain.USDCMicros
    NavMode    domain.NavMode
    Marked     []domain.MarkedPosition // required when NavMarked
}

type RedeemDebit struct {
    GroupID         uuid.UUID
    UserID          uuid.UUID
    Shares          domain.ShareUnits
    RedeemID        uuid.UUID // idempotency for full redeem flow
    PayoutProof     PayoutProof
}

type PayoutProof struct {
    Pubkey        string
    Message       string
    Signature     string // ed25519 — verified before debit
}

type ShareBalance struct {
    GroupID uuid.UUID
    UserID  uuid.UUID
    Shares  domain.ShareUnits
}

// internal/ledger/ledger.go
type Ledger interface {
    ApplySweepCredit(ctx context.Context, in SweepCredit) error
    // idempotent on Signature; rejects if USDC only in member wallet (caller must confirm treasury credit)

    DebitShares(ctx context.Context, in RedeemDebit) (domain.PotNAV, error)
    // row-lock share_ledger; returns NAV at debit for slice math; idempotent on RedeemID

    RecordRedeemPayout(ctx context.Context, redeemID uuid.UUID, sig TxSignature, usdc domain.USDCMicros) error
    // decrements net_usdc_in; writes nav_snapshot; idempotent on payout sig

    GetShareBalance(ctx context.Context, groupID, userID uuid.UUID) (ShareBalance, error)
    GetTotalShares(ctx context.Context, groupID uuid.UUID) (domain.ShareUnits, error)
    WriteSnapshot(ctx context.Context, groupID uuid.UUID, reason SnapshotReason, nav domain.PotNAV) error
}

// internal/governance/governance.go
type JoinPolicy struct {
    Mode     string // "open" | "password"
    Password *string
}

type CreateGroupInput struct {
    Name          string
    CreatorID     uuid.UUID
    JoinPolicy    JoinPolicy
    VoterSetMode  domain.VoterSetMode
    VoterMemberIDs []uuid.UUID // min 1 when named
    Threshold     domain.VoteThreshold
    VoteExpiry    time.Duration
}

type Proposal struct {
    ID          uuid.UUID
    GroupID     uuid.UUID
    ProposerID  uuid.UUID
    Symbol      string
    UsdcAmount  domain.USDCMicros
    Status      domain.ProposalStatus
    ExpiresAt   time.Time
}

type Governance interface {
    CreateGroup(ctx context.Context, in CreateGroupInput) (uuid.UUID, error) // M4 replaces M1 thin create
    JoinGroup(ctx context.Context, groupID, userID uuid.UUID, password *string) error
    CreateProposal(ctx context.Context, p Proposal) (uuid.UUID, error)
    // M4-T12: refuse if swap.PreviewBuy returns no route
    CastVote(ctx context.Context, proposalID, voterID uuid.UUID, choice domain.VoteChoice) error
    CloseExpired(ctx context.Context, now time.Time) error // cron/tick
    OnPassed(ctx context.Context, proposalID uuid.UUID) error // emits to buy_on_pass
}

// internal/swap/swap.go
type OrderSide string
const (
    SideBuy  OrderSide = "buy"
    SideSell OrderSide = "sell"
)

type SwapOrder struct {
    ID        uuid.UUID
    GroupID   uuid.UUID
    Side      OrderSide
    InputMint string
    OutputMint string
    Status    string // pending|success|failed
}

type Swap interface {
    PreviewBuy(ctx context.Context, symbol string, usdc domain.USDCMicros) (bool, error) // false ⇒ no route
    Buy(ctx context.Context, groupID uuid.UUID, symbol string, usdc domain.USDCMicros, idempotencyKey string) (TxSignature, error)
    SellSlice(ctx context.Context, groupID uuid.UUID, positions []domain.MarkedPosition, frac float64, idempotencyKey string) (TxSignature, error)
    // polls Jupiter execute until Success code 0; persists jupiter_orders + fills + cost_basis
}

// internal/treasury/treasury.go
type WalletKind string
const (
    WalletMember   WalletKind = "member"
    WalletTreasury WalletKind = "treasury"
)

type Treasury interface {
    MemberUsdcBalance(ctx context.Context, userID uuid.UUID) (domain.USDCMicros, error)
    TreasuryBalances(ctx context.Context, groupID uuid.UUID) (usdc domain.USDCMicros, tokens []TokenBalance, error)
    SweepUsdcToTreasury(ctx context.Context, userID, groupID uuid.UUID, amount domain.USDCMicros) (TxSignature, error)
    SendUsdcPayout(ctx context.Context, groupID uuid.UUID, destPubkey string, amount domain.USDCMicros) (TxSignature, error)
    // relayer pays SOL fees on all signed txs
}

type TokenBalance struct {
    Mint   string
    Symbol string
    Units  string
}

// internal/service/deposit.go
type DepositService interface {
    StartIntent(ctx context.Context, userID, groupID uuid.UUID, expected domain.USDCMicros) (uuid.UUID, error)
    PollIntent(ctx context.Context, intentID uuid.UUID) (DepositStatus, error)
    // tick: detect member balance → sweep → on confirmed sig call Ledger.ApplySweepCredit(NavUSDCOnly)
}

// internal/service/buy_on_pass.go  (M4)
type BuyOnPassService interface {
    Execute(ctx context.Context, proposalID uuid.UUID) error
    // load passed proposal → swap.Buy → ledger.WriteSnapshot(fill) → update proposal linked fill
}

// internal/service/redeem.go  (M4)
type RedeemService interface {
    Complete(ctx context.Context, req RedeemRequest) (RedeemResult, error)
}

type RedeemRequest struct {
    GroupID     uuid.UUID
    UserID      uuid.UUID
    Shares      domain.ShareUnits // or derive from UsdcTarget via inverse equity
    UsdcTarget  *domain.USDCMicros
    PayoutProof ledger.PayoutProof
}

type RedeemResult struct {
    RedeemID    uuid.UUID
    UsdcPaid    domain.USDCMicros
    PayoutSig   ledger.TxSignature
}
// ordering inside Complete:
// 1. verify PayoutProof
// 2. ledger.DebitShares (txn)
// 3. swap.SellSlice if stock positions
// 4. treasury.SendUsdcPayout
// 5. ledger.RecordRedeemPayout
```

---

## Postgres data models (by milestone)

### M1

| Table | Key columns |
|-------|-------------|
| `users` | `id`, `privy_user_id` UNIQUE, `display_name`, `created_at` |
| `member_wallets` | `id`, `user_id` UNIQUE FK, `privy_wallet_id`, `solana_address`, `created_at` |
| `groups` | `id`, `name`, `creator_user_id` FK, `created_at` — M4 adds policy columns |
| `treasuries` | `id`, `group_id` UNIQUE FK, `privy_wallet_id`, `solana_address`, `created_at` |

M1 also inserts `group_members(creator)` so M2 deposit has membership FK.

### M2

| Table | Key columns |
|-------|-------------|
| `deposit_intents` | `id`, `user_id`, `group_id`, `expected_usdc_micros`, `status` (pending\|swept\|failed), `created_at` |
| `sweep_tx_log` | `signature` PK, `intent_id`, `group_id`, `user_id`, `usdc_micros`, `status`, `confirmed_at` |
| `share_ledger` | `group_id`, `user_id` PK, `shares` NUMERIC, `updated_at` |
| `member_net_usdc_in` | `group_id`, `user_id` PK, `net_micros` — increment on sweep credit |

NAV in M2: `domain.ComputePotNAV(NavUSDCOnly{TreasuryUsdc, TotalShares})`.

### M3

| Table | Key columns |
|-------|-------------|
| `jupiter_orders` | `id`, `group_id`, `side`, `input_mint`, `output_mint`, `notional_micros`, `status`, `idempotency_key` UNIQUE |
| `fills` | `id`, `order_id`, `signature` UNIQUE, `input_amount`, `output_amount`, `price_usdc`, `filled_at` |
| `cost_basis` | `group_id`, `symbol` PK, `units`, `avg_cost_usdc`, `updated_at` |

**M3 dev-only route** (deleted M4):

```
POST /v1/dev/groups/{groupId}/execute-buy
Body: { "symbol": "AAPLx", "usdc_micros": 10000000 }
→ swap.Buy directly, no proposal
```

### M4 (extends M1 groups, adds governance + snapshots)

| Table | Key columns |
|-------|-------------|
| `groups` + | `join_mode`, `join_password_hash`, `voter_set_mode`, `threshold`, `vote_expiry_seconds` |
| `group_members` | `group_id`, `user_id` PK, `joined_at` |
| `group_voters` | `group_id`, `user_id` — when named subset |
| `proposals` | `id`, `group_id`, `proposer_id`, `symbol`, `usdc_micros`, `status`, `expires_at`, `fill_signature` NULL |
| `votes` | `proposal_id`, `voter_id` PK, `choice`, `cast_at` |
| `nav_snapshots` | `id`, `group_id`, `pot_nav_micros`, `nav_per_share_micros`, `total_shares`, `reason`, `created_at` |
| `redeem_requests` | `id`, `group_id`, `user_id`, `shares`, `status`, `payout_pubkey`, `payout_sig` NULL |
| `payout_proofs` | `redeem_id`, `message`, `signature` — audit |

Marked NAV: treasury RPC balances + `pyth` marks → `domain.NavMarked`.

**M4 deletes:** `handler/dev/stub_buy.go`, route registration, mobile `DevSwap` from main flow.

### M5

No new tables. Swift `Codable` DTOs mirror M4 JSON responses.

---

## HTTP handlers (signatures)

```go
// M1
func (h *AuthHandler) PostSession(w http.ResponseWriter, r *http.Request)   // Privy token → user + member wallet
func (h *MeHandler) Get(w http.ResponseWriter, r *http.Request)
func (h *GroupsHandler) Post(w http.ResponseWriter, r *http.Request)          // M1 thin; M4 full CreateGroupInput
func (h *GroupsHandler) GetByID(w http.ResponseWriter, r *http.Request)

// M2
func (h *DepositsHandler) Post(w http.ResponseWriter, r *http.Request)       // start intent
func (h *DepositsHandler) GetByID(w http.ResponseWriter, r *http.Request)    // poll status + shares credited
func (h *GroupsHandler) GetShareBalance(w http.ResponseWriter, r *http.Request)
func (h *GroupsHandler) GetTreasuryUsdc(w http.ResponseWriter, r *http.Request)

// M3
func (h *DevStubBuyHandler) PostExecuteBuy(w http.ResponseWriter, r *http.Request) // DELETE M4
func (h *SwapHandler) GetOrder(w http.ResponseWriter, r *http.Request)
func (h *SwapHandler) GetCostBasis(w http.ResponseWriter, r *http.Request)

// M4
func (h *GroupsHandler) PostJoin(w http.ResponseWriter, r *http.Request)
func (h *CatalogHandler) GetSearch(w http.ResponseWriter, r *http.Request)    // xStocks symbols
func (h *ProposalsHandler) Post(w http.ResponseWriter, r *http.Request)
func (h *ProposalsHandler) GetByID(w http.ResponseWriter, r *http.Request)
func (h *VotesHandler) Post(w http.ResponseWriter, r *http.Request)
func (h *PortfolioHandler) GetGroup(w http.ResponseWriter, r *http.Request)  // pot, you, member board
func (h *PortfolioHandler) GetHome(w http.ResponseWriter, r *http.Request)   // group + people boards
func (h *RedeemHandler) Post(w http.ResponseWriter, r *http.Request)
```

---

## Per-milestone request flows

### M1 — Auth and wallets

```
Mobile Privy login
  → POST /v1/auth/session { privy_token }
  → privy.Verify → db.UpsertUser → privy.CreateMemberWallet (idempotent)
  → GET /v1/me { user_id, display_name, member_wallet_address }

Create group (thin)
  → POST /v1/groups { name }
  → db.InsertGroup → privy.CreateTreasuryWallet → db.InsertTreasury
  → db.InsertGroupMember(creator)
  → GET /v1/groups/{id} { name, treasury_address }
```

### M2 — Deposits and sweeps

```
POST /v1/groups/{id}/deposits { expected_usdc }
  → deposit_intents row (pending)

Background poller (or GET poll triggers tick)
  → treasury.MemberUsdcBalance > 0
  → treasury.SweepUsdcToTreasury → sweep_tx_log(signature)
  → RPC confirm signature
  → ledger.ApplySweepCredit(NavUSDCOnly) — idempotent on signature
       → domain.SharesForDeposit
       → share_ledger += shares
       → member_net_usdc_in += swept
       → nav_snapshots (reason=deposit) [optional M2; required M4-T37]

Reject path: USDC in member wallet but sweep not confirmed → zero share_ledger change
```

### M3 — Jupiter buy and sell (stub)

```
POST /v1/dev/groups/{id}/execute-buy { symbol, usdc_micros }  // no vote
  → xstocks.ResolveMint(symbol)
  → swap.PreviewBuy — 404 if no route
  → swap.Buy → poll Jupiter execute Success+0
  → fills + cost_basis rows (idempotent on signature)

Sell (backend-only prep for M4 redeem)
  → swap.SellSlice or swap.Buy inverse path for treasury xStock → USDC
```

### M4 — Domain logic

```
Create group (full)
  → governance.CreateGroup { join, voter set, threshold, expiry }

Join
  → POST /v1/groups/{id}/join { password? }
  → group_members row

Propose buy
  → POST /v1/proposals { group_id, symbol, usdc_micros }
  → swap.PreviewBuy must pass
  → proposals row (open), expires_at = now + group.vote_expiry

Vote
  → POST /v1/proposals/{id}/votes { choice }
  → domain.TallyProposal → update status
  → on passed: buy_on_pass.Execute
       → swap.Buy (treasury signs)
       → ledger.WriteSnapshot(fill)
       → link proposal.fill_signature
  → DELETE dev stub route

Portfolio
  → treasury.TreasuryBalances + pyth marks
  → domain.ComputePotNAV(NavMarked)
  → per-member domain.MemberEquity + domain.PercentReturn
  → domain.RankByPercentReturn (skip net_in=0)

Redeem (debit-first)
  → POST /v1/redeems { group_id, shares | usdc_target, payout_proof }
  → verify ed25519 ownership proof
  → ledger.DebitShares (row lock) → PotNAV at debit
  → domain.ComputeRedeemSlice
  → swap.SellSlice for stock fraction (skip if USDC-only pot)
  → treasury.SendUsdcPayout to verified pubkey only
  → ledger.RecordRedeemPayout → nav_snapshot(redeem)
  → boards recompute from net_usdc_in + current marks

Open decisions (M4-T39–T41): ticket files only, no branching logic chosen.
```

### M5 — Mobile product UI

```
Session gate → HomeBoardsView (group + people)
  → GroupDetailView (pot, you, member board, deposit, propose, redeem)
  → CreateGroupView / JoinGroupView
  → ProposalDetailView (vote yes/no, status chips)
  → RedeemView (slider, payout proof signer, dust min)
  → Settings/Advanced (explorer links, debug addresses)

MonacoAPIClient methods map 1:1 to M4 routes.
Display formatters unit-tested; no domain math in Swift.
```

---

## Swift API client (sketch)

```swift
// API/MonacoAPIClient.swift
final class MonacoAPIClient {
    init(baseURL: URL, tokenProvider: () async throws -> String)

    func openSession(privyAccessToken: String) async throws -> SessionDTO
    func me() async throws -> MeDTO

    func createGroup(_ req: CreateGroupRequest) async throws -> GroupDTO
    func joinGroup(id: UUID, password: String?) async throws
    func groupPortfolio(id: UUID) async throws -> GroupPortfolioDTO

    func startDeposit(groupId: UUID, expectedUsdc: Decimal) async throws -> DepositIntentDTO
    func depositStatus(id: UUID) async throws -> DepositIntentDTO

    func searchCatalog(query: String) async throws -> [CatalogSymbolDTO]
    func proposeBuy(groupId: UUID, symbol: String, usdc: Decimal) async throws -> ProposalDTO
    func castVote(proposalId: UUID, choice: VoteChoiceDTO) async throws -> ProposalDTO

    func homeBoards() async throws -> HomeBoardsDTO
    func redeem(_ req: RedeemRequestDTO) async throws -> RedeemResultDTO
}

// DTOs mirror Go json tags — example
struct GroupPortfolioDTO: Codable {
    let pot: PotDTO           // usdc + positions with mark, after_hours
    let you: MemberSliceDTO   // equity, pct_return, dollar_pnl
    let members: [LeaderboardRowDTO]
}
```

---

## Red-flag self-screen

| Flag | Check |
|------|-------|
| Shallow module | Handlers do not coordinate multi-step flows; `RedeemService.Complete` owns ordering. |
| Information leakage | Jupiter/Privy structs stay in `internal/jupiter`, `internal/privy`. HTTP uses DTOs. |
| Temporal decomposition | No `validate`/`persist` packages; ledger+governance own their knowledge. |
| Pass-through | No handler → db direct; services add policy (idempotency, NAV mode, proof verify). |

---

## Idempotency keys

| Operation | Key |
|-----------|-----|
| Sweep credit | Solana tx `signature` |
| Jupiter buy/sell | `signature` or Jupiter `execute_request_id` |
| Share debit | `redeem_requests.id` |
| Payout | Solana payout `signature` |
| Auth wallet provision | `privy_user_id` |

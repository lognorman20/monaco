# Monaco whole-shape sketch

Candidate: luna. Bodies intentionally omitted. `panic("not implemented")` marks signatures,
not implementation advice.

## Caller-first contract

### Backend callers

```go
func (h Handler) CreateGroup(w http.ResponseWriter, r *http.Request) {
    actor := h.sessions.RequiredUser(r.Context())
    input := decode[createGroupJSON](r)
    view, err := h.treasury.CreateGroup(r.Context(), actor, input.domain())
    writeResult(w, view, err)
}

func (h Handler) Redeem(w http.ResponseWriter, r *http.Request) {
    actor := h.sessions.RequiredUser(r.Context())
    input := decode[redeemJSON](r)
    result, err := h.treasury.Redeem(r.Context(), input.domain(actor))
    writeResult(w, result, err)
}

func (w Worker) SweepConfirmed(ctx context.Context, fact ChainSweep) error {
    _, err := w.treasury.ObserveSweep(ctx, fact.domain())
    return err
}
```

The HTTP contract is:

```text
POST /v1/auth/session
GET  /v1/me
POST /v1/groups
POST /v1/groups/{group}/join
GET  /v1/groups/{group}
POST /v1/groups/{group}/deposits
GET  /v1/deposits/{deposit}
GET  /v1/groups/{group}/assets?query=...
POST /v1/groups/{group}/proposals
POST /v1/proposals/{proposal}/votes
POST /v1/groups/{group}/redeems
GET  /v1/home
```

M3-only development seam, deleted by M4:

```text
POST /v1/dev/groups/{group}/buy
```

### Swift callers

Swift owns wire-shaped `Codable` structs, but screens call one API method per user action:

```swift
let home = try await api.home()
let created = try await api.createGroup(CreateGroupRequest(
    name: name, joinPolicy: joinPolicy, voterSet: voterSet,
    threshold: threshold, expiry: expiry
))
let proposal = try await api.proposeBuy(
    groupID: groupID, request: BuyProposalRequest(symbol: symbol, usdc: amount)
)
let result = try await api.redeem(
    groupID: groupID,
    request: RedeemRequest(shares: shares, payoutProof: proof)
)
```

Screens render `HomeResponse`, `GroupResponse`, `ProposalResponse`, and
`RedeemResponse`; they never calculate share price, pot NAV, rank, or net USDC in.

## Domain types

Go package: `packages/domain`. All constructors return `(T, error)` and reject invalid
precision, negative quantities, wrong mints, zero IDs, and malformed signatures.

```go
type UserID uuid.UUID
type GroupID uuid.UUID
type DepositID uuid.UUID
type ProposalID uuid.UUID
type RedeemID uuid.UUID
type PrivyUserID string

type USDC struct { Atomic uint64 }             // fixed USDC mint and 6 decimals
type ShareUnits struct { Atomic uint64 }       // fixed share scale
type StockUnits struct { Atomic uint64 }      // asset-specific token scale
type PotNAV struct { Value USDC }
type NavPerShare struct { Value USDC }
type PercentReturn struct { BasisPoints int64 }
type SolanaAddress string
type TxSignature string
type AssetSymbol string
type AssetMint SolanaAddress

type User struct { ID UserID; PrivyID PrivyUserID; DisplayName *string }
type WalletRef struct {
    Owner UserID
    PrivyWalletID string
    Address SolanaAddress
}
type TreasuryRef struct {
    Group GroupID
    PrivyWalletID string
    Address SolanaAddress
}

type JoinPolicy interface{ isJoinPolicy() }
type OpenJoin struct{}
type PasswordJoin struct{ PasswordHash []byte }

type VoterSet interface{ isVoterSet() }
type EveryMember struct{}
type NamedVoters struct{ Users NonEmptySet[UserID] }
type VoteThreshold interface{ isVoteThreshold() }
type Unanimous struct{}
type Majority struct{}

type GroupRules struct {
    Join JoinPolicy
    Voters VoterSet
    Threshold VoteThreshold
    VoteExpiry time.Duration
}
type ProposalPermissionRule interface{ isProposalPermissionRule() }
type UndecidedProposalPermission struct{}
type AnyMemberMayPropose struct{}       // only after product decision
type VoterSetMayPropose struct{}        // only after product decision
type CreatorMayPropose struct{}         // only after product decision

type Group struct {
    ID GroupID; Name string; Creator UserID
    Rules GroupRules
    ProposalPermission ProposalPermissionRule
}
type Membership struct { Group GroupID; User UserID; JoinedAt time.Time }

type DepositIntent struct {
    ID DepositID; User UserID; Group GroupID; Expected USDC
    Status DepositStatus
}
type DepositStatus interface{ isDepositStatus() }
type AwaitingMemberFunds struct{}
type SweepPending struct{ Signature *TxSignature }
type SharesCredited struct{ Signature TxSignature; Shares ShareUnits }

type ObservedSweep struct {
    Signature TxSignature
    From WalletRef
    To TreasuryRef
    Amount USDC
    ConfirmedAt time.Time
}
type ShareBalance struct { Group GroupID; User UserID; Units ShareUnits }

type Holding struct {
    Mint AssetMint; Symbol AssetSymbol; Units StockUnits
    Mark USDC; CostBasis USDC; MarkState MarkState
}
type MarkState interface{ isMarkState() }
type CurrentMark struct{ AsOf time.Time }
type FrozenEquityMark struct{ AsOf time.Time }
type Portfolio struct { TreasuryUSDC USDC; Holdings []Holding }
type NavSnapshot struct {
    Group GroupID; At time.Time; Reason SnapshotReason
    Portfolio Portfolio; TotalShares ShareUnits
    Pot PotNAV; PerShare NavPerShare
}

type BuyProposal struct {
    ID ProposalID; Group GroupID; Proposer UserID
    Symbol AssetSymbol; Mint AssetMint; Amount USDC
    ExpiresAt time.Time; Status ProposalStatus
}
type ProposalStatus interface{ isProposalStatus() }
type OpenProposal struct{}
type PassedProposal struct{ PassedAt time.Time }
type FailedProposal struct{ Reason FailureReason }
type ExpiredProposal struct{ At time.Time }
type VoteChoice interface{ isVoteChoice() }
type Yes struct{}; type No struct{}
type Vote struct { Proposal ProposalID; User UserID; Choice VoteChoice }

type PayoutProof struct {
    Address SolanaAddress; Message string; Signature []byte
    VerifiedFor UserID
}
type RedeemRequest struct {
    ID RedeemID; User UserID; Group GroupID
    Shares ShareUnits; Payout PayoutProof
}
type RedeemResult struct {
    ID RedeemID; SharesDebited ShareUnits; Payout USDC
    Signature *TxSignature; Status RedeemStatus
}
type RedeemStatus interface{ isRedeemStatus() }
type RedeemPending struct{}
type RedeemPaid struct{ Signature TxSignature }
type RedeemFailed struct{ Reason FailureReason }

type Equity struct {
    Shares ShareUnits; ShareOfPot Percent
    Value USDC; NetUSDCIn USDC; DollarPnL SignedUSDC
    Return PercentReturn
}
type MemberBoardRow struct { User User; Equity Equity }
type GroupBoardRow struct { Group GroupID; Name string; Equity Equity }
type PeopleBoardRow struct { User User; Equity Equity }
type GroupView struct {
    Group Group; Portfolio Portfolio; Snapshot NavSnapshot
    Viewer *Equity; Members []MemberBoardRow; Proposals []BuyProposal
}
type HomeView struct {
    Groups []GroupBoardRow
    People []PeopleBoardRow
}
```

`NonEmptySet`, `Percent`, `SignedUSDC`, `FailureReason`, and `SnapshotReason` are domain
types with constructors. Boards contain only rows with `NetUSDCIn > 0`; member rows also
require positive share balance. Ranking is a pure sort by `PercentReturn`, not dollars.

## Pure domain signatures

```go
func SharePrice(portfolio Portfolio, shares ShareUnits) (NavPerShare, error)
func SharesForDeposit(amount USDC, price NavPerShare) (ShareUnits, error)
func PotValue(portfolio Portfolio) (PotNAV, error)
func EquityFor(shares ShareUnits, total ShareUnits, pot PotNAV) (USDC, error)
func ReturnFor(equity USDC, netIn USDC) (PercentReturn, error)
func TallyProposal(p BuyProposal, rules GroupRules, members []Membership,
    votes []Vote, now time.Time) (ProposalStatus, error)
func PlanRedeem(req RedeemRequest, snapshot NavSnapshot,
    holdings []Holding) (RedeemPlan, error)
func GroupBoard(rows []GroupBoardRow) []GroupBoardRow
func PeopleBoard(rows []PeopleBoardRow) []PeopleBoardRow
```

`SharePrice` returns $1 only when total shares are zero; otherwise it returns marked pot
NAV divided by total shares. M2 passes a portfolio with USDC only. M4 passes current
USDC plus xStock marks. `PlanRedeem` computes `shares / total shares * pot NAV`, identifies
the proportional stock sale, and never computes a deposit refund.

## Backend module map

```text
apps/backend/
  cmd/api/                       startup, config, route registration
  internal/httpapi/               auth, decode, encode, error mapping only
  internal/treasury/              deep Treasury capability and command orchestration
  internal/treasury/ports.go      Repository, Wallets, SwapRail, Marks, Clock
  internal/postgres/               SQL repository and transaction/row-lock implementation
  internal/privy/                  Wallets implementation
  internal/jupiter/                SwapRail implementation; execute polling
  internal/xstocks/                catalog/mint resolver behind SwapRail
  internal/pyth/                   Marks implementation
  internal/worker/                 balance polling and confirmed sweep observation
  internal/wire/                   private HTTP DTOs
```

`Treasury` is constructed with ports:

```go
type TreasuryService struct {
    repo Repository
    wallets Wallets
    swaps SwapRail
    marks Marks
    clock Clock
}
func NewTreasuryService(deps Dependencies) Treasury { panic("not implemented") }
```

Ports expose domain facts, not SQL rows or Jupiter/Privy structs:

```go
type Repository interface {
    InTx(ctx context.Context, fn func(TxRepository) error) error
    LoadGroup(ctx context.Context, id GroupID) (Group, error)
    GroupViewData(ctx context.Context, viewer UserID, id GroupID) (ViewData, error)
    HomeViewData(ctx context.Context, viewer UserID) (HomeData, error)
}
type TxRepository interface {
    UpsertSession(User) (User, error)
    CreateGroup(Group, TreasuryRef) (Group, error)
    CreateDeposit(DepositIntent) (DepositIntent, error)
    ApplyConfirmedSweep(ObservedSweep, ShareCredit) (DepositStatus, error)
    CastVote(Vote) (ProposalViewData, error)
    DebitSharesFirst(RedeemRequest, RedeemPlan) (DebitReceipt, error)
    RecordPayoutAndSnapshot(RedeemResult, NavSnapshot) error
}
type Wallets interface {
    VerifySession(PrivyToken) (PrivyIdentity, error)
    EnsureMemberWallet(UserID) (WalletRef, error)
    EnsureTreasury(GroupID) (TreasuryRef, error)
    VerifyPayoutProof(UserID, PayoutProof) (PayoutProof, error)
    SubmitSweep(DepositIntent, WalletRef, TreasuryRef) (TxSignature, error)
    PayUSDC(TreasuryRef, SolanaAddress, USDC) (TxSignature, error)
}
type SwapRail interface {
    Search(AssetQuery) ([]Asset, error)
    QuoteBuy(TreasuryRef, Asset, USDC) (BuyQuote, error)
    ExecuteBuy(TreasuryRef, BuyQuote, ProposalID) (Fill, error)
    SellToUSDC(TreasuryRef, HoldingSlice) (Fill, error)
}
type Marks interface {
    USDCOnly(TreasuryRef) (Portfolio, error)
    Marked(TreasuryRef, []CostBasis) (Portfolio, error)
}
```

The real repository adds tables and locks privately: users/member wallets/groups/treasuries
(M1); deposit intents, signature-keyed sweep log, and share ledger (M2); orders, fills, and
cost basis (M3); group members, rules, proposals/votes, NAV snapshots, payout proofs, and
redeem transitions (M4). `share_ledger` stores tickets, never dollars. Net USDC in derives
from confirmed sweeps minus confirmed payout records.

## Per-milestone flow

### M0 — scaffold only

`cmd/api` exposes health, mobile target builds, local Docker Postgres and migration runner
start. No domain tables, wallet calls, or product routes. `packages/domain` directory may
exist as a Go module boundary, but contains no product behavior.

### M1 — identity and wallet ownership

1. Handler authenticates Privy token through `Wallets.VerifySession`.
2. `Treasury.Session` runs one repository transaction: upsert user, ensure member wallet.
3. `CreateGroup` validates name and thin group identity, provisions one server treasury,
   then atomically persists group/treasury mapping.
4. `GET /me` and group detail return API response DTOs; address visibility is temporary M1
   debug behavior and not a domain permission.

M1 persists only `users`, `member_wallets`, `groups`, and `treasuries`; relayer key is config.

### M2 — deposit, sweep, USDC-only NAV

1. Mobile creates an intent; `CreateDeposit` verifies membership and positive USDC.
2. Worker polls member wallet balances and builds a sweep only for the intent's member wallet.
3. After on-chain confirmation, worker sends `ObservedSweep` with signature, source, exact
   treasury destination, and amount.
4. Repository transaction inserts signature once, locks group share state, calls
   `SharePrice(Marks.USDCOnly(...))`, credits `amount / price`, and writes a deposit NAV
   snapshot. Replaying signature returns prior credit.
5. Member-wallet-only USDC has no `ObservedSweep`, therefore no shares.

### M3 — real Jupiter rail, temporary dev buy

1. Asset search resolves xStocks metadata to a Solana mint; no mint means no asset.
2. Quote requires USDC input and xStock output; no route returns a refusal and creates no
   order.
3. Dev-only buy calls `SwapRail.QuoteBuy`, signs via the treasury wallet, POSTs execute, and
   polls until `Success` and `code == 0`; only then records fill and cost basis.
4. Sell uses the same rail for treasury xStock to USDC and records proceeds.
5. Signature/request idempotency prevents duplicate fill/cost-basis effects.

No vote exists in M3. The route is explicitly marked for deletion.

### M4 — rules, Postgres votes, marked NAV, redeem, boards

1. Group creation persists `GroupRules`; join validates open/password policy and adds a
   membership row. One user can have many membership rows.
2. Proposal creation resolves catalog and quote before inserting an open proposal. It calls
   `ProposalPermissionRule`; `Undecided` returns a product-decision error, not an invented
   permission. This keeps the open README decision visible.
3. Vote writes occur in Postgres. A transaction locks the proposal, rejects non-voters,
   duplicate votes, and late votes, then `TallyProposal` produces passed/failed/expired.
4. Only the passed transition calls the Jupiter buy adapter. M3 dev route and command are
   deleted. Confirmed fill updates holdings, cost basis, and marked NAV snapshot.
5. Group and home reads use `Marks.Marked`, persisted snapshots, share tickets, and derived
   net USDC in. Pyth frozen marks carry `FrozenEquityMark` for after-hours UI.
6. Redeem verifies `PayoutProof`, computes `PlanRedeem`, and enters a Postgres transaction
   that locks the member ledger row and debits it first. The executor sells the proportional
   holdings, pays USDC only to the verified address, then records payout and NAV snapshot.
   Existing redeem ID/signature returns the previous result. No tokenized stock is paid out.

The failed-Jupiter-after-pass result remains a typed terminal/pending state for the unresolved
product decision; no retry policy is selected here. Creator leave/dissolve remains absent.

### M5 — Swift product projection

1. Session gate calls `/me`, then home calls `/home`; wallet addresses stay out of main copy.
2. Create/join screens submit group rules and password where required.
3. Deposit screen shows intent/sweep status, not member-wallet balance as credited money.
4. Search/propose/vote screens render server status and refuse no-route assets.
5. Group screen renders holdings, slice, dollar/percent P&L, and in-group board; home renders
   group and people boards from the same response model.
6. Redeem collects signed payout proof, submits one command, and refreshes group/home after
   success. Settings/Advanced alone may expose explorer links.

Swift never imports Go, SQL, Jupiter, Privy server, `NAV`, wallet, gas, seed phrase, or mint
implementation concepts into product copy.

## Red-flag screen

- **Shallow module:** one `Treasury` command returns each complete operation; callers do not
  coordinate repository, mark, wallet, and swap methods.
- **Information leakage:** ports carry domain facts only; SQL rows, wire DTOs, Jupiter payloads,
  and Privy IDs stay behind adapters. Swift has independent Codable types.
- **Temporal decomposition:** modules own knowledge (ledger, wallet, swap, marks), not
  `validate -> transform -> save` stages. One transaction boundary protects each transition.
- **Pass-through methods:** handlers decode/authenticate and call one capability; workers adapt
  chain facts. No service simply forwards a same-shaped repository method.

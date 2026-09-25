# Sketch: ledger-first group kernel

Call sites in `rationale.md` win if this file drifts. Bodies stay `not implemented`. No proposer / failed-execute-retry / dissolve policy.

## 1. Module map

```
monaco/
  apps/backend/                 Go API shell
    main.go                     mux, config, startup (relayer keys required M1+)
    internal/httpapi/           decode/encode JSON, authn header, status codes
    internal/identity/          users, member wallets, treasuries, session upsert
    internal/kernel/            pot economics: credit, votes, NAV, boards, redeem
    internal/deposits/          intent row + poller loop (calls kernel.CreditSweep)
    internal/buytrigger/        port; stub.go (M3); passed.go (M4); stub deleted M4
    internal/privy/             WalletPort adapter (wire types stay here)
    internal/jupiter/           SwapPort adapter
    internal/xstocks/           CatalogPort adapter
    internal/pyth/              MarksPort adapter
    internal/chain/             RPC confirm + USDC mint constant
    internal/db/                pgx, migrations already applied by just
  packages/domain/              Go module imported by API + tests only
    atoms.go                    USDC, Shares, Price
    valuation.go                USDCOnly | Marked
    ledger.go                   Credit, Debit, Equity, CashEvent
    vote.go                     Tally, clock, statuses
    board.go                    PercentReturn, ranked rows
    redeem.go                   Slice, RedeemAmount
  apps/mobile/                  SwiftUI iOS 18+, slim sim
    API/MonacoClient.swift      hand-written Codable = Go JSON
    API/DTOs.swift              screen + command types (no Jupiter fields)
    Auth/PrivySession.swift
    Home/, Group/, Settings/
  supabase/migrations/          additive SQL; M4 does not recreate M1/M2 tables
```

HTTP is the only Go↔Swift contract. `packages/domain` is never imported from Swift.

Public Go surface (deep):

- `identity.Service` — session, wallets, thin then full group create
- `kernel.Service` — membership, credit, proposals, tally, screens, redeem
- Ports: `WalletPort`, `SwapPort`, `CatalogPort`, `MarksPort`, `BuyTrigger`

Not public: Jupiter execute JSON, Privy wallet payloads, `pgx.Row`, poller internals.

## 2. Caller's signatures (HTTP)

Auth header: `Authorization: Bearer <privy-access-token>` on all `/v1/*` except `GET /health`.

| Method | Path | Returns | Milestone |
| --- | --- | --- | --- |
| GET | `/health` | `{ "ok": true }` | M0 |
| POST | `/v1/auth/session` | `Me` | M1 |
| GET | `/v1/me` | `Me` | M1 |
| POST | `/v1/groups` | `Group` | M1 thin / M4 full body |
| GET | `/v1/groups/{id}` | `GroupScreen` | M1 address fields; M4 product payload |
| POST | `/v1/groups/{id}/join` | `GroupScreen` | M4 |
| POST | `/v1/groups/{id}/deposits` | `DepositIntent` | M2 |
| GET | `/v1/groups/{id}/deposits/{intentID}` | `DepositIntent` | M2 |
| GET | `/v1/groups/{id}/treasury` | `{ usdc, tokens[] }` | M2/M3 debug; M5 unused on main flow |
| GET | `/v1/catalog?q=` | `{ assets[] }` | M4 (M3 may use for mint resolve) |
| POST | `/v1/groups/{id}/quotes` | `Quote` or 409 no-route | M3/M4 |
| POST | `/v1/groups/{id}/proposals` | `Proposal` | M4 |
| POST | `/v1/proposals/{id}/votes` | `Proposal` | M4 |
| GET | `/v1/home` | `Home` | M4 |
| POST | `/v1/groups/{id}/redeems` | `RedeemJob` | M4 |
| POST | `/v1/dev/execute-buy` | `Order` | **M3 only; delete in M4** |

Open decisions: no `/dissolve`, no proposerRole field, no retryExecute route.

## 3. Domain types (invariants)

```go
package domain

var ErrNotImplemented = errors.New("not implemented")

// USDC mint is a typed constant, not a string at call sites.
type Mint string

const USDCMint Mint = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"

// Atoms: USDC 6 decimals. Shares use the same scale so $1 empty-group price
// credits 1 share per 1 USDC without floats.
type USDC int64
type Shares int64

func USDCFromDecimal(s string) (USDC, error) { return 0, ErrNotImplemented }
func (u USDC) Add(v USDC) USDC               { panic("not implemented") }

type UserID string
type GroupID string
type ProposalID string
type Sig string // Solana tx signature
type ExecuteID string

// SharePrice is USDC per one whole share (1e6 share atoms).
type SharePrice struct{ USDCPerShare USDC }

var FirstSharePrice = SharePrice{USDCPerShare: 1_000_000} // $1

type Supply struct{ Total Shares }

func EmptySupply() Supply { return Supply{Total: 0} }

func (s Supply) IsEmpty() bool { return s.Total == 0 }

// Valuation is the only way to read pot NAV / share price.
// M2 constructs USDCOnly. M4 constructs Marked. No bool "includeTokens".
type Valuation interface {
	PotNAV() (USDC, error)
	SharePrice(s Supply) (SharePrice, error)
}

type USDCOnly struct{ Treasury USDC }

func (USDCOnly) PotNAV() (USDC, error)                    { return 0, ErrNotImplemented }
func (USDCOnly) SharePrice(Supply) (SharePrice, error)    { return SharePrice{}, ErrNotImplemented }
// TODO empty supply → FirstSharePrice even if Treasury==0
// TODO non-empty → Treasury / Total (integer division policy: document remainder)

type Position struct {
	Mint      Mint
	Units     int64 // token atoms from chain; decimals per mint stay in adapter
	CostBasis USDC  // Jupiter fill; never a live mark
	Mark      USDC  // units * mark price, dollars in USDC atoms
	AfterHours bool
}

type Marked struct {
	Treasury  USDC
	Positions []Position
}

func (Marked) PotNAV() (USDC, error)                 { return 0, ErrNotImplemented }
func (Marked) SharePrice(Supply) (SharePrice, error) { return SharePrice{}, ErrNotImplemented }

func Credit(in USDC, px SharePrice) (Shares, error) { return 0, ErrNotImplemented }
// shares credited = USDC swept in / NAV per share  (README)

func Equity(member Shares, total Supply, pot USDC) (USDC, error) {
	return 0, ErrNotImplemented
}
// your equity = (your shares / total shares) × pot NAV
// TODO total empty → error, not zero equity with leftover USDC

type CashKind uint8

const (
	CashSweepIn CashKind = iota
	CashRedeemOut
)

type CashEvent struct {
	User  UserID
	Group GroupID
	Kind  CashKind
	USDC  USDC
	Key   string // "sweep:"+sig or "redeem:"+id
}

func NetUSDCIn(events []CashEvent) USDC { panic("not implemented") }
// per member per group: sweeps credited − redeem payouts
// people board: sum of those. Never a writable net_usdc_in field.

type PercentReturn struct{ Millionths int64 } // (equity/netIn - 1)

func Return(equity, netIn USDC) (PercentReturn, bool) {
	// false when netIn==0: caller skips the row. Cannot be 0% club.
	return PercentReturn{}, false
}

type MemberBoardRow struct {
	User     UserID
	Name     string
	Equity   USDC
	DollarPnL USDC
	Pct      PercentReturn
}

func RankMembers(rows []MemberBoardRow) []MemberBoardRow { return nil } // percent desc, not dollars

type JoinPolicy interface{ joinPolicy() }

type OpenJoin struct{}

func (OpenJoin) joinPolicy() {}

type PasswordJoin struct{ ArgonHash []byte } // hash at HTTP edge; domain never sees plaintext

func (PasswordJoin) joinPolicy() {}

type VoterSet interface{ voterSet() }

type EveryMember struct{}

func (EveryMember) voterSet() {}

type NamedVoters struct{ IDs []UserID } // len >= 1, may be creator only

func (NamedVoters) voterSet() {}

type Threshold uint8

const (
	Unanimous Threshold = iota
	Majority
)

type VoteChoice uint8

const (
	VoteYes VoteChoice = iota
	VoteNo
)

type ProposalStatus uint8

const (
	ProposalOpen ProposalStatus = iota
	ProposalPassed
	ProposalFailed
	ProposalExpired
)

type TallyInput struct {
	VoterSet  VoterSet
	Threshold Threshold
	Cast      map[UserID]VoteChoice
	Now       time.Time
	Deadline  time.Time
}

func Tally(in TallyInput) (ProposalStatus, error) { return 0, ErrNotImplemented }
// unanimous among voter set OR majority among voter set
// open + now>=deadline → Expired, no swap
// votes from non-voters: error, not ignored-as-no

type RedeemDebit struct {
	User   UserID
	Group  GroupID
	Shares Shares // dust min checked at kernel, not here
}

type Slice struct {
	USDC USDC // sharesRedeemed/totalShares × potNAV  — not a deposit refund
}

func RedeemSlice(debit Shares, total Supply, pot USDC) (Slice, error) {
	return Slice{}, ErrNotImplemented
}

type RedeemStatus uint8

const (
	RedeemDebited RedeemStatus = iota
	RedeemSelling
	RedeemPaying
	RedeemSettled
	RedeemFailedAfterDebit // no auto policy; visible for operators; not a README pick
)

type RedeemJob struct {
	ID     string
	Status RedeemStatus
	Debit  RedeemDebit
	Slice  Slice
}

func NewRedeemAfterDebit(id string, d RedeemDebit, sl Slice) RedeemJob {
	return RedeemJob{ID: id, Status: RedeemDebited, Debit: d, Slice: sl}
}
```

No `ProposerRole` type. No `DissolveCommand`. Execution after pass:

```go
type ExecAttempt struct {
	Proposal ProposalID
	Status   ExecStatus // Submitted | Success | TerminalFail
	Sig      Sig
	Execute  ExecuteID
}

const (
	ExecSubmitted ExecStatus = iota
	ExecSuccess              // Jupiter status Success AND code 0
	ExecTerminalFail         // persist; do not invent retry/refund
)
```

## 4. Kernel and identity signatures

```go
package identity

type Me struct {
	UserID         domain.UserID
	DisplayName    string
	MemberAddress  string // M1 proof; M5 Settings only
}

type Service struct{ /* wallets WalletPort; db */ }

func (Service) Session(ctx context.Context, privyJWT string) (Me, error) {
	return Me{}, domain.ErrNotImplemented
	// verify token → upsert users on privy_user_id → ensure member_wallets row
}

func (Service) CreateGroup(ctx context.Context, actor domain.UserID, in CreateGroup) (Group, error) {
	return Group{}, domain.ErrNotImplemented
	// M1: name only + treasury wallet
	// M4: name + JoinPolicy + VoterSet + Threshold + expiry duration; creator membership row
}

type Group struct {
	ID               domain.GroupID
	Name             string
	TreasuryAddress  string // M1 proof; M5 advanced
}

type WalletPort interface {
	VerifyAccessToken(ctx context.Context, jwt string) (privyUserID string, err error)
	EnsureMemberWallet(ctx context.Context, privyUserID string) (privyWalletID, solanaAddr string, err error)
	CreateTreasury(ctx context.Context) (privyWalletID, solanaAddr string, err error)
	USDCBalance(ctx context.Context, addr string) (domain.USDC, error)
	SignSend(ctx context.Context, privyWalletID string, relayerFeePayer bool, tx []byte) (domain.Sig, error)
}
```

```go
package kernel

type Service struct {
	DB     Store
	Wallets identity.WalletPort
	Swap   SwapPort
	Catalog CatalogPort
	Marks  MarksPort
	Trigger BuyTrigger
}

type SwapPort interface {
	QuoteBuy(ctx context.Context, usdc domain.USDC, out domain.Mint) (Quote, error) // ErrNoRoute
	QuoteSell(ctx context.Context, in domain.Mint, fractionUnits int64) (Quote, error)
	Execute(ctx context.Context, q Quote, treasuryPrivyID string) (Fill, error)
}

type Quote struct {
	InMint, OutMint domain.Mint
	InUSDC          domain.USDC
	Routable        bool
}

type Fill struct {
	Sig        domain.Sig
	ExecuteID  domain.ExecuteID
	InAmount   int64
	OutAmount  int64
	PriceUSDC  domain.USDC // cost basis
}

type CatalogPort interface {
	Search(ctx context.Context, q string) ([]Asset, error)
	SolanaMint(ctx context.Context, symbol string) (domain.Mint, error)
}

type MarksPort interface {
	Mark(ctx context.Context, mint domain.Mint, units int64) (domain.Position, error)
}

type BuyTrigger interface {
	// M3 StubTrigger: allow if process env allows dev stub.
	// M4 PassedTrigger: allow iff proposal Passed and no successful fill for proposal id.
	Allow(ctx context.Context, group domain.GroupID, reason BuyReason) error
}

type BuyReason struct {
	Proposal *domain.ProposalID // nil only on M3 stub
	Symbol   string
	USDC     domain.USDC
}

func (Service) Join(ctx context.Context, actor domain.UserID, g domain.GroupID, password *string) error {
	return domain.ErrNotImplemented
}

func (Service) OpenDeposit(ctx context.Context, actor domain.UserID, g domain.GroupID, expect domain.USDC) (DepositIntent, error) {
	return DepositIntent{}, domain.ErrNotImplemented
}

func (Service) CreditSweep(ctx context.Context, sig domain.Sig, intent DepositIntent, swept domain.USDC, val domain.Valuation, supply domain.Supply) error {
	return domain.ErrNotImplemented
	// 1. insert sweep_tx_log on signature (no-op if exists)
	// 2. if already credited for this sig: return nil
	// 3. shares = Credit(swept, val.SharePrice(supply))
	// 4. upsert share_ledger; insert cash_events sweep
	// 5. nav_snapshots (M4; M2 may write USDC-only snapshot or skip until M4-T37)
	// never credit from member-wallet balance alone
}

func (Service) Propose(ctx context.Context, actor domain.UserID, g domain.GroupID, symbol string, usdc domain.USDC) (Proposal, error) {
	return Proposal{}, domain.ErrNotImplemented
	// membership required. Finer who-may-propose is unset (M4-T39).
	// Catalog.SolanaMint; Swap.QuoteBuy; refuse ErrNoRoute; persist open + deadline
}

func (Service) Vote(ctx context.Context, actor domain.UserID, p domain.ProposalID, c domain.VoteChoice) (Proposal, error) {
	return Proposal{}, domain.ErrNotImplemented
	// persist vote unique (proposal, user); Tally; if Passed → executeBuy
}

func (Service) executeBuy(ctx context.Context, p Proposal) error {
	return domain.ErrNotImplemented
	// Trigger.Allow; Jupiter order USDC→mint; Privy treasury sign; poll Success code 0
	// idempotent on proposal_id + sig/execute_id; fill → cost_basis; NAV snapshot
}

func (Service) SubmitRedeem(ctx context.Context, actor domain.UserID, g domain.GroupID, amount RedeemAmount, proof PayoutProof) (domain.RedeemJob, error) {
	return domain.RedeemJob{}, domain.ErrNotImplemented
	// verify proof (reject attacker pubkey)
	// BEGIN; LOCK share_ledger row; Debit; insert redeem job Debited; COMMIT
	// then sell slice of each xStock if any; pay USDC only to proven address
	// cash_events redeem; NAV snapshot; never stock in kind
}

type RedeemAmount struct {
	Shares *domain.Shares
	USDC   *domain.USDC // dollar target converted with current marked price
}

type PayoutProof struct {
	Address string
	Message []byte
	Sig     []byte
}

func (Service) GroupScreen(ctx context.Context, actor domain.UserID, g domain.GroupID) (GroupScreen, error) {
	return GroupScreen{}, domain.ErrNotImplemented
}

func (Service) Home(ctx context.Context, actor domain.UserID) (Home, error) {
	return Home{}, domain.ErrNotImplemented
}
```

Screen DTOs (JSON names match Swift `Codable`; dollars as decimal strings):

```go
type GroupScreen struct {
	GroupID     domain.GroupID
	Name        string
	Join        string // "open" | "password" — not hash
	Pot         []PotRow
	You         You
	MemberBoard []domain.MemberBoardRow
	Proposal    *Proposal
}

type PotRow struct {
	Symbol     string
	IsUSDC     bool
	Units      string
	MarkUSD    string
	ValueUSD   string
	AfterHours bool
}

type You struct {
	Shares        string
	EquityUSD     string
	SlicePercent  string
	DollarPnL     string
	PercentReturn *string // null when net USDC in == 0
}

type Home struct {
	Groups []GroupBoardRow // skip net USDC in == 0; rank by percent
	People []PeopleBoardRow
}
```

## 5. Access patterns (structure must answer these)

1. **Sweep credit.** Key = signature. Lookup `sweep_tx_log`; if credited, stop. Else lock `(group_id, user_id)` share row, apply `Credit`, append cash event. No later “rebuild from chain” for share counts.
2. **Group screen.** Load ledger rows + treasury balances + marks + cash events for this group. One `Valuation`, one `Supply`. Rank in process. No N+1 NAV endpoints.
3. **People board.** Sum equity and net USDC in across memberships per user. Skip net 0. One query for cash events grouped by user, one for share rows; merge in kernel.
4. **Vote pass → swap.** Tally pure. Execute once per proposal via unique `(proposal_id)` success fill. Replay of same sig does not double cost basis.
5. **Redeem.** Debit is the commit point. Restart loads job `Debited`/`Selling`/`Paying` and continues; does not debit again (`idempotent transitions`).

If a cache appears necessary for boards, the cash_event + share_ledger shape is wrong.

## 6. Postgres models (additive)

**M0** `schema_migrations` only (already in repo).

**M1**

- `users(id uuid pk, privy_user_id text unique not null, display_name text, created_at timestamptz)`
- `member_wallets(id uuid pk, user_id uuid unique references users, privy_wallet_id text not null, solana_address text not null, created_at timestamptz)`
- `groups(id uuid pk, name text not null, creator_user_id uuid references users, created_at timestamptz)`
- `treasuries(id uuid pk, group_id uuid unique references groups, privy_wallet_id text not null, solana_address text not null, created_at timestamptz)`
- Relayer: process config, not a table. Startup fails if missing.

**M2**

- `deposit_intents(id, user_id, group_id, expected_usdc_atoms bigint, status text, created_at)`
- `sweep_tx_log(signature text pk, intent_id, from_address, to_address, usdc_atoms bigint, status text, created_at)`
- `share_ledger(group_id, user_id, share_atoms bigint, primary key (group_id, user_id))`
- `share_entries(id, group_id, user_id, delta_share_atoms bigint, cause text unique, created_at)` — cause `sweep:<sig>`
- `cash_events(id, group_id, user_id, kind text, usdc_atoms bigint, cause text unique, created_at)`

**M3**

- `jupiter_orders(id, group_id, proposal_id null, side text, input_mint, output_mint, notional_atoms, status)`
- `fills(id, order_id, execute_id text, signature text, in_amount, out_amount, price_usdc_atoms, unique(signature), unique(execute_id))`
- `cost_basis(group_id, mint, units, usdc_atoms, primary key (group_id, mint))` — from fill, not mark

**M4** (do not recreate M1/M2 tables)

- `groups` columns added: `join_policy text`, `password_hash text null`, `voter_mode text`, `threshold text`, `expiry_seconds int`
- `group_voters(group_id, user_id, primary key)` for named subset
- `group_members(group_id, user_id, joined_at, primary key)` — one user, many groups
- `proposals(id, group_id, proposer_user_id, symbol, output_mint, usdc_atoms, status, deadline, created_at)`
- `votes(proposal_id, user_id, choice, created_at, primary key (proposal_id, user_id))`
- `nav_snapshots(id, group_id, reason text /* deposit|fill|redeem */, pot_usdc_atoms, total_share_atoms, at timestamptz)`
- `payout_proofs(id, user_id, address, verified_at)`
- `redeem_jobs(id, group_id, user_id, share_atoms, slice_usdc_atoms, payout_address, status, created_at)`

M2 NAV: `USDCOnly{treasury USDC}`. M4: `Marked` with positions × Pyth/Jupiter mark; fill remains cost basis.

## 7. Per-milestone flow

**M0.** `GET /health`. Empty `packages/domain` module path. Compose Postgres `monaco` / port `54322`. No kernel methods.

**M1.** Session → upsert user → member wallet. Create group → treasury server wallet. Relayer config load. Screens may show addresses. No shares, no join policy columns.

**M2.** Intent → poll member USDC (no Privy webhooks) → relayer-paid sweep to treasury → confirm sig → `CreditSweep` with `USDCOnly`. Member-wallet-only balance: zero `share_entries`. Duplicate sig: one credit. Empty group: `FirstSharePrice`.

**M3.** Catalog mint resolve. Quote refuse no-route. Stub `POST /v1/dev/execute-buy` → `StubTrigger.Allow` → treasury sign → poll `/execute` Success code 0 → fill + cost basis. Sell path for later redeem. Idempotent fill on signature. No votes.

**M4.** Alter groups; members; proposals; votes. `PassedTrigger`. Delete stub route and `StubTrigger`. Tally in domain. On pass, same execute as M3 but reason.Proposal set. Marked NAV. Debit-first redeem + payout proof. Boards from cash_events + valuation. Snapshots on deposit, fill, redeem. Tickets T39–T41 recorded only.

**M5.** `MonacoClient` maps the table in §2. Home = `GET /v1/home`. Group = `GET /v1/groups/{id}`. Copy: invite / add money / buy Apple / cash out. Addresses under Settings, Advanced. Format tests only; no Swift share math. Email/password may remain on launch for testers.

## 8. Swift client (usage-derived)

```swift
struct MonacoClient {
    func session() async throws -> Me
    func me() async throws -> Me
    func createGroup(_ body: CreateGroupBody) async throws -> GroupDTO
    func join(groupID: UUID, password: String?) async throws -> GroupScreenDTO
    func home() async throws -> HomeDTO
    func groupScreen(id: UUID) async throws -> GroupScreenDTO
    func createDeposit(groupID: UUID, expectedUSDC: String) async throws -> DepositIntentDTO
    func depositStatus(groupID: UUID, intentID: UUID) async throws -> DepositIntentDTO
    func searchCatalog(q: String) async throws -> [AssetDTO]
    func quote(groupID: UUID, symbol: String, usdc: String) async throws -> QuoteDTO
    func createProposal(groupID: UUID, symbol: String, usdc: String) async throws -> ProposalDTO
    func vote(proposalID: UUID, yes: Bool) async throws -> ProposalDTO
    func redeem(groupID: UUID, shares: String, proof: PayoutProofDTO) async throws -> RedeemJobDTO
}
```

M3 debug control may call `devExecuteBuy` on a `#if DEBUG` method that must not compile into the M5 main flow.

## 9. Red-flag screen (this candidate)

| Flag | How this shape avoids it |
| --- | --- |
| Shallow module | Two services + ports. Group screen is one method, not NAV+ledger+pnl+votes for the caller. |
| Information leakage | Jupiter/Privy/SQL types stay in adapters. Swift never sees `code: 0` polling. Cost basis ≠ mark. |
| Temporal decomposition | Poller lives next to deposits but credit policy lives in kernel. No load/validate/save packages. Snapshot writes ride mutations. |
| Pass-through | `httpapi` may not expose `func CreateProposal` that only calls `kernel.CreateProposal` with the same struct: parse password/JWT/decimal there, then kernel. No `BuyFacade` wrapping `BuyTrigger` with identical args. |

Deliberately not done: shared OpenAPI codegen, `net_usdc_in` column, float64 money, on-chain votes, second execute path after M4, picking T39–T41.

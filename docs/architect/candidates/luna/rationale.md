## Problem

Monaco has one economic object—a group pot—but five milestones currently describe it as
separate screens, handlers, and integrations. The design must preserve the README formulas,
three-wallet ownership model, Postgres voting, debit-first redeem, and HTTP-only Go/Swift
interop while allowing M2's USDC-only mark to become M4's marked NAV. M0 remains a runnable
scaffold; M3's vote-free buy exists only as a temporary seam and is removed when M4 owns buy
execution. The three README open decisions stay unresolved rather than being encoded as
product policy.

## Usage (caller's view)

The backend handler should be able to express a complete operation without coordinating
storage, wallet, and Jupiter calls:

```go
treasury := app.TreasuryFor(requestContext)
result, err := treasury.CreateDeposit(ctx, DepositRequest{
    User: userID, Group: groupID, Amount: USDCAmountFromDecimal(input.Amount),
})
```

The sweep worker asks the same capability for observed chain facts. A member-wallet balance
alone cannot produce shares:

```go
result, err := treasury.ObserveSweep(ctx, ObservedSweep{
    Signature: signature, From: memberWallet, To: groupTreasury,
    Amount: usdcAmount, Confirmed: true,
})
```

The group screen reads one stable projection, not a sequence of balance, NAV, P&L, and
leaderboard calls:

```go
view, err := treasury.GroupView(ctx, viewer, groupID)
home, err := treasury.HomeView(ctx, viewer)
```

The mobile client has hand-written Codable response models and one request per user action:

```swift
let group = try await api.groupView(groupID)
let redeem = try await api.redeem(
    groupID: groupID,
    request: RedeemRequest(shares: selectedShares, payoutProof: proof)
)
```

## Shape

`packages/domain` is the policy kernel. It owns value types, pure NAV/share/P&L math, vote
tallying, redeem planning, and response projections. It has no HTTP, SQL, Privy, Jupiter, or
Swift types. `apps/backend/internal/treasury` is the deep application module: one
`Treasury` capability loads a group aggregate, validates a command, invokes ports, and commits
an idempotent transition. Handlers only authenticate, decode wire input, call one capability,
and encode a response.

The ports are knowledge boundaries, not temporal pipeline stages:

```go
type Treasury interface {
    Session(ctx context.Context, token PrivyToken) (SessionResult, error)
    CreateGroup(ctx context.Context, actor UserID, cmd CreateGroup) (GroupView, error)
    JoinGroup(ctx context.Context, actor UserID, cmd JoinGroup) (GroupView, error)
    CreateDeposit(ctx context.Context, cmd DepositRequest) (DepositStatus, error)
    ObserveSweep(ctx context.Context, fact ObservedSweep) (DepositStatus, error)
    SearchAssets(ctx context.Context, query AssetQuery) ([]Asset, error)
    ProposeBuy(ctx context.Context, cmd BuyProposalCommand) (ProposalView, error)
    Vote(ctx context.Context, cmd VoteCommand) (ProposalView, error)
    Redeem(ctx context.Context, cmd RedeemCommand) (RedeemResult, error)
    GroupView(ctx context.Context, viewer UserID, group GroupID) (GroupView, error)
    HomeView(ctx context.Context, viewer UserID) (HomeView, error)
}
```

`Repository` owns Postgres transactions and row locks. `Wallets` owns member/treasury
provisioning, signing, balances, and payout proof verification. `SwapRail` owns xStocks
metadata, Jupiter quote/execute polling, and fill facts. `Marks` owns Pyth/current marks.
`Treasury` is the only module allowed to combine them. `GroupView` and `HomeView` are
read-boundary projections from persisted NAV snapshots, holdings, shares, and net USDC in;
history is not reconstructed from current wallets.

Core values are non-interchangeable: `USDC`, `ShareUnits`, `PotNAV`, `NavPerShare`,
`SolanaAddress`, `TxSignature`, and `PayoutProof` have constructors that reject invalid
mint, sign, scale, or ownership states. `AssetHolding` requires a confirmed mint and
cost-basis fill. `GroupRules` contains join policy, voter set, threshold, and expiry, while
`ProposalPermissionRule` remains `Undecided` until the README decision is made. No code path
silently treats that state as any concrete proposer policy.

M2 uses `USDCOnlyMark` to calculate NAV and first-deposit price. M4 swaps the mark provider
to `MarkedPortfolio` (USDC plus xStock units times current marks, with Pyth frozen metadata);
the share-credit formula stays the same. Deposits credit only from a confirmed sweep whose
destination is the group treasury, keyed by signature. Redeem executes one Postgres
transaction that locks the member share row, debits it before outbound sale/payout, plans
the proportional stock sale, then records the payout and snapshot idempotently. A retry
returns the existing transition rather than debiting twice.

M3's `DevBuy` command is an explicitly named temporary adapter to `SwapRail`, reachable only
behind a development build flag. M4's passed-proposal transition becomes the sole buy entry
point and deletes that adapter and route. This makes the deletion visible in the shape rather
than allowing a second permanent buy path.

Interface depth is concentrated in `Treasury`: callers know commands and projections, not
row locking, share-price timing, Jupiter order fields, Privy wallet IDs, or wire formats.
The public HTTP surface is consequently small and stable; transport DTOs stay in handlers and
Swift's Codable models are deliberately separate from Go domain types (per
boundary-discipline and information-hiding).

## Synthesis decision

This is one candidate from the Phase B design run. It chooses a deep group-pot capability
over milestone-shaped services because deposits, buys, marks, redeem, and boards all protect
the same ledger invariants. It keeps ports narrow and private to the backend so the domain
package remains pure. No alternate candidate was merged into this package.

## Tradeoffs accepted

- We accept a richer backend implementation in exchange for one command/projection boundary
  that prevents handlers and Swift from learning transaction policy.
- We accept explicit persisted snapshots and event/idempotency records in exchange for
  replayable leaderboards and safe retries.
- We accept a temporary M3 development adapter in exchange for testing real Jupiter execution
  before voting exists; M4 must delete it.
- We accept unresolved proposer and failed-execute policy states in exchange for not choosing
  any README open decision by accident.
- We accept hand-written Swift Codable models in exchange for a clean HTTP contract and no
  generated/shared Go package.

## Alternatives considered

- **Milestone services** (`DepositService`, `VoteService`, `RedeemService`, `LeaderboardService`)
  lost: callers must coordinate several methods and modules, exposing temporal decomposition
  and making debit-first ordering easy to violate.
- **Repository-first CRUD handlers** lost: it hides little domain complexity; every caller or
  handler would need to know share-price timing, mark selection, vote closure, and idempotency.
- **Event-sourced wallet ledger as the public model** lost: it would expose event ordering and
  projection lag to callers without improving the required Postgres source-of-truth behavior
  for this MVP.

## Open questions and risks

- When will the proposer permission decision be resolved, and which concrete policy should
  replace `Undecided`?
- What is the operational resolution for Jupiter execute failure after a proposal passes?
- What happens when a creator leaves, and can a group dissolve?
- Can the external swap rail sell every held xStock slice atomically enough for redeem, or does
  the product need a clearly modeled pending-redeem state?
- Which mark freshness and decimal policy should production use when Pyth is frozen?

## Next implementation step

Create the domain value types and repository transaction interfaces first, then implement one
end-to-end local Postgres `CreateDeposit`/`ObserveSweep` transition against the M0 scaffold.

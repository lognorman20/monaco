## Problem

Monaco is greenfield besides README formulas and milestone tickets. M0 is directories and health. M1–M5 must land a Go API, Swift client, Docker Postgres, and a Go-only `packages/domain` without a shared compiled contract. The non-obvious part is keeping one money model while NAV, execute gates, and screens change by milestone: M2 values the pot as treasury USDC only; M4 marks xStocks; M3 buys through a stub that M4 must delete; redeem must debit shares before any Jupiter sell or payout; votes live in Postgres; three README decisions stay unset. Callers are the iOS app (HTTP) and domain tests (pure Go). Wrong shape is a stack of handlers that each re-quote NAV, or Swift that recomputes equity.

## Usage (caller's view)

Mobile talks to one client. Domain tests talk to the kernel types. Neither imports Jupiter JSON, Privy wallet DTOs, or SQL.

Swift (M5 group screen, after session):

```swift
let home = try await client.home()
let screen = try await client.groupScreen(id: groupID)
// pot rows, you.equityUSD, you.percentReturn, memberBoard already ranked
let intent = try await client.createDeposit(groupID: groupID, expectedUSDC: "25.00")
_ = try await client.createProposal(groupID: groupID, symbol: "AAPLx", usdc: "10.00")
// propose disabled when quote.routable == false
try await client.vote(proposalID: pid, yes: true)
try await client.redeem(groupID: groupID, shares: screen.you.shares, proof: signedPayout)
```

Go domain test (README Alex/Blair, no HTTP):

```go
pot := domain.MustUSDCOnly(usdc("0"))
price, _ := pot.SharePrice(domain.EmptySupply)
got, _ := domain.Credit(usdc("100"), price) // 100 shares
// after mark: MarkedPot{USDC: 0, Positions: [{AAPLx, mark: 110}]}
// Blair credit 110 at 1.10 → 100 shares; redeem 50 of 200 → slice 55 USDC
```

M3 debug (deleted in M4): `POST /v1/dev/execute-buy` exists only while `BuyTrigger` is the stub. Product clients never call it after M4.

## Shape

Ledger-first group kernel. Data structures: integer USDC atoms and share atoms; a `Valuation` interface with `USDCOnly` (M2) and `Marked` (M4); append-only cash events as the only source of net USDC in; share balances as a current row plus idempotent entries keyed by sweep signature or redeem id; proposal status in Postgres with a sibling execution record, not on-chain votes; redeem as a job that cannot be constructed in a paying state until shares are debited.

Flow: HTTP parses wire JSON into domain commands (`boundary-discipline`). `identity` owns users, the three-wallet table (member, treasury; relayer in config), and session. `kernel` owns join, credit, tally, NAV, boards, redeem ordering. Adapters (Privy, Jupiter, xStocks, Pyth, RPC) stay behind ports. Snapshots write from the same kernel methods that mutate, not a later “history” job (`single source of truth`).

Invariants in types: empty supply yields share price $1, not 0/0; `PercentReturn` is unconstructable when net USDC in is 0 so boards skip by absence; `JoinPolicy` / `VoterSet` / `Threshold` are sum types; cost basis and mark are different fields; `RedeemJob` status is a state machine (debited → selling → paying → settled). Validation at the HTTP/adapter edge. Inside domain, functions are pure.

Interface depth: public Go surface is `identity` + `kernel` + screen queries (`Home`, `GroupScreen`). That hides sweep polling, relayer fee payer, Jupiter poll, tally, and mark mix. HTTP is command writes plus two screen GETs so Swift does not orchestrate five resources for one screen. Wire types are not exported. Complexity left with the caller: Privy login, payout message sign, social copy. No proposer-role enum, no execute-retry policy, no dissolve API (`do not pick open decisions`).

Buy execute is a `BuyTrigger` port: M3 stub implements it and exposes the dev route; M4 wires `PassedProposal` and deletes the stub (`laziness-protocol`: one hook, two adapters, no parallel buy paths).

## Synthesis decision

Phase B candidate only. Arena fills this after comparing runners.

## Tradeoffs accepted

- We accept one kernel type that spans deposits, votes, and redeem in exchange for a single NAV/share implementation (no per-handler formula copies).
- We accept screen-sized GET payloads in exchange for less mobile coordination and occasional over-fetch on small mutations.
- We accept integer scaled atoms in exchange for no float in money math.
- We accept an extra cash-event table instead of a mutable `net_usdc_in` column so boards cannot drift from the ledger.
- We accept M3 `BuyTrigger` indirection (tiny extra type) in exchange for deleting the stub without a second execute path lingering.
- We accept leaving failed-execute recovery unset (passed proposal + failed attempt row, no retry loop) so we do not invent README policy.

## Alternatives considered

- **Resource CRUD API** (`/shares`, `/nav`, `/pnl` as separate GETs). Smaller handlers, but the caller assembles the group screen and can display stale NAV next to new shares. Shallow modules; rejected on interface depth.
- **Swift-side formulas.** Hides less in the API; JSON drift plus two money implementations. Rejected (`packages/domain` is Go-only; HTTP is the contract).
- **Event-sourced event bus as the runtime.** Would hide history well but forces a dispatcher every milestone and is more surface than a kernel plus append-only tables. Rejected for M1–M5.
- **On-chain vote or share token.** Violates README; rejected.

## Open questions and risks

- Until M4-T39 lands, who does the demo use to tap Propose without encoding a chosen rule in types?
- If Jupiter `/execute` fails after pass, does the operator only read the attempt row, or is a human runbook required before Friday?
- Redeem crash after debit and before payout: is “retry same job id” enough for the live demo, or do we need a visible failed-cash-out state on mobile?
- Pyth frozen vs Jupiter still quoting: after-hours label only, or do we still mark with the last Hermes price?

## Next implementation step

After synthesis: M0 `apps/backend` `GET /health`, `packages/domain` module with atom types and `ErrNotImplemented` kernel signatures, no product tables yet.

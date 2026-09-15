# Monaco M1–M5 architecture (composer candidate)

## Problem

Monaco is a greenfield monorepo: Go API, SwiftUI mobile, Docker Postgres, Privy wallets, Jupiter swaps. Product rules in `README.md` are strict — share units not dollar IOUs, debit-first redeem, votes in Postgres only, M2 USDC-only NAV then M4 marked NAV, M3 dev stub buy removed in M4. HTTP is the only Go↔Swift contract; `packages/domain` is Go-only pure math.

Shape is non-obvious because three wallet roles (member inbox, group treasury, relayer), two NAV modes across milestones, and on-chain settlement must stay idempotent while ledger invariants (credit only after treasury sweep, debit before payout) hold under crash/replay. Milestones layer capability without rewriting core types.

## Usage (caller's view)

### Mobile quickstart (M5 product; M1–M4 thin/debug variants)

```swift
// Session + wallets (M1)
let api = MonacoAPIClient(baseURL: Config.apiURL, tokenProvider: privy)
try await api.openSession()                         // POST /v1/auth/session
let me = try await api.me()                         // member wallet address (debug M1; Settings M5)

// Group with rules (M4; thin name-only create in M1)
let group = try await api.createGroup(.init(
    name: "Friday crew",
    joinPolicy: .password("potluck"),
    voterSet: .namedSubset(memberIds: [me.id, friend.id]),
    threshold: .majority,
    voteExpiry: .hours(24)
))

// Deposit → sweep → shares (M2+)
let intent = try await api.startDeposit(groupId: group.id, expectedUsdc: 50.00)
let status = try await api.pollDeposit(intent.id)   // credits only after treasury sweep

// Buy via vote (M4; M3 used dev stub — deleted in M4)
let proposal = try await api.proposeBuy(groupId: group.id, symbol: "AAPLx", usdc: 40.00)
try await api.castVote(proposalId: proposal.id, choice: .yes)

// Portfolio + boards (M4+)
let pot = try await api.groupPortfolio(groupId: group.id)   // pot, you, member board
let home = try await api.homeBoards()                       // group + people boards

// Redeem (M4+)
let proof = payoutSigner.signOwnership(pubkey: myPayoutAddress)
try await api.redeem(.init(groupId: group.id, shares: 50, payoutProof: proof))
```

### Backend orchestration call sites

```go
// M2: poller hands confirmed sweep to ledger — idempotent on tx signature
if err := deps.Ledger.ApplySweepCredit(ctx, ledger.SweepCredit{
    GroupID: groupID, UserID: userID, Signature: sig, UsdcMicros: swept,
    NavMode: domain.NavUSDCOnly, // M2; M4 passes NavMarked when xStocks exist
}); err != nil { return err }

// M4: passed proposal triggers swap then snapshot (stub path gone)
if err := deps.BuyOnPass.Execute(ctx, proposalID); err != nil { return err }

// M4: redeem debits shares inside one DB txn before any Jupiter sell or USDC send
out, err := deps.Redeem.Complete(ctx, redeem.Request{
    GroupID: groupID, UserID: userID, Shares: shares, PayoutProof: proof,
})
```

### HTTP contract (hand-written Go structs ↔ Swift Codable)

| Milestone | Representative routes |
|-----------|----------------------|
| M1 | `POST /v1/auth/session`, `GET /v1/me`, `POST /v1/groups`, `GET /v1/groups/{id}` |
| M2 | `POST /v1/groups/{id}/deposits`, `GET /v1/deposits/{id}`, share balance GETs |
| M3 | `POST /v1/dev/groups/{id}/execute-buy` (**deleted M4**), sell + order status GETs |
| M4 | join, proposals, votes, portfolio, home boards, redeem |
| M5 | same routes; UI only |

## Shape

**Ledger kernel + orchestration services.** One module owns all share-unit, net-USDC-in, and NAV-snapshot writes. Domain math stays pure in `packages/domain`. Handlers parse HTTP → domain DTOs → services; Jupiter/Privy/Pyth types never cross the HTTP boundary.

```
SwiftUI → MonacoAPIClient (Codable) → handler → service → ledger | treasury | swap | governance
                                              ↘ packages/domain (pure)
Postgres ← db repositories
Privy / Jupiter / xStocks / Pyth ← adapters (internal only)
```

**Load-bearing decisions**

1. **`internal/ledger` is the sole writer** for `share_ledger`, `member_net_usdc_in`, `nav_snapshots`. Deposit, buy-fill, and redeem services produce verified facts (confirmed signature, passed vote, ownership proof) then call ledger once. Invariant: no share credit without treasury sweep sig; no payout without prior debit. Encoded via `ledger.SweepCredit`, `ledger.RedeemDebit` input types that require `TxSignature` / `PayoutProof` — not raw floats at the boundary (`encode-lessons-in-structure`).

2. **`packages/domain` owns formulas only** — `SharesForDeposit`, `NavPerShare`, `MemberEquity`, `PercentReturn`, `VoteTally`, `RedeemSlice`. No SQL, no HTTP. M2 `NavUSDCOnly` vs M4 `NavMarked` is a typed `domain.NavInput`, not an `if` in handlers (`boundary-discipline`).

3. **NAV mode is explicit per call.** M2 sweep credit passes `NavUSDCOnly` (treasury USDC ÷ total shares; empty pot → $1). M4 redeem and portfolio pass `NavMarked` (USDC + Σ units×mark). Same ledger entrypoints; mode selects domain function (`single source of truth`).

4. **`internal/swap` hides Jupiter** — quote, Privy treasury sign, execute POST, poll until `Success`+`code:0`, idempotent on signature/request-id. Callers see `swap.Buy` / `swap.SellSlice`; no Jupiter wire types exported (`information leakage` guard).

5. **`internal/governance` owns vote lifecycle** in Postgres — proposals, yes/no, unanimous/majority tally, expiry → terminal status. Pass emits `GovernanceEventPassed` consumed by `buy_on_pass` service; no on-chain voting (`README` constraint).

6. **Redeem is one service, three ordered steps inside orchestration** — (a) row-lock debit in ledger txn, (b) sell redeemed fraction of each xStock via swap, (c) USDC transfer to verified payout address. Not split into load/validate/save packages (`temporal decomposition` guard). Debit-first ordering is structural: `Redeem.Complete` calls `Ledger.DebitShares` before `Swap.SellSlice`.

7. **M3 stub is a separate handler package** `handler/dev` registered only before M4; M4-T19 deletes route and package. Production path: proposal pass → `buy_on_pass.Execute` only.

8. **Wallet table honored** — `member_wallets` (user deposit inbox), `treasuries` (group portfolio), relayer in config only. Sweeps member→treasury; swaps and redeems treasury-only; users never sign Solana txs in happy path.

**Interface depth.** Mobile sees ~15 intent-oriented API methods (session, group, deposit, propose, vote, portfolio, redeem, boards). Complexity hidden: sweep polling, NAV mode switch, Jupiter poll loop, vote tally, debit-first redeem, idempotency upserts. Handlers stay ~10 lines: bind, authorize, call service, map error. Repositories are private to ledger/swap/governance — not a public layer (`shallow module` guard).

**Deliberately not done.** No shared Go/Swift package. No OpenAPI codegen M0–M5. No answers to README open decisions (proposer ACL, failed execute retry, creator dissolve) — M4 records tickets only. No Privy webhooks; poll signatures. No Jupiter WebSocket.

## Synthesis decision

Composer candidate for arena Phase B. Synthesis pending — compare against sibling candidates on ledger-kernel vs aggregate-root vs pipeline-stage shapes.

## Tradeoffs accepted

- **Central ledger writer** — all share/NAV paths serialize on one module; simpler invariants, but ledger package grows. Accept coupling in exchange for one place to audit credit/debit rules.
- **Poll-based chain confirmation** — no webhooks; simpler ops, slower feedback on deposit/swap. Accept latency in exchange for hackathon-compatible Privy/Jupiter integration.
- **Balance-table share ledger** — row-locked updates vs event log; faster M2 redeem debit, but less audit replay from events alone (mitigated by `nav_snapshots` + `sweep_tx_log` / `fills`).
- **Hand-written JSON contract** — no codegen; accept drift risk in exchange for zero tooling deps (tests catch mismatch).
- **Marked NAV depends on Pyth adapter** — after-hours flag when frozen; accept external mark dependency in exchange for realistic P&L demo.

## Alternatives considered

- **Event-sourced share ledger** — append-only `share_events` with derived balances. Hides replay complexity but forces M4 redeem and idempotency through projection rebuilds; exposes event-type choreography to every service. Rejected: more caller burden for hackathon scope; balance table + snapshot table suffices.
- **Stage pipeline packages** (`validate` → `transform` → `persist` per flow). Each stage repeats NAV and share invariants. Rejected: temporal decomposition; vote tally and share debit belong with the knowledge they protect.
- **Fat handlers with `packages/domain` ifs** — no `ledger` module. Rejected: M4 milestone explicitly requires rules in domain package, not scattered handler conditionals; would leak Jupiter idempotency into HTTP layer.

## Open questions and risks

- Should `group_members` be created at M1 thin group create (creator only) or only when M4 join lands? Sketch assumes creator membership row at first group insert so M2 deposits have a member FK.
- Pyth mark staleness threshold for after-hours label — product default vs per-symbol config?
- Sell-slice during redeem when treasury is USDC-only: skip Jupiter (domain returns zero stock slice) — confirm no spurious sell orders.
- Relayer SOL depletion in demo — manual fund only, or health endpoint warning?
- Concurrent deposit sweeps for same user/group — idempotent per signature, but two in-flight sweeps: ledger total ordering via DB txn sufficient?

## Next implementation step

M0 scaffold, then M1: migration for `users` / `member_wallets` / `groups` / `treasuries`, `packages/domain/money.go` stubs, `POST /v1/auth/session` calling Privy verify + idempotent wallet provision.

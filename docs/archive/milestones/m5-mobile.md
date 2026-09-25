# M5. Mobile product UI

**Goal.** Replace scaffold and debug screens with the demo-ready SwiftUI product. Social investing copy throughout. Invite friends, add money, buy Apple. The [`docs/product.md`](../product.md) hackathon demo checklist is the ship bar.

**Depends on.** M4 domain APIs for the seven demo steps. M1 Privy sign-in. M2 deposit sweep. Hide M1 address debug UI on the main flow. Keep explorer links under Settings, Advanced. M1 email and password login may stay on the launch screen for internal testers.

**Owns.** `apps/mobile/` SwiftUI app, iOS 18+. Gold slim sim is `$SIMSLIM_UDID` (per machine). Use `just build mobile`, `just test mobile`, and `just run mobile`. API types are hand-written `Codable` models matched to `apps/backend` JSON. No shared compile-time package with Go.

**API boundary.** Product flows never call `api.xstocks.fi`, Jupiter Swap API, Pyth Hermes, or Solana RPC from Swift. No xStocks or Jupiter SDKs on iOS. Swift talks HTTP to `apps/backend` only, plus Privy Swift for auth and member wallets. Catalog search, quotes, proposal create refusal, Pyth prices for the pot, after-hours flags, and proposal status all come from backend JSON. Explorer links under Settings, Advanced open URLs in Safari. They are not product API calls.

## Structure

```text
apps/mobile/
  API/
    MonacoAPIClient.swift     one async method per HTTP route
    DTOs/
      SessionDTO.swift
      GroupViewDTO.swift
      HomeViewDTO.swift
      DepositDTO.swift
      CatalogAssetDTO.swift
      BuyQuoteDTO.swift
      ProposalDTO.swift
      RedeemJobDTO.swift
  Features/
    Auth/           launch, session gate
    Home/           group + people boards
    Groups/         create, join, group detail (pot, you, member board)
    Deposit/        add money, poll status
    Proposals/      search, propose, vote, status chips
    Redeem/         slider, payout proof signer
    Settings/       Advanced explorer links only
  Display/          percent and P&L formatters (unit tested)
```

No `packages/domain` import. No share-price or NAV math in Swift. Swift never calls xStocks, Jupiter, Pyth, or Solana RPC directly.

## Flow

1. Launch → Privy login (SMS, email+password for testers) → `POST /v1/auth/session` → session gate.
2. `GET /v1/home` → group board + people board tabs.
3. `POST /v1/groups` (rules form) or `POST /v1/groups/{id}/join` (password if needed).
4. `GET /v1/groups/{id}`: pot rows (mark, `afterHours`), you slice, member board, proposals, deposit/propose/redeem actions.
5. Deposit: `POST /v1/groups/{id}/deposits` → poll `GET /v1/deposits/{id}` until credited. Show sweep status, not member-wallet balance as money.
6. Propose: `GET /v1/groups/{id}/assets?query=` → `POST /v1/groups/{id}/quotes` → disable submit when `routable` is false → `POST /v1/groups/{id}/proposals`.
7. Vote: `POST /v1/proposals/{id}/votes`. Show open/passed/failed/expired chips from proposal DTOs.
8. Redeem: collect signed payout proof → `POST /v1/groups/{id}/redeems` → refresh `GET /v1/groups/{id}` and `GET /v1/home` on success.
9. Settings → Advanced: explorer links and debug addresses only.

Copy audit: no wallet, gas, seed phrase, mint, or "NAV" on main flow strings.

## Data models

Swift `Codable` types mirror Go JSON field names. Backend owns mint resolution, Jupiter quotes, Pyth marks, and routability. Examples:

```swift
struct CatalogAssetDTO: Codable {
    let symbol: String
    let name: String
}
struct BuyQuoteDTO: Codable {
    let symbol: String
    let usdcMicros: String
    let routable: Bool
}
struct PotRowDTO: Codable {
    let symbol: String
    let units: String
    let markUsd: String
    let valueUsd: String
    let afterHours: Bool?
}
struct GroupViewDTO: Codable {
    let id: UUID
    let name: String
    let pot: [PotRowDTO]
    let you: MemberSliceDTO
    let members: [LeaderboardRowDTO]
    let proposals: [ProposalDTO]?
}
struct MemberSliceDTO: Codable {
    let shareUnits: String
    let equityUsd: String
    let slicePercent: String
    let dollarPnl: String
    let percentReturn: String?  // null when net USDC in == 0
}
struct RedeemJobDTO: Codable {
    let id: UUID
    let status: String  // debited | selling | paying | settled
}
```

Display formatters turn server decimal strings into UI copy. Tests use fixture inputs, not recomputed equity.

## Parallelization

Milestone gate: complete **Wave 4** for demo ship. M5-T25–T26 optional after Wave 4.

| Wave | Tracks (run in parallel within the wave) | Gate for next wave |
|------|------------------------------------------|--------------------|
| **1** | **Auth shell** M5-T1, M5-T2 · **DTOs** (implicit in each screen ticket) | M4 APIs live |
| **2** | **Groups** M5-T3, M5-T4 · **Deposit** M5-T5 · **Settings** M5-T21 | Wave 1 |
| **3** | **Proposals** M5-T6–T10 · **Group detail** M5-T11–T13, M5-T20 · **Home boards** M5-T14–T16 | Wave 2 |
| **4** | **Redeem** M5-T17–T19 · **Copy audit** M5-T22 · **Formatter tests** M5-T23 · **Demo script** M5-T26 | Wave 3 |
| **5** (optional) | **Snapshots** M5-T24 · **TestFlight** M5-T25 | Wave 4 |

All feature screens in Waves 2–3 are parallel once DTOs and session gate exist. Redeem and copy audit run in parallel in Wave 4.

## Tickets

1. M5-T1 Replace the M1 wallet-debug login shell with the product launch screen. Keep SMS via Privy as the demo sign-in path. Email and password may stay for internal testers.
2. M5-T2 Add the session gate and send signed-in users to app home.  
   Depends on: M5-T1
3. M5-T3 Build create group with join policy, voter set picker, threshold, and expiry duration.  
   Depends on: M5-T2
4. M5-T4 Build join group with open join and password entry.  
   Depends on: M5-T2
5. M5-T5 Build deposit with add money copy and sweep status feedback. Replace the M2 thin screen.  
   Depends on: M5-T2
6. M5-T6 Build catalog search via `GET /v1/groups/{id}/assets`.  
   Depends on: M5-T3
7. M5-T7 Disable propose when `POST /v1/groups/{id}/quotes` returns `routable: false`.  
   Depends on: M5-T6
8. M5-T8 Build propose buy with amount entry and social copy. Buy Apple, not mint.  
   Depends on: M5-T7
9. M5-T9 Build proposal detail with yes and no for voter set members.  
   Depends on: M5-T8
10. M5-T10 Show proposal status for open, passed, failed, and expired.  
    Depends on: M5-T9
11. M5-T11 Build the group pot section with USDC and xStock rows, units, mark, and dollar value.  
    Depends on: M5-T3
12. M5-T12 Build the you section with slice dollars, slice percent, dollar P&L, and percent return.  
    Depends on: M5-T11
13. M5-T13 Build the in-group member board ranked by percent return.  
    Depends on: M5-T12
14. M5-T14 Build the app home group board with name, percent, and dollar P&L per row.  
    Depends on: M5-T2
15. M5-T15 Build the app home people board with one row per user across all groups.  
    Depends on: M5-T14
16. M5-T16 Wire tap-through from boards to group detail and to a profile list of groups.  
    Depends on: M5-T13, M5-T15
17. **Parent** M5-T17 Build partial redeem with slider, dust minimum, and cash out copy. Full exit is the slider at max.  
    Depends on: M5-T13
    - └ **Subissue of M5-T17** M5-T18 Collect the signed payout address proof before redeem submit
    - └ **Subissue of M5-T17** M5-T19 Refresh all three boards after redeem success
18. M5-T20 Add the after-hours label on xStock pot rows when `afterHours` is true on the group view.  
    Depends on: M5-T11
19. M5-T21 Add Settings with Advanced explorer links only. Move Solana addresses off the main flow.  
    Depends on: M5-T2
20. M5-T22 Copy audit on main flow screens. No wallet, gas, seed phrase, mint, or NAV in user-visible strings.  
    Depends on: M5-T5, M5-T8, M5-T13, M5-T15, M5-T17
21. M5-T23 Add Swift unit tests for percent return and net USDC in display formatting.
22. M5-T24 Optional snapshot tests for group screen and app home.  
    Depends on: M5-T13, M5-T15
23. M5-T25 Optional TestFlight upload and internal tester invite.  
    Depends on: M5-T26
24. M5-T26 Run the full demo script on the gold slim sim UDID above.  
    Depends on: M5-T16, M5-T19, M5-T22

Do not hard-code answers to M4-T39 through M4-T41. Show API error states. Do not invent retry or dissolve UI.

## Ticket details

#### M5-T1: Replace M1 wallet-debug login shell with product launch screen

**Context**
M5 follows M4 domain APIs. Replaces debug/scaffold mobile screens with demo-ready SwiftUI product shell.

Ticket `M5-T1` is wave **1** in `docs/milestones/m5-mobile.md` under milestone **M5 — Mobile UI**.

**Problem**
SwiftUI flow in `apps/mobile/Features/Auth/` is missing or still scaffold; product/API wiring for `Replace M1 wallet-debug login shell with product launch screen` not shipped.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Features/Auth/`. No drive-by refactors.
2. Build SwiftUI screen in **Owns**; call `MonacoAPIClient` only (no xStocks/Jupiter/Pyth/Solana RPC).
3. Compile gate: `just build mobile` and `just test mobile` exit 0.
4. **Manual (after automated green):** SMS sign-in on slim sim

**API / data contract**
**Mobile consumes existing backend JSON only.** No direct calls to xStocks, Jupiter, Pyth, or Solana RPC from Swift. Use `MonacoAPIClient` methods matching backend routes in milestone doc.
**Test fakes (locked names):**
- `MockURLProtocol` / stub `MonacoAPIClient` session
- fixture JSON under `apps/mobile/Tests/Fixtures/`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/Features/Auth/`

**Out of scope (do not touch)**
- Android/web
- App Store public listing
- On-chain governance UI
- Production Privy webhooks
- Hiding addresses (M5-T21)

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Auth/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Manual (only for this ticket):**
- SMS sign-in on slim sim

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Auth/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- None
- **Wave:** 1
#### M5-T2: Add session gate and send signed-in users to app home

**Context**
M5 follows M4 domain APIs. Replaces debug/scaffold mobile screens with demo-ready SwiftUI product shell.

Ticket `M5-T2` is wave **1** in `docs/milestones/m5-mobile.md` under milestone **M5 — Mobile UI**.
Blocked until dependencies land: M5-T1.

**Problem**
SwiftUI flow in `apps/mobile/Features/Auth/`, `apps/mobile/Features/Home/` is missing or still scaffold; product/API wiring for `Add session gate and send signed-in users to app home` not shipped.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Features/Auth/`, `apps/mobile/Features/Home/`. No drive-by refactors.
2. Add `testAPIClient_getHome_callsV1Home` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**Mobile consumes existing backend JSON only.** No direct calls to xStocks, Jupiter, Pyth, or Solana RPC from Swift. Use `MonacoAPIClient` methods matching backend routes in milestone doc.
**Test fakes (locked names):**
- `MockURLProtocol` / stub `MonacoAPIClient` session
- fixture JSON under `apps/mobile/Tests/Fixtures/`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/Features/Auth/`
- `apps/mobile/Features/Home/`

**Out of scope (do not touch)**
- Board layout (M5-T14+)

**Acceptance Criteria**
- [ ] Test `testAPIClient_getHome_callsV1Home` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Auth/`, `apps/mobile/Features/Home/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `testAPIClient_getHome_callsV1Home` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Auth/`, `apps/mobile/Features/Home/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M5-T1 — **do not start while open**
- **Wave:** 1
#### M5-T3: Build create group with join policy, voter set picker, threshold, and expiry duration

**Context**
M5 follows M4 domain APIs. Replaces debug/scaffold mobile screens with demo-ready SwiftUI product shell.

Ticket `M5-T3` is wave **2** in `docs/milestones/m5-mobile.md` under milestone **M5 — Mobile UI**.
Blocked until dependencies land: M5-T2.

**Problem**
SwiftUI flow in `apps/mobile/Features/Groups/CreateGroupView.swift` is missing or still scaffold; product/API wiring for `Build create group with join policy, voter set picker, threshold, and expiry duration` not shipped.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Features/Groups/CreateGroupView.swift`. No drive-by refactors.
2. Build SwiftUI screen in **Owns**; call `MonacoAPIClient` only (no xStocks/Jupiter/Pyth/Solana RPC).
3. Compile gate: `just build mobile` and `just test mobile` exit 0.

**API / data contract**
**Mobile consumes existing backend JSON only.** No direct calls to xStocks, Jupiter, Pyth, or Solana RPC from Swift. Use `MonacoAPIClient` methods matching backend routes in milestone doc.
**Test fakes (locked names):**
- `MockURLProtocol` / stub `MonacoAPIClient` session
- fixture JSON under `apps/mobile/Tests/Fixtures/`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/Features/Groups/CreateGroupView.swift`

**Out of scope (do not touch)**
- Proposer permission UI until M4-T39 decided

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Groups/CreateGroupView.swift`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Groups/CreateGroupView.swift`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M5-T2 — **do not start while open**
- **Wave:** 2
#### M5-T4: Build join group with open join and password entry

**Context**
M5 follows M4 domain APIs. Replaces debug/scaffold mobile screens with demo-ready SwiftUI product shell.

Ticket `M5-T4` is wave **2** in `docs/milestones/m5-mobile.md` under milestone **M5 — Mobile UI**.
Blocked until dependencies land: M5-T2.

**Problem**
SwiftUI flow in `apps/mobile/Features/Groups/JoinGroupView.swift` is missing or still scaffold; product/API wiring for `Build join group with open join and password entry` not shipped.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Features/Groups/JoinGroupView.swift`. No drive-by refactors.
2. Build SwiftUI screen in **Owns**; call `MonacoAPIClient` only (no xStocks/Jupiter/Pyth/Solana RPC).
3. Compile gate: `just build mobile` and `just test mobile` exit 0.

**API / data contract**
**Mobile consumes existing backend JSON only.** No direct calls to xStocks, Jupiter, Pyth, or Solana RPC from Swift. Use `MonacoAPIClient` methods matching backend routes in milestone doc.
**Test fakes (locked names):**
- `MockURLProtocol` / stub `MonacoAPIClient` session
- fixture JSON under `apps/mobile/Tests/Fixtures/`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/Features/Groups/JoinGroupView.swift`

**Out of scope (do not touch)**
- Dissolve UX until M4-T41 decided

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Groups/JoinGroupView.swift`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Groups/JoinGroupView.swift`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M5-T2 — **do not start while open**
- **Wave:** 2
#### M5-T5: Build deposit with add money copy and sweep status feedback

**Context**
M5 follows M4 domain APIs. Replaces debug/scaffold mobile screens with demo-ready SwiftUI product shell.

Ticket `M5-T5` is wave **2** in `docs/milestones/m5-mobile.md` under milestone **M5 — Mobile UI**.
Blocked until dependencies land: M5-T2.

**Problem**
SwiftUI flow in `apps/mobile/Features/Deposit/` is missing or still scaffold; product/API wiring for `Build deposit with add money copy and sweep status feedback` not shipped.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Features/Deposit/`. No drive-by refactors.
2. Add `Deposit poll state machine: pending → confirmed from fixture deposit DTO` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**Mobile consumes existing backend JSON only.** No direct calls to xStocks, Jupiter, Pyth, or Solana RPC from Swift. Use `MonacoAPIClient` methods matching backend routes in milestone doc.
**Test fakes (locked names):**
- `MockURLProtocol` / stub `MonacoAPIClient` session
- fixture JSON under `apps/mobile/Tests/Fixtures/`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/Features/Deposit/`

**Out of scope (do not touch)**
- Member-wallet balance as money display

**Acceptance Criteria**
- [ ] Test `Deposit poll state machine: pending → confirmed from fixture deposit DTO` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Deposit/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `Deposit poll state machine: pending → confirmed from fixture deposit DTO` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Deposit/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M5-T2 — **do not start while open**
- **Wave:** 2
#### M5-T6: Build catalog search via GET /v1/groups/{id}/assets

**Context**
M5 follows M4 domain APIs. Replaces debug/scaffold mobile screens with demo-ready SwiftUI product shell.

Ticket `M5-T6` is wave **3** in `docs/milestones/m5-mobile.md` under milestone **M5 — Mobile UI**.
Blocked until dependencies land: M5-T3.

**Problem**
`GET /v1/groups/{id}/assets` and handler code under `apps/mobile/Features/Proposals/`, `apps/mobile/API/MonacoAPIClient.swift` do not exist (or fail locked tests).

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Features/Proposals/`, `apps/mobile/API/MonacoAPIClient.swift`. No drive-by refactors.
2. Register `GET /v1/groups/{id}/assets` in route table (`apps/backend/cmd/api/` or `internal/httpapi/`).
3. Implement handler in `apps/mobile/API/MonacoAPIClient.swift` calling `internal/app` method; return JSON status codes from **API / data contract** below.
4. Add `testAPIClient_searchAssets_usesGroupsAssetsQueryParam` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
5. Add `testProductFeatures_noDirectXStocksJupiterPythOrSolanaRpcUrls` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**Route:** `GET /v1/groups/{id}/assets`
**Auth:** Privy bearer token in `Authorization` header (same as M1 session middleware).
**Happy:** 200 with JSON body defined in milestone doc for this route.
**Failure codes:** 401 missing/invalid auth; 400 invalid body; 403 non-member; 404 unknown resource.
**Idempotency:** follow milestone doc keys (`tx_signature`, `execute_request_id`, `privy_user_id`, etc.).
**Test fakes (locked names):**
- `MockURLProtocol` / stub `MonacoAPIClient` session
- fixture JSON under `apps/mobile/Tests/Fixtures/`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/Features/Proposals/`
- `apps/mobile/API/MonacoAPIClient.swift`
- Route: `GET /v1/groups/{id}/assets`

**Out of scope (do not touch)**
- Direct xStocks API calls

**Acceptance Criteria**
- [ ] Test `testAPIClient_searchAssets_usesGroupsAssetsQueryParam` exists, uses AAA comments, and passes.
- [ ] Test `testProductFeatures_noDirectXStocksJupiterPythOrSolanaRpcUrls` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Proposals/`, `apps/mobile/API/MonacoAPIClient.swift`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `testAPIClient_searchAssets_usesGroupsAssetsQueryParam` exists, uses AAA comments, and passes.
- [ ] Test `testProductFeatures_noDirectXStocksJupiterPythOrSolanaRpcUrls` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Proposals/`, `apps/mobile/API/MonacoAPIClient.swift`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M5-T3 — **do not start while open**
- **Wave:** 3
#### M5-T7: Disable propose when POST /v1/groups/{id}/quotes returns routable false

**Context**
M5 follows M4 domain APIs. Replaces debug/scaffold mobile screens with demo-ready SwiftUI product shell.

Ticket `M5-T7` is wave **3** in `docs/milestones/m5-mobile.md` under milestone **M5 — Mobile UI**.
Blocked until dependencies land: M5-T6.

**Problem**
`POST /v1/groups/{id}/quotes` and handler code under `apps/mobile/Features/Proposals/` do not exist (or fail locked tests).

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Features/Proposals/`. No drive-by refactors.
2. Register `POST /v1/groups/{id}/quotes` in route table (`apps/backend/cmd/api/` or `internal/httpapi/`).
3. Implement handler in `apps/mobile/Features/Proposals/` calling `internal/app` method; return JSON status codes from **API / data contract** below.
4. Add `testAPIClient_postQuotes_sendsSymbolAndUsdc` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
5. Add `testAPIClient_postProposal_doesNotCallWhenRoutableFalse` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**Route:** `POST /v1/groups/{id}/quotes`
**Auth:** Privy bearer token in `Authorization` header (same as M1 session middleware).
**Happy:** 200 with JSON body defined in milestone doc for this route.
**Failure codes:** 401 missing/invalid auth; 400 invalid body; 403 non-member; 404 unknown resource.
**Idempotency:** follow milestone doc keys (`tx_signature`, `execute_request_id`, `privy_user_id`, etc.).
**Test fakes (locked names):**
- `MockURLProtocol` / stub `MonacoAPIClient` session
- fixture JSON under `apps/mobile/Tests/Fixtures/`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/Features/Proposals/`
- Route: `POST /v1/groups/{id}/quotes`

**Out of scope (do not touch)**
- Backend quote logic

**Acceptance Criteria**
- [ ] Test `testAPIClient_postQuotes_sendsSymbolAndUsdc` exists, uses AAA comments, and passes.
- [ ] Test `testAPIClient_postProposal_doesNotCallWhenRoutableFalse` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Proposals/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `testAPIClient_postQuotes_sendsSymbolAndUsdc` exists, uses AAA comments, and passes.
- [ ] Test `testAPIClient_postProposal_doesNotCallWhenRoutableFalse` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Proposals/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M5-T6 — **do not start while open**
- **Wave:** 3
#### M5-T8: Build propose buy with amount entry and social copy

**Context**
M5 follows M4 domain APIs. Replaces debug/scaffold mobile screens with demo-ready SwiftUI product shell.

Ticket `M5-T8` is wave **3** in `docs/milestones/m5-mobile.md` under milestone **M5 — Mobile UI**.
Blocked until dependencies land: M5-T7.

**Problem**
SwiftUI flow in `apps/mobile/Features/Proposals/` is missing or still scaffold; product/API wiring for `Build propose buy with amount entry and social copy` not shipped.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Features/Proposals/`. No drive-by refactors.
2. Build SwiftUI screen in **Owns**; call `MonacoAPIClient` only (no xStocks/Jupiter/Pyth/Solana RPC).
3. Compile gate: `just build mobile` and `just test mobile` exit 0.

**API / data contract**
**Mobile consumes existing backend JSON only.** No direct calls to xStocks, Jupiter, Pyth, or Solana RPC from Swift. Use `MonacoAPIClient` methods matching backend routes in milestone doc.
**Test fakes (locked names):**
- `MockURLProtocol` / stub `MonacoAPIClient` session
- fixture JSON under `apps/mobile/Tests/Fixtures/`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/Features/Proposals/`

**Out of scope (do not touch)**
- Mint jargon in copy (M5-T22 audit)

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Proposals/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Proposals/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M5-T7 — **do not start while open**
- **Wave:** 3
#### M5-T9: Build proposal detail with yes and no for voter set members

**Context**
M5 follows M4 domain APIs. Replaces debug/scaffold mobile screens with demo-ready SwiftUI product shell.

Ticket `M5-T9` is wave **3** in `docs/milestones/m5-mobile.md` under milestone **M5 — Mobile UI**.
Blocked until dependencies land: M5-T8.

**Problem**
SwiftUI flow in `apps/mobile/Features/Proposals/ProposalDetailView.swift` is missing or still scaffold; product/API wiring for `Build proposal detail with yes and no for voter set members` not shipped.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Features/Proposals/ProposalDetailView.swift`. No drive-by refactors.
2. Build SwiftUI screen in **Owns**; call `MonacoAPIClient` only (no xStocks/Jupiter/Pyth/Solana RPC).
3. Compile gate: `just build mobile` and `just test mobile` exit 0.

**API / data contract**
**Mobile consumes existing backend JSON only.** No direct calls to xStocks, Jupiter, Pyth, or Solana RPC from Swift. Use `MonacoAPIClient` methods matching backend routes in milestone doc.
**Test fakes (locked names):**
- `MockURLProtocol` / stub `MonacoAPIClient` session
- fixture JSON under `apps/mobile/Tests/Fixtures/`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/Features/Proposals/ProposalDetailView.swift`

**Out of scope (do not touch)**
- Failed execute retry UI until M4-T40 decided

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Proposals/ProposalDetailView.swift`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Proposals/ProposalDetailView.swift`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M5-T8 — **do not start while open**
- **Wave:** 3
#### M5-T10: Show proposal status for open, passed, failed, and expired

**Context**
M5 follows M4 domain APIs. Replaces debug/scaffold mobile screens with demo-ready SwiftUI product shell.

Ticket `M5-T10` is wave **3** in `docs/milestones/m5-mobile.md` under milestone **M5 — Mobile UI**.
Blocked until dependencies land: M5-T9.

**Problem**
SwiftUI flow in `apps/mobile/Features/Proposals/` is missing or still scaffold; product/API wiring for `Show proposal status for open, passed, failed, and expired` not shipped.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Features/Proposals/`. No drive-by refactors.
2. Add `testProposalDTO_decodesOpenPassedFailedExpiredStatuses` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**Mobile consumes existing backend JSON only.** No direct calls to xStocks, Jupiter, Pyth, or Solana RPC from Swift. Use `MonacoAPIClient` methods matching backend routes in milestone doc.
**Test fakes (locked names):**
- `MockURLProtocol` / stub `MonacoAPIClient` session
- fixture JSON under `apps/mobile/Tests/Fixtures/`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/Features/Proposals/`

**Out of scope (do not touch)**
- On-chain status

**Acceptance Criteria**
- [ ] Test `testProposalDTO_decodesOpenPassedFailedExpiredStatuses` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Proposals/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `testProposalDTO_decodesOpenPassedFailedExpiredStatuses` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Proposals/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M5-T9 — **do not start while open**
- **Wave:** 3
#### M5-T11: Build group pot section with USDC and xStock rows, units, mark, and dollar value

**Context**
M5 follows M4 domain APIs. Replaces debug/scaffold mobile screens with demo-ready SwiftUI product shell.

Ticket `M5-T11` is wave **3** in `docs/milestones/m5-mobile.md` under milestone **M5 — Mobile UI**.
Blocked until dependencies land: M5-T3.

**Problem**
SwiftUI flow in `apps/mobile/Features/Groups/GroupDetailView.swift` is missing or still scaffold; product/API wiring for `Build group pot section with USDC and xStock rows, units, mark, and dollar value` not shipped.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Features/Groups/GroupDetailView.swift`. No drive-by refactors.
2. Add `testGroupViewDTO_decodesFixtureWithPotAndMemberBoard` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**Mobile consumes existing backend JSON only.** No direct calls to xStocks, Jupiter, Pyth, or Solana RPC from Swift. Use `MonacoAPIClient` methods matching backend routes in milestone doc.
**Test fakes (locked names):**
- `MockURLProtocol` / stub `MonacoAPIClient` session
- fixture JSON under `apps/mobile/Tests/Fixtures/`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/Features/Groups/GroupDetailView.swift`

**Out of scope (do not touch)**
- Recomputing marks in Swift

**Acceptance Criteria**
- [ ] Test `testGroupViewDTO_decodesFixtureWithPotAndMemberBoard` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Groups/GroupDetailView.swift`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `testGroupViewDTO_decodesFixtureWithPotAndMemberBoard` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Groups/GroupDetailView.swift`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M5-T3 — **do not start while open**
- **Wave:** 3
#### M5-T12: Build you section with slice dollars, slice percent, dollar P&L, and percent return

**Context**
M5 follows M4 domain APIs. Replaces debug/scaffold mobile screens with demo-ready SwiftUI product shell.

Ticket `M5-T12` is wave **3** in `docs/milestones/m5-mobile.md` under milestone **M5 — Mobile UI**.
Blocked until dependencies land: M5-T11.

**Problem**
SwiftUI flow in `apps/mobile/Features/Groups/` is missing or still scaffold; product/API wiring for `Build you section with slice dollars, slice percent, dollar P&L, and percent return` not shipped.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Features/Groups/`. No drive-by refactors.
2. Build SwiftUI screen in **Owns**; call `MonacoAPIClient` only (no xStocks/Jupiter/Pyth/Solana RPC).
3. Compile gate: `just build mobile` and `just test mobile` exit 0.

**API / data contract**
**Mobile consumes existing backend JSON only.** No direct calls to xStocks, Jupiter, Pyth, or Solana RPC from Swift. Use `MonacoAPIClient` methods matching backend routes in milestone doc.
**Test fakes (locked names):**
- `MockURLProtocol` / stub `MonacoAPIClient` session
- fixture JSON under `apps/mobile/Tests/Fixtures/`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/Features/Groups/`

**Out of scope (do not touch)**
- Equity math in Swift

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Groups/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Groups/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M5-T11 — **do not start while open**
- **Wave:** 3
#### M5-T13: Build in-group member board ranked by percent return

**Context**
M5 follows M4 domain APIs. Replaces debug/scaffold mobile screens with demo-ready SwiftUI product shell.

Ticket `M5-T13` is wave **3** in `docs/milestones/m5-mobile.md` under milestone **M5 — Mobile UI**.
Blocked until dependencies land: M5-T12.

**Problem**
SwiftUI flow in `apps/mobile/Features/Groups/` is missing or still scaffold; product/API wiring for `Build in-group member board ranked by percent return` not shipped.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Features/Groups/`. No drive-by refactors.
2. Add `Board cells render server rank order without re-sorting by dollars` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**Mobile consumes existing backend JSON only.** No direct calls to xStocks, Jupiter, Pyth, or Solana RPC from Swift. Use `MonacoAPIClient` methods matching backend routes in milestone doc.
**Test fakes (locked names):**
- `MockURLProtocol` / stub `MonacoAPIClient` session
- fixture JSON under `apps/mobile/Tests/Fixtures/`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/Features/Groups/`

**Out of scope (do not touch)**
- People board (M5-T15)

**Acceptance Criteria**
- [ ] Test `Board cells render server rank order without re-sorting by dollars` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Groups/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `Board cells render server rank order without re-sorting by dollars` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Groups/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M5-T12 — **do not start while open**
- **Wave:** 3
#### M5-T14: Build app home group board with name, percent, and dollar P&L per row

**Context**
M5 follows M4 domain APIs. Replaces debug/scaffold mobile screens with demo-ready SwiftUI product shell.

Ticket `M5-T14` is wave **3** in `docs/milestones/m5-mobile.md` under milestone **M5 — Mobile UI**.
Blocked until dependencies land: M5-T2.

**Problem**
SwiftUI flow in `apps/mobile/Features/Home/` is missing or still scaffold; product/API wiring for `Build app home group board with name, percent, and dollar P&L per row` not shipped.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Features/Home/`. No drive-by refactors.
2. Add `testHomeViewDTO_decodesGroupAndPeopleBoards` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**Mobile consumes existing backend JSON only.** No direct calls to xStocks, Jupiter, Pyth, or Solana RPC from Swift. Use `MonacoAPIClient` methods matching backend routes in milestone doc.
**Test fakes (locked names):**
- `MockURLProtocol` / stub `MonacoAPIClient` session
- fixture JSON under `apps/mobile/Tests/Fixtures/`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/Features/Home/`

**Out of scope (do not touch)**
- Cross-group math in Swift

**Acceptance Criteria**
- [ ] Test `testHomeViewDTO_decodesGroupAndPeopleBoards` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Home/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `testHomeViewDTO_decodesGroupAndPeopleBoards` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Home/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M5-T2 — **do not start while open**
- **Wave:** 3
#### M5-T15: Build app home people board with one row per user across all groups

**Context**
M5 follows M4 domain APIs. Replaces debug/scaffold mobile screens with demo-ready SwiftUI product shell.

Ticket `M5-T15` is wave **3** in `docs/milestones/m5-mobile.md` under milestone **M5 — Mobile UI**.
Blocked until dependencies land: M5-T14.

**Problem**
SwiftUI flow in `apps/mobile/Features/Home/` is missing or still scaffold; product/API wiring for `Build app home people board with one row per user across all groups` not shipped.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Features/Home/`. No drive-by refactors.
2. Build SwiftUI screen in **Owns**; call `MonacoAPIClient` only (no xStocks/Jupiter/Pyth/Solana RPC).
3. Compile gate: `just build mobile` and `just test mobile` exit 0.

**API / data contract**
**Mobile consumes existing backend JSON only.** No direct calls to xStocks, Jupiter, Pyth, or Solana RPC from Swift. Use `MonacoAPIClient` methods matching backend routes in milestone doc.
**Test fakes (locked names):**
- `MockURLProtocol` / stub `MonacoAPIClient` session
- fixture JSON under `apps/mobile/Tests/Fixtures/`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/Features/Home/`

**Out of scope (do not touch)**
- Profile drill-down (M5-T16)

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Home/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Home/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M5-T14 — **do not start while open**
- **Wave:** 3
#### M5-T16: Wire tap-through from boards to group detail and profile list of groups

**Context**
M5 follows M4 domain APIs. Replaces debug/scaffold mobile screens with demo-ready SwiftUI product shell.

Ticket `M5-T16` is wave **3** in `docs/milestones/m5-mobile.md` under milestone **M5 — Mobile UI**.
Blocked until dependencies land: M5-T13, M5-T15.

**Problem**
SwiftUI flow in `apps/mobile/Features/Home/`, `apps/mobile/Features/Groups/` is missing or still scaffold; product/API wiring for `Wire tap-through from boards to group detail and profile list of groups` not shipped.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Features/Home/`, `apps/mobile/Features/Groups/`. No drive-by refactors.
2. Build SwiftUI screen in **Owns**; call `MonacoAPIClient` only (no xStocks/Jupiter/Pyth/Solana RPC).
3. Compile gate: `just build mobile` and `just test mobile` exit 0.

**API / data contract**
**Mobile consumes existing backend JSON only.** No direct calls to xStocks, Jupiter, Pyth, or Solana RPC from Swift. Use `MonacoAPIClient` methods matching backend routes in milestone doc.
**Test fakes (locked names):**
- `MockURLProtocol` / stub `MonacoAPIClient` session
- fixture JSON under `apps/mobile/Tests/Fixtures/`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/Features/Home/`
- `apps/mobile/Features/Groups/`

**Out of scope (do not touch)**
- Deep linking

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Home/`, `apps/mobile/Features/Groups/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Home/`, `apps/mobile/Features/Groups/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M5-T13 — **do not start while open**
- M5-T15 — **do not start while open**
- **Wave:** 3
#### M5-T17: Build partial redeem with slider, dust minimum, and cash out copy

**Context**
M5 follows M4 domain APIs. Replaces debug/scaffold mobile screens with demo-ready SwiftUI product shell.

Ticket `M5-T17` is wave **4** in `docs/milestones/m5-mobile.md` under milestone **M5 — Mobile UI**.
Parent ticket; subissues: M5-T18, M5-T19.
Blocked until dependencies land: M5-T13.

**Problem**
SwiftUI flow in `apps/mobile/Features/Redeem/` is missing or still scaffold; product/API wiring for `Build partial redeem with slider, dust minimum, and cash out copy` not shipped.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Features/Redeem/`. No drive-by refactors.
2. Parent glue: wire subissues M5-T18, M5-T19 into `apps/mobile/Features/Redeem/` after each subissue lands.
3. Add `testRedeemSlider_dustMinimum_disablesSubmitBelowThreshold` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**Parent orchestration.** Public contract is union of subissue routes/symbols: M5-T18, M5-T19.
**Test fakes (locked names):**
- `MockURLProtocol` / stub `MonacoAPIClient` session
- fixture JSON under `apps/mobile/Tests/Fixtures/`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/Features/Redeem/`

**Out of scope (do not touch)**
- Full exit special case beyond slider max

**Acceptance Criteria**
- [ ] Test `testRedeemSlider_dustMinimum_disablesSubmitBelowThreshold` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Redeem/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] All subissues (M5-T18, M5-T19) closed before parent closes.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `testRedeemSlider_dustMinimum_disablesSubmitBelowThreshold` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Redeem/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] All subissues (M5-T18, M5-T19) closed before parent closes.
- [ ] Subissues closed: M5-T18, M5-T19.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M5-T13 — **do not start while open**
- **Wave:** 4
- **Subissue:** M5-T18
- **Subissue:** M5-T19
#### M5-T18: Collect signed payout address proof before redeem submit

**Context**
M5 follows M4 domain APIs. Replaces debug/scaffold mobile screens with demo-ready SwiftUI product shell.

Ticket `M5-T18` is wave **4** in `docs/milestones/m5-mobile.md` under milestone **M5 — Mobile UI**.
Subissue of M5-T17.
Blocked until dependencies land: M5-T17.

**Problem**
SwiftUI flow in `apps/mobile/Features/Redeem/` is missing or still scaffold; product/API wiring for `Collect signed payout address proof before redeem submit` not shipped.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Features/Redeem/`. No drive-by refactors.
2. Subissue of `M5-T17`. Do not add routes or flows owned by parent/sibling subissues.
3. Add `testAPIClient_postRedeem_includesPayoutProofPayload` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new public route.** Subissue of `M5-T17`. Internal symbols only in **Owns** paths.
**Test fakes (locked names):**
- `MockURLProtocol` / stub `MonacoAPIClient` session
- fixture JSON under `apps/mobile/Tests/Fixtures/`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/Features/Redeem/`

**Out of scope (do not touch)**
- Backend proof verification (M4-T34)

**Acceptance Criteria**
- [ ] Test `testAPIClient_postRedeem_includesPayoutProofPayload` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Redeem/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `testAPIClient_postRedeem_includesPayoutProofPayload` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Redeem/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M5-T17 — **do not start while open**
- **Wave:** 4
- **Parent:** M5-T17
#### M5-T19: Refresh all three boards after redeem success

**Context**
M5 follows M4 domain APIs. Replaces debug/scaffold mobile screens with demo-ready SwiftUI product shell.

Ticket `M5-T19` is wave **4** in `docs/milestones/m5-mobile.md` under milestone **M5 — Mobile UI**.
Subissue of M5-T17.
Blocked until dependencies land: M5-T18.

**Problem**
SwiftUI flow in `apps/mobile/Features/Redeem/`, `apps/mobile/Features/Home/`, `apps/mobile/Features/Groups/` is missing or still scaffold; product/API wiring for `Refresh all three boards after redeem success` not shipped.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Features/Redeem/`, `apps/mobile/Features/Home/`, `apps/mobile/Features/Groups/`. No drive-by refactors.
2. Subissue of `M5-T17`. Do not add routes or flows owned by parent/sibling subissues.
3. Add `testAPIClient_afterRedeemSuccess_refreshesGroupAndHome` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new public route.** Subissue of `M5-T17`. Internal symbols only in **Owns** paths.
**Test fakes (locked names):**
- `MockURLProtocol` / stub `MonacoAPIClient` session
- fixture JSON under `apps/mobile/Tests/Fixtures/`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/Features/Redeem/`
- `apps/mobile/Features/Home/`
- `apps/mobile/Features/Groups/`

**Out of scope (do not touch)**
- Push notifications

**Acceptance Criteria**
- [ ] Test `testAPIClient_afterRedeemSuccess_refreshesGroupAndHome` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Redeem/`, `apps/mobile/Features/Home/`, `apps/mobile/Features/Groups/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `testAPIClient_afterRedeemSuccess_refreshesGroupAndHome` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Redeem/`, `apps/mobile/Features/Home/`, `apps/mobile/Features/Groups/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M5-T18 — **do not start while open**
- **Wave:** 4
- **Parent:** M5-T17
#### M5-T20: Add after-hours label on xStock pot rows when afterHours is true on group view

**Context**
M5 follows M4 domain APIs. Replaces debug/scaffold mobile screens with demo-ready SwiftUI product shell.

Ticket `M5-T20` is wave **3** in `docs/milestones/m5-mobile.md` under milestone **M5 — Mobile UI**.
Blocked until dependencies land: M5-T11.

**Problem**
SwiftUI flow in `apps/mobile/Features/Groups/` is missing or still scaffold; product/API wiring for `Add after-hours label on xStock pot rows when afterHours is true on group view` not shipped.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Features/Groups/`. No drive-by refactors.
2. Add `testPotRowDTO_afterHoursTrue_decodesLabelFlag` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**Mobile consumes existing backend JSON only.** No direct calls to xStocks, Jupiter, Pyth, or Solana RPC from Swift. Use `MonacoAPIClient` methods matching backend routes in milestone doc.
**Test fakes (locked names):**
- `MockURLProtocol` / stub `MonacoAPIClient` session
- fixture JSON under `apps/mobile/Tests/Fixtures/`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/Features/Groups/`

**Out of scope (do not touch)**
- Pyth client in Swift

**Acceptance Criteria**
- [ ] Test `testPotRowDTO_afterHoursTrue_decodesLabelFlag` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Groups/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `testPotRowDTO_afterHoursTrue_decodesLabelFlag` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Groups/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M5-T11 — **do not start while open**
- **Wave:** 3
#### M5-T21: Add Settings with Advanced explorer links only. Move Solana addresses off main flow

**Context**
M5 follows M4 domain APIs. Replaces debug/scaffold mobile screens with demo-ready SwiftUI product shell.

Ticket `M5-T21` is wave **2** in `docs/milestones/m5-mobile.md` under milestone **M5 — Mobile UI**.
Blocked until dependencies land: M5-T2.

**Problem**
SwiftUI flow in `apps/mobile/Features/Settings/` is missing or still scaffold; product/API wiring for `Add Settings with Advanced explorer links only. Move Solana addresses off main flow` not shipped.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Features/Settings/`. No drive-by refactors.
2. Add `testSettingsAdvanced_containsExplorerLinksOnly` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**Mobile consumes existing backend JSON only.** No direct calls to xStocks, Jupiter, Pyth, or Solana RPC from Swift. Use `MonacoAPIClient` methods matching backend routes in milestone doc.
**Test fakes (locked names):**
- `MockURLProtocol` / stub `MonacoAPIClient` session
- fixture JSON under `apps/mobile/Tests/Fixtures/`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/Features/Settings/`

**Out of scope (do not touch)**
- Wallet copy on main flow

**Acceptance Criteria**
- [ ] Test `testSettingsAdvanced_containsExplorerLinksOnly` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Settings/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `testSettingsAdvanced_containsExplorerLinksOnly` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Settings/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M5-T2 — **do not start while open**
- **Wave:** 2
#### M5-T22: Copy audit on main flow screens. No wallet, gas, seed phrase, mint, or NAV in user-visible strings

**Context**
M5 follows M4 domain APIs. Replaces debug/scaffold mobile screens with demo-ready SwiftUI product shell.

Ticket `M5-T22` is wave **4** in `docs/milestones/m5-mobile.md` under milestone **M5 — Mobile UI**.
Blocked until dependencies land: M5-T5, M5-T8, M5-T13, M5-T15, M5-T17.

**Problem**
SwiftUI flow in `apps/mobile/Features/` is missing or still scaffold; product/API wiring for `Copy audit on main flow screens. No wallet, gas, seed phrase, mint, or NAV in user-visible strings` not shipped.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Features/`. No drive-by refactors.
2. Add `testMainFlowStrings_excludeWalletGasSeedPhraseMintAndNav` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**Mobile consumes existing backend JSON only.** No direct calls to xStocks, Jupiter, Pyth, or Solana RPC from Swift. Use `MonacoAPIClient` methods matching backend routes in milestone doc.
**Test fakes (locked names):**
- `MockURLProtocol` / stub `MonacoAPIClient` session
- fixture JSON under `apps/mobile/Tests/Fixtures/`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/Features/`

**Out of scope (do not touch)**
- Settings Advanced explorer strings

**Acceptance Criteria**
- [ ] Test `testMainFlowStrings_excludeWalletGasSeedPhraseMintAndNav` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `testMainFlowStrings_excludeWalletGasSeedPhraseMintAndNav` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M5-T5 — **do not start while open**
- M5-T8 — **do not start while open**
- M5-T13 — **do not start while open**
- M5-T15 — **do not start while open**
- M5-T17 — **do not start while open**
- **Wave:** 4
#### M5-T23: Add Swift unit tests for percent return and net USDC in display formatting

**Context**
M5 follows M4 domain APIs. Replaces debug/scaffold mobile screens with demo-ready SwiftUI product shell.

Ticket `M5-T23` is wave **4** in `docs/milestones/m5-mobile.md` under milestone **M5 — Mobile UI**.

**Problem**
Locked test names for `M5-T23` are absent or not wired into `just test` recipes; `apps/mobile/Tests/Display/` incomplete.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Tests/Display/`. No drive-by refactors.
2. Add `testPercentReturnFormatter_positiveReturn_showsPlusPrefix` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
3. Add `testPercentReturnFormatter_nilPercentReturn_showsEmDashOrHidden` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
4. Add `testDollarPnlFormatter_negativeShowsLossCopy` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
5. Add `testSlicePercentFormatter_formatsOneDecimal` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new API.** Tests and/or `Justfile` wiring only.
**Test fakes (locked names):**
- `MockURLProtocol` / stub `MonacoAPIClient` session
- fixture JSON under `apps/mobile/Tests/Fixtures/`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/Tests/Display/`

**Out of scope (do not touch)**
- Recomputing equity

**Acceptance Criteria**
- [ ] Test `testPercentReturnFormatter_positiveReturn_showsPlusPrefix` exists, uses AAA comments, and passes.
- [ ] Test `testPercentReturnFormatter_nilPercentReturn_showsEmDashOrHidden` exists, uses AAA comments, and passes.
- [ ] Test `testDollarPnlFormatter_negativeShowsLossCopy` exists, uses AAA comments, and passes.
- [ ] Test `testSlicePercentFormatter_formatsOneDecimal` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Tests/Display/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `testPercentReturnFormatter_positiveReturn_showsPlusPrefix` exists, uses AAA comments, and passes.
- [ ] Test `testPercentReturnFormatter_nilPercentReturn_showsEmDashOrHidden` exists, uses AAA comments, and passes.
- [ ] Test `testDollarPnlFormatter_negativeShowsLossCopy` exists, uses AAA comments, and passes.
- [ ] Test `testSlicePercentFormatter_formatsOneDecimal` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Tests/Display/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- None
- **Wave:** 4
#### M5-T24: Optional snapshot tests for group screen and app home

**Context**
M5 follows M4 domain APIs. Replaces debug/scaffold mobile screens with demo-ready SwiftUI product shell.

Ticket `M5-T24` is wave **5** in `docs/milestones/m5-mobile.md` under milestone **M5 — Mobile UI**.
Blocked until dependencies land: M5-T13, M5-T15.

**Problem**
Locked test names for `M5-T24` are absent or not wired into `just test` recipes; `apps/mobile/Tests/Snapshots/` incomplete.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Tests/Snapshots/`. No drive-by refactors.
2. Add `testGroupScreen_snapshot_matchesFixture` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
3. Add `testAppHome_snapshot_matchesFixture` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new API.** Tests and/or `Justfile` wiring only.
**Test fakes (locked names):**
- `MockURLProtocol` / stub `MonacoAPIClient` session
- fixture JSON under `apps/mobile/Tests/Fixtures/`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/Tests/Snapshots/`

**Out of scope (do not touch)**
- Required for demo gate

**Acceptance Criteria**
- [ ] Test `testGroupScreen_snapshot_matchesFixture` exists, uses AAA comments, and passes.
- [ ] Test `testAppHome_snapshot_matchesFixture` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Tests/Snapshots/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `testGroupScreen_snapshot_matchesFixture` exists, uses AAA comments, and passes.
- [ ] Test `testAppHome_snapshot_matchesFixture` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Tests/Snapshots/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M5-T13 — **do not start while open**
- M5-T15 — **do not start while open**
- **Wave:** 5
#### M5-T25: Optional TestFlight upload and internal tester invite

**Context**
M5 follows M4 domain APIs. Replaces debug/scaffold mobile screens with demo-ready SwiftUI product shell.

Ticket `M5-T25` is wave **5** in `docs/milestones/m5-mobile.md` under milestone **M5 — Mobile UI**.
Blocked until dependencies land: M5-T26.

**Problem**
Locked test names for `M5-T25` are absent or not wired into `just test` recipes; `apps/mobile/` incomplete.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/`. No drive-by refactors.
2. Satisfy milestone gate: `just test backend` and/or `just test mobile` exit 0 with no new failing tests.
3. **Manual (after automated green):** Upload build; Invite internal testers

**API / data contract**
**No new API.** Tests and/or `Justfile` wiring only.
**Test fakes (locked names):**
- `MockURLProtocol` / stub `MonacoAPIClient` session
- fixture JSON under `apps/mobile/Tests/Fixtures/`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/`

**Out of scope (do not touch)**
- App Store public listing

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Manual (only for this ticket):**
- Upload build
- Invite internal testers

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M5-T26 — **do not start while open**
- **Wave:** 5
#### M5-T26: Run full demo script on gold slim sim UDID

**Context**
M5 follows M4 domain APIs. Replaces debug/scaffold mobile screens with demo-ready SwiftUI product shell.

Ticket `M5-T26` is wave **4** in `docs/milestones/m5-mobile.md` under milestone **M5 — Mobile UI**.
Blocked until dependencies land: M5-T16, M5-T19, M5-T22.

**Problem**
SwiftUI flow in `docs/` is missing or still scaffold; product/API wiring for `Run full demo script on gold slim sim UDID` not shipped.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `docs/`. No drive-by refactors.
2. Build SwiftUI screen in **Owns**; call `MonacoAPIClient` only (no xStocks/Jupiter/Pyth/Solana RPC).
3. Compile gate: `just build mobile` and `just test mobile` exit 0.
4. **Manual (after automated green):** docs/product.md hackathon demo checklist on `$SIMSLIM_UDID`

**API / data contract**
**Mobile consumes existing backend JSON only.** No direct calls to xStocks, Jupiter, Pyth, or Solana RPC from Swift. Use `MonacoAPIClient` methods matching backend routes in milestone doc.
**Test fakes (locked names):**
- `MockURLProtocol` / stub `MonacoAPIClient` session
- fixture JSON under `apps/mobile/Tests/Fixtures/`
- `MockURLProtocol` for API client unit tests

### Scope
- `docs/`

**Out of scope (do not touch)**
- Android demo

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `docs/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Manual (only for this ticket):**
- docs/product.md hackathon demo checklist on `$SIMSLIM_UDID`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `docs/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M5-T16 — **do not start while open**
- M5-T19 — **do not start while open**
- M5-T22 — **do not start while open**
- **Wave:** 4

## Automated verification

**Commands.** `just test mobile`. Backend domain math is covered in M4 `just test backend`; mobile tests prove HTTP client behavior, DTO decoding, display formatting, and copy constraints — not NAV or vote tally recomputation in Swift.

### Test layers

| Layer | Runs under | What it proves |
|-------|------------|----------------|
| **Swift unit — Display** | `just test mobile` | Percent return, dollar P&L, slice percent, and dust-minimum formatters from **fixture server strings** (M5-T23). |
| **Swift unit — API / DTOs** | `just test mobile` | `Codable` decode of `GroupViewDTO`, `HomeViewDTO`, `BuyQuoteDTO`, `ProposalDTO`, `RedeemJobDTO` from recorded JSON fixtures. |
| **Swift unit — API client** | `just test mobile` | `MonacoAPIClient` builds correct paths and auth headers; uses backend base URL only (M5-T6–T19). |
| **Swift unit — boundary** | `just test mobile` | No references to `api.xstocks.fi`, Jupiter, Pyth Hermes, or Solana RPC hosts in product feature code (static scan or allowlist test). |
| **Snapshot (optional)** | `just test mobile` | Group screen and app home layout (M5-T24 only). |

View models should not recompute equity or percent return; tests pass through backend values and assert formatted output only.

### Test map

**M5-T23 — display formatters (unit)**

- `testPercentReturnFormatter_positiveReturn_showsPlusPrefix`
- `testPercentReturnFormatter_nilPercentReturn_showsEmDashOrHidden` (net USDC in == 0)
- `testDollarPnlFormatter_negativeShowsLossCopy`
- `testSlicePercentFormatter_formatsOneDecimal`
- `testRedeemSlider_dustMinimum_disablesSubmitBelowThreshold`

**DTO decoding (unit)** — one test file per major DTO

- `testGroupViewDTO_decodesFixtureWithPotAndMemberBoard`
- `testHomeViewDTO_decodesGroupAndPeopleBoards`
- `testBuyQuoteDTO_routableFalse_decodes`
- `testProposalDTO_decodesOpenPassedFailedExpiredStatuses`
- `testRedeemJobDTO_decodesDebitedSellingPayingSettled`
- `testPotRowDTO_afterHoursTrue_decodesLabelFlag` (M5-T20)

**MonacoAPIClient (unit)** — URLSession stub or protocol injection

- `testAPIClient_getHome_callsV1Home`
- `testAPIClient_searchAssets_usesGroupsAssetsQueryParam` (M5-T6)
- `testAPIClient_postQuotes_sendsSymbolAndUsdc` (M5-T7)
- `testAPIClient_postProposal_doesNotCallWhenRoutableFalse` (view-model gate)
- `testAPIClient_postRedeem_includesPayoutProofPayload` (M5-T18)
- `testAPIClient_afterRedeemSuccess_refreshesGroupAndHome` (M5-T19 — verify call sequence with stub)

**Copy audit (unit or snapshot)**

- `testMainFlowStrings_excludeWalletGasSeedPhraseMintAndNav` (M5-T22)
- `testSettingsAdvanced_containsExplorerLinksOnly`

**API boundary (unit)**

- `testProductFeatures_noDirectXStocksJupiterPythOrSolanaRpcUrls`

**M5-T24 — snapshots (optional)**

- `testGroupScreen_snapshot_matchesFixture`
- `testAppHome_snapshot_matchesFixture`

**Ticket → test coverage (parent features)**

| Ticket | Automated focus |
|--------|-----------------|
| M5-T1–T2 | Session gate routing with mocked auth state |
| M5-T3–T4 | Create/join form validation unit tests (password required when policy says so) |
| M5-T5 | Deposit poll state machine: pending → confirmed from fixture deposit DTO |
| M5-T6–T10 | Proposal flow: search → quote routable gate → vote buttons enabled for voter set |
| M5-T11–T16 | Board cells render server rank order without re-sorting by dollars |
| M5-T17–T19 | Redeem slider max == full equity; refresh triggers after success |
| M5-T20–T21 | After-hours label visible; addresses only in Settings Advanced strings |

### Fixtures / fakes

- `FixtureLoader` — JSON files under `apps/mobile/Tests/Fixtures/` recorded from Go API golden responses.
- `MockURLProtocol` or `MonacoAPIClientProtocol` — stub HTTP without network.
- `MockPrivySession` — signed-in / signed-out for session gate tests.
- Display tests use **server-provided** `"percentReturn": "0.12"` style strings; never derive expected % in the test.

### Property / invariant tests

None in Swift for M5. Fairness and ranking invariants live in M4 `packages/domain`. Mobile only asserts presentation faithfulness to fixtures.

## Manual verification

docs/product.md hackathon demo checklist. Two accounts on this machine’s gold slim sim (`$SIMSLIM_UDID`). Run `just run` for full stack.

1. Create group with join policy, voter set, threshold, and expiry.
2. Join second account. Two names on in-group board.
3. Both deposit mainnet USDC. Sweep UX and 0% boards until mark moves.
4. Search, propose `AAPLx`, pass vote. Swap success visible in UI and explorer.
5. Group screen: pot, slices, dollar P&L, in-group percent board.
6. App home: group board and people board.
7. Partial redeem to verified payout address. All three boards update; other member remains.
8. Copy audit on main flow: no wallet, gas, seed phrase, mint, or NAV. Explorer links only under Settings → Advanced.

## Out of scope

Android or web. App Store public listing. Full KYC or AML. On-chain governance UI. Meteora or Clawpump. Primary issuer mint or redeem. Production Privy webhooks.

## Open decisions

Same three as M4. Propose permissions, failed execute UX, creator dissolve. Wait for M4-T39 through M4-T41 before locking copy or buttons.

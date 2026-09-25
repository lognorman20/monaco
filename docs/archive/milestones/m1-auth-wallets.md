# M1. Auth and Privy wallets

**Goal.** Sign in with SMS or email and password via Privy Swift. Email and password are for M1 testing so you can create two accounts without SMS friction. The API upserts the user, provisions a member Solana wallet, and stores the map in Postgres. Thin group create provisions a Privy server treasury and stores the map. The app relayer fee payer exists in config. You can log in, see the member wallet, create a group, and see a treasury the backend controls.

**Depends on.** M0 complete.

**Owns.** `apps/backend/internal/config/`, `apps/backend/internal/privy/`, `apps/backend/internal/postgres/`, `apps/backend/internal/httpapi/`, `apps/backend/internal/app/`, `supabase/migrations/` for the four tables below, thin mobile login and create-group screens, `docs/how-to` only if you add Privy setup notes while implementing.

## Structure

```text
apps/backend/
  internal/app/
    session.go            Session, CreateGroup, GroupView (thin)
  internal/postgres/      Postgres store: users, member_wallets, groups, treasuries
  internal/privy/         Privy client (verify token, create member + group treasury wallets)
  internal/httpapi/       auth + groups handlers
apps/mobile/
  API/MonacoAPIClient.swift
  Features/Auth/          login (SMS, email+password)
  Features/Groups/        create group, show treasury address
```

`packages/domain` stays empty in M1. No deposit or share types yet.

## Flow

1. Mobile completes Privy login (SMS or email+password for testing).
2. `POST /v1/auth/session` with Privy access token.
3. `app.Session`: Privy client verifies token → Postgres upserts `users` on `privy_user_id` → `EnsureMemberWallet` (idempotent).
4. `GET /v1/me` returns user id, display name, member wallet Solana address.
5. `POST /v1/groups { name }`: insert thin `groups`, Privy client provisions group treasury, insert `treasuries`.
6. `GET /v1/groups/{id}` returns name and treasury address.
7. Relayer key material loads at API startup. Missing key fails boot. No signing until M2 sweeps.

Screens show Solana addresses as wiring proof. M5 hides them from the main flow.

## Data models

Voting, share units, NAV, and leaderboards stay out of M1.

**users**

- `id` uuid PK
- `privy_user_id` text unique not null
- `display_name` text, optional in M1
- `created_at` timestamptz

**member_wallets** (one per user)

- `id` uuid PK
- `user_id` uuid FK `users`, unique
- `privy_wallet_id` text not null
- `solana_address` text not null
- `created_at` timestamptz

**groups** (thin create only)

- `id` uuid PK
- `name` text not null
- `creator_user_id` uuid FK `users`
- `created_at` timestamptz

No join policy, voter set, threshold, or expiry columns yet.

**treasuries** (one per group)

- `id` uuid PK
- `group_id` uuid FK `groups`, unique
- `privy_wallet_id` text not null
- `solana_address` text not null
- `created_at` timestamptz

The relayer fee payer lives in API config, not a Postgres table. M1 loads the key material and fails startup if it is missing. No funding UX.

These screens may show Solana addresses. That is the proof for this milestone. M5 hides them on the main flow.

## Parallelization

Milestone gate: complete **Wave 5** before M2.

| Wave | Tracks (run in parallel within the wave) | Gate for next wave |
|------|------------------------------------------|--------------------|
| **1** | **Schema** M1-T1 · **Config** M1-T2 · **Privy client** M1-T3 · **Mobile auth setup** M1-T9, M1-T10 | M0 |
| **2** | **Backend session** M1-T4 · **Relayer boot** M1-T8 · **Mobile login UI** M1-T11 | Wave 1 |
| **3** | **Read APIs** M1-T5, M1-T7 · **Write API** M1-T6 | M1-T4 |
| **4** | **Mobile screens** M1-T12 · **Mobile screens** M1-T13 | M1-T5 + M1-T6 respectively |
| **5** | **Go tests** M1-T14, M1-T15 · **Swift test** M1-T17 · **just wiring** M1-T16 | Waves 3–4 |

Backend and mobile tracks run in parallel from Wave 1. Handler tests (T14–T15) and Swift client test (T17) run in parallel in Wave 5.

## Tickets

1. M1-T1 Add the migration for `users`, `member_wallets`, `groups`, and `treasuries`.
2. M1-T2 Add the Go config loader for Privy, localhost Postgres, and relayer credentials.
3. M1-T3 Add Privy server helpers that create Solana wallets.  
   Depends on: M1-T2
4. **Parent** M1-T4 Implement `POST /v1/auth/session` to verify the Privy token, upsert the user, and provision the member wallet idempotently.  
   Depends on: M1-T1, M1-T3
   - └ **Subissue of M1-T4** M1-T4a Verify Privy access token via Privy client
   - └ **Subissue of M1-T4** M1-T4b Upsert `users` row on `privy_user_id`
   - └ **Subissue of M1-T4** M1-T4c `EnsureMemberWallet` idempotent provision and persist `member_wallets`
5. M1-T5 Implement `GET /v1/me` returning user id, display name, and member wallet Solana address.  
   Depends on: M1-T4
6. **Parent** M1-T6 Implement `POST /v1/groups` to insert the group row and provision the treasury server wallet.  
   Depends on: M1-T4
   - └ **Subissue of M1-T6** M1-T6a Insert thin `groups` row
   - └ **Subissue of M1-T6** M1-T6b Privy client provisions group treasury and persist `treasuries`
7. M1-T7 Implement `GET /v1/groups/{id}` returning group name and treasury Solana address.  
   Depends on: M1-T6
8. M1-T8 Register relayer fee payer config and fail API startup if the key material does not load.  
   Depends on: M1-T2
9. M1-T9 Add the Privy SMS login path.
10. M1-T10 Enable Privy email and password login in the dashboard and app config.
11. M1-T11 Add email and password fields on the M1 login screen.  
    Depends on: M1-T9, M1-T10
12. M1-T12 Add the post-login screen that calls `GET /v1/me` and shows the connected member wallet address.  
    Depends on: M1-T5, M1-T11
13. M1-T13 Add the create-group screen that calls `POST /v1/groups` and shows the treasury address.  
    Depends on: M1-T6, M1-T7, M1-T12
14. M1-T14 Add Go handler tests for auth session and group create happy paths.  
    Depends on: M1-T4, M1-T6
15. M1-T15 Add a Go integration test that asserts Postgres rows after member and treasury wallet provision.  
    Depends on: M1-T4, M1-T6
16. M1-T16 Wire `just test backend` to cover auth and group Postgres integration against local Docker Postgres.  
    Depends on: M1-T14, M1-T15
17. M1-T17 Add a Swift unit test that the API client sends the Privy auth header after login.  
    Depends on: M1-T11

## Ticket details

#### M1-T1: Add the migration for users, member_wallets, groups, and treasuries

**Context**
M1 follows M0 scaffold. Privy auth, member wallets, and group treasuries must exist before deposits (M2).

Ticket `M1-T1` is wave **1** in `docs/milestones/m1-auth-wallets.md` under milestone **M1 — Auth and wallets**.

**Problem**
SQL tables/columns for `M1-T1` are missing from `supabase/migrations/`; migration runner cannot apply them.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `supabase/migrations/`. No drive-by refactors.
2. Add next sequential SQL file under `supabase/migrations/` with tables/columns from `docs/milestones/m1-auth-wallets.md`.
3. Run migration via existing runner (`apps/backend/internal/postgres/`). Do not hand-apply in prod.
4. Satisfy milestone gate: `just test backend` and/or `just test mobile` exit 0 with no new failing tests.

**API / data contract**
**No HTTP.** SQL migration only. Apply via migration runner invoked by API boot and `just test backend`.
**Test fakes (locked names):**
- `fakePrivyClient` (VerifySession, EnsureMemberWallet, EnsureTreasury)
- `integrationApp(t)` + `resetTables(t)`
- `fixtureSessionToken`

### Scope
- `supabase/migrations/`

**Out of scope (do not touch)**
- USDC deposits
- sweeps
- share units
- NAV
- votes
- Jupiter
- P&L boards
- Deposit or vote tables

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `supabase/migrations/`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `supabase/migrations/`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- None
- **Wave:** 1
#### M1-T2: Add the Go config loader for Privy, localhost Postgres, and relayer credentials

**Context**
M1 follows M0 scaffold. Privy auth, member wallets, and group treasuries must exist before deposits (M2).

Ticket `M1-T2` is wave **1** in `docs/milestones/m1-auth-wallets.md` under milestone **M1 — Auth and wallets**.

**Problem**
Behavior for `Add the Go config loader for Privy, localhost Postgres, and relayer credentials` is not implemented under `apps/backend/internal/config/`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/config/`. No drive-by refactors.
2. Satisfy milestone gate: `just test backend` and/or `just test mobile` exit 0 with no new failing tests.

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `fakePrivyClient` (VerifySession, EnsureMemberWallet, EnsureTreasury)
- `integrationApp(t)` + `resetTables(t)`
- `fixtureSessionToken`

### Scope
- `apps/backend/internal/config/`

**Out of scope (do not touch)**
- Privy API calls
- HTTP handlers

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/config/`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/config/`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- None
- **Wave:** 1
#### M1-T3: Add Privy server helpers that create Solana wallets

**Context**
M1 follows M0 scaffold. Privy auth, member wallets, and group treasuries must exist before deposits (M2).

Ticket `M1-T3` is wave **1** in `docs/milestones/m1-auth-wallets.md` under milestone **M1 — Auth and wallets**.
Blocked until dependencies land: M1-T2.

**Problem**
Behavior for `Add Privy server helpers that create Solana wallets` is not implemented under `apps/backend/internal/privy/`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/privy/`. No drive-by refactors.
2. Satisfy milestone gate: `just test backend` and/or `just test mobile` exit 0 with no new failing tests.

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `fakePrivyClient` (VerifySession, EnsureMemberWallet, EnsureTreasury)
- `integrationApp(t)` + `resetTables(t)`
- `fixtureSessionToken`

### Scope
- `apps/backend/internal/privy/`

**Out of scope (do not touch)**
- HTTP handlers
- Postgres store methods
- Session orchestration in internal/app

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/privy/`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/privy/`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M1-T2 — **do not start while open**
- **Wave:** 1
#### M1-T4: Implement POST /v1/auth/session to verify Privy token, upsert user, provision member wallet idempotently

**Context**
M1 follows M0 scaffold. Privy auth, member wallets, and group treasuries must exist before deposits (M2).

Ticket `M1-T4` is wave **2** in `docs/milestones/m1-auth-wallets.md` under milestone **M1 — Auth and wallets**.
Parent ticket; subissues: M1-T4a, M1-T4b, M1-T4c.
Blocked until dependencies land: M1-T1, M1-T3.

**Problem**
`POST /v1/auth/session` and handler code under `apps/backend/internal/app/session.go`, `apps/backend/internal/httpapi/auth.go` do not exist (or fail locked tests).

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/app/session.go`, `apps/backend/internal/httpapi/auth.go`. No drive-by refactors.
2. Parent glue: wire subissues M1-T4a, M1-T4b, M1-T4c into `apps/backend/internal/app/session.go` after each subissue lands.
3. Register `POST /v1/auth/session` in route table (`apps/backend/cmd/api/` or `internal/httpapi/`).
4. Implement handler in `apps/backend/internal/httpapi/auth.go` calling `internal/app` method; return JSON status codes from **API / data contract** below.
5. Add `TestPOST_auth_session_happyPath_returns200AndSetsSession` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**Route:** `POST /v1/auth/session`
**Auth:** Request body `{ "accessToken": "<privy_access_token>" }`. No session cookie.
**Happy:** 200 `{ userId, displayName, memberWalletAddress }`
**Failure codes:** 401 invalid/expired Privy token.
**Idempotency:** follow milestone doc keys (`tx_signature`, `execute_request_id`, `privy_user_id`, etc.).
**Test fakes (locked names):**
- `fakePrivyClient` (VerifySession, EnsureMemberWallet, EnsureTreasury)
- `integrationApp(t)` + `resetTables(t)`
- `fixtureSessionToken`

### Scope
- `apps/backend/internal/app/session.go`
- `apps/backend/internal/httpapi/auth.go`
- Route: `POST /v1/auth/session`

**Out of scope (do not touch)**
- Group create
- Deposits

**Acceptance Criteria**
- [ ] Test `TestPOST_auth_session_happyPath_returns200AndSetsSession` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/session.go`, `apps/backend/internal/httpapi/auth.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] All subissues (M1-T4a, M1-T4b, M1-T4c) closed before parent closes.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestPOST_auth_session_happyPath_returns200AndSetsSession` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/session.go`, `apps/backend/internal/httpapi/auth.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] All subissues (M1-T4a, M1-T4b, M1-T4c) closed before parent closes.
- [ ] Subissues closed: M1-T4a, M1-T4b, M1-T4c.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M1-T1 — **do not start while open**
- M1-T3 — **do not start while open**
- **Wave:** 2
- **Subissue:** M1-T4a
- **Subissue:** M1-T4b
- **Subissue:** M1-T4c
#### M1-T4a: Verify Privy access token via Privy client

**Context**
M1 follows M0 scaffold. Privy auth, member wallets, and group treasuries must exist before deposits (M2).

Ticket `M1-T4a` is wave **2** in `docs/milestones/m1-auth-wallets.md` under milestone **M1 — Auth and wallets**.
Subissue of M1-T4.
Blocked until dependencies land: M1-T3.

**Problem**
Subissue `M1-T4a` of `M1-T4`: symbols under `apps/backend/internal/privy/verify.go` not implemented; parent cannot close.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/privy/verify.go`. No drive-by refactors.
2. Subissue of `M1-T4`. Do not add routes or flows owned by parent/sibling subissues.
3. Add `TestSession_validPrivyToken_upsertsUserAndReturnsSession` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
4. Add `TestSession_invalidPrivyToken_returns401` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
5. Add `TestSession_expiredPrivyToken_returns401` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new public route.** Subissue of `M1-T4`. Internal symbols only in **Owns** paths.
**Test fakes (locked names):**
- `fakePrivyClient` (VerifySession, EnsureMemberWallet, EnsureTreasury)
- `integrationApp(t)` + `resetTables(t)`
- `fixtureSessionToken`

### Scope
- `apps/backend/internal/privy/verify.go`

**Out of scope (do not touch)**
- Postgres upsert
- Wallet provision

**Acceptance Criteria**
- [ ] Test `TestSession_validPrivyToken_upsertsUserAndReturnsSession` exists, uses AAA comments, and passes.
- [ ] Test `TestSession_invalidPrivyToken_returns401` exists, uses AAA comments, and passes.
- [ ] Test `TestSession_expiredPrivyToken_returns401` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/privy/verify.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestSession_validPrivyToken_upsertsUserAndReturnsSession` exists, uses AAA comments, and passes.
- [ ] Test `TestSession_invalidPrivyToken_returns401` exists, uses AAA comments, and passes.
- [ ] Test `TestSession_expiredPrivyToken_returns401` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/privy/verify.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M1-T3 — **do not start while open**
- **Wave:** 2
- **Parent:** M1-T4
#### M1-T4b: Upsert users row on privy_user_id

**Context**
M1 follows M0 scaffold. Privy auth, member wallets, and group treasuries must exist before deposits (M2).

Ticket `M1-T4b` is wave **2** in `docs/milestones/m1-auth-wallets.md` under milestone **M1 — Auth and wallets**.
Subissue of M1-T4.
Blocked until dependencies land: M1-T1, M1-T4a.

**Problem**
Subissue `M1-T4b` of `M1-T4`: symbols under `apps/backend/internal/postgres/users.go` not implemented; parent cannot close.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/postgres/users.go`. No drive-by refactors.
2. Subissue of `M1-T4`. Do not add routes or flows owned by parent/sibling subissues.
3. Add `TestSession_samePrivyUserTwice_doesNotDuplicateUsersRow` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new public route.** Subissue of `M1-T4`. Internal symbols only in **Owns** paths.
**Test fakes (locked names):**
- `fakePrivyClient` (VerifySession, EnsureMemberWallet, EnsureTreasury)
- `integrationApp(t)` + `resetTables(t)`
- `fixtureSessionToken`

### Scope
- `apps/backend/internal/postgres/users.go`

**Out of scope (do not touch)**
- Wallet provision
- HTTP routing

**Acceptance Criteria**
- [ ] Test `TestSession_samePrivyUserTwice_doesNotDuplicateUsersRow` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/postgres/users.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestSession_samePrivyUserTwice_doesNotDuplicateUsersRow` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/postgres/users.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M1-T1 — **do not start while open**
- M1-T4a — **do not start while open**
- **Wave:** 2
- **Parent:** M1-T4
#### M1-T4c: EnsureMemberWallet idempotent provision and persist member_wallets

**Context**
M1 follows M0 scaffold. Privy auth, member wallets, and group treasuries must exist before deposits (M2).

Ticket `M1-T4c` is wave **2** in `docs/milestones/m1-auth-wallets.md` under milestone **M1 — Auth and wallets**.
Subissue of M1-T4.
Blocked until dependencies land: M1-T4b, M1-T3.

**Problem**
Subissue `M1-T4c` of `M1-T4`: symbols under `apps/backend/internal/app/session.go`, `apps/backend/internal/postgres/member_wallets.go` not implemented; parent cannot close.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/app/session.go`, `apps/backend/internal/postgres/member_wallets.go`. No drive-by refactors.
2. Subissue of `M1-T4`. Do not add routes or flows owned by parent/sibling subissues.
3. Add `TestEnsureMemberWallet_firstSession_createsMemberWalletRow` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
4. Add `TestEnsureMemberWallet_repeatSession_reusesSameWallet` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new public route.** Subissue of `M1-T4`. Internal symbols only in **Owns** paths.
**Test fakes (locked names):**
- `fakePrivyClient` (VerifySession, EnsureMemberWallet, EnsureTreasury)
- `integrationApp(t)` + `resetTables(t)`
- `fixtureSessionToken`

### Scope
- `apps/backend/internal/app/session.go`
- `apps/backend/internal/postgres/member_wallets.go`

**Out of scope (do not touch)**
- Group treasury wallets

**Acceptance Criteria**
- [ ] Test `TestEnsureMemberWallet_firstSession_createsMemberWalletRow` exists, uses AAA comments, and passes.
- [ ] Test `TestEnsureMemberWallet_repeatSession_reusesSameWallet` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/session.go`, `apps/backend/internal/postgres/member_wallets.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestEnsureMemberWallet_firstSession_createsMemberWalletRow` exists, uses AAA comments, and passes.
- [ ] Test `TestEnsureMemberWallet_repeatSession_reusesSameWallet` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/session.go`, `apps/backend/internal/postgres/member_wallets.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M1-T4b — **do not start while open**
- M1-T3 — **do not start while open**
- **Wave:** 2
- **Parent:** M1-T4
#### M1-T5: Implement GET /v1/me returning user id, display name, and member wallet Solana address

**Context**
M1 follows M0 scaffold. Privy auth, member wallets, and group treasuries must exist before deposits (M2).

Ticket `M1-T5` is wave **3** in `docs/milestones/m1-auth-wallets.md` under milestone **M1 — Auth and wallets**.
Blocked until dependencies land: M1-T4.

**Problem**
`GET /v1/me` and handler code under `apps/backend/internal/httpapi/me.go`, `apps/backend/internal/app/session.go` do not exist (or fail locked tests).

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/httpapi/me.go`, `apps/backend/internal/app/session.go`. No drive-by refactors.
2. Register `GET /v1/me` in route table (`apps/backend/cmd/api/` or `internal/httpapi/`).
3. Implement handler in `apps/backend/internal/app/session.go` calling `internal/app` method; return JSON status codes from **API / data contract** below.
4. Add `TestGET_me_authenticated_returnsUserIdDisplayNameAndMemberAddress` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
5. Add `TestGET_me_missingAuth_returns401` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
6. Add `TestGET_me_unknownUser_returns404` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**Route:** `GET /v1/me`
**Auth:** Privy bearer token in `Authorization` header (same as M1 session middleware).
**Happy:** 200 with JSON body defined in milestone doc for this route.
**Failure codes:** 401 missing/invalid auth; 400 invalid body; 403 non-member; 404 unknown resource.
**Idempotency:** follow milestone doc keys (`tx_signature`, `execute_request_id`, `privy_user_id`, etc.).
**Test fakes (locked names):**
- `fakePrivyClient` (VerifySession, EnsureMemberWallet, EnsureTreasury)
- `integrationApp(t)` + `resetTables(t)`
- `fixtureSessionToken`

### Scope
- `apps/backend/internal/httpapi/me.go`
- `apps/backend/internal/app/session.go`
- Route: `GET /v1/me`

**Out of scope (do not touch)**
- Group endpoints

**Acceptance Criteria**
- [ ] Test `TestGET_me_authenticated_returnsUserIdDisplayNameAndMemberAddress` exists, uses AAA comments, and passes.
- [ ] Test `TestGET_me_missingAuth_returns401` exists, uses AAA comments, and passes.
- [ ] Test `TestGET_me_unknownUser_returns404` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/httpapi/me.go`, `apps/backend/internal/app/session.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestGET_me_authenticated_returnsUserIdDisplayNameAndMemberAddress` exists, uses AAA comments, and passes.
- [ ] Test `TestGET_me_missingAuth_returns401` exists, uses AAA comments, and passes.
- [ ] Test `TestGET_me_unknownUser_returns404` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/httpapi/me.go`, `apps/backend/internal/app/session.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M1-T4 — **do not start while open**
- **Wave:** 3
#### M1-T6: Implement POST /v1/groups to insert group row and provision treasury server wallet

**Context**
M1 follows M0 scaffold. Privy auth, member wallets, and group treasuries must exist before deposits (M2).

Ticket `M1-T6` is wave **3** in `docs/milestones/m1-auth-wallets.md` under milestone **M1 — Auth and wallets**.
Parent ticket; subissues: M1-T6a, M1-T6b.
Blocked until dependencies land: M1-T4.

**Problem**
`POST /v1/groups` and handler code under `apps/backend/internal/app/group.go`, `apps/backend/internal/httpapi/groups.go` do not exist (or fail locked tests).

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/app/group.go`, `apps/backend/internal/httpapi/groups.go`. No drive-by refactors.
2. Parent glue: wire subissues M1-T6a, M1-T6b into `apps/backend/internal/app/group.go` after each subissue lands.
3. Register `POST /v1/groups` in route table (`apps/backend/cmd/api/` or `internal/httpapi/`).
4. Implement handler in `apps/backend/internal/httpapi/groups.go` calling `internal/app` method; return JSON status codes from **API / data contract** below.
5. Add `TestCreateGroup_insertsGroupAndTreasuryRows` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**Route:** `POST /v1/groups`
**Auth:** Privy bearer token in `Authorization` header (same as M1 session middleware).
**Happy:** 200 with JSON body defined in milestone doc for this route.
**Failure codes:** 401 missing/invalid auth; 400 invalid body; 403 non-member; 404 unknown resource.
**Idempotency:** follow milestone doc keys (`tx_signature`, `execute_request_id`, `privy_user_id`, etc.).
**Test fakes (locked names):**
- `fakePrivyClient` (VerifySession, EnsureMemberWallet, EnsureTreasury)
- `integrationApp(t)` + `resetTables(t)`
- `fixtureSessionToken`

### Scope
- `apps/backend/internal/app/group.go`
- `apps/backend/internal/httpapi/groups.go`
- Route: `POST /v1/groups`

**Out of scope (do not touch)**
- Join policy
- Votes
- Deposits

**Acceptance Criteria**
- [ ] Test `TestCreateGroup_insertsGroupAndTreasuryRows` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/group.go`, `apps/backend/internal/httpapi/groups.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] All subissues (M1-T6a, M1-T6b) closed before parent closes.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestCreateGroup_insertsGroupAndTreasuryRows` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/app/group.go`, `apps/backend/internal/httpapi/groups.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] All subissues (M1-T6a, M1-T6b) closed before parent closes.
- [ ] Subissues closed: M1-T6a, M1-T6b.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M1-T4 — **do not start while open**
- **Wave:** 3
- **Subissue:** M1-T6a
- **Subissue:** M1-T6b
#### M1-T6a: Insert thin groups row

**Context**
M1 follows M0 scaffold. Privy auth, member wallets, and group treasuries must exist before deposits (M2).

Ticket `M1-T6a` is wave **3** in `docs/milestones/m1-auth-wallets.md` under milestone **M1 — Auth and wallets**.
Subissue of M1-T6.
Blocked until dependencies land: M1-T4.

**Problem**
Subissue `M1-T6a` of `M1-T6`: symbols under `apps/backend/internal/postgres/groups.go`, `apps/backend/internal/app/group.go` not implemented; parent cannot close.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/postgres/groups.go`, `apps/backend/internal/app/group.go`. No drive-by refactors.
2. Subissue of `M1-T6`. Do not add routes or flows owned by parent/sibling subissues.
3. Add `TestCreateGroup_insertsGroupAndTreasuryRows` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new public route.** Subissue of `M1-T6`. Internal symbols only in **Owns** paths.
**Test fakes (locked names):**
- `fakePrivyClient` (VerifySession, EnsureMemberWallet, EnsureTreasury)
- `integrationApp(t)` + `resetTables(t)`
- `fixtureSessionToken`

### Scope
- `apps/backend/internal/postgres/groups.go`
- `apps/backend/internal/app/group.go`

**Out of scope (do not touch)**
- Treasury Privy provision

**Acceptance Criteria**
- [ ] Test `TestCreateGroup_insertsGroupAndTreasuryRows` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/postgres/groups.go`, `apps/backend/internal/app/group.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestCreateGroup_insertsGroupAndTreasuryRows` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/postgres/groups.go`, `apps/backend/internal/app/group.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M1-T4 — **do not start while open**
- **Wave:** 3
- **Parent:** M1-T6
#### M1-T6b: Privy client provisions group treasury and persist treasuries

**Context**
M1 follows M0 scaffold. Privy auth, member wallets, and group treasuries must exist before deposits (M2).

Ticket `M1-T6b` is wave **3** in `docs/milestones/m1-auth-wallets.md` under milestone **M1 — Auth and wallets**.
Subissue of M1-T6.
Blocked until dependencies land: M1-T6a, M1-T3.

**Problem**
Subissue `M1-T6b` of `M1-T6`: symbols under `apps/backend/internal/privy/treasury.go`, `apps/backend/internal/postgres/treasuries.go` not implemented; parent cannot close.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/privy/treasury.go`, `apps/backend/internal/postgres/treasuries.go`. No drive-by refactors.
2. Subissue of `M1-T6`. Do not add routes or flows owned by parent/sibling subissues.
3. Add `TestCreateGroup_provisionsTreasuryViaPrivyClient` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new public route.** Subissue of `M1-T6`. Internal symbols only in **Owns** paths.
**Test fakes (locked names):**
- `fakePrivyClient` (VerifySession, EnsureMemberWallet, EnsureTreasury)
- `integrationApp(t)` + `resetTables(t)`
- `fixtureSessionToken`

### Scope
- `apps/backend/internal/privy/treasury.go`
- `apps/backend/internal/postgres/treasuries.go`

**Out of scope (do not touch)**
- Join policy columns

**Acceptance Criteria**
- [ ] Test `TestCreateGroup_provisionsTreasuryViaPrivyClient` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/privy/treasury.go`, `apps/backend/internal/postgres/treasuries.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestCreateGroup_provisionsTreasuryViaPrivyClient` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/privy/treasury.go`, `apps/backend/internal/postgres/treasuries.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M1-T6a — **do not start while open**
- M1-T3 — **do not start while open**
- **Wave:** 3
- **Parent:** M1-T6
#### M1-T7: Implement GET /v1/groups/{id} returning group name and treasury Solana address

**Context**
M1 follows M0 scaffold. Privy auth, member wallets, and group treasuries must exist before deposits (M2).

Ticket `M1-T7` is wave **3** in `docs/milestones/m1-auth-wallets.md` under milestone **M1 — Auth and wallets**.
Blocked until dependencies land: M1-T6.

**Problem**
`GET /v1/groups/{id}` and handler code under `apps/backend/internal/httpapi/groups.go` do not exist (or fail locked tests).

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/httpapi/groups.go`. No drive-by refactors.
2. Register `GET /v1/groups/{id}` in route table (`apps/backend/cmd/api/` or `internal/httpapi/`).
3. Implement handler in `apps/backend/internal/httpapi/groups.go` calling `internal/app` method; return JSON status codes from **API / data contract** below.
4. Add `TestGET_group_byId_returnsNameAndTreasuryAddress` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
5. Add `TestGET_group_nonMemberOrUnknown_returns404` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
6. Add `TestGET_group_missingAuth_returns401` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**Route:** `GET /v1/groups/{id}`
**Auth:** Privy bearer token in `Authorization` header (same as M1 session middleware).
**Happy:** 200 with JSON body defined in milestone doc for this route.
**Failure codes:** 401 missing/invalid auth; 400 invalid body; 403 non-member; 404 unknown resource.
**Idempotency:** follow milestone doc keys (`tx_signature`, `execute_request_id`, `privy_user_id`, etc.).
**Test fakes (locked names):**
- `fakePrivyClient` (VerifySession, EnsureMemberWallet, EnsureTreasury)
- `integrationApp(t)` + `resetTables(t)`
- `fixtureSessionToken`

### Scope
- `apps/backend/internal/httpapi/groups.go`
- Route: `GET /v1/groups/{id}`

**Out of scope (do not touch)**
- Member list
- NAV

**Acceptance Criteria**
- [ ] Test `TestGET_group_byId_returnsNameAndTreasuryAddress` exists, uses AAA comments, and passes.
- [ ] Test `TestGET_group_nonMemberOrUnknown_returns404` exists, uses AAA comments, and passes.
- [ ] Test `TestGET_group_missingAuth_returns401` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/httpapi/groups.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestGET_group_byId_returnsNameAndTreasuryAddress` exists, uses AAA comments, and passes.
- [ ] Test `TestGET_group_nonMemberOrUnknown_returns404` exists, uses AAA comments, and passes.
- [ ] Test `TestGET_group_missingAuth_returns401` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/httpapi/groups.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M1-T6 — **do not start while open**
- **Wave:** 3
#### M1-T8: Register relayer fee payer config and fail API startup if key material does not load

**Context**
M1 follows M0 scaffold. Privy auth, member wallets, and group treasuries must exist before deposits (M2).

Ticket `M1-T8` is wave **2** in `docs/milestones/m1-auth-wallets.md` under milestone **M1 — Auth and wallets**.
Blocked until dependencies land: M1-T2.

**Problem**
Behavior for `Register relayer fee payer config and fail API startup if key material does not load` is not implemented under `apps/backend/internal/config/relayer.go`, `apps/backend/cmd/api/main.go`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/config/relayer.go`, `apps/backend/cmd/api/main.go`. No drive-by refactors.
2. Add `TestAPIServer_missingRelayerKey_failsStartup` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
3. Add `TestAPIServer_validRelayerKey_startsSuccessfully` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `fakePrivyClient` (VerifySession, EnsureMemberWallet, EnsureTreasury)
- `integrationApp(t)` + `resetTables(t)`
- `fixtureSessionToken`

### Scope
- `apps/backend/internal/config/relayer.go`
- `apps/backend/cmd/api/main.go`

**Out of scope (do not touch)**
- Signing sweeps until M2
- Funding UX

**Acceptance Criteria**
- [ ] Test `TestAPIServer_missingRelayerKey_failsStartup` exists, uses AAA comments, and passes.
- [ ] Test `TestAPIServer_validRelayerKey_startsSuccessfully` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/config/relayer.go`, `apps/backend/cmd/api/main.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestAPIServer_missingRelayerKey_failsStartup` exists, uses AAA comments, and passes.
- [ ] Test `TestAPIServer_validRelayerKey_startsSuccessfully` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/config/relayer.go`, `apps/backend/cmd/api/main.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M1-T2 — **do not start while open**
- **Wave:** 2
#### M1-T9: Add the Privy SMS login path

**Context**
M1 follows M0 scaffold. Privy auth, member wallets, and group treasuries must exist before deposits (M2).

Ticket `M1-T9` is wave **1** in `docs/milestones/m1-auth-wallets.md` under milestone **M1 — Auth and wallets**.

**Problem**
SwiftUI flow in `apps/mobile/Features/Auth/` is missing or still scaffold; product/API wiring for `Add the Privy SMS login path` not shipped.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Features/Auth/`. No drive-by refactors.
2. Wire Privy Swift SDK in `apps/mobile/Features/Auth/`; on success obtain access token for `POST /v1/auth/session`.
3. Build SwiftUI screen in **Owns**; call `MonacoAPIClient` only (no xStocks/Jupiter/Pyth/Solana RPC).
4. Compile gate: `just build mobile` and `just test mobile` exit 0.

**API / data contract**
**Mobile consumes existing backend JSON only.** No direct calls to xStocks, Jupiter, Pyth, or Solana RPC from Swift. Use `MonacoAPIClient` methods matching backend routes in milestone doc.
**Test fakes (locked names):**
- `fakePrivyClient` (VerifySession, EnsureMemberWallet, EnsureTreasury)
- `integrationApp(t)` + `resetTables(t)`
- `fixtureSessionToken`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/Features/Auth/`

**Out of scope (do not touch)**
- Backend session handler
- Email/password UI

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Auth/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Auth/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- None
- **Wave:** 1
#### M1-T10: Enable Privy email and password login in dashboard and app config

**Context**
M1 follows M0 scaffold. Privy auth, member wallets, and group treasuries must exist before deposits (M2).

Ticket `M1-T10` is wave **1** in `docs/milestones/m1-auth-wallets.md` under milestone **M1 — Auth and wallets**.

**Problem**
SwiftUI flow in `apps/mobile/Features/Auth/`, `.env.example` is missing or still scaffold; product/API wiring for `Enable Privy email and password login in dashboard and app config` not shipped.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Features/Auth/`, `.env.example`. No drive-by refactors.
2. Wire Privy Swift SDK in `apps/mobile/Features/Auth/`; on success obtain access token for `POST /v1/auth/session`.
3. Build SwiftUI screen in **Owns**; call `MonacoAPIClient` only (no xStocks/Jupiter/Pyth/Solana RPC).
4. Compile gate: `just build mobile` and `just test mobile` exit 0.
5. **Manual (after automated green):** Enable email+password in Privy dashboard

**API / data contract**
**Mobile consumes existing backend JSON only.** No direct calls to xStocks, Jupiter, Pyth, or Solana RPC from Swift. Use `MonacoAPIClient` methods matching backend routes in milestone doc.
**Test fakes (locked names):**
- `fakePrivyClient` (VerifySession, EnsureMemberWallet, EnsureTreasury)
- `integrationApp(t)` + `resetTables(t)`
- `fixtureSessionToken`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/Features/Auth/`
- `.env.example`

**Out of scope (do not touch)**
- Backend changes

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Auth/`, `.env.example`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Manual (only for this ticket):**
- Enable email+password in Privy dashboard

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Auth/`, `.env.example`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- None
- **Wave:** 1
#### M1-T11: Add email and password fields on the M1 login screen

**Context**
M1 follows M0 scaffold. Privy auth, member wallets, and group treasuries must exist before deposits (M2).

Ticket `M1-T11` is wave **2** in `docs/milestones/m1-auth-wallets.md` under milestone **M1 — Auth and wallets**.
Blocked until dependencies land: M1-T9, M1-T10.

**Problem**
SwiftUI flow in `apps/mobile/Features/Auth/LoginView.swift` is missing or still scaffold; product/API wiring for `Add email and password fields on the M1 login screen` not shipped.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Features/Auth/LoginView.swift`. No drive-by refactors.
2. Build SwiftUI screen in **Owns**; call `MonacoAPIClient` only (no xStocks/Jupiter/Pyth/Solana RPC).
3. Compile gate: `just build mobile` and `just test mobile` exit 0.

**API / data contract**
**Mobile consumes existing backend JSON only.** No direct calls to xStocks, Jupiter, Pyth, or Solana RPC from Swift. Use `MonacoAPIClient` methods matching backend routes in milestone doc.
**Test fakes (locked names):**
- `fakePrivyClient` (VerifySession, EnsureMemberWallet, EnsureTreasury)
- `integrationApp(t)` + `resetTables(t)`
- `fixtureSessionToken`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/Features/Auth/LoginView.swift`

**Out of scope (do not touch)**
- Product launch shell (M5)

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Auth/LoginView.swift`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Auth/LoginView.swift`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M1-T9 — **do not start while open**
- M1-T10 — **do not start while open**
- **Wave:** 2
#### M1-T12: Add post-login screen calling GET /v1/me and showing member wallet address

**Context**
M1 follows M0 scaffold. Privy auth, member wallets, and group treasuries must exist before deposits (M2).

Ticket `M1-T12` is wave **4** in `docs/milestones/m1-auth-wallets.md` under milestone **M1 — Auth and wallets**.
Blocked until dependencies land: M1-T5, M1-T11.

**Problem**
`GET /v1/me` and handler code under `apps/mobile/Features/Auth/`, `apps/mobile/API/MonacoAPIClient.swift` do not exist (or fail locked tests).

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Features/Auth/`, `apps/mobile/API/MonacoAPIClient.swift`. No drive-by refactors.
2. Register `GET /v1/me` in route table (`apps/backend/cmd/api/` or `internal/httpapi/`).
3. Implement handler in `apps/mobile/API/MonacoAPIClient.swift` calling `internal/app` method; return JSON status codes from **API / data contract** below.
4. Build SwiftUI screen in **Owns**; call `MonacoAPIClient` only (no xStocks/Jupiter/Pyth/Solana RPC).
5. Compile gate: `just build mobile` and `just test mobile` exit 0.
6. **Manual (after automated green):** Confirm Solana address on slim sim

**API / data contract**
**Route:** `GET /v1/me`
**Auth:** Privy bearer token in `Authorization` header (same as M1 session middleware).
**Happy:** 200 with JSON body defined in milestone doc for this route.
**Failure codes:** 401 missing/invalid auth; 400 invalid body; 403 non-member; 404 unknown resource.
**Idempotency:** follow milestone doc keys (`tx_signature`, `execute_request_id`, `privy_user_id`, etc.).
**Test fakes (locked names):**
- `fakePrivyClient` (VerifySession, EnsureMemberWallet, EnsureTreasury)
- `integrationApp(t)` + `resetTables(t)`
- `fixtureSessionToken`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/Features/Auth/`
- `apps/mobile/API/MonacoAPIClient.swift`
- Route: `GET /v1/me`

**Out of scope (do not touch)**
- Hiding addresses from main flow (M5)

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Auth/`, `apps/mobile/API/MonacoAPIClient.swift`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Manual (only for this ticket):**
- Confirm Solana address on slim sim

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Features/Auth/`, `apps/mobile/API/MonacoAPIClient.swift`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M1-T5 — **do not start while open**
- M1-T11 — **do not start while open**
- **Wave:** 4
#### M1-T13: Add create-group screen calling POST /v1/groups and showing treasury address

**Context**
M1 follows M0 scaffold. Privy auth, member wallets, and group treasuries must exist before deposits (M2).

Ticket `M1-T13` is wave **4** in `docs/milestones/m1-auth-wallets.md` under milestone **M1 — Auth and wallets**.
Blocked until dependencies land: M1-T6, M1-T7, M1-T12.

**Problem**
`POST /v1/groups` and handler code under `apps/mobile/Features/Groups/` do not exist (or fail locked tests).

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Features/Groups/`. No drive-by refactors.
2. Register `POST /v1/groups` in route table (`apps/backend/cmd/api/` or `internal/httpapi/`).
3. Implement handler in `apps/mobile/Features/Groups/` calling `internal/app` method; return JSON status codes from **API / data contract** below.
4. Build SwiftUI screen in **Owns**; call `MonacoAPIClient` only (no xStocks/Jupiter/Pyth/Solana RPC).
5. Compile gate: `just build mobile` and `just test mobile` exit 0.

**API / data contract**
**Route:** `POST /v1/groups`
**Auth:** Privy bearer token in `Authorization` header (same as M1 session middleware).
**Happy:** 200 with JSON body defined in milestone doc for this route.
**Failure codes:** 401 missing/invalid auth; 400 invalid body; 403 non-member; 404 unknown resource.
**Idempotency:** follow milestone doc keys (`tx_signature`, `execute_request_id`, `privy_user_id`, etc.).
**Test fakes (locked names):**
- `fakePrivyClient` (VerifySession, EnsureMemberWallet, EnsureTreasury)
- `integrationApp(t)` + `resetTables(t)`
- `fixtureSessionToken`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/Features/Groups/`
- Route: `POST /v1/groups`

**Out of scope (do not touch)**
- Join policy UI (M5)

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
- M1-T6 — **do not start while open**
- M1-T7 — **do not start while open**
- M1-T12 — **do not start while open**
- **Wave:** 4
#### M1-T14: Add Go handler tests for auth session and group create happy paths

**Context**
M1 follows M0 scaffold. Privy auth, member wallets, and group treasuries must exist before deposits (M2).

Ticket `M1-T14` is wave **5** in `docs/milestones/m1-auth-wallets.md` under milestone **M1 — Auth and wallets**.
Blocked until dependencies land: M1-T4, M1-T6.

**Problem**
Locked test names for `M1-T14` are absent or not wired into `just test` recipes; `apps/backend/internal/httpapi/*_test.go` incomplete.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/httpapi/*_test.go`. No drive-by refactors.
2. Add `TestPOST_auth_session_happyPath_returns200AndSetsSession` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
3. Add `TestPOST_groups_missingAuth_returns401` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new API.** Tests and/or `Justfile` wiring only.
**Test fakes (locked names):**
- `fakePrivyClient` (VerifySession, EnsureMemberWallet, EnsureTreasury)
- `integrationApp(t)` + `resetTables(t)`
- `fixtureSessionToken`

### Scope
- `apps/backend/internal/httpapi/*_test.go`

**Out of scope (do not touch)**
- Integration Postgres row asserts (M1-T15)

**Acceptance Criteria**
- [ ] Test `TestPOST_auth_session_happyPath_returns200AndSetsSession` exists, uses AAA comments, and passes.
- [ ] Test `TestPOST_groups_missingAuth_returns401` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/httpapi/*_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestPOST_auth_session_happyPath_returns200AndSetsSession` exists, uses AAA comments, and passes.
- [ ] Test `TestPOST_groups_missingAuth_returns401` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/httpapi/*_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M1-T4 — **do not start while open**
- M1-T6 — **do not start while open**
- **Wave:** 5
#### M1-T15: Add Go integration test asserting Postgres rows after member and treasury wallet provision

**Context**
M1 follows M0 scaffold. Privy auth, member wallets, and group treasuries must exist before deposits (M2).

Ticket `M1-T15` is wave **5** in `docs/milestones/m1-auth-wallets.md` under milestone **M1 — Auth and wallets**.
Blocked until dependencies land: M1-T4, M1-T6.

**Problem**
Locked test names for `M1-T15` are absent or not wired into `just test` recipes; `apps/backend/internal/postgres/*_integration_test.go` incomplete.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/postgres/*_integration_test.go`. No drive-by refactors.
2. Add `TestEnsureMemberWallet_firstSession_createsMemberWalletRow` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
3. Add `TestCreateGroup_insertsGroupAndTreasuryRows` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new API.** Tests and/or `Justfile` wiring only.
**Test fakes (locked names):**
- `fakePrivyClient` (VerifySession, EnsureMemberWallet, EnsureTreasury)
- `integrationApp(t)` + `resetTables(t)`
- `fixtureSessionToken`

### Scope
- `apps/backend/internal/postgres/*_integration_test.go`

**Out of scope (do not touch)**
- Mobile tests

**Acceptance Criteria**
- [ ] Test `TestEnsureMemberWallet_firstSession_createsMemberWalletRow` exists, uses AAA comments, and passes.
- [ ] Test `TestCreateGroup_insertsGroupAndTreasuryRows` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/postgres/*_integration_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestEnsureMemberWallet_firstSession_createsMemberWalletRow` exists, uses AAA comments, and passes.
- [ ] Test `TestCreateGroup_insertsGroupAndTreasuryRows` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/postgres/*_integration_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M1-T4 — **do not start while open**
- M1-T6 — **do not start while open**
- **Wave:** 5
#### M1-T16: Wire just test backend to cover auth and group Postgres integration

**Context**
M1 follows M0 scaffold. Privy auth, member wallets, and group treasuries must exist before deposits (M2).

Ticket `M1-T16` is wave **5** in `docs/milestones/m1-auth-wallets.md` under milestone **M1 — Auth and wallets**.
Blocked until dependencies land: M1-T14, M1-T15.

**Problem**
Locked test names for `M1-T16` are absent or not wired into `just test` recipes; `Justfile` incomplete.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `Justfile`. No drive-by refactors.
2. Update root `Justfile` recipes so locked tests run under `just test backend` and/or `just test mobile`. Exit 0 required.
3. Satisfy milestone gate: `just test backend` and/or `just test mobile` exit 0 with no new failing tests.

**API / data contract**
**No new API.** Tests and/or `Justfile` wiring only.
**Test fakes (locked names):**
- `fakePrivyClient` (VerifySession, EnsureMemberWallet, EnsureTreasury)
- `integrationApp(t)` + `resetTables(t)`
- `fixtureSessionToken`

### Scope
- `Justfile`

**Out of scope (do not touch)**
- just test mobile wiring beyond M0

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `Justfile`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `Justfile`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M1-T14 — **do not start while open**
- M1-T15 — **do not start while open**
- **Wave:** 5
#### M1-T17: Add Swift unit test that API client sends Privy auth header after login

**Context**
M1 follows M0 scaffold. Privy auth, member wallets, and group treasuries must exist before deposits (M2).

Ticket `M1-T17` is wave **5** in `docs/milestones/m1-auth-wallets.md` under milestone **M1 — Auth and wallets**.
Blocked until dependencies land: M1-T11.

**Problem**
Locked test names for `M1-T17` are absent or not wired into `just test` recipes; `apps/mobile/Tests/` incomplete.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Tests/`. No drive-by refactors.
2. Wire Privy Swift SDK in `apps/mobile/Features/Auth/`; on success obtain access token for `POST /v1/auth/session`.
3. Add `testAPIClient_afterLogin_sendsAuthorizationHeader` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
4. Add `testMeDTO_decodesFixtureJSON` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new API.** Tests and/or `Justfile` wiring only.
**Test fakes (locked names):**
- `fakePrivyClient` (VerifySession, EnsureMemberWallet, EnsureTreasury)
- `integrationApp(t)` + `resetTables(t)`
- `fixtureSessionToken`
- `MockURLProtocol` for API client unit tests

### Scope
- `apps/mobile/Tests/`

**Out of scope (do not touch)**
- UI snapshot tests

**Acceptance Criteria**
- [ ] Test `testAPIClient_afterLogin_sendsAuthorizationHeader` exists, uses AAA comments, and passes.
- [ ] Test `testMeDTO_decodesFixtureJSON` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Tests/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `testAPIClient_afterLogin_sendsAuthorizationHeader` exists, uses AAA comments, and passes.
- [ ] Test `testMeDTO_decodesFixtureJSON` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Tests/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M1-T11 — **do not start while open**
- **Wave:** 5

## Automated verification

**Commands.** `just test backend`, `just test mobile`.

### Test layers

| Layer | Runs under | What it proves |
|-------|------------|----------------|
| **Go unit** | `just test backend` | `app.Session`, `app.CreateGroup`, and HTTP handlers with **fake Privy client** — no network, no real DB. |
| **Go integration** | `just test backend` | `POST /v1/auth/session`, `POST /v1/groups`, `GET /v1/me`, `GET /v1/groups/{id}` against local Docker Postgres with fake Privy. |
| **Swift unit** | `just test mobile` | `MonacoAPIClient` attaches Privy auth header after login; DTO decode from fixture JSON. |

All Go tests use AAA comments (`// Arrange`, `// Act`, `// Assert`). One key assertion per test. Split tests when the name would join unrelated behaviors with "and".

### Test map

**M1-T4 — session (unit + integration)**

- `TestSession_validPrivyToken_upsertsUserAndReturnsSession` (M1-T4a–c)
- `TestSession_invalidPrivyToken_returns401`
- `TestSession_expiredPrivyToken_returns401`
- `TestSession_samePrivyUserTwice_doesNotDuplicateUsersRow` (idempotent upsert on `privy_user_id`)
- `TestEnsureMemberWallet_firstSession_createsMemberWalletRow`
- `TestEnsureMemberWallet_repeatSession_reusesSameWallet` (idempotent provision)
- `TestPOST_auth_session_happyPath_returns200AndSetsSession` (integration)

**M1-T5 — GET /v1/me (integration)**

- `TestGET_me_authenticated_returnsUserIdDisplayNameAndMemberAddress`
- `TestGET_me_missingAuth_returns401`
- `TestGET_me_unknownUser_returns404`

**M1-T6 — group create (unit + integration)**

- `TestCreateGroup_insertsGroupAndTreasuryRows` (M1-T6a–b)
- `TestCreateGroup_provisionsTreasuryViaPrivyClient`
- `TestCreateGroup_repeatCallForSameUser_createsDistinctGroups` (not idempotent on group name — each create is new)
- `TestPOST_groups_missingAuth_returns401`
- `TestPOST_groups_emptyName_returns400`

**M1-T7 — GET /v1/groups/{id} (integration)**

- `TestGET_group_byId_returnsNameAndTreasuryAddress`
- `TestGET_group_nonMemberOrUnknown_returns404` (authz: only creator/member per M1 thin rules)
- `TestGET_group_missingAuth_returns401`

**M1-T8 — relayer boot (unit)**

- `TestAPIServer_missingRelayerKey_failsStartup`
- `TestAPIServer_validRelayerKey_startsSuccessfully`

**M1-T17 — Swift API client (unit)**

- `testAPIClient_afterLogin_sendsAuthorizationHeader`
- `testMeDTO_decodesFixtureJSON`

**Product constraints encoded in tests**

- Auth paths: SMS and email+password both reach `POST /v1/auth/session` (handler does not branch on login method).
- No Sign in with Apple.
- No deposit, share, or vote tables touched.

### Fixtures / fakes

- `fakePrivyClient` — implements `VerifySession`, `EnsureMemberWallet`, `EnsureTreasury`; returns deterministic Solana addresses per user/group id.
- `fakeRelayerConfig` — valid/invalid key material for boot tests.
- `buildUser(overrides)`, `buildGroup(overrides)` — domain factories.
- `integrationApp(t)` — real Postgres store + fake Privy; `resetTables(t)` in `beforeEach`.
- `fixtureSessionToken` — signed or stub token accepted by test middleware.

**Testability note.** If `app.Session` or `CreateGroup` instantiate Privy internally, refactor to inject `PrivyClient` before writing handler tests (M1-T14).

### Property / invariant tests

None in M1. Wallet idempotency covered by example-based repeat-session tests above.

## Manual verification

Human checks for Privy dashboard, slim sim UX, and explorer-style address confirmation. Automated tests already assert Postgres rows and JSON shapes.

1. In the Privy dashboard, enable SMS and email and password.
2. Copy secrets into `.env`. Keep `DATABASE_URL` on localhost from compose.
3. Run `just run` (or `just run backend` then `just run mobile`). Confirm the API listens and relayer config loaded.
4. Sign in with email and password on the slim sim. SMS also works for the same flow.
5. On the post-login screen, confirm a Solana address appears as the connected member wallet.
6. In the Privy dashboard user record, confirm a member Solana wallet exists and matches that address.
7. Sign out. Sign in with a second email and password. Confirm a different member wallet address in the UI.
8. On mobile, create a group with a name and submit.
9. On the group screen, confirm a treasury Solana address appears.
10. In the Privy dashboard, confirm a server wallet for that treasury matches the mobile address.

## Out of scope

USDC deposits, sweeps, share units, NAV, votes, join passwords, Jupiter, P&L boards, payouts, Privy production webhooks, relayer funding beyond config existence, custom Solana programs, hosted Supabase for local dev.

## Open decisions

None from the README list. Do not lock buy proposer, failed execute retry, or creator dissolve here.

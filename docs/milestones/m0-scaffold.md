# M0. Scaffold the monorepo

**Goal.** Land a runnable skeleton. The Go API serves health. The SwiftUI iOS 18 app builds. Local Docker Postgres starts for dev and test. Later milestones run through `just`.

**Depends on.** Nothing. The repo is greenfield besides `README.md`.

**Owns.** `Justfile`, `docker-compose.yml`, `apps/backend/`, `apps/mobile/`, `packages/domain/`, `supabase/migrations/`, `docs/`, `scripts/`, `.env.example`, `.gitignore`.

## Structure

```text
monaco/
  Justfile                      build, test, run per backend|mobile; root just run
  docker-compose.yml            Postgres 16, project monaco, port 54322
  .env.example                  DATABASE_URL localhost, Privy placeholders
  apps/backend/
    cmd/api/                    main, route registration
    internal/httpapi/           health handler only in M0
    internal/app/               package stub (no product methods yet)
    internal/postgres/          migration runner hook
  apps/mobile/
    Monaco.xcodeproj            iOS 18+, scheme Monaco, bundle com.monaco.app
  packages/domain/              go.mod, empty package (no product types yet)
  supabase/migrations/          000001 schema_migrations
  scripts/                      docker check, localhost DB guard, apply migrations
```

API listens on `localhost:8080` unless env overrides. Mobile `Config.apiBaseURL` points at the same host. Relayer env var names land in M1 `.env.example`.

## Parallelization

Milestone gate: complete **Wave 3** before M1.

| Wave | Tracks (run in parallel within the wave) | Gate for next wave |
|------|------------------------------------------|--------------------|
| **1** | **Repo** M0-T1 | — |
| **2** | **Backend** M0-T3 · **Mobile** M0-T4 · **Infra** M0-T5, M0-T7 · **Tooling** M0-T2 | Wave 1 |
| **3** | **DB** M0-T6 | M0-T3, M0-T5 |
| **4** | **Tests** M0-T8 · **CI wiring** M0-T9 | M0-T2, M0-T3, M0-T4, M0-T6 |

Sequential gates: M0-T6 waits on API stub + Postgres compose. M0-T9 waits on all build/test targets.

## Tickets

1. M0-T1 Create monorepo directories under `apps/` and `packages/`.
2. M0-T2 Add root `Justfile` with `build`, `test`, and `run` for `backend` and `mobile` only.  
   Depends on: M0-T1
3. M0-T3 Scaffold the Go API module with `GET /health`.  
   Depends on: M0-T1
4. M0-T4 Scaffold the SwiftUI iOS 18 app target in `apps/mobile`.  
   Depends on: M0-T1
5. M0-T5 Add `docker-compose.yml` with a local Postgres service for dev and test.  
   Depends on: M0-T1
6. M0-T6 Add `supabase/migrations/` and a migration runner the API and `just test backend` invoke against local Postgres.  
   Depends on: M0-T3, M0-T5
7. M0-T7 Add `.env.example` with localhost `DATABASE_URL` and Privy placeholders. Gitignore `.env`.  
   Depends on: M0-T1
8. M0-T8 Add a Go unit test for the health handler.  
   Depends on: M0-T3
9. M0-T9 Wire `just test backend` and `just test mobile` to pass on a clean clone.  
   Depends on: M0-T2, M0-T4, M0-T6, M0-T8

## Ticket details

#### M0-T1: Create monorepo directories under apps/ and packages/

**Context**
Greenfield monorepo. M0 lands runnable Go health API, SwiftUI shell, local Docker Postgres, and `just` recipes before product work.

Ticket `M0-T1` is wave **1** in `docs/milestones/m0-scaffold.md` under milestone **M0 — Scaffold**.

**Problem**
Behavior for `Create monorepo directories under apps/ and packages/` is not implemented under `apps/backend/`, `apps/mobile/`, `packages/domain/`, `docs/`, `scripts/`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/`, `apps/mobile/`, `packages/domain/`, `docs/`, `scripts/`. No drive-by refactors.
2. Satisfy milestone gate: `just test backend` and/or `just test mobile` exit 0 with no new failing tests.

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `newTestHealthHandler()`
- `testHTTPRequest(method, path)`
- `integrationDB(t)`

### Scope
- `apps/backend/`
- `apps/mobile/`
- `packages/domain/`
- `docs/`
- `scripts/`

**Out of scope (do not touch)**
- Privy auth
- wallet provision
- domain tables beyond migration plumbing
- Jupiter
- deposits
- group rules
- hosted Supabase for local dev

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/`, `apps/mobile/`, `packages/domain/`, `docs/`, `scripts/`.
- [ ] `just build backend` exits 0 with no new failures.
- [ ] `just build mobile` exits 0 with no new failures.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just build backend`
- `just build mobile`
- `just test backend`
- `just test mobile`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/`, `apps/mobile/`, `packages/domain/`, `docs/`, `scripts/`.
- [ ] `just build backend` exits 0 with no new failures.
- [ ] `just build mobile` exits 0 with no new failures.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- None
- **Wave:** 1
#### M0-T2: Add root Justfile with build, test, and run for backend and mobile only

**Context**
Greenfield monorepo. M0 lands runnable Go health API, SwiftUI shell, local Docker Postgres, and `just` recipes before product work.

Ticket `M0-T2` is wave **4** in `docs/milestones/m0-scaffold.md` under milestone **M0 — Scaffold**.
Blocked until dependencies land: M0-T1.

**Problem**
Locked test names for `M0-T2` are absent or not wired into `just test` recipes; `Justfile` incomplete.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `Justfile`. No drive-by refactors.
2. Update root `Justfile` recipes so locked tests run under `just test backend` and/or `just test mobile`. Exit 0 required.
3. Add `TestJustTestBackend_exitsZeroOnCleanClone` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new API.** Tests and/or `Justfile` wiring only.
**Test fakes (locked names):**
- `newTestHealthHandler()`
- `testHTTPRequest(method, path)`
- `integrationDB(t)`

### Scope
- `Justfile`

**Out of scope (do not touch)**
- Product recipes beyond build/test/run
- just db
- just check

**Acceptance Criteria**
- [ ] Test `TestJustTestBackend_exitsZeroOnCleanClone` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `Justfile`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`
- `just test mobile`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestJustTestBackend_exitsZeroOnCleanClone` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `Justfile`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M0-T1 — **do not start while open**
- **Wave:** 4
#### M0-T3: Scaffold the Go API module with GET /health

**Context**
Greenfield monorepo. M0 lands runnable Go health API, SwiftUI shell, local Docker Postgres, and `just` recipes before product work.

Ticket `M0-T3` is wave **2** in `docs/milestones/m0-scaffold.md` under milestone **M0 — Scaffold**.
Blocked until dependencies land: M0-T1.

**Problem**
Behavior for `Scaffold the Go API module with GET /health` is not implemented under `apps/backend/cmd/api/`, `apps/backend/internal/httpapi/`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/cmd/api/`, `apps/backend/internal/httpapi/`. No drive-by refactors.
2. Add `TestHealthHandler_returns200AndOkBody` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
3. Add `TestHealthHandler_setsContentTypeJson` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `newTestHealthHandler()`
- `testHTTPRequest(method, path)`
- `integrationDB(t)`

### Scope
- `apps/backend/cmd/api/`
- `apps/backend/internal/httpapi/`

**Out of scope (do not touch)**
- Product routes
- Postgres in handler

**Acceptance Criteria**
- [ ] Test `TestHealthHandler_returns200AndOkBody` exists, uses AAA comments, and passes.
- [ ] Test `TestHealthHandler_setsContentTypeJson` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/cmd/api/`, `apps/backend/internal/httpapi/`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestHealthHandler_returns200AndOkBody` exists, uses AAA comments, and passes.
- [ ] Test `TestHealthHandler_setsContentTypeJson` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/cmd/api/`, `apps/backend/internal/httpapi/`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M0-T1 — **do not start while open**
- **Wave:** 2
#### M0-T4: Scaffold the SwiftUI iOS 18 app target in apps/mobile

**Context**
Greenfield monorepo. M0 lands runnable Go health API, SwiftUI shell, local Docker Postgres, and `just` recipes before product work.

Ticket `M0-T4` is wave **2** in `docs/milestones/m0-scaffold.md` under milestone **M0 — Scaffold**.
Blocked until dependencies land: M0-T1.

**Problem**
SwiftUI flow in `apps/mobile/Monaco.xcodeproj`, `apps/mobile/` is missing or still scaffold; product/API wiring for `Scaffold the SwiftUI iOS 18 app target in apps/mobile` not shipped.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/mobile/Monaco.xcodeproj`, `apps/mobile/`. No drive-by refactors.
2. Build SwiftUI screen in **Owns**; call `MonacoAPIClient` only (no xStocks/Jupiter/Pyth/Solana RPC).
3. Compile gate: `just build mobile` and `just test mobile` exit 0.

**API / data contract**
**Mobile consumes existing backend JSON only.** No direct calls to xStocks, Jupiter, Pyth, or Solana RPC from Swift. Use `MonacoAPIClient` methods matching backend routes in milestone doc.
**Test fakes (locked names):**
- `newTestHealthHandler()`
- `testHTTPRequest(method, path)`
- `integrationDB(t)`

### Scope
- `apps/mobile/Monaco.xcodeproj`
- `apps/mobile/`

**Out of scope (do not touch)**
- Privy
- API client beyond shell

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Monaco.xcodeproj`, `apps/mobile/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test mobile`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `apps/mobile/Monaco.xcodeproj`, `apps/mobile/`.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M0-T1 — **do not start while open**
- **Wave:** 2
#### M0-T5: Add docker-compose.yml with a local Postgres service for dev and test

**Context**
Greenfield monorepo. M0 lands runnable Go health API, SwiftUI shell, local Docker Postgres, and `just` recipes before product work.

Ticket `M0-T5` is wave **2** in `docs/milestones/m0-scaffold.md` under milestone **M0 — Scaffold**.
Blocked until dependencies land: M0-T1.

**Problem**
Locked test names for `M0-T5` are absent or not wired into `just test` recipes; `docker-compose.yml` incomplete.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `docker-compose.yml`. No drive-by refactors.
2. Satisfy milestone gate: `just test backend` and/or `just test mobile` exit 0 with no new failing tests.

**API / data contract**
**No new API.** Tests and/or `Justfile` wiring only.
**Test fakes (locked names):**
- `newTestHealthHandler()`
- `testHTTPRequest(method, path)`
- `integrationDB(t)`

### Scope
- `docker-compose.yml`

**Out of scope (do not touch)**
- Hosted Supabase
- Application schema tables

**Acceptance Criteria**
- [ ] Files and symbols named in **Proposal** exist under `docker-compose.yml`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Files and symbols named in **Proposal** exist under `docker-compose.yml`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M0-T1 — **do not start while open**
- **Wave:** 2
#### M0-T6: Add supabase/migrations/ and a migration runner the API and just test backend invoke

**Context**
Greenfield monorepo. M0 lands runnable Go health API, SwiftUI shell, local Docker Postgres, and `just` recipes before product work.

Ticket `M0-T6` is wave **3** in `docs/milestones/m0-scaffold.md` under milestone **M0 — Scaffold**.
Blocked until dependencies land: M0-T3, M0-T5.

**Problem**
SQL tables/columns for `M0-T6` are missing from `supabase/migrations/`; migration runner cannot apply them.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `supabase/migrations/`, `apps/backend/internal/postgres/`, `scripts/`. No drive-by refactors.
2. Add next sequential SQL file under `supabase/migrations/` with tables/columns from `docs/milestones/m0-scaffold.md`.
3. Run migration via existing runner (`apps/backend/internal/postgres/`). Do not hand-apply in prod.
4. Add `TestMigrationRunner_appliesInitialMigrationOnEmptyDb` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
5. Add `TestMigrationRunner_isIdempotentOnSecondRun` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No HTTP.** SQL migration only. Apply via migration runner invoked by API boot and `just test backend`.
**Test fakes (locked names):**
- `newTestHealthHandler()`
- `testHTTPRequest(method, path)`
- `integrationDB(t)`

### Scope
- `supabase/migrations/`
- `apps/backend/internal/postgres/`
- `scripts/`

**Out of scope (do not touch)**
- M1+ domain tables

**Acceptance Criteria**
- [ ] Test `TestMigrationRunner_appliesInitialMigrationOnEmptyDb` exists, uses AAA comments, and passes.
- [ ] Test `TestMigrationRunner_isIdempotentOnSecondRun` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `supabase/migrations/`, `apps/backend/internal/postgres/`, `scripts/`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestMigrationRunner_appliesInitialMigrationOnEmptyDb` exists, uses AAA comments, and passes.
- [ ] Test `TestMigrationRunner_isIdempotentOnSecondRun` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `supabase/migrations/`, `apps/backend/internal/postgres/`, `scripts/`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M0-T3 — **do not start while open**
- M0-T5 — **do not start while open**
- **Wave:** 3
#### M0-T7: Add .env.example with localhost DATABASE_URL and Privy placeholders. Gitignore .env

**Context**
Greenfield monorepo. M0 lands runnable Go health API, SwiftUI shell, local Docker Postgres, and `just` recipes before product work.

Ticket `M0-T7` is wave **2** in `docs/milestones/m0-scaffold.md` under milestone **M0 — Scaffold**.
Blocked until dependencies land: M0-T1.

**Problem**
Behavior for `Add .env.example with localhost DATABASE_URL and Privy placeholders. Gitignore .env` is not implemented under `.env.example`, `.gitignore`, `scripts/`.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `.env.example`, `.gitignore`, `scripts/`. No drive-by refactors.
2. Add `TestDatabaseURLGuard_rejectsHostedSupabaseUrl` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
3. Add `TestDatabaseURLGuard_acceptsLocalhostComposeUrl` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only.
**Test fakes (locked names):**
- `newTestHealthHandler()`
- `testHTTPRequest(method, path)`
- `integrationDB(t)`

### Scope
- `.env.example`
- `.gitignore`
- `scripts/`

**Out of scope (do not touch)**
- Real Privy secrets
- Production env

**Acceptance Criteria**
- [ ] Test `TestDatabaseURLGuard_rejectsHostedSupabaseUrl` exists, uses AAA comments, and passes.
- [ ] Test `TestDatabaseURLGuard_acceptsLocalhostComposeUrl` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `.env.example`, `.gitignore`, `scripts/`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestDatabaseURLGuard_rejectsHostedSupabaseUrl` exists, uses AAA comments, and passes.
- [ ] Test `TestDatabaseURLGuard_acceptsLocalhostComposeUrl` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `.env.example`, `.gitignore`, `scripts/`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M0-T1 — **do not start while open**
- **Wave:** 2
#### M0-T8: Add a Go unit test for the health handler

**Context**
Greenfield monorepo. M0 lands runnable Go health API, SwiftUI shell, local Docker Postgres, and `just` recipes before product work.

Ticket `M0-T8` is wave **4** in `docs/milestones/m0-scaffold.md` under milestone **M0 — Scaffold**.
Blocked until dependencies land: M0-T3.

**Problem**
Locked test names for `M0-T8` are absent or not wired into `just test` recipes; `apps/backend/internal/httpapi/*_test.go` incomplete.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `apps/backend/internal/httpapi/*_test.go`. No drive-by refactors.
2. Add `TestHealthHandler_returns200AndOkBody` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).
3. Add `TestHealthHandler_setsContentTypeJson` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new API.** Tests and/or `Justfile` wiring only.
**Test fakes (locked names):**
- `newTestHealthHandler()`
- `testHTTPRequest(method, path)`
- `integrationDB(t)`

### Scope
- `apps/backend/internal/httpapi/*_test.go`

**Out of scope (do not touch)**
- Integration tests requiring Postgres

**Acceptance Criteria**
- [ ] Test `TestHealthHandler_returns200AndOkBody` exists, uses AAA comments, and passes.
- [ ] Test `TestHealthHandler_setsContentTypeJson` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/httpapi/*_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestHealthHandler_returns200AndOkBody` exists, uses AAA comments, and passes.
- [ ] Test `TestHealthHandler_setsContentTypeJson` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `apps/backend/internal/httpapi/*_test.go`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M0-T3 — **do not start while open**
- **Wave:** 4
#### M0-T9: Wire just test backend and just test mobile to pass on a clean clone

**Context**
Greenfield monorepo. M0 lands runnable Go health API, SwiftUI shell, local Docker Postgres, and `just` recipes before product work.

Ticket `M0-T9` is wave **4** in `docs/milestones/m0-scaffold.md` under milestone **M0 — Scaffold**.
Blocked until dependencies land: M0-T2, M0-T4, M0-T6, M0-T8.

**Problem**
Locked test names for `M0-T9` are absent or not wired into `just test` recipes; `Justfile`, `scripts/` incomplete.

**Proposal**
Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.

1. Edit **only** **Owns** paths: `Justfile`, `scripts/`. No drive-by refactors.
2. Update root `Justfile` recipes so locked tests run under `just test backend` and/or `just test mobile`. Exit 0 required.
3. Add `TestJustTestBackend_exitsZeroOnCleanClone` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`).

**API / data contract**
**No new API.** Tests and/or `Justfile` wiring only.
**Test fakes (locked names):**
- `newTestHealthHandler()`
- `testHTTPRequest(method, path)`
- `integrationDB(t)`

### Scope
- `Justfile`
- `scripts/`

**Out of scope (do not touch)**
- CI deploy
- TestFlight

**Acceptance Criteria**
- [ ] Test `TestJustTestBackend_exitsZeroOnCleanClone` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `Justfile`, `scripts/`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.

**Verification**
**Baseline (run before marking done):**
- `just test backend`
- `just test mobile`

**Done when (zero wiggle room):**
- [ ] Every **Acceptance Criteria** checkbox is satisfied.
- [ ] Test `TestJustTestBackend_exitsZeroOnCleanClone` exists, uses AAA comments, and passes.
- [ ] Files and symbols named in **Proposal** exist under `Justfile`, `scripts/`.
- [ ] `just test backend` exits 0 with no new failures.
- [ ] `just test mobile` exits 0 with no new failures.
- [ ] **Out of scope** paths untouched; no unrelated refactors.
- [ ] No drive-by edits outside **Scope**; **Out of scope** untouched.

**Dependencies**
- M0-T2 — **do not start while open**
- M0-T4 — **do not start while open**
- M0-T6 — **do not start while open**
- M0-T8 — **do not start while open**
- **Wave:** 4

## Automated verification

**Commands.** `just build backend`, `just build mobile`, `just test backend`, `just test mobile`. `just run backend` and root `just run` are smoke checks, not the test suite.

### Test layers

| Layer | Runs under | What it proves |
|-------|------------|----------------|
| **Go unit** | `just test backend` | `GET /health` handler returns 200 and expected JSON without starting Postgres. |
| **Go integration** | `just test backend` | Migration runner applies `supabase/migrations/` to compose Postgres; `schema_migrations` row exists. |
| **Swift compile** | `just test mobile` | iOS 18 target builds and the default test target runs (may be empty until M1). |
| **Env guard** | `just test backend` | Scripts reject hosted Supabase `DATABASE_URL` values before tests start. |

Unit tests use `httptest` and in-memory request/response only. Integration tests start compose, wait on healthcheck, apply migrations, then hit the real local DB. No Privy, Jupiter, or Solana in M0.

### Test map

**M0-T3 / M0-T8 — health handler (unit)**

- `TestHealthHandler_returns200AndOkBody`
- `TestHealthHandler_setsContentTypeJson`

**M0-T6 — migration runner (integration)**

- `TestMigrationRunner_appliesInitialMigrationOnEmptyDb`
- `TestMigrationRunner_isIdempotentOnSecondRun`

**M0-T2 / M0-T9 — just wiring (integration smoke)**

- `TestJustTestBackend_exitsZeroOnCleanClone` (CI or local script smoke; optional if `just test` is the gate)

**M0-T7 — env guard (unit or script test)**

- `TestDatabaseURLGuard_rejectsHostedSupabaseUrl`
- `TestDatabaseURLGuard_acceptsLocalhostComposeUrl`

### Fixtures / fakes

- `newTestHealthHandler()` — minimal handler under test.
- `testHTTPRequest(method, path)` — shared `httptest` helper.
- `integrationDB(t)` — compose Postgres URL from env; `t.Cleanup` truncates or uses a fresh schema per test file.
- No external service fakes in M0.

### Property / invariant tests

None in M0. Scaffold only.

## Manual verification

Human checks only. Do not duplicate automated assertions above.

1. Confirm `docker` is on PATH and the daemon is running. This repo does not install Docker for you.
2. Open `apps/mobile/Monaco.xcodeproj` in Xcode. Confirm the iOS 18 deployment target.
3. Run `just run`. Confirm Postgres is up, the API listens, and the slim sim shows the default SwiftUI shell.
4. Or run one side only: `just run backend` or `just run mobile`. Run `docker compose ps` after backend boot.
5. Confirm `DATABASE_URL` in `.env` points at `localhost` and the compose-mapped port. It must not reference `supabase.co` or any hosted prod project.
6. Run `docker compose exec postgres psql -U monaco -d monaco -c '\conninfo'`. Confirm database `monaco` on container `monaco-postgres`, not a hosted Supabase project.
7. `curl -sf http://localhost:<port>/health` returns OK when the API is running (smoke only; handler behavior is covered by unit tests).



## Out of scope

Privy auth, wallet provision, domain tables beyond migration plumbing, Jupiter, deposits, group rules, hosted Supabase for local dev, CI deploy, TestFlight.

## Open decisions

None.

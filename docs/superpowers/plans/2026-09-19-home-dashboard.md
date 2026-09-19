# Home dashboard implementation plan (#157)

> **For agentic workers:** implement this plan task-by-task against the spec. Do not merge `oh-yea`. Port backend pieces from `origin/feat/157-home-tab` onto the #161 shell.

**Goal:** Home tab becomes a scrollable dashboard (net worth, viewer positions, 1H P&L chart, ranged leaderboard, missed votes) with new home dashboard HTTP routes.

**Architecture:** Keep `GET /v1/home` for Cabals-tab discovery. Add `GET /v1/home/dashboard`, `GET /v1/home/pnl-series`, `GET /v1/home/missed-proposals`. Replay `nav_snapshots` plus a live “now” point for the chart. Mobile reads dashboard from `AppSessionStore` on the Orbix canvas.

**Tech stack:** Go API, Postgres `nav_snapshots` / `proposals` / `proposal_votes`, SwiftUI + Swift Charts, `packages/mobile-core` DTOs.

**Spec:** `docs/superpowers/specs/2026-09-19-home-dashboard-design.md`

## Global Constraints

- User-facing copy: “cabal”, never club/group. API JSON stays `groups` / `groupId`.
- No NAV, wallet, mint, or xStock in UI strings.
- Mobile never calls Jupiter, Pyth, xStocks, or Solana RPC.
- Personal Apple team only: `MONACO_IOS_DEVELOPMENT_TEAM=8VKB4AC8F9`.
- Never commit a UDID. `just test mobile` = host `swift test` in `packages/mobile-core`.
- Work on a git worktree branch `feat/157-home-dashboard` cut from `feat/161-five-tab-shell` (PR #197), not from stale `feat/157-home-tab`.
- Do not dirty the primary checkout if it is `main`.
- Toasts for vote confirmations; no main-UI banners.
- Net worth is cabal equity only. Account balance stays `GET /v1/me/balance`.

---

### Task 1: Dashboard DTOs and decode tests (mobile-core)

**Files:**
- Create: `packages/mobile-core/Sources/MonacoCore/HomeDashboardDTO.swift`
- Create: `packages/mobile-core/Tests/MonacoCoreTests/Fixtures/home_dashboard.json`
- Create: `packages/mobile-core/Tests/MonacoCoreTests/HomeDashboardDTOTests.swift`
- Modify: `packages/mobile-core/Sources/MonacoCore/MonacoAPIClient.swift`
- Modify: `packages/mobile-core/Sources/MonacoCore/MainFlowCopyAudit.swift`
- Modify: `packages/mobile-core/Tests/MonacoCoreTests/MainFlowCopyAuditTests.swift`

**Produces:** `HomeDashboardDTO`, `HomeMyGroupDTO`, `HomePnLSeriesPointDTO`, `HomeMissedProposalDTO`, `HomeLeaderboardRange`, `getHomeDashboard(leaderboardRange:)`, `getHomePnLSeries(range:)`.

- [ ] Add fixture JSON matching the spec keys (`netWorthUsd`, `netWorthDollarPnl`, `netWorthPercentReturn`, `myGroups`, `pnlSeries1H`, `leaderboard`, `missedProposals`).
- [ ] Decode test: 1 my-group row, 2 series points, 1 person, 1 missed proposal; `groupId` maps to Swift `groupID`.
- [ ] Client paths: `/v1/home/dashboard`, `/v1/home/pnl-series`, `/v1/home/missed-proposals`. Query `leaderboardRange` / `range`.
- [ ] Copy audit adds: `Your cabals`, `Join a cabal to see your positions here.`, `P&L history shows up after you fund a cabal.`, `You're caught up.`, `Needs your vote`.
- [ ] Run: `just test mobile`

---

### Task 2: Snapshot queries + missed-proposal SQL

**Files:**
- Modify: `apps/backend/internal/postgres/nav_snapshots.go`
- Modify: `apps/backend/internal/postgres/proposals.go`
- Test: `apps/backend/internal/postgres/nav_snapshots_test.go` (or new `home_dashboard_store_test.go`)
- Test: `apps/backend/internal/postgres/proposals_test.go`

**Produces:** `ListNavSnapshotsForGroupsSince(ctx, groupIDs, since)`, `GetNavSnapshotAtOrBefore(ctx, groupID, at)`, `ListMissedOpenProposalsForUser(ctx, userID, groupIDs, limit)`.

Port from `origin/feat/157-home-tab`. Missed query: `proposals.status = 'open'` AND `expires_at > now()` AND `group_id = ANY($groups)` AND no `proposal_votes` row for `userID`, `ORDER BY created_at DESC LIMIT 20`, join `groups.name`.

- [ ] Tests: snapshot at-or-before returns latest `created_at <= t`; missed list excludes voted and expired.
- [ ] Run: `just test backend` (or the package tests if the full suite is too long — still include these files).

---

### Task 3: HomeService dashboard + HTTP

**Files:**
- Create: `apps/backend/internal/app/home_dashboard.go`
- Create: `apps/backend/internal/app/home_dashboard_test.go`
- Modify: `apps/backend/internal/httpapi/home.go`
- Create: `apps/backend/internal/httpapi/home_dashboard_test.go`
- Modify: `apps/backend/cmd/api/main.go` (`apiRoutes` + `HandleFunc`)
- Modify: `apps/backend/cmd/api/boot_log.go` only if route list is generated elsewhere — keep `apiRoutes` in sync.

**Produces:** `GetHomeDashboard`, `GetHomePnLSeries`, `GetHomeMissedProposals`, `ParseHomeLeaderboardRange`.

- [ ] Port math from `origin/feat/157-home-tab` (`viewerGroupPositions`, `buildViewerPnLSeries` with live now-point, `buildRangedLeaderboard`, `listMissedProposals`).
- [ ] `GET /v1/me` stays exact. Do not register a conflicting `/v1/home` prefix that 404s dashboard (use `GET /v1/home/dashboard` etc. on Go 1.22 mux).
- [ ] Invalid range → 400 `{ "error": "invalid leaderboardRange" }`. Unauth → 401.
- [ ] Handler tests: 401 without bearer; 200 body includes `myGroups` for a joined funded user; missed proposal appears until a vote row exists.
- [ ] Comment on ranged leaderboard: current share units × historical snapshot NAV; not a daily rollup.
- [ ] Run: `just test backend`

---

### Task 4: Home UI on the Orbix shell

**Files:**
- Modify: `apps/mobile/Monaco/Features/Home/HomeView.swift`
- Create: `apps/mobile/Monaco/Features/Home/HomeNetWorthSection.swift`
- Create: `apps/mobile/Monaco/Features/Home/HomePositionsSection.swift`
- Create: `apps/mobile/Monaco/Features/Home/HomePnLChartSection.swift`
- Create: `apps/mobile/Monaco/Features/Home/HomeLeaderboardSection.swift`
- Create: `apps/mobile/Monaco/Features/Home/HomeMissedVotesSection.swift`
- Modify: `apps/mobile/Monaco/Features/Shell/AppSessionStore.swift`
- Modify: `apps/mobile/Monaco/API/MonacoAPIClient.swift` (app target client, match mobile-core)
- Create: `apps/mobile/Monaco/API/DTOs/HomeDashboardDTO.swift` if the app target does not import the mobile-core type (match existing `HomeViewDTO` split).

**Layout (top → bottom):** hero net worth → compact account + Deposit → positions → 1H chart → leaderboard chips → missed votes.

- [ ] Delete Home `Picker` (Cabals / People). Cabals tab still uses `GET /v1/home` groups.
- [ ] `MonacoHeroHeader` for the figure (no nested card around the hero). P&L tint: `MonacoTheme.success` / `.destructive`.
- [ ] Account row stays `PlatformBalanceCard` (or a compact sibling) + Deposit link. Do not add it into net worth.
- [ ] Chart: Swift Charts `AreaMark` + `LineMark`, kiln stroke, height 160, empty state under 2 points.
- [ ] Leaderboard: `MonacoChip` 1H/1D/1W/1M/All. Chip change refetches dashboard with that range only.
- [ ] Positions → `GroupDetailView`. People → `UserProfileGroupsView`. Missed → `ProposalDetailView(auth:proposalId:)`.
- [ ] Ignore `isRequestCancellation` on dashboard loads.
- [ ] A11y: `home-net-worth`, `home-my-group-{id}`, `home-pnl-chart`, `home-leaderboard-row-{id}`, `home-missed-{proposalId}`.
- [ ] Run: `just test mobile` and `just build mobile`

---

### Task 5: Gold-sim QA

**Do not skip.** Gold sim UDID from `scripts/gold-sim-udid.sh`. Backend must be the current `just run backend` (dashboard routes registered).

Manual:

1. Session restore lands on Home tab (not Cabals/People picker).
2. Hero net worth equals sum of Your positions equities.
3. Account balance is a separate number (can be $0.00).
4. Funded cabal with a deposit: chart has a line (2+ points).
5. Leaderboard All → 1W updates ranks (or stays one row if only one person).
6. Open unvoted proposal appears under Needs your vote; after Vote yes, row gone on refresh.
7. Cabals tab still lists all cabals including unjoined.

---

## Done when

- Dashboard routes live on the running API; Home matches the spec order; Cabals tab still discovery.
- `just test backend`, `just test mobile`, `just build mobile` green.
- Gold-sim steps above pass.
- Spec file is not edited during implementation unless the user asks.

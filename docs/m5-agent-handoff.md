# M5 agent handoff — Mobile product UI

**Read this first.** Pick up Milestone 5 after M4 landed on `milestone-4`. Plan of record: [`docs/milestones/m5-mobile.md`](milestones/m5-mobile.md). Prior handoff: [`docs/m4-agent-handoff.md`](m4-agent-handoff.md). Repo root: `/Users/logno/Documents/work/github/monaco`.

---

## 1. Where we left M4

| Item | Detail |
|------|--------|
| **Integration branch** | `milestone-4` @ **`5574fea48345393ff5001d0440e302758f553159`** (`test(backend): assert M4-T36 redeem slice matches domain math`) |
| **Verify HEAD** | `git rev-parse milestone-4` — if newer than above, use actual SHA in prompts |
| **Start M5 from** | `milestone-4`, **not** `main` |
| **M5 integration branch** | `gt create milestone-5` from current `milestone-4` tip |
| **GitHub M4** | Domain APIs shipped; issues **#73–#112** — close with gate evidence when green |
| **Open decisions** | **M4-T39–T41 stay open** — do not hard-code proposer rule, execute-retry, or dissolve UX in M5 |
| **Push policy** | `gt ss` submits milestone-4 PR when user asks; **never force-push** |

**Apps:**

- `apps/backend` — Go API (M4 domain complete; M5 consumes JSON only from mobile)
- `apps/mobile` — Swift/iOS 18+ product UI (this milestone)
- Host Swift unit tests: `packages/mobile-core`
- **Mobile never** calls Jupiter, xStocks, Pyth Hermes, or Solana RPC for product flows

**Gates that must stay green before/during M5 work:**

```bash
dotenvx run -f .env.local -- just test backend
just test mobile          # host swift test packages/mobile-core — no sim
just build mobile         # xcodebuild on gold sim UDID below
curl -s http://127.0.0.1:8080/health
```

**Known flake (M4 gate):** `just test backend` can hang >10m on `TestProperty_confirmedBuyExactlyOneTransactionRowPerSignature` in `apps/backend/internal/app` (Jupiter poll loop in integration test). If stuck, document and proceed; do not block M5 handoff on it alone.

---

## 2. M5 goal

Replace M1/M2 scaffold and debug screens with **demo-ready SwiftUI product**. Social investing copy throughout. README hackathon demo checklist is the ship bar.

**Depends on M4:** seven demo steps need live backend JSON — session, home, groups, deposit, catalog/quotes, proposals/votes, redeem.

**Depends on M1/M2:** Privy sign-in (SMS + email/password for testers), deposit sweep. Hide M1 address debug UI on main flow; explorer links under Settings → Advanced only.

**Out of scope:** Android/web, App Store listing, on-chain governance UI, production Privy webhooks, mobile-side NAV/equity math.

---

## 3. API boundary (hard)

Swift talks **HTTP to `apps/backend` only**, plus **Privy Swift** for auth and member wallets.

| From mobile | Allowed |
|-------------|---------|
| `MonacoAPIClient` → backend routes | Yes |
| Privy Swift (auth, member wallet sign) | Yes |
| `api.xstocks.fi`, Jupiter, Pyth Hermes, Solana RPC | **Never** for product flows |
| xStocks / Jupiter SDKs on iOS | **Never** |

Catalog search, quotes, proposal refusal, Pyth marks for pot, after-hours flags, proposal status — all from **backend JSON**. Explorer links in Settings → Advanced open Safari URLs; not product API calls.

No `packages/domain` import. No share-price or NAV math in Swift. Display formatters turn server decimal strings into UI copy; tests use fixtures, not recomputed equity.

---

## 4. Structure

```text
apps/mobile/
  API/
    MonacoAPIClient.swift     one async method per HTTP route
    DTOs/                     hand-written Codable matching Go JSON
  Features/
    Auth/           launch, session gate
    Home/           group + people boards
    Groups/         create, join, group detail (pot, you, member board)
    Deposit/        add money, poll status
    Proposals/      search, propose, vote, status chips
    Redeem/         slider, payout proof signer
    Settings/       Advanced explorer links only
  Display/          percent and P&L formatters (unit tested)
  Tests/
    Fixtures/       recorded backend JSON
    Display/        M5-T23 formatter tests
    Snapshots/      M5-T24 optional
```

DTO examples (mirror Go field names): `CatalogAssetDTO`, `BuyQuoteDTO` (`routable: Bool`), `PotRowDTO` (`afterHours: Bool?`), `GroupViewDTO`, `MemberSliceDTO`, `RedeemJobDTO` (`status`: debited | selling | paying | settled).

---

## 5. Product flow (nine steps)

1. Launch → Privy login → `POST /v1/auth/session` → session gate.
2. `GET /v1/home` → group board + people board tabs.
3. `POST /v1/groups` or `POST /v1/groups/{id}/join`.
4. `GET /v1/groups/{id}`: pot, you slice, member board, proposals, actions.
5. Deposit: `POST /v1/groups/{id}/deposits` → poll `GET /v1/deposits/{id}` until credited. Sweep status, not member-wallet balance as money.
6. Propose: `GET /v1/groups/{id}/assets?query=` → `POST /v1/groups/{id}/quotes` → disable submit when `routable` false → `POST /v1/groups/{id}/proposals`.
7. Vote: `POST /v1/proposals/{id}/votes`. Status chips: open / passed / failed / expired.
8. Redeem: signed payout proof → `POST /v1/groups/{id}/redeems` → refresh group + home.
9. Settings → Advanced: explorer links and debug addresses only.

**Copy audit:** no wallet, gas, seed phrase, mint, or "NAV" on main flow strings.

**M4 open decisions:** do not invent retry or dissolve UI for M4-T39–T41; show API error states only.

---

## 6. Env / how to run

```bash
dotenvx run -f .env.local -- just test backend
dotenvx run -f .env.local -- just run backend
dotenvx run -f .env.local -- just run           # postgres + backend + mobile
```

**M2+ env:** `DATABASE_URL`, Privy app id/secret/client id, `PRIVY_AUTHORIZATION_*`, `RELAYER_PRIVATE_KEY`, Solana mainnet RPC. Wrap with `dotenvx run -f .env.local --` (Justfile `dotenv-load` reads plain `.env` only).

**iOS:**

| Item | Value |
|------|-------|
| Gold sim UDID | `7B30D45E-62FD-42E2-871A-787B19D38CCF` |
| Bundle ID | `com.monaco.app` |
| `just test mobile` | Host `swift test` in `packages/mobile-core` — **no sim boot** |
| `just build mobile` | `xcodebuild build` on gold UDID only |
| Launch | `./scripts/ios-sim` or `just run mobile` from repo root |

**Never:** `simctl erase`, destination by device name (`iPhone 17`), AppleScript/CGEvent/coordinate tap hacks.

**Privy test account (fixed OTP):**

| Name | Email | OTP |
|------|-------|-----|
| Alfred | test-8081@privy.io | 465354 |

---

## 7. Orchestration workflow (M4 → copy for M5)

Skill: [`.cursor/skills/worktree-orchestrate/SKILL.md`](../.cursor/skills/worktree-orchestrate/SKILL.md).

1. **Parent = orchestrator** on `milestone-5`. **No nested orchestrator subagents.**
2. **Implementers:** `best-of-n-runner`, `model: composer-2.5`, worktrees `feat/m5-t*` from `milestone-5`.
3. **Light review** (`cursor-grok-4.5-high`) → **serial merge** into `milestone-5`. Poll `.git/index.lock`. One merge at a time.
4. Conventional Commits + `Refs #N`. Comment issues with verification evidence.
5. **Shared files** (`Justfile`, `MonacoAPIClient.swift`, root navigation): one writer at a time.
6. **Never force-push.** Push only when user asks.

**M5 waves** (from [`docs/milestones/m5-mobile.md`](milestones/m5-mobile.md)):

| Wave | Parallel tracks | Gate |
|------|-----------------|------|
| **1** | M5-T1 auth shell, M5-T2 session gate (+ DTOs implicit) | M4 APIs live |
| **2** | M5-T3 create group, M5-T4 join, M5-T5 deposit, M5-T21 settings | Wave 1 |
| **3** | M5-T6–T10 proposals, M5-T11–T13 + T20 group detail, M5-T14–T16 home boards | Wave 2 |
| **4** | M5-T17–T19 redeem, M5-T22 copy audit, M5-T23 formatters, M5-T26 demo script | Wave 3 |
| **5** (optional) | M5-T24 snapshots, M5-T25 TestFlight | Wave 4 |

Within a wave: parallel implementers on disjoint `Features/*` paths. Redeem parent M5-T17; subissues M5-T18, M5-T19 first.

**Sim QA:** XcodeBuildMCP or ios-simslim-fast-qa with `--simulator-id 7B30D45E-62FD-42E2-871A-787B19D38CCF`.

---

## 8. Overnight orchestrator prompt

Copy-paste into a **new parent chat**. Parent = orchestrator. Same pattern as M4 overnight.

```
/poteto-mode

Overnight M5. You are the orchestrator, not a nested subagent. Stay parent. Never spawn another orchestrator.

GOAL. Close every GitHub issue in milestone "M5 — Mobile UI" (tickets M5-T1–T26 in docs/milestones/m5-mobile.md). Land all product commits on local integration branch milestone-5. Do not force-push.

Read first, in order:
1. docs/m5-agent-handoff.md
2. docs/milestones/m5-mobile.md
3. .cursor/skills/worktree-orchestrate/SKILL.md
4. AGENTS.md
5. .cursor/skills/ios-simslim-fast-qa/SKILL.md (only if you touch sim)

Repo: /Users/logno/Documents/work/github/monaco

Branch:
- Base: milestone-4 (verify tip; was 5574fea — use actual `git rev-parse milestone-4`)
- Create/use milestone-5 FROM milestone-4, never from main
- gt create milestone-5   (after milestone-4 PR submitted if user asked)

Issues: gh issue list --label milestone-m5 --state open
Ticket bodies: write-ticket format. Implement exactly. Done-when + verification commands. Comment with evidence. Attach screenshots when UI. Conventional Commits + Refs #N.

Orchestrate like M4:
- Parent owns queue. Composer 2.5 implementers (best-of-n-runner / worktrees feat/m5-t*). Light review then SERIAL merge into milestone-5. One merge at a time. Poll index.lock. No nested orchestrator.
- Parallel only inside a wave. Shared files (Justfile, MonacoAPIClient.swift, app navigation shell) = one writer.
- Waves (do not skip gates; copy from docs/milestones/m5-mobile.md — do not invent tickets):
  1. T1 auth shell ‖ T2 session gate
  2. T3 create group ‖ T4 join ‖ T5 deposit ‖ T21 settings
  3. T6–T10 proposals ‖ T11–T13 + T20 group detail ‖ T14–T16 home boards
  4. T17–T19 redeem ‖ T22 copy audit ‖ T23 formatters ‖ T26 demo script
  5. T24 snapshots, T25 TestFlight (optional after Wave 4)
- One agent per overlapping task. Keep furthest-along.

Product:
- Replace scaffold/debug with demo-ready SwiftUI. Social investing copy. README hackathon demo = ship bar.
- Swift HTTP to apps/backend only + Privy Swift. NEVER Jupiter/xStocks/Pyth/Solana RPC from product code.
- Hand-written Codable DTOs matching Go JSON. No packages/domain. No NAV/equity math in Swift.
- Show API errors for M4-T39–T41 open decisions. Do NOT invent proposer/retry/dissolve UX.

Gates (must stay green). Do not change just test/build recipes:
  dotenvx run -f .env.local -- just test backend
  just test mobile          # host swift test packages/mobile-core — no sim
  just build mobile         # gold UDID 7B30D45E-62FD-42E2-871A-787B19D38CCF only
  curl -s http://127.0.0.1:8080/health

QA (if sim):
- Gold slim sim UDID 7B30D45E-62FD-42E2-871A-787B19D38CCF. Never simctl erase. Never destination by name.
- Launch ./scripts/ios-sim or just run mobile. Bundle com.monaco.app. Privy via with-ios-privy-env.
- Privy test Alfred: test-8081@privy.io OTP 465354.
- No AppleScript/CGEvent/coordinate tap hacks. Use XcodeBuildMCP accessibility labels/ids.
- No FAKE* wallet rows. No long arbitrary sleep — poll logs/SQL.
- No brew/npm/pip installs unless I name the package in-chat.

Done when: Wave 4 complete (demo script M5-T26), backend+mobile gates green, all required M5 issues closed with Done-when green. Wave 5 optional.

If blocked: comment the issue with exact error (no secrets). Next unit is a receipt, not more architecture.
```

---

## 9. Test map (mobile)

| Layer | Command | Proves |
|-------|---------|--------|
| Display formatters | `just test mobile` | M5-T23: percent return, P&L, slice %, dust minimum |
| DTO decode | `just test mobile` | Fixtures for GroupView, HomeView, BuyQuote, Proposal, RedeemJob |
| API client | `just test mobile` | Paths, auth headers, backend base URL only |
| Boundary scan | `just test mobile` | No xStocks/Jupiter/Pyth/RPC hosts in Features/ |
| Copy audit | `just test mobile` | M5-T22: no wallet/gas/seed/mint/NAV on main flow |

**Fakes (locked names):** `MockURLProtocol`, fixture JSON under `apps/mobile/Tests/Fixtures/`, `MockPrivySession` for session gate.

**Property tests:** none in Swift — fairness invariants live in M4 `packages/domain`.

---

## 10. M5 start checklist

1. **Read** [`docs/milestones/m5-mobile.md`](milestones/m5-mobile.md) + this file + [`AGENTS.md`](../AGENTS.md).
2. **Confirm gates** on `milestone-5` (§1).
3. **Branch:** `gt create milestone-5` from `milestone-4` tip.
4. **Wave 1:** `feat/m5-t1-auth` ‖ `feat/m5-t2-session-gate`.
5. **Wave 2+:** groups, deposit, settings, then proposals + boards, then redeem + copy audit + demo.
6. **Do not close M4-T39–T41** or bake their answers into M5 UI.

---

## 11. What NOT to do

- Nested orchestrator subagents
- `simctl erase` or destination by device name
- Fake `FAKE*` wallet rows
- Commit `.env.local` or paste private keys / OTP in docs or issues
- Force-push
- Arbitrary long `sleep` — poll logs/UI instead
- AppleScript / CGEvent / coordinate tap hacks for sim QA
- `npm` / `brew` / installs without explicit user permission in current message
- Mobile calls to Jupiter, xStocks, Pyth, or Solana RPC
- Branch M5 from `main` (start from `milestone-4`)
- Implement M4 backend tickets in M5 pass
- Hard-code M4-T39–T41 product decisions

---

## 12. Agent operating rules

- **Parent orchestrator** owns queue; implementers in worktrees only.
- **Subagents:** default `composer-2.5`; light review `cursor-grok-4.5-high`.
- **Ticket format:** write-ticket skill ([`.cursor/skills/write-ticket/SKILL.md`](../../.cursor/skills/write-ticket/SKILL.md)).
- **App dirs:** `apps/backend`, `apps/mobile` (not `apps/api` / `apps/ios`).
- **Commits:** only when user asks (or implementer on feat branch).
- **Privy dashboard:** use attached browser tab during QA.

---

## Quick links

| Resource | Path |
|----------|------|
| M5 plan | [`docs/milestones/m5-mobile.md`](milestones/m5-mobile.md) |
| M4 plan | [`docs/milestones/m4-domain.md`](milestones/m4-domain.md) |
| M4 handoff | [`docs/m4-agent-handoff.md`](m4-agent-handoff.md) |
| Worktree orchestrate | [`.cursor/skills/worktree-orchestrate/SKILL.md`](../.cursor/skills/worktree-orchestrate/SKILL.md) |
| iOS sim QA | [`.cursor/skills/ios-simslim-fast-qa/SKILL.md`](../.cursor/skills/ios-simslim-fast-qa/SKILL.md) |
| Mobile API client | `apps/mobile/API/MonacoAPIClient.swift` |

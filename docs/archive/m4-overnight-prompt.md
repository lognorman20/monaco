# M4 overnight orchestrator prompt

Copy-paste into a **new parent chat**. Parent = orchestrator. Same pattern as M3 overnight.

```
/poteto-mode

Overnight M4. You are the orchestrator, not a nested subagent. Stay parent. Never spawn another orchestrator.

GOAL. Close every GitHub issue in milestone “M4 — Domain” (tickets M4-T1–T41 in docs/milestones/m4-domain.md). Land all product commits on local integration branch milestone-4. Do not push. Do not force-push. Do not use gt.

Read first, in order:
1. docs/m4-agent-handoff.md
2. docs/milestones/m4-domain.md
3. .cursor/skills/worktree-orchestrate/SKILL.md
4. AGENTS.md
5. .cursor/skills/ios-simslim-fast-qa/SKILL.md (only if you touch sim)

Repo: clone root (not a personal home path)

Branch:
- Base: milestone-3 (verify tip; was 0cab6f0 — use actual `git rev-parse milestone-3`)
- Create/use milestone-4 FROM milestone-3, never from main
- git checkout milestone-3 && git checkout -b milestone-4   (if milestone-4 missing)

Issues: gh issue list --milestone "M4 — Domain" --state all
Ticket bodies: write-ticket format. Implement exactly. Done-when + verification commands. Comment with evidence. Attach screenshots when UI. Conventional Commits + Refs #N.

Orchestrate like M3:
- Parent owns queue. Composer 2.5 implementers (best-of-n-runner / worktrees feat/m4-t*). Light review then SERIAL merge into milestone-4. One merge at a time. Poll index.lock. No nested orchestrator.
- Parallel only inside a wave. Shared files (Justfile, migrations, packages/domain, docker-compose) = one writer.
- Waves (do not skip gates; copy from docs/milestones/m4-domain.md — do not invent tickets):
  1. T1 schema ‖ T2 domain types
  2. T3 deposit credit (+T4 tests) ‖ T5 pot NAV (+T6 tests) ‖ T38 Pyth ‖ T7–T10 group rules ‖ T11–T12 catalog/quotes
  3. T13 proposals ‖ T14 vote tally (+T15–T18 subissues) ‖ T23–T29 + T37 leaderboards/NAV snapshots
  4. T19 execute on pass (+T20–T22; DELETE M3 dev stub) ‖ T30 redeem (+T31–T36)
  5. T4, T6, T18, T22, T29, T36 test wiring; T39–T41 open-decision tickets ONLY (no product picks)
- One agent per overlapping task. Keep furthest-along.

Product:
- Replace M3 stubs with real group rules, vote lifecycle, marked pot NAV, redeem, P&L, leaderboards.
- Postgres + Go API source of truth. `packages/domain` for math (Go-only; mobile reads HTTP + hand-written Codable).
- Jupiter runs when proposal passes (M4-T19). Votes not on-chain.
- DELETE POST /v1/dev/groups/{id}/buy when vote pass is only StartBuy gate (grep DELETE_IN_M4).
- Mobile never Jupiter/xStocks/Pyth/Solana RPC. Catalog, quotes, Pyth marks = backend endpoints only.
- Claim units: M2 1:1 USDC when pot USDC-only; after marked xStock, mint shares from pot NAV (README Alex/Blair).
- Redeem: debit share_units first (row lock) → optional Jupiter sell slice → PayUSDC with ownership proof → withdrawals row.
- Open decisions T39–T41: ticket only. Do NOT pick proposer rule, execute-retry, or dissolve logic.

Signing (carry from M3):
- Treasury Privy sign via PRIVY_AUTHORIZATION_* quorum j2ygtljjgxmn5tzao5vjov1t.
- signTransaction omits caip2 (0cab6f0). signAndSend keeps caip2 for sweeps.
- Relayer RELAYER_PRIVATE_KEY base58; pubkey EpeyGQXFY9vhkxPUZbz1wVRhs5vphRQt8SeJN2Gx1DrX.

Gates (must stay green). Do not change just test/build recipes:
  dotenvx run -f .env.local -- just test backend
  just test mobile          # host swift test packages/mobile-core — no sim
just build mobile
  curl -s http://127.0.0.1:8080/health

QA (if sim):
- Gold slim sim `$SIMSLIM_UDID` (per machine). Never simctl erase. Never destination by name.
- Launch ./scripts/ios-sim or just run mobile. Bundle com.monaco.app. Privy via with-ios-privy-env.
- Privy test Alfred: test-8081@privy.io OTP 465354.
- No AppleScript/CGEvent/coordinate tap hacks. Use XcodeBuildMCP accessibility labels/ids.
- Phantom MCP plugin-phantom-connect-phantom-mcp for harness funding only.
- No FAKE* wallet rows. No long arbitrary sleep — poll logs/SQL.
- No brew/npm/pip installs unless I name the package in-chat.

M3 sim receipt (already proven — do not re-boil):
- Group b81faf3f-9687-45b5-b60d-4227222b67ac, treasury 6z3Cij1ghzJerCfxChe1NiSEWHdfkToFSmvRFw4FjmWk
- Tx 5xgL8XVH2Qs5JJZSeMikmMXuCD83bcwBkTz3swmy6retM9uKggY6yLX6sgvUXSgHttad1D7Gm5dyeSX4sDQaFgXd
- transactions 10cce074-c0e0-4164-9ddc-47f95601ad82 buy confirmed
- Screenshots: .qa-screenshots/m3-dev-buy-*.png

Done when: all M4 issues closed with Done-when green, Wave 5 complete, backend+mobile gates green, dev stub deleted (T19), at least one vote-gated buy path tested (unit/integration; live optional). No push.

If blocked: comment the issue with exact error (no secrets). Next unit is a receipt, not more architecture.
```

**Diff vs M3 overnight:** start `milestone-4` from `milestone-3`; handoff `docs/m4-agent-handoff.md`; delete dev buy stub; votes + NAV + redeem + leaderboards; open-decision tickets T39–T41 are record-only.

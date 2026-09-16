# M4 agent handoff — Domain logic

**Read this first.** Pick up Milestone 4 after M3 landed on `milestone-3`. Plan of record: [`docs/milestones/m4-domain.md`](milestones/m4-domain.md). Prior handoff: [`docs/m3-agent-handoff.md`](m3-agent-handoff.md). Repo root: `/Users/logno/Documents/work/github/monaco`.

---

## 1. Where we left M3

| Item | Detail |
|------|--------|
| **Integration branch** | `milestone-3` @ **`0cab6f0217b5e2d6550dc806f23bde8172d78aa7`** (`fix(privy): omit caip2 from signTransaction RPC`) |
| **Verify HEAD** | `git rev-parse milestone-3` — if newer than above, use actual SHA in prompts |
| **Start M4 from** | `milestone-3`, **not** `main` |
| **M4 integration branch** | `git checkout milestone-3 && git checkout -b milestone-4` |
| **GitHub M3** | Issues **#50–#73 CLOSED** — live Jupiter buy verified (see §2) |
| **Push policy** | **Never push** unless user explicitly asks |

**Apps:**

- `apps/backend` — Go API (Jupiter, xStocks, Privy treasury signing, vote-gated execute in M4)
- `apps/mobile` — Swift/iOS (HTTP to Go only + Privy Swift for auth/wallets)
- Host Swift unit tests: `packages/mobile-core`
- **Mobile never** calls Jupiter, xStocks, Pyth Hermes, or Solana RPC for product flows

**Gates that must stay green before M4 work:**

```bash
dotenvx run -f .env.local -- just test backend
just test mobile
just build mobile
curl -s http://127.0.0.1:8080/health
```

---

## 2. Product flow that already works (M3)

End-to-end Jupiter buy path is **live on mainnet** via the temporary dev route:

1. User logs in on iOS (Privy email/SMS OTP).
2. **Me** → **Create group** → treasury verified via `GET /v1/groups/{id}`.
3. **Dev buy AAPLx** (`DevBuyView`) → mobile `POST /v1/dev/groups/{id}/buy` only (no Jupiter from Swift).
4. Backend: xStocks mint resolve → Jupiter quote → Privy **treasury** `signTransaction` → Jupiter `/execute` → poll `status: Success` + `code: 0`.
5. `transactions` row: `action=buy`, `status=confirmed`, unique `tx_signature`.

**Live proof (M3 sim + API QA, 2026-09-16):**

| Field | Value |
|-------|-------|
| Group ID | `b81faf3f-9687-45b5-b60d-4227222b67ac` |
| Group name | `M3 AAPLx Buy QA` |
| Creator | Alfred (`did:privy:cmu2n94nk04bv0cjui6e8xw1z`) |
| Treasury address | `6z3Cij1ghzJerCfxChe1NiSEWHdfkToFSmvRFw4FjmWk` |
| USDC notional | `180000` micros (0.18 USDC; 0.1 failed Jupiter taker min for AAPLx) |
| Tx signature | `5xgL8XVH2Qs5JJZSeMikmMXuCD83bcwBkTz3swmy6retM9uKggY6yLX6sgvUXSgHttad1D7Gm5dyeSX4sDQaFgXd` |
| Explorer | https://solscan.io/tx/5xgL8XVH2Qs5JJZSeMikmMXuCD83bcwBkTz3swmy6retM9uKggY6yLX6sgvUXSgHttad1D7Gm5dyeSX4sDQaFgXd |
| Transaction ID | `10cce074-c0e0-4164-9ddc-47f95601ad82` |
| Input mint | USDC `EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v` |
| Output mint | AAPLx `XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp` |
| Cost basis | price `179820`, amount `54100` (micro-units) |

**Sim UI path (gold UDID `7B30D45E-62FD-42E2-871A-787B19D38CCF`):**

| Step | Screen | Screenshot |
|------|--------|------------|
| Launch with Privy env | Login (API ok) | `.qa-screenshots/m3-dev-buy-01-login-privy-ok.jpg` |
| Alfred email OTP (`test-8081@privy.io` / `465354`) | Me + member wallet | `.qa-screenshots/m3-dev-buy-02-me-after-login.png` |
| Me → Create group → treasury verified | Create group | `.qa-screenshots/39-issue14-create-group-treasury.png`, `40-issue14-treasury-verified.png` |
| Dev buy AAPLx → **Buy AAPLx (dev)** | `DevBuyView` → success row | Live buy above (API path; see note below) |

**Sim notes:**

- Launch **must** use `./scripts/with-ios-privy-env.sh` + `ios-sim` (or `just run mobile`). Plain `xcodebuildmcp build-and-run` without Privy xcconfig shows “Privy not configured”.
- `DevBuyView` hardcodes **0.1 USDC** (`100_000` micros). Live Jupiter taker minimum for AAPLx was **0.18 USDC** at QA time — bump to `180_000` before relying on sim button-only buys (M4 can fix when deleting dev route or gate behind debug flag).
- `POST /v1/dev/groups/{id}/buy` requires `DEV_BUY_ENABLED=true` on the API process.
- Second buy attempt on the known group (2026-09-16 afternoon) returned HTTP 500 after quote — treasury USDC likely below 0.18 after first buy; fund treasury before another live buy.

**M3 domain:** `transactions` table (not `jupiter_orders` / `fills`). M2 `deposits` / `positions` still used for member USDC sweep credit.

**M4 deletes:** `POST /v1/dev/groups/{id}/buy` when vote pass is the only `StartBuy` gate (M4-T19).

---

## 3. Signing model (critical for M4)

M4 Jupiter buys still sign with the **group treasury** Privy wallet.

### One app authorization keypair

| Env var | Purpose |
|---------|---------|
| `PRIVY_AUTHORIZATION_PRIVATE_KEY` | App authorization key (P-256) |
| `PRIVY_AUTHORIZATION_KEY_ID` | Privy dashboard **quorum id** — **not derivable** from private key |

Public quorum id (OK to document): **`j2ygtljjgxmn5tzao5vjov1t`**

### Privy RPC split (M3 fix @ `0cab6f0`)

| Method | `caip2` in body | Use |
|--------|-----------------|-----|
| `signTransaction` | **Omitted** | Jupiter treasury sign (M3 buy/sell) |
| `signAndSend` | **Present** (`solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp`) | M2 member USDC sweeps |

See `apps/backend/internal/privy/http.go` — `signSolanaTransaction` vs `signAndSendSolanaTransaction`.

### Wallet ownership

| Wallet | Owner | Who signs | M4 use |
|--------|-------|-----------|--------|
| **Treasury** | App-owned | Server + auth header | Jupiter buy/sell on passed vote |
| **Member** | User + `additional_signers` at create | User + app quorum | USDC sweep (M2), redeem payout (M4) |

### Relayer (SOL fees)

| Item | Value |
|------|-------|
| Env | `RELAYER_PRIVATE_KEY` — **base58** |
| Pubkey | `EpeyGQXFY9vhkxPUZbz1wVRhs5vphRQt8SeJN2Gx1DrX` |

### Chain constants (mainnet)

| Token | Mint |
|-------|------|
| USDC | `EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v` |
| AAPLx | `XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp` |
| TSLAx | `XsDoVfqeBukxuZHWhdvWHBhgEHjGNst4MLodqsJHzoB` |

xStocks: `GET https://api.xstocks.fi/api/v2/public/assets/{symbol}` → `deployments` where `network == Solana`.

---

## 4. Env / how to run

```bash
dotenvx run -f .env.local -- just test backend
dotenvx run -f .env.local -- just run backend   # add DEV_BUY_ENABLED=true for dev buy QA
dotenvx run -f .env.local -- just run           # postgres + backend + mobile
```

**M2+ env:** `DATABASE_URL`, Privy app id/secret/client id, `PRIVY_AUTHORIZATION_*`, `RELAYER_PRIVATE_KEY`, Solana mainnet RPC.

**iOS:**

| Item | Value |
|------|-------|
| Gold sim UDID | `7B30D45E-62FD-42E2-871A-787B19D38CCF` |
| Bundle ID | `com.monaco.app` |
| Launch | `./scripts/ios-sim` or `just run mobile` from repo root |

**Privy test accounts** (fixed OTP):

| Name | Email | OTP |
|------|-------|-----|
| Alfred | test-8081@privy.io | 465354 |

---

## 5. Orchestration workflow (M3 → copy for M4)

Skill: [`.cursor/skills/worktree-orchestrate/SKILL.md`](../.cursor/skills/worktree-orchestrate/SKILL.md).

1. **Parent = orchestrator** on `milestone-4`. **No nested orchestrator subagents.**
2. **Implementers:** `best-of-n-runner`, `model: composer-2.5`, worktrees `feat/m4-t*` from `milestone-4`.
3. **Light review** → **serial merge**. Poll `.git/index.lock`.
4. Conventional Commits + `Refs #N`. Comment issues with verification evidence.
5. **Shared files** (`Justfile`, migrations, `packages/domain`): one writer at a time. Schema wave first.
6. **Never push** unless user asks.

**M4 waves** (from [`docs/milestones/m4-domain.md`](milestones/m4-domain.md)):

| Wave | Parallel tracks | Gate |
|------|-----------------|------|
| 1 | T1 schema, T2 domain types | M3 |
| 2 | T3 deposit credit, T5 pot NAV, T38 Pyth, T7–T10 group rules, T11–T12 catalog | Wave 1 |
| 3 | T13 proposals, T14 vote tally, T23–T29 + T37 leaderboards | Wave 2 |
| 4 | T19 execute on pass (+ delete dev stub), T30 redeem | T13, T14 |
| 5 | T4, T6, T18, T22, T29, T36 tests; T39–T41 open-decision tickets only | Waves 3–4 |

**Sim QA:** XcodeBuildMCP or ios-simslim-fast-qa. **No** AppleScript, CGEvent, or coordinate hacks.

---

## 6. M3 unblocks that matter for M4

| # | Pain | Fix / carry forward |
|---|------|---------------------|
| 1 | Privy `signTransaction` rejected with `caip2` | Omit `caip2` for `signTransaction`; keep for `signAndSend` (`0cab6f0`) |
| 2 | Jupiter taker min above 0.1 USDC on AAPLx | Use ≥ `180000` micros for live buys |
| 3 | Dev route behind env | `DEV_BUY_ENABLED=true` on API; delete route in M4-T19 |
| 4 | Mobile debug amount | `DevBuyView` still 0.1 USDC — align before sim-only QA |
| 5 | Fake `FAKE*` wallet rows | Break SweepPoller — delete, never invent |

---

## 7. Leftover money

**Phantom refund target:** `68ZedqeBP7tkyLNCdmfrqvtf45WNmKQeHYPi6RjdECqu`

**Treasury `6z3Cij1ghzJerCfxChe1NiSEWHdfkToFSmvRFw4FjmWk` (group `b81faf3f-…`):**

- After M3 buy: ~0.02 USDC + AAPLx dust + small SOL fee buffer.
- No safe treasury→external refund helper in repo — note only; do not invent product refund path.
- Member wallet `8HoCbCqHoXCUUgEJvUx7T9Thm8i74RkL3MTpvCEZFDN` (Alfred): check on-chain before funding new QA.

**Funding QA:** Phantom MCP `plugin-phantom-connect-phantom-mcp` (harness wallet, not Privy product wallets).

---

## 8. Fast verify (copy-paste)

```bash
cd /Users/logno/Documents/work/github/monaco

dotenvx run -f .env.local -- just test backend
just test mobile
just build mobile
curl -s http://127.0.0.1:8080/health
```

**Live dev buy re-check** (API, after `DEV_BUY_ENABLED=true` + treasury USDC):

```bash
# Session: Privy bearer from sim export or test login
curl -s -X POST "http://127.0.0.1:8080/v1/dev/groups/b81faf3f-9687-45b5-b60d-4227222b67ac/buy" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"symbol":"AAPLx","usdc":180000}'
```

**SQL:**

```sql
SELECT id, action, status, tx_signature, amount
FROM transactions
WHERE group_id = 'b81faf3f-9687-45b5-b60d-4227222b67ac'
ORDER BY created_at DESC LIMIT 5;
```

---

## 9. M4 start checklist

1. **Read** [`docs/milestones/m4-domain.md`](milestones/m4-domain.md) + this file + [`AGENTS.md`](../AGENTS.md).
2. **Confirm M3 gates** on `milestone-3` (§8).
3. **Create branch:** `git checkout milestone-3 && git checkout -b milestone-4`.
4. **Wave 1:** `feat/m4-t1-schema` (migrations) ‖ `feat/m4-t2-domain` (`packages/domain` types).
5. **Wave 2+:** group rules, Pyth, pot NAV, proposals/votes, then execute-on-pass (deletes dev stub) and redeem.
6. **Open decisions T39–T41:** file tickets only — do not pick proposer rule, execute-retry, or dissolve logic.

### M4 scope summary

| In scope | Out of scope |
|----------|--------------|
| Vote lifecycle, proposals, tally | M5 product UI polish |
| Marked pot NAV, Pyth client | Mobile Jupiter/xStocks/Pyth/RPC |
| Redeem debit-first + Jupiter sell slice | Re-delete M3 tables |
| Leaderboards (group + people) | Privy production webhooks |
| Delete `POST /v1/dev/groups/{id}/buy` | |

---

## 10. What NOT to do

- Nested orchestrator subagents
- `simctl erase` or destination by device name
- Fake `FAKE*` wallet rows
- Commit `.env.local` or paste private keys / OTP in docs or issues
- Push unless user explicitly asks
- Arbitrary long `sleep` — poll logs/SQL instead
- AppleScript / CGEvent / coordinate tap hacks for sim QA
- `npm` / `brew` / installs without explicit user permission in current message
- Mobile calls to Jupiter, xStocks, Pyth, or Solana RPC
- Branch M4 from `main` (start from `milestone-3`)
- Invent M4 scope beyond [`docs/milestones/m4-domain.md`](milestones/m4-domain.md)

---

## 11. Agent operating rules

- **Parent orchestrator** owns queue; implementers in worktrees only.
- **Subagents:** default `composer-2.5`; light review `cursor-grok-4.5-high`.
- **Ticket format:** write-ticket skill.
- **App dirs:** `apps/backend`, `apps/mobile`.
- **Commits:** only when user asks.
- **Privy dashboard:** use attached browser tab during QA.

---

## Quick links

| Resource | Path |
|----------|------|
| M4 plan | [`docs/milestones/m4-domain.md`](milestones/m4-domain.md) |
| M3 plan | [`docs/milestones/m3-jupiter.md`](milestones/m3-jupiter.md) |
| M3 handoff | [`docs/m3-agent-handoff.md`](m3-agent-handoff.md) |
| M4 overnight prompt | [`docs/m4-overnight-prompt.md`](m4-overnight-prompt.md) |
| Worktree orchestrate | [`.cursor/skills/worktree-orchestrate/SKILL.md`](../.cursor/skills/worktree-orchestrate/SKILL.md) |
| iOS sim QA | [`.cursor/skills/ios-simslim-fast-qa/SKILL.md`](../.cursor/skills/ios-simslim-fast-qa/SKILL.md) |
| Dev buy UI | `apps/mobile/Monaco/Features/Debug/DevBuyView.swift` |
| Dev buy API | `apps/backend/internal/httpapi/dev_buy.go` |

# M3 agent handoff — Jupiter buy/sell

**Read this first.** Pick up Milestone 3 after M2 landed on `milestone-2`. Plan of record: [`docs/milestones/m3-jupiter.md`](milestones/m3-jupiter.md). Prior handoff: [`docs/m2-agent-handoff.md`](m2-agent-handoff.md). Repo root: the clone directory (not a personal home path).

---

## 1. Where we left M2

| Item | Detail |
|------|--------|
| **Integration branch** | `milestone-2` @ **`2d4539e094a8b8754111709b8ba1bf572a379992`** (`test(backend): add AAA comments to property ObserveSweep tests`) |
| **Verify HEAD** | `git rev-parse milestone-2` — if newer than above, use actual SHA in prompts |
| **Start M3 from** | `milestone-2`, **not** `main` |
| **M3 integration branch** | `git checkout milestone-2 && git checkout -b milestone-3` |
| **GitHub M2** | Issues **#32–#49 CLOSED** — live deposit UX verified (see §2) |
| **Push policy** | **Never push** unless user explicitly asks |

**Apps:**

- `apps/backend` — Go API (Jupiter, xStocks, Privy treasury signing live here)
- `apps/mobile` — Swift/iOS (HTTP to Go only + Privy Swift for auth/wallets)
- Host Swift unit tests: `packages/mobile-core`
- **Mobile never** calls Jupiter, xStocks, Pyth Hermes, or Solana RPC for product flows

**Gates that must stay green before M3 work:**

```bash
dotenvx run -f .env.local -- just test backend
just test mobile
just build mobile
```

---

## 2. Product flow that already works (M2)

End-to-end deposit path is **live on mainnet**:

1. User taps **Deposit USDC** (accessibility id `deposit-usdc-link`, above fold on group screen)
2. Copyable **member** Privy wallet address shown
3. User funds USDC (Phantom MCP or external wallet)
4. Backend poller detects USDC on member wallet → server-signed sweep to **app-owned group treasury**
5. `ObserveSweep` credits position: **1:1 `share_units`** with swept USDC
6. Deposit row reaches **confirmed** with `tx_signature`

**Live proof (M2 QA):**

| Field | Value |
|-------|-------|
| Deposit ID | `2a20a9c5-2142-438d-a781-b2518442aeb9` |
| Treasury address | `Axsc7Ct8NRcXS91B3eV682MCpv7bKw7TXqoaJXCh7udo` |
| Note | 0.3 USDC deposited, later swept back to Phantom for cleanup |

Sweep tx examples and evidence: GitHub issue **#35** comments.

**M2 domain tables in use:** `deposits`, `positions`, `withdrawals` (schema only for withdrawals — payout is M4). **Not** `sweep_tx_log` or `share_ledger`.

**M3 adds:** `transactions` table (not in M2 — Wave 1 migration).

---

## 3. Signing model (critical for M3)

M3 Jupiter buys must sign with the **group treasury** Privy wallet, not the member wallet.

### One app authorization keypair

In dotenvx `.env.local` (never commit, never paste private keys in issues/docs):

| Env var | Purpose |
|---------|---------|
| `PRIVY_AUTHORIZATION_PRIVATE_KEY` | App authorization key (P-256) |
| `PRIVY_AUTHORIZATION_KEY_ID` | Privy dashboard **quorum id** — **not derivable** from private key |

Public quorum id (OK to document): **`j2ygtljjgxmn5tzao5vjov1t`**

**Boot fails** if private key is set without `PRIVY_AUTHORIZATION_KEY_ID` (fail-fast added in M2).

Auth header: `privy-authorization-signature` (RFC 8785 canonical JSON + P-256). See `apps/backend/internal/privy/http.go`.

### Wallet ownership model

| Wallet | Owner | Who signs | M3 use |
|--------|-------|-----------|--------|
| **Treasury** | None (app-owned via `EnsureTreasury`) | Server + auth header | **Jupiter buy/sell** — sign on `privy_wallet_id` for treasury |
| **Member** | User + `additional_signers` at **create** | User owner + app quorum | USDC sweep only (M2) |

**Member wallets:** `additional_signers` must be set at **wallet create time**. OTP PATCH to add signers on old wallets returns 403/1010 — **not** the product path. Pre-fix wallets need one-time recreate.

**Treasury:** `EnsureTreasury` creates wallet with **no user owner** → server signs sweeps and (M3) Jupiter txs via auth key.

### Existing signing primitive

```text
apps/backend/internal/privy/http.go  →  signAndSendSolanaTransaction(ctx, walletID, txBase64)
apps/backend/internal/privy/sweep.go →  USDC transfer only (member → treasury)
```

M2 sweep = USDC SPL transfer. **M3 Jupiter tx builder is new.**

### Jupiter v2 execute flow

Jupiter Swap API v2 often wants:

1. **Sign** transaction (Privy treasury)
2. **POST** `/execute` with signed payload
3. **Poll** until `status: Success` **and** `code: 0`

May need a **sign-only** Privy wrapper, not only `signAndSendSolanaTransaction` (which broadcasts in one step). Design for sign → Jupiter execute → poll.

### Relayer (SOL fees)

| Item | Value |
|------|-------|
| Env | `RELAYER_PRIVATE_KEY` — **base58** (not JSON int array) |
| Pubkey | `EpeyGQXFY9vhkxPUZbz1wVRhs5vphRQt8SeJN2Gx1DrX` |
| Role | Fee payer on sweeps; may also be needed on Jupiter txs |

Ensure relayer has lamports before mainnet execute.

### Chain constants (mainnet)

| Token | Mint |
|-------|------|
| USDC | `EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v` |
| AAPLx (example) | `XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp` |
| TSLAx (example) | `XsDoVfqeBukxuZHWhdvWHBhgEHjGNst4MLodqsJHzoB` |

Resolve symbols via xStocks API: `GET https://api.xstocks.fi/api/v2/public/assets/{symbol}` → `deployments` where `network == Solana`.

---

## 4. Env / how to run

### dotenvx vs Justfile

Justfile `dotenv-load` reads plain `.env` only — **not** dotenvx ciphertext.

**Always wrap API/dev commands:**

```bash
dotenvx run -f .env.local -- just test backend
dotenvx run -f .env.local -- just run backend
dotenvx run -f .env.local -- just run          # postgres + backend + mobile
```

`just test mobile` and `just build mobile` do not need dotenvx for backend secrets, but build mobile runs ensure-ios-privy-config internally.

### M2+ required env (names only)

- `DATABASE_URL` — Docker Compose Postgres locally
- `PRIVY_APP_ID`, `PRIVY_APP_SECRET`, `PRIVY_APP_CLIENT_ID` (or legacy client id name)
- `PRIVY_AUTHORIZATION_PRIVATE_KEY` + `PRIVY_AUTHORIZATION_KEY_ID`
- `RELAYER_PRIVATE_KEY` (base58)
- Solana mainnet RPC URL (as configured in backend)

**Local DB:** Docker Compose Postgres only. **Not** hosted/production Supabase.

### Just recipes (do not change without user ask)

| Command | What it does |
|---------|--------------|
| `just test backend` | Docker Postgres + migrations + `go test -p 1 ./...` |
| `just test mobile` | Host `swift test` in `packages/mobile-core` (no sim) |
| `just build mobile` | `xcodebuild` on `$SIMSLIM_UDID` only |
| `just run mobile` | `./scripts/ios-sim` with Privy env |

**Never** run uncached `go test ./...` without `-p 1` while `monaco-api` is running — can deadlock. Use `just test backend`.

### iOS / Privy

| Item | Value |
|------|-------|
| Gold sim UDID | `$SIMSLIM_UDID` (per machine; never commit) |
| Bundle ID | `com.monaco.app` — must be on Privy iOS client |
| Missing bundle | OTP `sendCode` → 403 `invalid_native_app_id` |
| Launch | `./scripts/ios-sim` or `just run mobile` from **repo root** |
| Privy app id (public) | `cmu26uw5s00mp0cl81v6dud1n` |

**Never** `simctl erase`. **Never** destination by device name (`iPhone 17`). Always `$SIMSLIM_UDID`.

See README **SimSlim** section and [`.cursor/skills/ios-simslim-fast-qa/SKILL.md`](../.cursor/skills/ios-simslim-fast-qa/SKILL.md).

### Health check

```bash
curl -s http://127.0.0.1:8080/health
```

API must be up with full M2+ env (KEY_ID required when auth private key set).

### Mobile path note

Tickets may say `apps/mobile/Features/*` — actual tree is `apps/mobile/Monaco/Features/*`.

---

## 5. Orchestration workflow that worked (M2 → copy for M3)

Skill: [`.cursor/skills/worktree-orchestrate/SKILL.md`](../.cursor/skills/worktree-orchestrate/SKILL.md).

**Pattern:**

1. **Parent = orchestrator** on integration branch (`milestone-3`). **No nested orchestrator subagents.**
2. **Implementers:** `best-of-n-runner`, `model: composer-2.5`, worktrees `feat/m3-t*` from `milestone-3`.
3. **Light review** → **serial merge** (one at a time). Poll for `.git/index.lock`.
4. Conventional Commits + `Refs #N`. Comment issues with verification evidence.
5. **One agent per overlapping task** — consolidate to furthest-along if duplicated.
6. **Shared files** (`Justfile`, migrations, `docker-compose.yml`): one writer at a time. Schema wave first.
7. **Never push** unless user asks.

**M3 waves** (from plan — parallel within wave, serial across waves):

| Wave | Parallel tracks | Gate |
|------|-----------------|------|
| 1 | T1 transactions schema, T2 xStocks client, T14 logging | M2 complete |
| 2 | T3/T4 Jupiter quote, T16 xStocks tests | Wave 1 |
| 3 | T5 buy execute (+ T6–T8), T9 sell (+ T10) | T3 |
| 4 | T11 dev route, T12 delete marker, T13 read APIs, T15 mobile debug | T5 |
| 5 | T17–T22 tests + Justfile wiring | Waves 3–4 |

**Sim QA:** XcodeBuildMCP or ios-simslim-fast-qa skill. **No** AppleScript, CGEvent, or coordinate tap hacks.

**Fake DB wallets:** Rows with `FAKE*` addresses **break SweepPoller** (Privy 404). Delete them. Do not invent wallet rows.

---

## 6. M2 unblocks that actually mattered (don't re-learn)

| # | Pain | Fix |
|---|------|-----|
| 1 | Relayer key as JSON int array | Use **base58** for `RELAYER_PRIVATE_KEY` |
| 2 | Privy auth rejected | Wire `privy-authorization-signature` (RFC 8785 + P-256) |
| 3 | Member sweep 401 | `additional_signers` at member **create**; `KEY_ID` is dashboard quorum, not derivable |
| 4 | Poller stuck / double-credit risk | Persist `tx_signature` after broadcast; `ObserveSweep` even if member USDC already gone |
| 5 | Silent misconfig | Fail-fast boot when auth private key set without `PRIVY_AUTHORIZATION_KEY_ID` |
| 6 | Deposit UX buried | Deposit USDC **above fold** + copyable member address (`deposit-usdc-link`) |
| 7 | Old wallet missing `additional_signer` | REST PATCH with identity token **blocked** (dashboard identity tokens off → 403/1010). **One-time ops:** owner logs in on iOS → Privy SDK `wallet.addSigner(SignerInput(signerId: j2ygtljjgxmn5tzao5vjov1t))` on **every** `embeddedSolanaWallets` entry (not `.first`) → server `SubmitSweep` or `./scripts/sweep-wallets.sh`. **Product path:** `additional_signers` at wallet **create** only |

---

## 7. Leftover money (recovered)

User wanted leftover USDC returned to Phantom when M3 work is done.

**Phantom destination (refund target):** `68ZedqeBP7tkyLNCdmfrqvtf45WNmKQeHYPi6RjdECqu` (~6.73 USDC after all recoveries)

### What did **not** work

- Server REST `PATCH /v1/wallets/{id}` to add `additional_signers` — blocked without dashboard identity tokens (403 / 1010). **Not** the product path.

### What **did** work (one-time ops on pre-fix wallets)

1. Owner logs in on iOS sim (gold `$SIMSLIM_UDID`).
2. DEBUG `ensureServerSweepSigner` in `PrivyAuthService` — on login, `wallet.addSigner(SignerInput(signerId: j2ygtljjgxmn5tzao5vjov1t))` on **every** `embeddedSolanaWallets` entry (sim users can have multiple; `.first` hit wrong wallet).
3. Server sweep: `./scripts/sweep-wallets.sh --destination <addr> --dry-run` then live without `--dry-run`. `--all` uses Privy wallet list. See [`docs/ops-sweep-wallets.md`](ops-sweep-wallets.md).

**Ops only:** DEBUG `ensureServerSweepSigner` and `cmd/sweep-member-to-address` are uncommitted migration helpers — **not** M3 product code. New member wallets still get `additional_signers` at **create** (`EnsureMemberWallet`).

### Recovery results (all swept to Phantom)

| Address | Amount | Sweep tx |
|---------|--------|----------|
| `92EERtxMFN3xKJ3RXas5zjGsSfQ7AkQRVxC3tPvaVQ12` | 2 USDC | `5PFARmDavKXd7sfAmfjdgkVNPrnMtrNtF7GuMshpx1FFRc2YzrM7ThbVqJi36DPgbS5mtyfEHt1agNuremo3SX7e` |
| `ALMhUvVykjgyBgMaKYnmUHheLrAcLMac7J7vFko6Nmmn` | 2 USDC | `Tmnt8mSYDhMLxS9pi6TLHJPrxLajiA5A357J8LDrdEPo3MsrZRDsx7uznBRbNN6kpxWxeLtEQ2RC2GpPDJe2LnP` |
| `9AqPznPUoYsVZMG4q7ounVvTnSAUjZtS67zxu16RuaUZ` | 2 USDC | `EDnPzqRPU7FVN35ndYmXH7vgEa6Qft1doDX8naos9ViiAzorcTCekGK2VF42uyTPePga9jTLD7vUf1GbL92dsaU` |

All three wallets USDC balance **0** on-chain. No stuck member USDC remains from M2 QA.

**Funding new member wallets / balance checks:** Phantom MCP namespace `plugin-phantom-connect-phantom-mcp`. Separate harness wallet from Privy product wallets. `PHANTOM_APP_ID` belongs in Cursor MCP config, **not** Monaco `.env`.

---

## 8. Fast verify (copy-paste)

```bash
cd <clone-root>

dotenvx run -f .env.local -- just test backend
just test mobile
just build mobile
curl -s http://127.0.0.1:8080/health
```

**Sim smoke (M2 deposit UI still works):**

- UI test: `MonacoUITests/testM2DepositLinkReachable()` — finds `deposit-usdc-link`
- Skill: [`.cursor/skills/ios-simslim-fast-qa/SKILL.md`](../.cursor/skills/ios-simslim-fast-qa/SKILL.md)

**Live deposit re-check:**

1. Tap Deposit USDC → copy member address
2. Send tiny USDC (Phantom MCP)
3. Wait for poller (watch API logs, not arbitrary sleep)
4. Confirm treasury USDC balance + deposit `confirmed` in DB/API

**M3 backend test fakes** (locked names in plan): `fakeJupiterClient`, `fakeXStocksResolver`, `fakePrivyTreasurySigner`, `integrationApp(t)`.

---

## 9. M3 start checklist

1. **Read** [`docs/milestones/m3-jupiter.md`](milestones/m3-jupiter.md) + this file + [`AGENTS.md`](../AGENTS.md).
2. **Confirm M2 gates** on `milestone-2` (§8 commands).
3. **Create branch:** `git checkout milestone-2 && git checkout -b milestone-3`.
4. **Simplify first:** Prove **one** treasury Jupiter buy on mainnet with existing swept USDC before boiling the ocean. Use **tiny** USDC notional.
5. **Wave 1 dispatch** (parallel, disjoint ownership):
   - `feat/m3-t1-schema` → `transactions` migration (#T1)
   - `feat/m3-t2-xstocks` → `apps/backend/internal/xstocks/` (#T2)
   - `feat/m3-t14-logging` → structured Jupiter logs (#T14)
6. **Wave 2+:** Jupiter quote → treasury sign via Privy → execute → poll → persist `transactions`.
7. **Dev route:** `POST /v1/dev/groups/{id}/buy` — **delete in M4** when vote gate exists.
8. **Mobile:** thin debug control in `apps/mobile/Monaco/Features/Debug/` — calls dev buy endpoint only. **No Jupiter SDK on iOS.**

### M3 scope summary

| In scope | Out of scope |
|----------|--------------|
| Jupiter quote + execute + poll | Vote lifecycle, proposal UI |
| xStocks mint resolver | Member voting |
| `transactions` persistence | Redeem payout to external address |
| Dev buy stub route | Leaderboards, P&L boards |
| Backend sell path (xStock → USDC) | Pyth marks, chart history |
| Swift debug button → dev API | Jupiter WebSocket, Privy webhooks |

### Key implementation paths (from plan)

```text
apps/backend/internal/jupiter/     QuoteBuy, ExecuteBuy, SellToUSDC, poll
apps/backend/internal/xstocks/     mint resolver
apps/backend/internal/app/         swap.go, start_buy.go, DevExecuteBuy
apps/backend/internal/postgres/    transactions.go
apps/backend/internal/httpapi/     dev_buy.go, transactions.go (read APIs)
apps/mobile/Monaco/Features/Debug/  thin dev buy control
supabase/migrations/               transactions table
```

**Reuse:** `signAndSendSolanaTransaction` where one-shot broadcast fits; add sign-only if Jupiter v2 requires separate execute POST.

---

## 10. What NOT to do

- Nested orchestrator subagents
- `simctl erase` or fresh sim for QA
- Xcode destination by device name
- Fake `FAKE*` wallet rows in DB
- Commit `.env.local` or paste private keys / OTP codes in docs or issues
- Push to remote unless user explicitly asks
- Arbitrary long `sleep` / wait-for-task loops
- AppleScript / CGEvent / coordinate tap hacks for sim QA
- `npm` / `brew` / package installs without explicit user permission in current message
- Mobile calls to Jupiter, xStocks, Pyth, or Solana RPC
- OTP PATCH as product path for adding signers to old wallets
- Branch M3 from `main` (start from `milestone-2`)
- Change `just test` / `just build` recipes without user ask

---

## 11. Agent operating rules

- **Parent orchestrator** owns queue; implementers in worktrees only.
- **Subagents:** default `composer-2.5` for implement/fix; light review `cursor-grok-4.5-high`.
- **Ticket format:** write-ticket skill — Context / Problem / Proposal / Done when.
- **App dirs:** `apps/backend`, `apps/mobile` — not `apps/api` / `apps/ios`.
- **Commits:** only when user asks.
- **Privy dashboard:** use attached browser tab for client settings during QA; don't ask user to click dashboard for you.

---

## Quick links

| Resource | Path |
|----------|------|
| M3 plan | [`docs/milestones/m3-jupiter.md`](milestones/m3-jupiter.md) |
| M2 plan | [`docs/milestones/m2-deposits.md`](milestones/m2-deposits.md) |
| M2 handoff | [`docs/m2-agent-handoff.md`](m2-agent-handoff.md) |
| Product brief | [`docs/product.md`](product.md) |
| Clone / run | [`README.md`](../README.md) |
| Agent prefs | [`AGENTS.md`](../AGENTS.md) |
| Worktree orchestrate | [`.cursor/skills/worktree-orchestrate/SKILL.md`](../.cursor/skills/worktree-orchestrate/SKILL.md) |
| iOS sim QA | [`.cursor/skills/ios-simslim-fast-qa/SKILL.md`](../.cursor/skills/ios-simslim-fast-qa/SKILL.md) |
| Privy signing | `apps/backend/internal/privy/http.go`, `sweep.go` |
| M2 deposit UI test | `apps/mobile/MonacoUITests/MonacoUITests.swift` (`testM2DepositLinkReachable`) |

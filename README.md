# Monaco, group treasuries for tokenized stocks

**Monaco** is an iOS app where friends form a group, pool USDC, and buy tokenized US stocks on Solana. Built for [Stocklana](https://hackathons.solana.com/hackathons/stocklana). Submit by **18 Sep 2026, 4:00pm ET**.

This document is the canonical product and architecture brief. It is not legal advice.

Prize target is the general Stocklana pool. Judges ask whether this could be a real app people use. The build does **not** use Meteora DBC or Clawpump. Execution is Jupiter Swap API v2 on **Solana mainnet** with small real USDC.

## Getting started with development

**Prereqs:** Docker, Go, Xcode/Swift, [dotenvx CLI](https://dotenvx.com/docs/install), and gold `ios-sim` / `ios-build` on PATH (`~/.local/bin`).

**One-time env setup**

1. Copy `.env.example` → `.env.local`.
2. Set values: `dotenvx set KEY value -f .env.local` (encrypts by default; `--plain` for non-secrets).
3. Encrypt if needed: `dotenvx encrypt -f .env.local`.

Secrets, private keys, and pre-commit hooks: see **[Local env](#local-env)** below. Do not wrap `just` with `dotenvx run` manually — recipes that need secrets re-exec under `scripts/with-dotenv-local.sh`.

**Commands**

| Command | What it does |
| --- | --- |
| `just run` | Full stack: Postgres + API + iOS app (dotenvx re-exec) |
| `just run backend` | API only (dotenvx) |
| `just run mobile` | iOS on gold sim — Privy xcconfig + `SIMCTL_CHILD_*` via `./scripts/ios-sim` |
| `just stop` | Stop API + iOS app (kill port 8080, `simctl terminate` on gold sim) |
| `just stop backend` | Stop API only |
| `just stop mobile` | Terminate Monaco on gold sim; stop `xcodebuild` if running |
| `just reset` | Stop all + wipe local Postgres volume + re-apply migrations (dotenvx) |
| `just reset backend` | Stop API + remove `bin/monaco-api` |
| `just reset mobile` | Stop app + `xcodebuild clean` on gold sim |
| `just reset db` | Wipe local Docker Postgres volume + migrations (localhost only, dotenvx) |
| `just killports` | Kill listeners on API port (default 8080; not Postgres 54322) |
| `just test backend` | Go tests + local DB smoke (dotenvx) |
| `just test mobile` | Host `swift test` in `packages/mobile-core` — fast, no secrets |
| `just build backend` | `go build` only — no dotenvx |
| `just build mobile` | Privy xcconfig, then `xcodebuild` on gold sim |
| `./scripts/ios-sim` | Monaco run with Privy env (prefer over bare `ios-sim`) |
| `./scripts/ios-build` | Monaco compile with Privy xcconfig |

Gold sim UDID: `7B30D45E-62FD-42E2-871A-787B19D38CCF`. iOS sim details: **[Running the stack](#running-the-stack)**.

## Goals

- **Social investing, not crypto.** Copy is "invite friends", "add money", "buy Apple". No wallets, gas, seed phrases, or "mint" in user-facing copy.
- **Leaderboard and P&L first.** Two boards, both percent return. Inside a group: who in this pot is winning. Across the app: which groups and which people are winning. Buys and cash-out serve those screens.
- **Real on-chain execution.** Tokenized stocks land in the group treasury via Jupiter. A fiat-only mock does not meet the bar.
- **Fair equity.** Members hold share units (claim tickets on the pot), not dollar IOUs. A redeem pays that member's slice of what the pot is worth now, in USDC, not a refund of what they put in.
- **Cash out is a primary flow.** Partial redeem to USDC at a payout address the user proved they own. Full exit is the same path with the amount at max.
- **Shippable Friday scope.** Native SwiftUI, Go API, Supabase Postgres, Privy auth and wallets, Jupiter swaps. No custom Solana program.

## How it works

1. Sign in with SMS or email and password via Privy.
2. Create a group or join one of many. One user belongs to many groups. App home ranks groups and people across the whole app.
3. Deposit USDC into the member wallet. The backend sweeps it into the group treasury and credits share units at the current share price.
4. Propose a buy from the xStocks catalog. The group's voter set must pass it under the creator's threshold and expiry. Then the backend swaps treasury USDC for the token on Jupiter.
5. Live on the group screen: pot composition, your slice, dollar P&L, percent return, and the in-group member leaderboard.
6. Redeem some or all share units whenever you want. The backend sells that slice to USDC and pays a verified payout address.

## Groups and invites

Each group has one shared portfolio and one Privy Solana server wallet as the treasury.

At create, the **group creator** sets:

- **Join policy.** Anyone may join, or a join password is required.
- **Voter set.** Either a named subset of members (minimum size 1, which may be only the creator) or every member.
- **Vote threshold.** Unanimous among the voter set, or majority among the voter set.
- **Vote expiry.** A duration the creator chooses. If the proposal does not pass before expiry, it dies and no swap runs.

## Votes and buys

On-chain governance is out of scope. Votes live in Postgres. The Go API is the source of truth.

1. A buy proposal names an xStock from the **full public catalog** (search, not a fixed two-ticker list).
2. If Jupiter cannot quote a route, the UI refuses the proposal. Do not offer names the Meta-Aggregator cannot fill.
3. Members in the voter set vote yes or no before expiry.
4. On pass, the backend builds a Jupiter v2 order (`inputMint` = USDC, `outputMint` = xStock mint), signs with the treasury via Privy, and `POST`s `/execute`. Confirm `status: Success`, `code: 0`.
5. The token lands in the **group treasury**.

Constants:

- USDC mint: `EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v`
- xStock mints: `GET https://api.xstocks.fi/api/v2/public/assets/{symbol}` then `deployments` where `network == Solana` then `address`. Examples: `AAPLx` is `XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp`. `TSLAx` is `XsDoVfqeBukxuZHWhdvWHBhgEHjGNst4MLodqsJHzoB`.

The xStocks public API is mint metadata only. It is not an execution rail. Poll `/execute` for confirmation. Do not use a Jupiter WebSocket. Do not use Privy production webhooks (Enterprise-only).

## Architecture

No custom on-chain vault. Privy server wallets hold assets. Supabase Postgres holds member share units, votes, NAV snapshots, and P&L inputs (net USDC in). A Go API talks to Privy and Jupiter.

Solana transaction fees are paid by an **app relayer**. Treasuries may hold no SOL. Users never see gas.

The backend can sign the treasury. That custodial fact is accepted for the hackathon. Demo the buy. Do not spend UX on a trust explainer.

```
SwiftUI (iOS 17+)
  → Privy Swift (auth, member wallets)
  → Go API (groups, invites, votes, share ledger, sweeps, swaps, P&L)
  → Supabase Postgres DB
  → Privy Solana server wallet per group (treasury)
  → App fee payer (SOL fees)
  → Jupiter Swap API v2 (USDC → xStocks)
```

### Wallets


| Wallet              | Owner         | Role                                                                                            |
| ------------------- | ------------- | ----------------------------------------------------------------------------------------------- |
| Member wallet       | One per user  | Deposit inbox. Unique attribution for who funded. Backend-signable via Privy.                   |
| Group treasury      | One per group | Holds USDC and tokenized stocks. All group trades execute from here.                            |
| Relayer / Fee Payer | App           | Pays SOL fees so the treasury and the member wallets need not be SOL-funded for the happy path. |


Users never manage keys or approve individual Solana transactions in the happy path. The backend signs sweeps, swaps, and payouts.

### Deposit and sweep

1. The user funds **their** Privy wallet with USDC (onramp or external transfer).
2. The backend **sweeps** USDC from the member wallet into the group treasury (server-signed, no second approval sheet).
3. On **confirmed sweep into treasury**, credit share units at the current share price. Idempotent on transaction signature.
4. Do **not** credit shares when USDC only arrives in the member wallet. Sweep promptly.

See **NAV and share units** for the formula.

## NAV and share units

NAV means **net asset value**. It is the dollar value of the whole group pot right now.

Two numbers, keep them distinct:

- **Pot NAV.** USDC sitting in the treasury, plus every tokenized stock marked at its current price. Example: $40 USDC + 0.1 AAPLx worth $60 = $100 pot.
- **NAV per share** (share price). `pot NAV / total shares`. This is what one share unit is worth. On an empty group there are no shares yet, so the first deposit uses a share price of **$1**.

A **share unit** is a claim ticket, not a dollar IOU. The ledger stores how many tickets each member holds, not "Alex is owed $100." Your dollars in the app are:

`your equity = (your shares / total shares) × pot NAV`

When someone deposits, they buy tickets at today's share price:

`shares credited = USDC swept in / NAV per share`

When someone redeems, they return tickets and take that fraction of the pot in USDC. The pot is marked first, then (if needed) that slice of stock is sold to USDC.

**Why not track dollars deposited.** Alex puts in $100 and the group buys Apple. Apple goes up 10%. The pot is $110. If Blair then "deposits $110" as a dollar balance, she would own half of a pot that already includes Alex's gain, or Alex would eat her later losses. Share units fix that. Blair's $110 buys shares at $1.10, so she gets the same number of tickets Alex has, and she does not steal the bounce.

Worked numbers (ignore Jupiter slippage for the story):

1. Empty group. Share price $1.
2. Alex deposits $100. He gets 100 shares. Pot $100. Total shares 100. Share price $1.
3. The group buys AAPLx with the $100. Pot still about $100, now in stock.
4. AAPLx rises 10%. Pot $110. Alex still has 100 shares. His equity is $110. Share price is $1.10.
5. Blair deposits $110. She gets `110 / 1.10 = 100` shares. Pot $220. Total shares 200. Each still owns half.
6. Blair redeems 50 shares. That is `50 / 200` of the pot = $55 USDC. She keeps 50 shares. Alex still has 100.

Marks: Jupiter fill price is cost basis. Ongoing P&L may use Pyth equity feeds. If the token still trades on-chain after the cash equity market closes, show an after-hours label.

**UI copy.** Do not say "NAV" to users. Say the pot value, their slice, and gain or loss in dollars.

## Portfolio, P&L, and leaderboard

P&L is the product. It exists at two scopes. Same math, different rows.

Rank by **percent return**, never by dollars. A small pot can beat a whale. Dollar P&L sits beside the name.

`percent return = equity / net USDC in − 1`

Skip a row when net USDC in is 0 (no divide by zero, no fake 0% clubs).

**Inside a group** (group screen). Trade and cash-out are actions here.

- **Pot.** Holdings list: USDC plus each xStock with units, mark, and dollar value. Cost basis per position from the Jupiter fill. After-hours label when Pyth equity is frozen.
- **You.** Slice in dollars and as a percent of this pot. Dollar P&L and percent return versus **net USDC in this group** (sweeps credited here minus USDC paid out on redeems here).
- **Member board.** Every member with a share balance greater than zero in this group. Ranked by that in-group percent. A full exit from this group drops them off this board only.

**Across groups** (app home, first screen after sign-in). Two lists, both live off the same ledger.

- **Group board.** One row per group with net USDC in greater than 0. Equity is that group's pot NAV. Net USDC in is all member sweeps into that treasury minus all redeems out of it. This is how clubs compete with each other. A join password still hides entry, not the score. The row shows the group name, percent, and dollar P&L of the pot. Tap through to join or open.
- **People board.** One row per user with net USDC in greater than 0 across **all** groups they belong to. Equity is the sum of their slices. Net USDC in is the sum of their per-group net USDC in. Alex in three clubs is one row, not three. Tap through to their profile list of groups.

**Why two boards.** Friends care who is winning this pot. The app-wide loop is which clubs are hot and who is good across clubs. The people board only works if one user can sit in many groups.

Postgres stores NAV snapshots on deposit, fill, and redeem so charts and both boards are replayable. Do not recompute history only from live wallets.

Settings → Advanced may expose explorer links. The main flow never needs them.

## Withdraw

Partial redeem is a first-class action on the group screen, same weight as buy. Payout is **USDC only**. Never send tokenized stock in kind.

The user picks how many dollars (or how many shares) to take, from a dust minimum up to their full equity. Full exit is the same flow with the slider at max.

1. **Debit share units** first (row-locked in Postgres).
2. Compute the member's slice of the pot (`shares redeemed / total shares × pot NAV`). If the treasury holds stock, **sell that slice to USDC** on Jupiter first.
3. Send USDC only to a **payout address the user proved they own** (signed message). The proof is required on every redeem, including partials. Reject attacker-supplied pubkeys.

They receive USDC equal to their redeemed fraction of the pot at that moment, not a refund of dollars they put in. That group's member board, the global group board, and the global people board all recompute from the new net-USDC-in figure.

## Stack


| Layer            | Choice                                                                                                       |
| ---------------- | ------------------------------------------------------------------------------------------------------------ |
| Mobile           | SwiftUI, iOS 18+ only                                                                                        |
| Auth and wallets | [Privy Swift](https://docs.privy.io/basics/swift/quickstart). Member wallets plus per-group server treasury. |
| API              | Go                                                                                                           |
| Ledger           | [Supabase](https://supabase.com/) Postgres. Share units, votes, NAV snapshots, idempotent tx log.            |
| Execution        | [Jupiter Swap API v2](https://dev.jup.ag/docs/swap) on mainnet                                               |
| Fees             | App relayer (SOL)                                                                                            |
| Asset metadata   | [xStocks public API](https://api.xstocks.fi/api/v2/public/assets) (mints only)                               |
| Marks            | Jupiter fill + [Pyth Hermes](https://docs.pyth.network/price-feeds/core/api-instances-and-providers/hermes)  |


## Local env

Secrets use [dotenvx](https://dotenvx.com). Install the CLI (not a repo dependency):

```bash
brew tap dotenvx/brew && brew trust dotenvx/brew && brew install dotenvx
```

Or `curl -sfS https://dotenvx.sh | sh`. See [install docs](https://dotenvx.com/docs/install).

1. Copy `.env.example` → `.env.local` for local dev. Optionally add `.env.production`.
2. Encrypt: `dotenvx encrypt -f .env.local` (and `-f .env.production` if used).
3. Set values: `dotenvx set KEY value -f .env.local` (encrypts by default; `--plain` for non-secrets).

Justfile `dotenv-load` only reads plain `.env` — not dotenvx ciphertext. Recipes that need secrets re-exec once under `dotenvx run -f .env.local` (via `scripts/with-dotenv-local.sh`). Mobile Privy uses `scripts/ensure-ios-privy-config.sh` (xcconfig) + `SIMCTL_CHILD_*` at sim launch.

Day-to-day commands: **[Getting started with development](#getting-started-with-development)**.

### Running the stack

Need `.env.local` from one-time setup above. For iOS use `./scripts/ios-sim` or `just run mobile` from repo root. Bare `ios-sim` from `apps/mobile` skips Privy.

#### Gold slim simulator

Monaco uses one **gold slim** iOS Simulator: SimSlim RAM-thinned, fixed UDID `7B30D45E-62FD-42E2-871A-787B19D38CCF` (~0.9 GB vs ~4 GB stock). Reuse it across builds and QA. Never `simctl erase`, never spin up a fresh sim for QA, and never target by device name (e.g. `iPhone 17`) — always by UDID.

| Command | When to use |
| --- | --- |
| `ios-build` / `ios-sim` (`~/.local/bin`) | Bare compile or run on the gold sim. No Privy env. |
| `./scripts/ios-build` | Monaco compile: generates Privy xcconfig from `.env.local`, then calls `ios-build`. |
| `./scripts/ios-sim` | Monaco run: injects Privy via dotenvx, then calls `ios-sim`. Prefer this over bare `ios-sim`. |
| `just build mobile` | Compile gate with Privy xcconfig (same UDID). |
| `just run mobile` | Full run with Privy; delegates to `./scripts/ios-sim`. |

**Agent / sim QA.** Fast smoke (launch, primary nav, one critical path) — follow `.cursor/skills/ios-simslim-fast-qa/SKILL.md` (SimSlim verify, MobAI tap-through). Run unit tests first (`just test mobile`, no sim). XcodeBuildMCP: always pass `--simulator-id 7B30D45E-62FD-42E2-871A-787B19D38CCF`.

Privy test accounts and OTP codes: see **Privy (M1)** in `AGENTS.md`.

Private keys: `DOTENV_PRIVATE_KEY` for `.env` / `.env.local`; `DOTENV_PRIVATE_KEY_PRODUCTION` for `.env.production`. On macOS, new keys often land in Keychain, not `.env.keys`. Export with `dotenvx native pull` or `dotenvx keypair -f .env.local`.

Encrypted `.env*` files (public key in repo) may be committed. Never commit `.env.keys`, `.env.local`, or private keys. `.gitignore` covers `.env`; keep `.env.keys` and `.env.local` out of git locally.

A pre-commit hook runs `dotenvx precommit` and blocks commits of plaintext `.env*` files. Reinstall after clone: `chmod +x scripts/githooks/pre-commit && cp scripts/githooks/pre-commit .git/hooks/pre-commit` (or `dotenvx precommit --install`).

## Hackathon demo checklist

Judges should spend most of the live pass on P&L. Show the in-group member board and the app-wide group and people boards. The buy exists so those numbers are real.

1. Create a group. Set join policy (open or password), voter set, threshold, and expiry.
2. Join from a second account. Two names on the in-group board.
3. Both deposit mainnet USDC → sweep → share credit. Boards show 0% until a mark moves.
4. Search xStocks, propose a buy, pass the vote, Jupiter `/execute` success (prefer `AAPLx` on stage).
5. Group screen: pot composition, both slices, dollar P&L, in-group percent board.
6. App home: this group on the group board, both people on the people board (second group optional if time).
7. One member partial-redeems to USDC at a verified payout address. In-group board, group board, and people board update. The other member still in.

## Out of scope (MVP)

- Custom on-chain vault or share-token program
- On-chain voting
- Meteora DBC, DAMM, and Clawpump prize tracks
- Privy production webhooks (Enterprise)
- Android, web client, copy-trading network
- Primary issuer mint or redeem APIs (Backed client, institutional gates). Secondary Jupiter path only.
- App Store public listing, full KYC and AML, securities licensing. Demo may use TestFlight and geo-labeled test assets.

## Open decisions

These were not locked in the spec session. Do not invent them in code until they are.

- Who may **propose** a buy (any member, voter set only, or creator only).
- Failed `/execute` after a passed vote (mark failed, do not retry forever).
- Creator leave and group dissolve.

## Notes for production (not blockers for demo)

Tokenized stock exposure (`AAPLx`) is on-chain tracker exposure, not DTCC shares. Pooled custody and trade execution trigger broker-dealer, adviser, and money-transmitter questions in the US. Confirm the path with securities and fintech counsel before a consumer launch. Geo-fencing and licensed partner rails may be required for US persons depending on asset issuer terms.
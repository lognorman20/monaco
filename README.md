# Monaco

**Monaco** is an iOS app where friends form a group, pool USDC, and buy tokenized US stocks on Solana.

This document is the canonical product and architecture brief. It is not legal advice.

Prize target is the general Stocklana pool. Judges ask whether this could be a real app people use. The build does **not** use Meteora DBC or Clawpump. Execution is Jupiter Swap API v2 on **Solana mainnet** with small real USDC.

## Getting started with development

**Prereqs:** macOS, Xcode (iOS 18+ simulator), Docker, Go 1.23+, [just](https://github.com/casey/just), [dotenvx CLI](https://dotenvx.com/docs/install). SimSlim is optional.

**Clone setup**

1. Clone this repo. `cd` into the clone. Do not hard-code another machine's home path.
2. Place gitignored `.env.keys` in the repo root if a teammate encrypted `.env.local` for you. Also place that `.env.local`. dotenvx reads both from the clone root.
3. If you have no `.env.local` yet, copy `.env.example` to `.env.local` and set Privy plus relayer values with `dotenvx set KEY value -f .env.local`.
4. Run `./scripts/install-dev.sh` (or `just install`). It asks before each install (Go, just, dotenvx, optional SimSlim). `just install --check` only reports.
5. `just run` starts Postgres, the API, and the iOS app. Privy is injected via `scripts/ensure-ios-privy-config.sh` and `SIMCTL_CHILD_*`. If SimSlim is missing, the scripts warn and boot a stock simulator.

Do not wrap `just` with `dotenvx run` yourself. Recipes that need secrets re-exec under `scripts/with-dotenv-local.sh`. More on secrets: **[Local env](#local-env)**.

**Privy test logins** (fixed OTP; dashboard Login Methods must have **Email** and **SMS** on). Product path is OTP, not a password field. iOS bundle `com.monaco.app` must be on the Privy iOS client or `sendCode` returns 403 `invalid_native_app_id`. Sign out in-app to switch users.


| Name        | Method       | Login                                                          | OTP      |
| ----------- | ------------ | -------------------------------------------------------------- | -------- |
| Alfred      | Email or SMS | `test-8081@privy.io` or `+1 555 555 7177`                      | `465354` |
| Bartholomez | Email or SMS | `test-4952@privy.io` or `+1 555 555 9638`                      | `648588` |
| Cayman      | Email or SMS | `test-3510@privy.io` or `+1 555 555 8215`                      | `115543` |

**Commands**


| Command                      | What it does                                                                                                                                                           |
| ---------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `just install`               | Ask before installing missing tools. `just install --check` reports only                                                                                               |
| `just encrypt`               | `dotenvx encrypt` on `.env.local` (and `.env.production` if present)                                                                                                   |
| `just decrypt`               | `dotenvx decrypt` on `.env.local` (and `.env.production` if present)                                                                                                   |
| `just show-env`              | Print decrypted `.env.local` keys/values via dotenvx (`export KEY='value'` lines; `.env.production` omitted). Needs `.env.local`, dotenvx, and `.env.keys` or Keychain |
| `just run`                   | Full stack: Postgres + API + iOS app (dotenvx re-exec, Privy on sim)                                                                                                   |
| `just run backend`           | API only (dotenvx)                                                                                                                                                     |
| `just run mobile`            | iOS with Privy xcconfig + `SIMCTL_CHILD_*` via `./scripts/ios-sim`                                                                                                     |
| Logs                         | `just run*` tee stdout/stderr to `.logs/<timestamp>/` (`backend.log`, `mobile.log`)                                                                                    |
| `just stop`                  | Stop API + iOS app (kill port 8080, `simctl terminate` on the resolved sim)                                                                                            |
| `just stop backend`          | Stop API only                                                                                                                                                          |
| `just stop mobile`           | Terminate Monaco on the resolved sim; stop `xcodebuild` if running                                                                                                     |
| `just reset`                 | Stop all + wipe local Postgres volume + re-apply migrations (dotenvx)                                                                                                  |
| `just reset backend`         | Stop API + remove `bin/monaco-api`                                                                                                                                     |
| `just reset mobile`          | Stop app + `xcodebuild clean` on the resolved sim                                                                                                                      |
| `just reset db`              | Wipe local Docker Postgres volume + migrations (localhost only, dotenvx)                                                                                               |
| `just killports`             | Kill listeners on API port (default 8080; not Postgres 54322)                                                                                                          |
| `just test backend`          | Go tests + local DB smoke (dotenvx)                                                                                                                                    |
| `just test mobile`           | Host `swift test` in `packages/mobile-core` — fast, no secrets                                                                                                         |
| `just build backend`         | `go build` only — no dotenvx                                                                                                                                           |
| `just build mobile`          | Privy xcconfig, then `xcodebuild` on the resolved sim                                                                                                                  |
| `just relayer balance`       | Fee payer pubkey + mainnet SOL balance (dotenvx; no private key)                                                                                                       |
| `./scripts/ios-sim`          | Monaco run with Privy env. Falls back to a stock sim if slim is missing                                                                                                |
| `./scripts/ios-build`        | Monaco compile with Privy xcconfig                                                                                                                                     |
| `./scripts/sweep-wallets.sh` | **Ops.** Sweep USDC out of Privy wallets. See **[Ops: sweep USDC](#ops-sweep-usdc-out-of-privy-wallets)**                                                              |


Simulator UDID is **per machine**. Never commit one. Recipes call `scripts/resolve-ios-sim.sh`. Optional slim: **[SimSlim](#simslim-gold-simulator)**.

Live deposit QA on mainnet needs a **Phantom agent wallet** (not Privy, not `.env.local`). Install, fund (~$1 SOL + ~$4 USDC on Solana), send into the member inbox, then redeem leftover back: **[Agent QA: Phantom MCP](#agent-qa-phantom-mcp)**.

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

A **wallet** is a keypair on a chain. On Solana the public key is the **address** (base58). The private key **signs** transactions. The address holds:

- **SOL** — native token. Every tx burns a tiny amount as a fee. No SOL → send fails even if you hold USDC.
- **SPL tokens** — e.g. USDC. Same address, different mint. Monaco USDC mint: `EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v` ([Solana mainnet USDC](https://solscan.io/token/EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v)). USDC on Ethereum or Base is a different token; the deposit poller will not see it.

You do not “log into Solana.” You hold keys that can move whatever sits at that address. Whoever can sign, spends.

Product copy hides this. Devs still need it for QA.


| Wallet                   | Owner                            | Role                                                                                                                                    |
| ------------------------ | -------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| Member wallet            | One per user (Privy)             | Deposit inbox. Unique attribution for who funded. Backend-signable via Privy.                                                           |
| Group treasury (vault)   | One per group (Privy, app-owned) | Holds USDC and tokenized stocks. All group trades execute from here.                                                                    |
| Relayer / fee payer      | App                              | Pays SOL fees so treasury and member wallets need no SOL on the happy path.                                                             |
| Phantom **agent** wallet | Coding-agent harness             | **Not product.** Funds member inboxes for mainnet QA; leftover USDC returns here. Separate keys from Privy and from your phone Phantom. |




### Fee payer (relayer)

The app **fee payer** is a dedicated Solana keypair loaded from `RELAYER_PRIVATE_KEY` (base58 secret in `.env.local`). Never commit or log the private key. At API startup the backend derives the public key and refuses to boot unless that address holds **more than 0.001 SOL** on mainnet (gas + ATA rent for sweeps and Jupiter txs).


| Item            | Value                                                                                                                    |
| --------------- | ------------------------------------------------------------------------------------------------------------------------ |
| Env (secret)    | `RELAYER_PRIVATE_KEY` — base58 Solana secret key (not a JSON `[1,2,...]` array)                                          |
| Pubkey          | Derived at startup from the secret; logged as `pubkey=` on boot (no private key)                                         |
| Role            | Jupiter swap `payer`; relayer on deposit sweeps (Privy `SubmitSweep`)                                                    |
| SOL requirement | Balance **> 0.001 SOL** (`1_000_000` lamports). Fund on [Solana mainnet](https://solscan.io/) before `just run backend`. |


Print the fee payer pubkey and mainnet SOL/USDC balances without echoing the secret:

```bash
just relayer balance
```

Example output:

```text
address  EpeyGQXFY9vhkxPUZbz1wVRhs5vphRQt8SeJN2Gx1DrX
sol      0.003044217
usdc     0.00
```

Users never manage keys or approve individual Solana transactions in the happy path. The backend signs sweeps, swaps, and payouts.

Three wallets people mix up:

1. **Personal Phantom** (iOS / Android / browser extension). Your money. Create it yourself. See [Create a Phantom wallet](#create-a-phantom-wallet).
2. **Agent Phantom** (MCP). New dedicated wallet the first time the agent signs in. Empty until you fund it. This is the QA faucet and refund target.
3. **Privy product wallets.** Member inbox + group vault. Phantom MCP **cannot** spend these. The agent can only **send USDC to** the copyable member address, then **receive USDC back** when you redeem to the agent address.

Do not put `PHANTOM_APP_ID` in Monaco `.env.local`. If a Cursor plugin still wants it, put it in Cursor MCP env only. Current `@phantom/mcp-server` device-code login does not require a Portal app id.

### Deposit and sweep

1. The user funds **their** Privy wallet with USDC (onramp or external transfer).
2. The backend **sweeps** USDC from the member wallet into the group treasury (server-signed, no second approval sheet).
3. On **confirmed sweep into treasury**, credit share units at the current share price. Idempotent on transaction signature.
4. Do **not** credit shares when USDC only arrives in the member wallet. Sweep promptly.

See **NAV and share units** for the formula.

## Agent QA: Phantom MCP

Use this when a coding agent (or you, in Cursor chat) must move **real Solana mainnet** USDC into a sim user’s member wallet, then pull leftover cash out of the group vault when the run is done.

Keep the agent wallet thin. Preview software. Do not park rent money here.

### Create a Phantom wallet

Personal wallet first — that is how you buy SOL/USDC and top up the agent address.

1. Download only from [phantom.com/download](https://phantom.com/download) (iOS, Android, Chrome, Brave, Firefox, Edge). App Store: [Phantom](https://apps.apple.com/us/app/phantom-trade-markets/id1598432977). Play: [Phantom](https://play.google.com/store/apps/details?id=app.phantom).
2. Follow [How to create a new Phantom wallet](https://phantom.com/learn/guides/how-to-create-a-new-wallet): Create a New Wallet → Google or Apple, or a secret recovery phrase.
3. Write down the recovery phrase / PIN. Never paste it into git, tickets, or chat.
4. Overview: [Get started](https://phantom.com/get-started). Help: [help.phantom.com](https://help.phantom.com).



### Install the Phantom MCP (agent wallet)

This is the **wallet MCP** (`@phantom/mcp-server`): sign, transfer, swap. It is not the docs-only MCP at `https://docs.phantom.com/mcp`.

Docs: [Phantom MCP server](https://docs.phantom.com/phantom-mcp-server) · [Setup](https://docs.phantom.com/phantom-mcp-server/setup) · npm `[@phantom/mcp-server](https://www.npmjs.com/package/@phantom/mcp-server)` · [Cursor MCP](https://cursor.com/docs/context/mcp)

**Cursor plugin (easiest):** marketplace search `phantom-connect` / Add Plugin. Bundles wallet MCP + docs MCP. See [AI-assisted development](https://docs.phantom.com/developer-powertools/ai-tools).

**Manual Cursor:** add to `~/.cursor/mcp.json` (merge into existing `mcpServers`; this repo’s `.cursor/mcp.json` is XcodeBuildMCP + Pyth only):

```json
{
  "mcpServers": {
    "phantom": {
      "command": "npx",
      "args": ["-y", "@phantom/mcp-server@latest"]
    }
  }
}
```

Restart Cursor. First wallet tool call opens a browser for Google/Apple device-code sign-in. Session lives in `~/.phantom-mcp/session.json`. Reset: delete that file, restart, sign in again.

**Claude Code:** `claude mcp add phantom -- npx -y @phantom/mcp-server@latest`

On auth, Phantom mints a **new agent wallet**. It is not your extension wallet. Ask the agent for Solana addresses (`wallet_addresses` / `get_wallet_addresses`). Copy the Solana pubkey. That string is the refund target for leftover QA USDC. Each developer has their own; do not hardcode someone else’s address in the repo.

### Fund the agent wallet (~$1 SOL + ~$4 USDC on Solana)

The agent cannot transact on an empty wallet.


| Asset                     | Why                                                                                                 | Ballpark            |
| ------------------------- | --------------------------------------------------------------------------------------------------- | ------------------- |
| SOL on **Solana mainnet** | Fees when the agent sends USDC to a member inbox (and ATA rent if the dest has no USDC account yet) | about **$1** of SOL |
| USDC on **Solana**        | What the app actually credits after sweep                                                           | about **$4**        |


Buy or swap inside personal Phantom, then send **SOL** and **Solana USDC** to the **agent** Solana address. Or buy in-app onto the agent address if Phantom shows it. Confirm mint `EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v`. Ask the agent for `wallet_balances` before the first transfer.

Product path does **not** need SOL on the member wallet or vault (relayer pays). The **agent** still needs SOL because the agent is the sender.

### Send USDC into Monaco (member inbox → vault)

1. `just run` (API + gold sim). Sign in (SMS or email OTP).
2. Join or create a group → Add money. Copy the **Privy member** address (deposit inbox). Not the group treasury.
3. In Cursor: transfer **small** USDC on `solana:mainnet` to that address, mint above. MCP `transfer` / `transfer_tokens` simulates first; approve only if dest matches the copied inbox.
4. Poller detects member USDC, **sweeps** to the group treasury, then credits shares. Watch API logs / group view. Do not treat member-wallet balance as credited pot.
5. Explorer: [solscan.io](https://solscan.io) on the sweep signature.

Never insert `FAKE*` wallet rows in local Postgres. The poller will break.

### Sweep leftover back to the agent wallet (vault → Phantom)

Phantom MCP cannot pull from Privy. Reverse of deposit is **in-app redeem** to the agent Solana address.

1. Agent: print Solana address again. Confirm it is **your** MCP wallet.
2. Group screen → redeem leftover equity (slider at max if you want the pot empty). Payout address = that agent Solana address.
3. Wait for payout confirm. Agent: `wallet_balances` — USDC should be back. Treasury USDC for that test should be ~0 (dust from swaps possible).
4. If USDC is still sitting **only** in the member inbox (sweep not confirmed): do not “withdraw with Phantom.” Wait for sweep, then redeem. Or stop funding that inbox.
5. If the pot holds xStocks, redeem sells that slice to USDC first, then pays USDC. Tiny leftover stock/USDC dust can remain; keep QA notionals small.

After a funding run, leftover **agent-test USDC belongs on the agent Phantom**, not in a group vault and not in a sim user’s inbox.

### Ops: sweep USDC out of Privy wallets

Product path is poller member-inbox → treasury, then **in-app redeem**. Use this script only when USDC is stuck in Privy (inbox or treasury) and you must send it to a known Solana address (usually the agent Phantom).

**Danger.** Mainnet USDC. Wrong `DATABASE_URL` or `--all` against the prod Privy app can empty live pots and break share credits. Relayer still pays SOL fees.

```bash
# Always dry-run first. --all = every Solana wallet Privy returns for this app (not just local DB rows).
./scripts/sweep-wallets.sh --destination <solana_address> --all --dry-run

# Live: same flags without --dry-run. Type exactly:
#   I UNDERSTAND THIS MAY MESS WITH PROD
# then paste the destination address again.
./scripts/sweep-wallets.sh --destination <solana_address> --all
```


| Flag               | Meaning                                                                                  |
| ------------------ | ---------------------------------------------------------------------------------------- |
| `--destination`    | Required. Receives all swept USDC.                                                       |
| `--all`            | Source of truth = Privy `GET /v1/wallets?chain_type=solana` (paginated). Skips Postgres. |
| *(omit* `--all`*)* | Source = local `member_wallets` + `treasuries` for the `DATABASE_URL` in `.env.local`.   |
| `--dry-run`        | Print balances and `would sweep` lines. No txs. No confirm prompt.                       |


Needs `.env.local` (`PRIVY_*`, `RELAYER_PRIVATE_KEY`, `DATABASE_URL`). Wrapper is `scripts/with-dotenv-local.sh`. Amounts are micro-USDC (`1000000` = $1). Zero-balance wallets skip. Destination equal to a source skips.

Code: `apps/backend/cmd/sweep-member-to-address`. Full notes: `[docs/ops-sweep-wallets.md](docs/ops-sweep-wallets.md)`.

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
2. Encrypt: `just encrypt` (or `dotenvx encrypt -f .env.local`; also encrypts `.env.production` when that file exists). Decrypt: `just decrypt`.
3. Inspect: `just show-env` prints decrypted `.env.local` as `export KEY='value'` lines via dotenvx (`.env.production` omitted). Needs `.env.local`, dotenvx, and `.env.keys` or Keychain.
4. Set values: `dotenvx set KEY value -f .env.local` (encrypts by default; `--plain` for non-secrets).

Justfile `dotenv-load` only reads plain `.env` — not dotenvx ciphertext. Recipes that need secrets re-exec once under `dotenvx run -f .env.local` (via `scripts/with-dotenv-local.sh`). Mobile Privy uses `scripts/ensure-ios-privy-config.sh` (xcconfig) + `SIMCTL_CHILD_*` at sim launch.

Day-to-day commands: **[Getting started with development](#getting-started-with-development)**.

### Running the stack

Need `.env.local` (and `.env.keys` when the file is encrypted) from clone setup above. API: `just run backend`. iOS: `just run mobile` always injects Privy. SimSlim is optional RAM savings, not required.

#### SimSlim (gold simulator)

**SimSlim** turns one iOS Simulator into a RAM-thin “gold” device (~0.9 GB vs ~4 GB stock) by disabling unused sim daemons. Pick **one** sim per machine, slim it, reuse it. Apple mints a new UUID on `simctl create` — **never commit a UDID**. Recipes read `SIMSLIM_UDID`.

Never `simctl erase` that device (wipe kills slim + the app container). Never target by device name (`iPhone 17`). Always `$SIMSLIM_UDID`.

Slim is **not** required to develop. `just run`, `just run mobile`, and `./scripts/ios-sim` warn and use a stock simulator when SimSlim is missing or `SIMSLIM_UDID` is unset.

##### Wire SimSlim on a new machine

1. **Xcode** with an **iOS 18.5+** simulator runtime (slim does not persist across reboot below 18.5).
2. **Install SimSlim** (Homebrew tap; not a repo dependency):
  ```bash
   brew install mobai-app/tap/simslim
  ```
3. **Create or pick one iPhone sim**, copy the UDID:
  ```bash
   xcrun simctl list devices available
   xcrun simctl list runtimes
   # Example — Apple assigns a new UDID:
   xcrun simctl create "Monaco Gold" com.apple.CoreSimulator.SimDeviceType.iPhone-16 <runtime-identifier>
  ```
4. **Export** `SIMSLIM_UDID` (not a secret). Shell rc **and/or** plain gitignored `.env` (Justfile `dotenv-load` reads `.env`, not dotenvx `.env.local`):
  ```bash
   export SIMSLIM_UDID="<YOUR_UDID>"
   export PATH="$HOME/.local/bin:$PATH"
   # optional: echo "SIMSLIM_UDID=<YOUR_UDID>" >> .env
  ```
   `just build mobile`, `just reset mobile`, and `./scripts/ios-sim` call `scripts/resolve-ios-sim.sh` (stock fallback if slim is missing). Agent QA that must hit gold uses `scripts/gold-sim-udid.sh`, which exits 1 unless `SIMSLIM_UDID` is set and that device exists. If slim verify fails, recipes still use that UDID as a normal simulator. They do not pick a different device.
5. **Slim profile.** Repo copy: `[ci/profiles/base-slim.json](ci/profiles/base-slim.json)` (`{"except": []}` = max slim). Copy to the default path the wrappers look for, or point `SIMSLIM_PROFILE` at the repo file:
  ```bash
   mkdir -p ~/.config/simslim
   cp ci/profiles/base-slim.json ~/.config/simslim/base-slim.json
   simslim on "$SIMSLIM_UDID" --profile ~/.config/simslim/base-slim.json --json
  ```
   Every session before driving UI:
   `except` in a profile means **keep** that daemon category on (less slim). Photos QA would use `"except": ["photos"]`. Monaco product smoke is tabs + HTTP; base-slim is enough.
6. **Optional PATH wrappers.** Some agent machines keep `ios-sim` / `ios-build` in `~/.local/bin`. Those are **not** in git and are **not** required. Repo `./scripts/ios-sim` and `./scripts/ios-build` call `xcodebuild` and `simctl` themselves after Privy injection.
  Optional: `export SIMSLIM_PROFILE="$PWD/ci/profiles/base-slim.json"` if you slim from a checkout whose cwd is `apps/mobile`.

Keep gold **booted** between agent sessions when you can. Clone gold after slim-once if you need a second sim (clone inherits slim + apps).

##### Daily use (slim installed)

Repo scripts inject Privy. Bare `xcodebuild` from `apps/mobile` without `ensure-ios-privy-config.sh` skips Privy.


| Command               | When to use                                                                             |
| --------------------- | --------------------------------------------------------------------------------------- |
| `./scripts/ios-build` | Monaco compile: Privy xcconfig from `.env.local`, then `xcodebuild` on the resolved sim |
| `./scripts/ios-sim`   | Monaco run: dotenvx Privy + `SIMCTL_CHILD_*`, then build/install/launch                 |
| `just build mobile`   | Compile gate on the resolved sim                                                        |
| `just run mobile`     | Full run with Privy; calls `./scripts/ios-sim`                                          |
| `just run`            | Postgres + API + iOS                                                                    |


**Agent / sim QA.** Unit tests first (`just test mobile`, no sim). Fast smoke: `.cursor/skills/ios-simslim-fast-qa/SKILL.md`. MobAI desktop is optional. XcodeBuildMCP: `--simulator-id` from `./scripts/gold-sim-udid.sh` (requires `SIMSLIM_UDID`). Human `just run` does not.

#### Without slim sim

Stock Xcode Simulator runs the same app. You do not need Homebrew `simslim`, `~/.local/bin/ios-sim`, MobAI, or `$SIMSLIM_UDID`.

`just run mobile` is the path. You should see a warning that SimSlim is not installed or `SIMSLIM_UDID` is unset, then a stock sim boot. Privy xcconfig and `SIMCTL_CHILD_*` still apply.

Xcode Cmd+R also works after `./scripts/ensure-ios-privy-config.sh generate`. Without that file the app shows “Privy not configured”.

Do not `simctl erase` a sim you later want as gold. Creating extra stock sims for local play is fine.

Privy test emails/SMS and OTP: **[Getting started](#getting-started-with-development)**.

Private keys: `DOTENV_PRIVATE_KEY` for `.env` / `.env.local`; `DOTENV_PRIVATE_KEY_PRODUCTION` for `.env.production`. On macOS, new keys often land in Keychain, not `.env.keys`. Export with `dotenvx native pull` or `dotenvx keypair -f .env.local`.

Encrypted `.env*` files (public key in repo) may be committed. Never commit `.env.keys`, `.env.local`, or private keys. `.gitignore` covers `.env`; keep `.env.keys` and `.env.local` out of git locally.

A pre-commit hook checks **staged** `.env*` files only (not `.worktrees` or the rest of the tree) and blocks plaintext secrets / `.env.keys`. `.env.example` is allowed. Reinstall after clone: `ln -sfn ../../scripts/githooks/pre-commit .git/hooks/pre-commit`. Do not run `dotenvx precommit --install` — that full-tree scan is slow.

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
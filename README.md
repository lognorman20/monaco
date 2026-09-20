# Monaco

iOS app: friends pool USDC and buy tokenized US stocks on Solana. Product rules: [`docs/product.md`](docs/product.md). Milestone backlog: [`docs/index.md`](docs/index.md).

Monaco lets you create a hedge fund with friends by pooling money to buy stocks together. Members propose and vote on trades, and approved trades execute for the group; as the pool profits, each member’s stake increases in value through NAV. You can even add an agent to your cabal to trade on your behalf. Built as a social trading app, Monaco turns investing into an easy group game anyone can join simply by depositing money.

## Prereqs

macOS, Xcode (iOS 18+ simulator), Docker, Go 1.25+, [just](https://github.com/casey/just), [dotenvx CLI](https://dotenvx.com/docs/install). SimSlim is optional.

## Clone setup

1. Clone this repo. `cd` into the clone. Do not hard-code another machine's home path.
2. Place gitignored `.env.keys` in the repo root if a teammate encrypted `.env.local` for you. Also place that `.env.local`. dotenvx reads both from the clone root.
3. If you have no `.env.local` yet, copy `.env.example` to `.env.local` and set Privy plus relayer values with `dotenvx set KEY value -f .env.local`.
4. Run `./scripts/install-dev.sh` (or `just install`). It asks before each install (Go, just, dotenvx, optional SimSlim). `just install --check` only reports.
5. `just run` starts Postgres, the API, and the iOS app. Privy is injected via `scripts/ensure-ios-privy-config.sh` and `SIMCTL_CHILD_*`. If SimSlim is missing, the scripts warn and boot a stock simulator.

Do not wrap `just` with `dotenvx run` yourself. Recipes that need secrets re-exec under `scripts/with-dotenv-local.sh`.

Local DB is Docker Compose Postgres only (`monaco`, host port `54322`). Never point `just run` / `just test backend` at hosted or production Supabase.

## Privy test logins

Fixed OTP. Dashboard Login Methods must have **Email** and **SMS** on. Product path is OTP, not a password field. iOS bundle `com.monaco.app` must be on the Privy iOS client or `sendCode` returns 403 `invalid_native_app_id`. Sign out in-app to switch users.

| Name        | Phone Number       | Login                                     | OTP      |
| ----------- | ------------ | ----------------------------------------- | -------- |
| Alfred      | `+1 555 555 7177` | `test-8081@privy.io` | `465354` |
| Bartholomez | `+1 555 555 9638` | `test-4952@privy.io` | `648588` |
| Cayman      | `+1 555 555 8215` | `test-3510@privy.io` | `115543` |

## Deposits

You can fund a group from **personal Phantom** (iOS app or browser extension). That is your wallet, not the [agent MCP wallet](#agent-qa-phantom-mcp). No Cursor or coding agent required.

1. `just run`. Sign in (OTP above).
2. Join or create a group → **Add money**. Copy the **Privy member** deposit address (deposit inbox). Not the group treasury.
3. In Phantom, send **USDC on Solana mainnet**. Mint must be `EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v`. USDC on Ethereum or Base is a different token; the poller will not see it.
4. The backend poller detects USDC in the member wallet, **sweeps** it into the group treasury, then credits share units. Watch API logs or the group view. Do not treat USDC sitting only in the member wallet as credited pot — wait for sweep confirm.
5. Member wallet and vault do not need SOL (relayer pays fees).

To pull leftover QA cash back out, use in-app **redeem** (Phantom cannot spend Privy wallets). Agent-driven deposit and refund: [Agent QA: Phantom MCP](#agent-qa-phantom-mcp).

## Commands

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
| `just test backend`          | Go tests with the race detector + local DB smoke (dotenvx)                                                                                                             |
| `just test mobile`           | Host `swift test` in `packages/mobile-core` — fast, no secrets                                                                                                         |
| `just build backend`         | `go build` only — no dotenvx                                                                                                                                           |
| `just build mobile`          | Privy xcconfig, then `xcodebuild` on the resolved sim                                                                                                                  |
| `just relayer balance`       | Fee payer pubkey + mainnet SOL and USDC (dotenvx; no private key)                                                                                                      |
| `just seed demo`             | Backfill local Postgres with demo cabals, trades, proposals and NAV history for the most recent user. Flags: `--user-id`, `--privy-user-id`, `--if-empty=false`       |
| `just faker <profile>`       | Seed scale or mixed fake data into local Postgres. See **[Demo data](#demo-data-faker-seed)**                                                                          |
| `./scripts/ios-sim`          | Monaco run with Privy env. Falls back to a stock sim if slim is missing                                                                                                |
| `./scripts/ios-build`        | Monaco compile with Privy xcconfig                                                                                                                                     |
| `./scripts/sweep-wallets.sh` | **Ops.** Sweep USDC out of Privy wallets. See **[Ops: sweep USDC](#sweep-usdc-out-of-privy-wallets)**                                                              |

Simulator UDID is **per machine**. Never commit one. Recipes call `scripts/resolve-ios-sim.sh`.

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

Private keys: `DOTENV_PRIVATE_KEY` for `.env` / `.env.local`; `DOTENV_PRIVATE_KEY_PRODUCTION` for `.env.production`. On macOS, new keys often land in Keychain, not `.env.keys`. Export with `dotenvx native pull` or `dotenvx keypair -f .env.local`.

Encrypted `.env*` files (public key in repo) may be committed. Never commit `.env.keys`, `.env.local`, or private keys. `.gitignore` covers `.env`; keep `.env.keys` and `.env.local` out of git locally.

A pre-commit hook checks **staged** `.env*` files only (not `.worktrees` or the rest of the tree) and blocks plaintext secrets / `.env.keys`. `.env.example` is allowed. Reinstall after clone: `ln -sfn ../../scripts/githooks/pre-commit .git/hooks/pre-commit`. Do not run `dotenvx precommit --install` — that full-tree scan is slow.

## Relayer (fee payer)

The app **fee payer** is a dedicated Solana keypair from `RELAYER_PRIVATE_KEY` (base58 secret in `.env.local`). Not a Privy wallet. Clones that decrypt the same shared env share the same fee payer. Never commit or log the private key.

At API startup the backend derives the public key and refuses to boot unless that address holds **more than 0.001 SOL** on mainnet.

| Item            | Value                                                                                                                    |
| --------------- | ------------------------------------------------------------------------------------------------------------------------ |
| Env (secret)    | `RELAYER_PRIVATE_KEY` — base58 Solana secret key (not a JSON `[1,2,...]` array)                                          |
| Pubkey          | Derived at startup from the secret; logged as `pubkey=` on boot (no private key)                                         |
| Role            | Jupiter swap `payer`; relayer on deposit sweeps (Privy `SubmitSweep`)                                                    |
| SOL requirement | Balance **> 0.001 SOL** (`1_000_000` lamports). Fund on [Solana mainnet](https://solscan.io/) before `just run backend`. |

```bash
just relayer balance
```

Example:

```text
address  EpeyGQXFY9vhkxPUZbz1wVRhs5vphRQt8SeJN2Gx1DrX
sol      0.003044217
usdc     0.00
```

## Swap provider (Jupiter or Definitive Flash)

Treasury buys and sells go through `swapprovider.Provider` (`apps/backend/internal/swapprovider`). `SWAP_PROVIDER` picks the venue at API boot; the choice is logged as `swap provider ready`.

| `SWAP_PROVIDER`     | Venue                                                                 | Needs                                  |
| ------------------- | --------------------------------------------------------------------- | -------------------------------------- |
| `jupiter` (default) | Jupiter Swap API v2: order → treasury + relayer sign → execute → poll | nothing new                            |
| `flash`             | [Definitive Flash](https://flash.definitive.fi/docs): quote → sign → order → poll | `FLASH_API_KEY`, Privy authorization key |

Flash on Solana, per trade: `POST /quote` with the treasury as `funderAddress`, the treasury wallet signs the quote's plaintext `svm.orderMessage` (Privy `signMessage`, Ed25519), `POST /order`, then poll `GET /orders/{orderId}` until `ORDER_STATUS_FILLED`. The backend refuses to sign unless the message commits to the mint and atomic amount it asked for, and unless the quote deadline is still ahead.

First Flash trade of a token per treasury also needs an onchain setup: create the token account and `Approve` the Flash program as SPL delegate. The backend sends both in one transaction with the **relayer as fee payer and rent payer** and the treasury as co-signer, then re-quotes until Flash sees it. That costs the relayer about 0.002 SOL per new token account.

Get a key at [app.definitive.fi](https://app.definitive.fi) → More → Flash → Create Flash Key, then set `SWAP_PROVIDER=flash` and `FLASH_API_KEY` in `.env.local`. `FLASH_MAX_SLIPPAGE` (default `0.01`) bounds executed vs quoted output. Unset `SWAP_PROVIDER` to go back to Jupiter; no data migration either way.

Every treasury swap follows the flag, including the sells a cash-out triggers (`RedeemService` calls `SwapService.SellToUSDC`). Still on Jupiter regardless: the price quotes shown in the app, catalog routability probes, and `cmd/sweep-member-to-address`.

## Demo data (faker seed)

Seeds fake-but-realistic data so Home, Groups, proposals, and activity look alive without a
full Privy setup. Local Postgres only. It never calls Privy, Solana RPC, or Jupiter.

Two profiles:

- **scale**: six fake clubs (Ridgewood Value Club, Night Shift Traders, Harbor Street Fund, plus the
  smaller Dorm 4B fund, Rent money and Index huggers).
  Each has a fake creator, five depositors, deposits spread over the last week, a confirmed
  AAPLx/TSLAx buy, a governed sell (Ridgewood and Night Shift), failed and open proposals, votes,
  and NAV history for charts. Any signed-in user sees them on Home (group board and people
  leaderboard) and the Cabals tab (search, leaderboard, P&L history) and can open them read-only. You are never added as a member. Join, fund/deposit,
  quote, propose (buy, sell, or agent), vote, agent intents, and leave/withdraw return
  `403 faker_group_read_only`.
- **mixed**: adds ghost members Maya Chen, Jordan Hale, and Priya Shah to **your own real club**
  (you must be its creator). They show up on the member board with P&L, deposits, and ghost-only
  proposals. They never count toward the pot, surplus credits, or the voter set, and they have no
  wallets or swaps. You can still deposit and propose for real.
- **demo**: the recording variant of **mixed**. Same ghosts, plus six chat messages from the last
  90 minutes, a thesis on the ghost Tesla proposal and one ghost comment on it. It skips the
  pending and failed ghost deposits and the failed and expired ghost proposals, so no "Failed"
  rows show on screen. Pass a second id, a real member's open proposal in the same club, to add
  two ghost comments to it (Maya asks, Jordan replies).

```bash
just reset db                        # optional: start from an empty DB
just faker scale                     # six fake clubs
just faker mixed <your_group_id>     # ghosts on your real club
just faker all <your_group_id>       # both
just faker demo <your_group_id> [<real_proposal_id>]   # recording setup
```

Ghost votes cannot make a proposal votable (ghosts are outside the voter set), so the live vote
in a demo is on a proposal a second real account creates in the app. Seed `demo` first, then
re-run it with that proposal's id to add the comments. Re-running is safe.

Photos: set `FAKER_PHOTO_BASE_URL` (for example
`https://<project>.supabase.co/storage/v1/object/public/avatars/faker`) and upload `maya.jpg`,
`jordan.jpg` and `priya.jpg` (square, 200 KB or less) there; see `docs/ops-profile-photos.md`.
Without it the ghosts show initials.

Re-running is safe: users are keyed by `faker:user:<name>` and clubs by `groups.faker_key`, so
a re-run replaces the fake rows and moves the timestamps up to now, with no duplicates.
`just reset db` wipes the seed data too.

The API can also seed over HTTP when `FAKER_ENABLED=1` (off by default: the route returns 404). It
only accepts loopback callers with no proxy headers and a local `DATABASE_URL`:

```bash
curl -s -X POST http://127.0.0.1:8080/v1/dev/faker \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"profile":"all","group_id":"<your_group_id>"}'   # profile: mixed | scale | all | demo
# demo also takes "proposal_id": a real member's proposal in that club
```

Safety: faker rows are flagged (`users.is_faker`, `groups.is_faker`, migration 000016). The
sweep poller, surplus reconcile, execute poller (buys and sells), swap, redeem/withdraw, and
`sweep-wallets` treasury sources skip them whether or not
`FAKER_ENABLED` is set, so leftover seed rows stay inert. A DB trigger rejects member wallets
for faker users.

## iOS API environments

The app's API base URL comes from the build, not from source: `apps/mobile/Config/Monaco.xcconfig` → Info.plist (`MONACO_ENVIRONMENT`, `MONACO_API_BASE_URL`) → `MonacoConfig.api` in `packages/mobile-core`, which both API clients use.

| Environment  | Default for | Base URL                                                                 |
| ------------ | ----------- | ------------------------------------------------------------------------ |
| `local`      | Debug       | `http://localhost:8080` (`Config/Environments/Local.xcconfig`)           |
| `staging`    | —           | `MONACO_STAGING_API_BASE_URL` — **placeholder, empty until you set it**   |
| `production` | Release     | `MONACO_PRODUCTION_API_BASE_URL` — **placeholder, empty until you set it** |

- `just run` / `just run mobile` need nothing extra: Debug is `local`.
- Set the remote URLs once (not secrets, `https://` only): `dotenvx set MONACO_STAGING_API_BASE_URL https://… -f .env.local --plain` (same for `MONACO_PRODUCTION_API_BASE_URL`). `scripts/ensure-ios-privy-config.sh` writes them to the gitignored `Config/Environment.local.xcconfig` and rejects a non-https value.
- Build for another environment: `xcodebuild … MONACO_ENVIRONMENT=staging` (or pass `MONACO_STAGING_API_BASE_URL=https://…` on the same command line).
- Point an already-built Debug sim at staging or a tunnel without rebuilding: `MONACO_API_BASE_URL=https://<tunnel-host> just run mobile` (exported as `SIMCTL_CHILD_MONACO_API_BASE_URL`; add `MONACO_ENVIRONMENT=staging` to label it). Debug builds only.
- Release builds ignore the process environment and refuse to launch (`fatalError` naming the setting to fix) when the URL is empty, malformed, not `https`, a local host, or the environment is `local`. The rules live in `MonacoAPIConfiguration` and are covered by `just test mobile`.
- ATS stays strict. Only the Debug Info.plist carries `NSAllowsLocalNetworking`; there is no `NSAllowsArbitraryLoads`, so a Debug tunnel/staging URL must be `https` too.
- The active environment is logged at launch (`API environment: …`); Debug builds also show it under the session error on the sign-in gate.

## Simulator

Slim is **not** required. `just run`, `just run mobile`, and `./scripts/ios-sim` warn and use a stock Xcode simulator when SimSlim is missing or `SIMSLIM_UDID` is unset. Privy xcconfig and `SIMCTL_CHILD_*` still apply.

`just test mobile` never boots a sim (host `swift test` in `packages/mobile-core`).

Fail only if no iOS Simulator exists: Xcode → Settings → Platforms, download an iOS 18+ runtime, create an iPhone sim.

Xcode Cmd+R also works after `./scripts/ensure-ios-privy-config.sh generate`. Without that file the app shows “Privy not configured”.

Never `simctl erase` a sim you later want as gold. Never commit a UDID. Never target by device name (`iPhone 17`).

### SimSlim (optional)

**SimSlim** turns one iOS Simulator into a RAM-thin “gold” device (~0.9 GB vs ~4 GB stock). Pick **one** sim per machine, slim it, reuse it.

1. **Xcode** with an **iOS 18.5+** simulator runtime (slim does not persist across reboot below 18.5).
2. Install: `brew install mobai-app/tap/simslim`
3. Create or pick one iPhone sim, copy the UDID:

   ```bash
   xcrun simctl list devices available
   xcrun simctl list runtimes
   # Example — Apple assigns a new UDID:
   xcrun simctl create "Monaco Gold" com.apple.CoreSimulator.SimDeviceType.iPhone-16 <runtime-identifier>
   ```

4. Export `SIMSLIM_UDID` (not a secret) in shell rc **and/or** plain gitignored `.env` (Justfile `dotenv-load` reads `.env`, not dotenvx `.env.local`):

   ```bash
   export SIMSLIM_UDID="<YOUR_UDID>"
   # optional: echo "SIMSLIM_UDID=<YOUR_UDID>" >> .env
   ```

   Agent QA that must hit gold uses `scripts/gold-sim-udid.sh`, which exits 1 unless `SIMSLIM_UDID` is set and that device exists. If slim verify fails, human recipes still use that UDID as a normal simulator.

5. Slim profile. Repo copy: [`ci/profiles/base-slim.json`](ci/profiles/base-slim.json).

   ```bash
   mkdir -p ~/.config/simslim
   cp ci/profiles/base-slim.json ~/.config/simslim/base-slim.json
   simslim on "$SIMSLIM_UDID" --profile ~/.config/simslim/base-slim.json --json
   ```

   `except` in a profile means **keep** that daemon category on. Monaco product smoke is tabs + HTTP; base-slim is enough.

Repo `./scripts/ios-sim` and `./scripts/ios-build` call `xcodebuild` and `simctl` after Privy injection. Optional PATH wrappers in `~/.local/bin` are **not** in git and **not** required.

Keep gold **booted** between agent sessions when you can. Clone gold after slim-once if you need a second sim.

## Agent QA: Phantom MCP

Use this when a coding agent (or you, in Cursor chat) must move **real Solana mainnet** USDC into a sim user’s member wallet, then pull leftover cash out of the group vault when the run is done.

Keep the agent wallet thin. Preview software. Do not park rent money here.

Three wallets people mix up:

1. **Personal Phantom** (iOS / Android / browser extension). Your money. Create it yourself (below).
2. **Agent Phantom** (MCP). New dedicated wallet the first time the agent signs in. Empty until you fund it. QA faucet and refund target.
3. **Privy product wallets.** Member inbox + group vault. Phantom MCP **cannot** spend these. The agent can only **send USDC to** the copyable member address, then **receive USDC back** when you redeem to the agent address.

Do not put `PHANTOM_APP_ID` in Monaco `.env.local`. If a Cursor plugin still wants it, put it in Cursor MCP env only. Current `@phantom/mcp-server` device-code login does not require a Portal app id.

Never insert `FAKE*` wallet rows in local Postgres. The poller will break.

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

Buy or swap inside personal Phantom, then send **SOL** and **Solana USDC** to the **agent** Solana address. Confirm mint `EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v`. Ask the agent for `wallet_balances` before the first transfer.

Product path does **not** need SOL on the member wallet or vault (relayer pays). The **agent** still needs SOL because the agent is the sender.

### Send USDC into Monaco (member inbox → vault)

Same deposit path from **personal Phantom** works without MCP; see [Deposits](#deposits).

1. `just run` (API + sim). Sign in (SMS or email OTP).
2. Join or create a group → Add money. Copy the **Privy member** address (deposit inbox). Not the group treasury.
3. In Cursor: transfer **small** USDC on `solana:mainnet` to that address, mint above. MCP `transfer` / `transfer_tokens` simulates first; approve only if dest matches the copied inbox.
4. Poller detects member USDC, **sweeps** to the group treasury, then credits shares. Watch API logs / group view. Do not treat member-wallet balance as credited pot.
5. Explorer: [solscan.io](https://solscan.io) on the sweep signature.

### Sweep leftover back to the agent wallet (vault → Phantom)

Phantom MCP cannot pull from Privy. Reverse of deposit is **in-app redeem** to the agent Solana address.

1. Agent: print Solana address again. Confirm it is **your** MCP wallet.
2. Group screen → redeem leftover equity (slider at max if you want the pot empty). Payout address = that agent Solana address.
3. Wait for payout confirm. Agent: `wallet_balances` — USDC should be back. Treasury USDC for that test should be ~0 (dust from swaps possible).
4. If USDC is still sitting **only** in the member inbox (sweep not confirmed): do not “withdraw with Phantom.” Wait for sweep, then redeem. Or stop funding that inbox.
5. If the pot holds xStocks, redeem sells that slice to USDC first, then pays USDC. Tiny leftover stock/USDC dust can remain; keep QA notionals small.

After a funding run, leftover **agent-test USDC belongs on the agent Phantom**, not in a group vault and not in a sim user’s inbox.

## Sweep USDC out of Privy wallets

Product path is poller member-inbox → treasury, then **in-app redeem**. Use this script only when USDC is stuck in Privy (inbox or treasury) and you must send it to a known Solana address (usually the agent Phantom).

**Danger.** Mainnet USDC. Wrong `DATABASE_URL` or `--all` against the prod Privy app can empty live pots and break share credits. Relayer still pays SOL fees.

```bash
# One wallet (or list). Always dry-run first.
./scripts/sweep-wallets.sh --destination <solana_address> --source <wallet> --dry-run
./scripts/sweep-wallets.sh --destination <solana_address> --source <wallet_a> --source <wallet_b> --dry-run

# --all = every Solana wallet Privy returns for this app (not just local DB rows).
./scripts/sweep-wallets.sh --destination <solana_address> --all --dry-run

# Live: same flags without --dry-run. Type exactly:
#   I UNDERSTAND THIS MAY MESS WITH PROD
# then paste the destination address again.
./scripts/sweep-wallets.sh --destination <solana_address> --source <wallet>
./scripts/sweep-wallets.sh --destination <solana_address> --all
```

| Flag               | Meaning                                                                                  |
| ------------------ | ---------------------------------------------------------------------------------------- |
| `--destination`    | Required. Receives all swept USDC.                                                       |
| `--source`         | Drain only listed wallet(s). Repeatable; comma-separate in one value. Do not mix with `--all`. |
| `--all`            | Source of truth = Privy `GET /v1/wallets?chain_type=solana` (paginated). Skips Postgres. |
| *(omit both)*      | Source = local `member_wallets` + `treasuries` for the `DATABASE_URL` in `.env.local`.   |
| `--dry-run`        | Print balances and `would sweep` lines. No txs. No confirm prompt.                       |

Needs `.env.local` (`PRIVY_*`, `RELAYER_PRIVATE_KEY`, `DATABASE_URL`). Wrapper is `scripts/with-dotenv-local.sh`. Amounts are micro-USDC (`1000000` = $1). Zero-balance wallets skip. Destination equal to a source skips.

Code: `apps/backend/cmd/sweep-member-to-address`. Full notes: [`docs/ops-sweep-wallets.md`](docs/ops-sweep-wallets.md).

## Agent skills (Cursor)

Cursor loads repo skills from [`.cursor/skills/`](.cursor/skills/). Attach one in chat, or let the agent pick it from the description. Humans do not need these to `just run`.

Learned prefs and durable facts live in [`AGENTS.md`](AGENTS.md). Skills are the step-by-step workflows.

| Skill | Path | When to use |
| ----- | ---- | ----------- |
| **write-ticket** | [`.cursor/skills/write-ticket/SKILL.md`](.cursor/skills/write-ticket/SKILL.md) | Draft GitHub (or Linear) issue bodies. Six-section shape: Context, Problem, Proposal (with Scope), Acceptance Criteria (≥2 checkboxes), Verification commands, Done when. Keep Context vs Problem distinct. No nested triple-backtick fences inside the ticket body. |
| **worktree-orchestrate** | [`.cursor/skills/worktree-orchestrate/SKILL.md`](.cursor/skills/worktree-orchestrate/SKILL.md) | Parallel milestone work. Parent stays on the integration branch (`milestone-N`). Implementers ship in git worktrees on `feat/*`. Default implementer model is Composer 2.5. One light review, then merge. Do not nest another orchestrator. Split mobile vs backend to separate agents. Kickoff templates: [`prompts.md`](.cursor/skills/worktree-orchestrate/prompts.md). |
| **ios-simslim-fast-qa** | [`.cursor/skills/ios-simslim-fast-qa/SKILL.md`](.cursor/skills/ios-simslim-fast-qa/SKILL.md) | Agent sim smoke / tap-through. Unit tests first (`just test mobile`, no sim). Then one gold slim sim. Never `simctl erase`. Never destination by device name. XcodeBuildMCP needs `--simulator-id` from `scripts/gold-sim-udid.sh` (`SIMSLIM_UDID` required). Human `just run` uses stock-sim fallback instead. |
| **anti-ai-slop** | [`.cursor/skills/anti-ai-slop/SKILL.md`](.cursor/skills/anti-ai-slop/SKILL.md) | Any UI, SwiftUI, empty states, onboarding, or marketing copy. Banlist for purple gradients, emoji-as-icons, Inter/system-ui-as-brand, glassmorphism, generic SaaS card grids. Product copy stays social-investing language (no wallets/gas/mint in the UI). |
| **testing-expert** | [`.cursor/skills/testing-expert/SKILL.md`](.cursor/skills/testing-expert/SKILL.md) | How to write tests: small surface, deterministic, realistic data. This copy is TS/Jest-oriented; Monaco still follows the same bar in Go and Swift. `just test mobile` is host `swift test`. `just test backend` uses stubs — never hit live Jupiter. Skip property tests that run longer than ~2 minutes. |

Do not copy these skills into another machine's home path. Clone the repo; Cursor sees `.cursor/skills/` from the workspace.

## Tests and CI

| Suite | Command |
| --- | --- |
| Backend (needs Docker Postgres) | `just test backend` |
| Domain math, no database | `cd packages/domain && go test -race ./...` |
| Reference trading bot | `cd agents/momentum-bot && go test ./...` |
| Shared Swift logic | `just test mobile` |

Backend tests never touch the app database: they derive `{dbname}_test` from `DATABASE_URL`, create it if missing, and migrate it (`apps/backend/internal/postgres/testdb.go`). Without `just`: export `DATABASE_URL` and run `go test -race -p 1 ./...` from `apps/backend` (`-p 1` because the packages share that one test database).

`.github/workflows/ci.yml` runs on pull requests and pushes to `main`: a Go job (Postgres 16 service container, migrations on a clean database, `go vet`, `go test -race` for `apps/backend`, `packages/domain` and `agents/momentum-bot`) and a macOS job (`swift test` in `packages/mobile-core`). The iOS app target is not built in CI.

## Deploy

There is no deploy pipeline in this repo yet; the demo runs the API on a laptop. What a host needs:

**API.** One Go binary.

```bash
just build backend          # bin/monaco-api
API_ADDR=0.0.0.0:8080 MIGRATIONS_DIR=/path/to/supabase/migrations ./bin/monaco-api
```

- Migrations in `supabase/migrations` are applied at boot, in filename order, before the server listens. `go run ./cmd/migrate` (from `apps/backend`) applies them without starting the API.
- Required env: `DATABASE_URL`, `PRIVY_APP_ID`, `PRIVY_APP_SECRET`, `PRIVY_VERIFICATION_KEY`, `RELAYER_PRIVATE_KEY`. The API exits at boot if any is missing or malformed. Outside local dev also set `SOLANA_RPC_URL` to a paid RPC (unset falls back to the public mainnet endpoint, which has no SLA and is what confirms sweeps) and `APP_ENV` (`staging`, `prod`), which also switches stderr logs to JSON lines for the host's log collector. The Postgres pool is capped at `DB_MAX_OPEN_CONNS` (default 20); keep it under the database role's connection limit. The full list with comments is in `.env.example`. Use separate Privy apps, relayer keys and databases per environment; production values go in `.env.production` (dotenvx-encrypted), never in the image.
- The relayer address must hold more than 0.001 SOL or the API exits at boot. See [Relayer](#relayer-fee-payer).
- The API listens on `API_ADDR` (default `127.0.0.1:8080`). `GET /health` probes Postgres and the access-token verifier (critical, `503` when down), Solana RPC, the relayer's SOL balance, poller liveness, Privy and the price API, and reports `ok`, `degraded` or `down`.
- Metrics are at `GET /metrics` (Prometheus; bearer `METRICS_TOKEN`, or loopback only when unset). Set `SENTRY_DSN` and `ALERT_WEBHOOK_URL` so panics and money alerts reach a person. What is recorded and what to alert on: [`docs/ops-observability.md`](docs/ops-observability.md).
- Routes, rate limits, idempotency keys, body and timeout limits: [`docs/api.md`](docs/api.md).
- The deposit sweep, execute-on-pass and redeem recovery pollers run inside the API process. A panic in a tick is recovered, alerted and counted; the loop keeps running. The deposit sweep poller is safe to run in several instances: it leases each deposit (`FOR UPDATE SKIP LOCKED`) and records the sweep signature before broadcasting, so a crash or a second instance never sweeps a deposit twice. The other two pollers have not been tested with more than one instance.

**iOS.** Archive and upload steps are in [`apps/mobile/TestFlight.md`](apps/mobile/TestFlight.md).

**Trading agent.** `agents/momentum-bot` runs anywhere Go runs; see [`docs/how-to/connect-an-agent.md`](docs/how-to/connect-an-agent.md).

## Layout

```
monaco/
├── Justfile
├── docker-compose.yml
├── .env.example
├── AGENTS.md
├── README.md                 this file
├── apps/backend/             Go API
├── agents/momentum-bot/      reference trading agent
├── apps/mobile/              SwiftUI
├── packages/mobile-core/     host Swift tests
├── docs/product.md           product + architecture
├── docs/index.md             milestone backlog
├── docs/submission/          hackathon submission notes
└── scripts/
```

More: TestFlight notes in [`apps/mobile/TestFlight.md`](apps/mobile/TestFlight.md). Sweep ops in [`docs/ops-sweep-wallets.md`](docs/ops-sweep-wallets.md).

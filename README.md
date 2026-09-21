# Monaco

iOS app: friends pool **USDC on Base** and buy tokenized US stocks (B20). Auth is Dynamic. Relayer fees are **ETH on Base**. Product rules: [`docs/product.md`](docs/product.md). Milestone backlog: [`docs/index.md`](docs/index.md).

Monaco lets you create a hedge fund with friends by pooling money to buy stocks together. Members propose and vote on trades, and approved trades execute for the group; as the pool profits, each member’s stake increases in value through NAV. You can even add an agent to your cabal to trade on your behalf. Built as a social trading app, Monaco turns investing into an easy group game anyone can join simply by depositing money.

**Chain.** Base mainnet (chain id `8453`). Deposits and pots are USDC `0x833589fcd6edb6e08f4c7c32d4f71b54bda02913`. The fee payer spends ETH on Base, not ETH on Ethereum mainnet. Swaps are Kyber on Base. Dynamic holds the member inbox and group treasury. Do not send tokens on any other chain.

## Prereqs

macOS, Xcode (iOS 18+ simulator), Docker, Go 1.23+, [just](https://github.com/casey/just), [dotenvx CLI](https://dotenvx.com/docs/install). SimSlim is optional.

## Clone setup

1. Clone this repo. `cd` into the clone. Do not hard-code another machine's home path.
2. Place gitignored `.env.keys` in the repo root if a teammate encrypted `.env.local` for you. Also place that `.env.local`. dotenvx reads both from the clone root.
3. If you have no `.env.local` yet, copy `.env.example` to `.env.local` and set Dynamic plus relayer values with `dotenvx set KEY value -f .env.local`.
4. Run `./scripts/install-dev.sh` (or `just install`). It asks before each install (Go, just, dotenvx, optional SimSlim). `just install --check` only reports.
5. `just run` starts Postgres, the API, and the iOS app. Dynamic is injected via `scripts/ensure-ios-dynamic-config.sh` and `SIMCTL_CHILD_*`. If SimSlim is missing, the scripts warn and boot a stock simulator.

Do not wrap `just` with `dotenvx run` yourself. Recipes that need secrets re-exec under `scripts/with-dotenv-local.sh`.

Local DB is Docker Compose Postgres only (`monaco`, host port `54322`). Never point `just run` / `just test backend` at hosted or production Supabase.

## Sign in

Dashboard Login Methods must have **Email** and **SMS** on. Product path is OTP, not a password. iOS bundle `com.monaco.app` must be on the Dynamic iOS client or `sendCode` returns 403 `invalid_native_app_id`. Sign out in-app to switch users.

Use a real phone or email. Dynamic sends a new OTP each time; there are no fixed test codes.

## Deposits

You can fund a group from **personal external wallet** (iOS app or browser extension). That is your wallet, not the [agent MCP wallet](#agent-qa-phantom-mcp). No Cursor or coding agent required.

1. `just run`. Sign in with SMS or email OTP.
2. Join or create a group → **Add money**. Copy the **Dynamic member** deposit address (deposit inbox). Not the group treasury.
3. In an external wallet, switch the network to **Base**. Send **USDC on Base** (`0x833589fcd6edb6e08f4c7c32d4f71b54bda02913`). USDC on Ethereum mainnet is a different token; the poller will not see it.
4. The backend poller detects USDC in the member wallet, **sweeps** it into the group treasury, then credits share units. Watch API logs or the group view. Do not treat USDC sitting only in the member wallet as credited pot — wait for sweep confirm.
5. Member wallet and vault do not need ETH (the relayer pays Base gas). The **sender** still needs a little ETH on Base if they are paying their own gas.

To pull leftover QA cash back out, use in-app **redeem** (external wallet cannot spend Dynamic wallets). Agent-driven deposit and refund: [Agent QA: agent wallet](#agent-qa-phantom-mcp).

## Commands

| Command                      | What it does                                                                                                                                                           |
| ---------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `just install`               | Ask before installing missing tools. `just install --check` reports only                                                                                               |
| `just encrypt`               | `dotenvx encrypt` on `.env.local` (and `.env.production` if present)                                                                                                   |
| `just decrypt`               | `dotenvx decrypt` on `.env.local` (and `.env.production` if present)                                                                                                   |
| `just show-env`              | Print decrypted `.env.local` keys/values via dotenvx (`export KEY='value'` lines; `.env.production` omitted). Needs `.env.local`, dotenvx, and `.env.keys` or Keychain |
| `just run`                   | Full stack: Postgres + API + iOS app (dotenvx re-exec, Dynamic on sim)                                                                                                   |
| `just run backend`           | API only (dotenvx)                                                                                                                                                     |
| `just run mobile`            | iOS with Dynamic xcconfig + `SIMCTL_CHILD_*` via `./scripts/ios-sim`                                                                                                     |
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
| `just build mobile`          | Dynamic xcconfig, then `xcodebuild` on the resolved sim                                                                                                                  |
| `just relayer balance`       | Fee payer address + Base ETH and USDC (dotenvx; no private key)                                                                                                      |
| `./scripts/ios-sim`          | Monaco run with Dynamic env. Falls back to a stock sim if slim is missing                                                                                                |
| `./scripts/ios-build`        | Monaco compile with Dynamic xcconfig                                                                                                                                     |
| `./scripts/sweep-wallets.sh` | **Ops.** Sweep USDC out of Dynamic wallets. See **[Ops: sweep USDC](#ops-sweep-usdc-out-of-dynamic-wallets)**                                                              |

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

Justfile `dotenv-load` only reads plain `.env` — not dotenvx ciphertext. Recipes that need secrets re-exec once under `dotenvx run -f .env.local` (via `scripts/with-dotenv-local.sh`). Mobile Dynamic uses `scripts/ensure-ios-dynamic-config.sh` (xcconfig) + `SIMCTL_CHILD_*` at sim launch.

Private keys: `DOTENV_PRIVATE_KEY` for `.env` / `.env.local`; `DOTENV_PRIVATE_KEY_PRODUCTION` for `.env.production`. On macOS, new keys often land in Keychain, not `.env.keys`. Export with `dotenvx native pull` or `dotenvx keypair -f .env.local`.

Encrypted `.env*` files (public key in repo) may be committed. Never commit `.env.keys`, `.env.local`, or private keys. `.gitignore` covers `.env`; keep `.env.keys` and `.env.local` out of git locally.

A pre-commit hook checks **staged** `.env*` files only (not `.worktrees` or the rest of the tree) and blocks plaintext secrets / `.env.keys`. `.env.example` is allowed. Reinstall after clone: `ln -sfn ../../scripts/githooks/pre-commit .git/hooks/pre-commit`. Do not run `dotenvx precommit --install` — that full-tree scan is slow.

## Relayer (fee payer)

The app **fee payer** is a dedicated Base EOA from `RELAYER_PRIVATE_KEY` (`0x` + 64 hex in `.env.local`). Not a Dynamic wallet. Clones that decrypt the same shared env share the same fee payer. Never commit or log the private key.

At API startup the backend derives the address and refuses to boot unless that address holds **at least 0.002 ETH** on Base.

| Item            | Value                                                                                                                    |
| --------------- | ------------------------------------------------------------------------------------------------------------------------ |
| Env (secret)    | `RELAYER_PRIVATE_KEY` — `0x` + 64 hex (not a JSON `[1,2,...]` array)                                                     |
| Address         | Derived at startup from the secret; logged as `address=` on boot (no private key)                                         |
| Role            | Kyber swap fee payer; relayer on deposit sweeps                                                                          |
| ETH requirement | Balance **≥ 0.002 ETH**. Fund on [Base](https://basescan.org) before `just run backend`.                                 |

```bash
just relayer balance
```

Example:

```text
address  0x…
eth_wei  3000000000000000
```

## Demo data (faker seed)

Seeds fake-but-realistic data so Home, Groups, proposals, and activity look alive without a
full Dynamic setup. Local Postgres only. It never calls Dynamic, Base RPC, or Kyber.

Two profiles:

- **scale**: six fake clubs (Ridgewood Value Club, Night Shift Traders, Harbor Street Fund, plus the
  smaller Dorm 4B fund, Rent money and Index huggers).
  Each has a fake creator, five depositors, deposits spread over the last week, a confirmed
  AAPLc/TSLAc buy, a governed sell (Ridgewood and Night Shift), failed and open proposals, votes,
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

## Simulator

Slim is **not** required. `just run`, `just run mobile`, and `./scripts/ios-sim` warn and use a stock Xcode simulator when SimSlim is missing or `SIMSLIM_UDID` is unset. Dynamic xcconfig and `SIMCTL_CHILD_*` still apply.

`just test mobile` never boots a sim (host `swift test` in `packages/mobile-core`).

Fail only if no iOS Simulator exists: Xcode → Settings → Platforms, download an iOS 18+ runtime, create an iPhone sim.

Xcode Cmd+R also works after `./scripts/ensure-ios-dynamic-config.sh generate`. Without that file the app shows “Dynamic not configured”.

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

Repo `./scripts/ios-sim` and `./scripts/ios-build` call `xcodebuild` and `simctl` after Dynamic injection. Optional PATH wrappers in `~/.local/bin` are **not** in git and **not** required.

Keep gold **booted** between agent sessions when you can. Clone gold after slim-once if you need a second sim.

## Agent QA: agent wallet

Use this when a coding agent (or you, in Cursor chat) must move **real Base** USDC into a sim user’s member wallet, then pull leftover cash out of the group vault when the run is done.

Keep the agent wallet thin. Preview software. Do not park rent money here.

Three wallets people mix up:

1. **Personal external wallet** (iOS / Android / browser extension). Your money. Create it yourself (below).
2. **Agent external wallet** (MCP). New dedicated wallet the first time the agent signs in. Empty until you fund it. QA faucet and refund target.
3. **Dynamic product wallets.** Member inbox + group vault. agent wallet **cannot** spend these. The agent can only **send USDC to** the copyable member address, then **receive USDC back** when you redeem to the agent address.

Do not put `PHANTOM_APP_ID` in Monaco `.env.local`. If a Cursor plugin still wants it, put it in Cursor MCP env only. Current `@phantom/mcp-server` device-code login does not require a Portal app id.

Never insert `FAKE*` wallet rows in local Postgres. The poller will break.

### Create a external wallet wallet

Personal wallet first — that is how you buy ETH/USDC on Base and top up the agent address.

1. Download only from [phantom.com/download](https://phantom.com/download) (iOS, Android, Chrome, Brave, Firefox, Edge). App Store: [external wallet](https://apps.apple.com/us/app/phantom-trade-markets/id1598432977). Play: [external wallet](https://play.google.com/store/apps/details?id=app.phantom).
2. Follow [How to create a new external wallet wallet](https://phantom.com/learn/guides/how-to-create-a-new-wallet): Create a New Wallet → Google or Apple, or a secret recovery phrase.
3. Write down the recovery phrase / PIN. Never paste it into git, tickets, or chat.
4. Overview: [Get started](https://phantom.com/get-started). Help: [help.phantom.com](https://help.phantom.com).

### Install the agent wallet (agent wallet)

This is the **wallet MCP** (`@phantom/mcp-server`): sign, transfer, swap. It is not the docs-only MCP at `https://docs.phantom.com/mcp`.

Docs: [agent wallet server](https://docs.phantom.com/phantom-mcp-server) · [Setup](https://docs.phantom.com/phantom-mcp-server/setup) · npm `[@phantom/mcp-server](https://www.npmjs.com/package/@phantom/mcp-server)` · [Cursor MCP](https://cursor.com/docs/context/mcp)

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

On auth, external wallet mints a **new agent wallet**. It is not your extension wallet. Ask the agent for Base addresses (`wallet_addresses` / `get_wallet_addresses`). Copy the Base address. That string is the refund target for leftover QA USDC. Each developer has their own; do not hardcode someone else’s address in the repo.

### Fund the agent wallet (~$1 ETH + ~$4 USDC on Base)

The agent cannot transact on an empty wallet.

| Asset                     | Why                                                                                                 | Ballpark            |
| ------------------------- | --------------------------------------------------------------------------------------------------- | ------------------- |
| ETH on **Base** | Fees when the agent sends USDC to a member inbox | about **$1** of ETH |
| USDC on **Base**        | What the app actually credits after sweep                                                           | about **$4**        |

Buy or swap inside personal external wallet, then send **ETH** and **Base USDC** to the **agent** Base address. Confirm token `0x833589fcd6edb6e08f4c7c32d4f71b54bda02913`. Ask the agent for `wallet_balances` before the first transfer.

Product path does **not** need ETH on the member wallet or vault (relayer pays). The **agent** still needs ETH because the agent is the sender.

### Send USDC into Monaco (member inbox → vault)

Same deposit path from **personal external wallet** works without MCP; see [Deposits](#deposits).

1. `just run` (API + sim). Sign in (SMS or email OTP).
2. Join or create a group → Add money. Copy the **Dynamic member** address (deposit inbox). Not the group treasury.
3. In Cursor: transfer **small** USDC on `base:mainnet` to that address, mint above. MCP `transfer` / `transfer_tokens` simulates first; approve only if dest matches the copied inbox.
4. Poller detects member USDC, **sweeps** to the group treasury, then credits shares. Watch API logs / group view. Do not treat member-wallet balance as credited pot.
5. Explorer: [basescan.org](https://basescan.org) on the sweep hash.

### Sweep leftover back to the agent wallet (vault → external wallet)

agent wallet cannot pull from Dynamic. Reverse of deposit is **in-app redeem** to the agent Base address.

1. Agent: print Base address again. Confirm it is **your** MCP wallet.
2. Group screen → redeem leftover equity (slider at max if you want the pot empty). Payout address = that agent Base address.
3. Wait for payout confirm. Agent: `wallet_balances` — USDC should be back. Treasury USDC for that test should be ~0 (dust from swaps possible).
4. If USDC is still sitting **only** in the member inbox (sweep not confirmed): do not “withdraw with external wallet.” Wait for sweep, then redeem. Or stop funding that inbox.
5. If the pot holds B20, redeem sells that slice to USDC first, then pays USDC. Tiny leftover stock/USDC dust can remain; keep QA notionals small.

After a funding run, leftover **agent-test USDC belongs on the agent external wallet**, not in a group vault and not in a sim user’s inbox.

## Sweep USDC out of Dynamic wallets

Product path is poller member-inbox → treasury, then **in-app redeem**. Use this script only when USDC is stuck in Dynamic (inbox or treasury) and you must send it to a known Base address (usually the agent external wallet).

**Danger.** Mainnet USDC. Wrong `DATABASE_URL` or `--all` against the prod Dynamic app can empty live pots and break share credits. Relayer still pays ETH fees.

```bash
# One wallet (or list). Always dry-run first.
./scripts/sweep-wallets.sh --destination <base_address> --source <wallet> --dry-run
./scripts/sweep-wallets.sh --destination <base_address> --source <wallet_a> --source <wallet_b> --dry-run

# --all = every Base wallet Dynamic returns for this app (not just local DB rows).
./scripts/sweep-wallets.sh --destination <base_address> --all --dry-run

# Live: same flags without --dry-run. Type exactly:
#   I UNDERSTAND THIS MAY MESS WITH PROD
# then paste the destination address again.
./scripts/sweep-wallets.sh --destination <base_address> --source <wallet>
./scripts/sweep-wallets.sh --destination <base_address> --all
```

| Flag               | Meaning                                                                                  |
| ------------------ | ---------------------------------------------------------------------------------------- |
| `--destination`    | Required. Receives all swept USDC.                                                       |
| `--source`         | Drain only listed wallet(s). Repeatable; comma-separate in one value. Do not mix with `--all`. |
| `--all`            | Source of truth = Dynamic `GET /v1/wallets?chain_type=base` (paginated). Skips Postgres. |
| *(omit both)*      | Source = local `member_wallets` + `treasuries` for the `DATABASE_URL` in `.env.local`.   |
| `--dry-run`        | Print balances and `would sweep` lines. No txs. No confirm prompt.                       |

Needs `.env.local` (`DYNAMIC_*`, `RELAYER_PRIVATE_KEY`, `DATABASE_URL`). Wrapper is `scripts/with-dotenv-local.sh`. Amounts are micro-USDC (`1000000` = $1). Zero-balance wallets skip. Destination equal to a source skips.

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
| **testing-expert** | [`.cursor/skills/testing-expert/SKILL.md`](.cursor/skills/testing-expert/SKILL.md) | How to write tests: small surface, deterministic, realistic data. This copy is TS/Jest-oriented; Monaco still follows the same bar in Go and Swift. `just test mobile` is host `swift test`. `just test backend` uses stubs — never hit live Kyber. Skip property tests that run longer than ~2 minutes. |

Do not copy these skills into another machine's home path. Clone the repo; Cursor sees `.cursor/skills/` from the workspace.

## Tests and CI

| Suite | Command |
| --- | --- |
| Backend (needs Docker Postgres) | `just test backend` |
| Domain math, no database | `cd packages/domain && go test ./...` |
| Reference trading bot | `cd agents/momentum-bot && go test ./...` |
| Shared Swift logic | `just test mobile` |

Backend tests never touch the app database: they derive `{dbname}_test` from `DATABASE_URL`, create it if missing, and migrate it (`apps/backend/internal/postgres/testdb.go`).

`.github/workflows/ci.yml` runs on pull requests and pushes to `main`: a Go job (Postgres 16 service container, migrations on a clean database, `go vet`, `go test` for `apps/backend`, `packages/domain` and `agents/momentum-bot`) and a macOS job (`swift test` in `packages/mobile-core`). The iOS app target is not built in CI.

## Deploy

There is no deploy pipeline in this repo yet; the demo runs the API on a laptop. What a host needs:

**API.** One Go binary.

```bash
just build backend          # bin/monaco-api
API_ADDR=0.0.0.0:8080 MIGRATIONS_DIR=/path/to/supabase/migrations ./bin/monaco-api
```

- Migrations in `supabase/migrations` are applied at boot, in filename order, before the server listens. `go run ./cmd/migrate` (from `apps/backend`) applies them without starting the API.
- Required env: `DATABASE_URL`, `DYNAMIC_ENVIRONMENT_ID`, `DYNAMIC_API_TOKEN`, `RELAYER_PRIVATE_KEY`, signer secrets. The full list with comments is in `.env.example`. Use separate Dynamic environments, relayer keys and databases per environment; production values go in `.env.production` (dotenvx-encrypted), never in the image.
- The relayer address must hold more than 0.001 ETH or the API exits at boot. See [Relayer](#relayer-fee-payer).
- The API listens on `API_ADDR` (default `127.0.0.1:8080`). `GET /health` returns `{"status":"ok"}` once it is up; it does not probe Postgres or upstream APIs.
- The deposit sweep poller and the execute-on-pass poller run inside the API process. Running more than one instance has not been tested.

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

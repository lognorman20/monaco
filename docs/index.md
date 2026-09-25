# How to build Monaco in milestones

Monaco is an iOS app where friends form a group, pool USDC, and buy tokenized US stocks on Solana. This folder is the build backlog for that app. It is a how-to, not an architecture essay. Product rules live in [`docs/product.md`](product.md). How to clone and run lives in the root [`README.md`](../README.md).

Each milestone file lists ticket titles plus **Ticket details** blocks (Owns, Depends on, acceptance, out of scope) so agents can pick work without reading the whole milestone. Do not start a later milestone until the earlier one passes both its automated commands and its manual checks.

Phase D architecture lives in [`docs/architect/sketch.md`](architect/sketch.md). That file is the grafted design: Luna `internal/app` (session, deposit, sweep, swap, redeem, views), Composer `NavMode` and signature idempotency on Logan's table names, Grok `StartBuy` (who may start a buy) and `RedeemJob` states. [`docs/architect/synthesis.md`](architect/synthesis.md) records base, grafts, and rejections. Milestone files below add Structure, Flow, and Data models sections that track the sketch without replacing ticket titles.

## How to read this

Each milestone is one shippable slice. It names the paths it owns, the tickets that land it, the `just` commands that prove the code, and the manual steps you run on device, Privy, local Postgres, and explorer.

M0 is scaffold. Your five product steps are M1 through M5.

1. Auth with SMS and email OTP, member wallets, and group treasury wallets via Privy.
2. User deposits and treasury sweeps.
3. Buy and sell tokenized stocks via Jupiter.
4. Domain logic for groups, votes, NAV, redeem, and boards.
5. Product UI on iOS.

### Ticket conventions

Every milestone file has two planning sections above the ticket list:

1. **Parallelization** — a wave table. Tickets in the same wave run in parallel across tracks (backend vs mobile, schema vs client adapter, etc.). Finish the wave before starting the next.
2. **Tickets** — each line may carry:
   - `Depends on: Mx-Ty` — sequential gate. Do not start until the dependency lands.
   - `**Parent** Mx-Tn` — umbrella ticket. Sub-work is listed as indented `└ **Subissue of Mx-Tn** Mx-Tna` lines. Parent tickets stay open until all subissues ship.

Milestones still gate each other: M0 → M1 → M2 → M3 → M4 → M5. Parallelization is within a milestone only.

### GitHub mapping

Track work in GitHub so agents can use worktrees and poteto-mode against real issues.

| Item | Convention |
|------|------------|
| **Milestone title** | `M0 — Scaffold`, `M1 — Auth and wallets`, … `M5 — Mobile UI` (em dash, plain names) |
| **Issue title** | `Mx-Ty: <ticket title>` — e.g. `M1-T4a: Verify Privy access token` |
| **Labels** | `milestone-m0` … `milestone-m5`; `backend`, `mobile`, `schema`, `tests` by path; `parent` / `subissue` for umbrellas; `needs-decision` or `blocked` for open product tickets (M4-T39–T41) |
| **Issue body** | Summary, Owns, Depends on, Parent/subissues, Acceptance (test map checkboxes), Out of scope, Manual, `Wave: N` |
| **Idempotency** | Skip create when an issue title already starts with the same `Mx-Ty:` prefix |

Parent issues stay open until subissues close. Link `Depends on` and `Parent` with `#issue` numbers after the first create pass.

### Automated verification

Each milestone file ends with **Automated verification** and **Manual verification**. Read them with the ticket list and wave table.

1. **Commands** — the `just test` recipe(s) that gate the milestone.
2. **Test layers** — unit vs integration vs Swift; which fakes each layer uses (fake Privy, fake Jupiter, local Postgres, etc.).
3. **Test map** — readable `TestXxx` / `testXxx` names keyed to ticket IDs. Parent tickets may split coverage across subissues (e.g. M1-T4a–c).
4. **Fixtures / fakes** — shared doubles and JSON fixtures named in that milestone.
5. **Property / invariant tests** — fairness and idempotency checks where math matters (M2 deposits, M4 `packages/domain`). Omitted when not applicable (M0, M1, M5).
6. **Manual verification** — human-only steps: Privy dashboard, mainnet USDC, slim sim UX, explorer links. Does not repeat row counts or JSON shapes already in the test map.

## Sequence

```
M0 scaffold
  then M1 auth and wallets
    then M2 deposits and sweeps
      then M3 Jupiter buy and sell
        then M4 domain logic
          then M5 mobile product UI
```

M1 may create a thin group so you can attach a treasury. M4 replaces that stub with join policy, voter set, threshold, and expiry. M3 may expose a temporary stub buy so you can hit Jupiter before votes exist. M4 deletes that stub when a passed vote is the only execute path.

M1 login screens may show Solana addresses so you can prove Privy wiring. M5 moves those addresses to Settings, Advanced and keeps the main flow free of wallet copy.

## Local database

Local dev and test use Docker Compose Postgres only. Project name `monaco`. Default host port `54322`. Never point `just run`, `just run backend`, or `just test backend` at hosted Supabase or any remote prod database.

- `docker-compose.yml` at the repo root runs Postgres on localhost.
- `just run` starts compose, waits on the healthcheck, applies SQL from `supabase/migrations/`, runs the Go API, and launches the iOS app (stock sim if SimSlim is missing).
- `just run backend` and `just test backend` start compose, wait on the healthcheck, apply migrations, then run only the API or tests.
- Copy `.env.example` to `.env.local`. `DATABASE_URL` must target `localhost`. Scripts reject hosted Supabase URLs. Place `.env.keys` in the clone root when `.env.local` is encrypted.
- Manual checks must prove the DB is local. Note the compose project name, container name `monaco-postgres`, and mapped host port.
- There is no separate `just db` recipe. Production Supabase stays out of this repo.

## Monorepo

Everything runs through `just`. Inferred layout for an empty repo that only has `README.md` today.

```
monaco/
├── Justfile
├── docker-compose.yml
├── .env.example
├── .gitignore
├── AGENTS.md
├── README.md
├── apps/
│   ├── backend/             Go API
│   └── mobile/              SwiftUI, iOS 18+
├── packages/
│   └── domain/              Go-only share math, vote tally, P&L
├── supabase/
│   └── migrations/          SQL applied to local Postgres
├── docs/
│   ├── index.md
│   ├── product.md           product + architecture
│   └── milestones/
└── scripts/
```

`packages/domain` is a Go module the API and its tests import. Do not share it into Swift. Mobile talks to `apps/backend` over HTTP. Swift never calls xStocks, Jupiter, Pyth Hermes, or Solana RPC for product flows. Privy Swift handles auth and member wallets only.

## Contracts

No shared compiled package spans Go and Swift this week. HTTP is the contract.

- Hand-write Go response structs and Swift `Codable` models to match.
- `just test backend` and mobile client tests catch JSON drift.
- A future OpenAPI file may live under `packages/` as a source document both sides copy from. It is not a library either side compiles.
- Do not add a shared Swift package or Go-to-Swift codegen in M0 through M5.

## Just recipes

App names are `backend` and `mobile`. Wire them in M0. Each later milestone extends what `just test backend` or `just test mobile` runs internally. Do not add other top-level recipes such as `just db`, `just check`, `just bootstrap`, `just test-deposits`, `smoke-*`, or `ios-build`. `just install` is the clone setup script; `just encrypt`, `just decrypt`, and `just show-env` wrap dotenvx on `.env.local`; `just relayer balance` prints fee payer pubkey and mainnet SOL (no private key). Do not add `just build` or `just test` umbrellas.

- `just run` starts local Postgres, the Go API, and the iOS app together. Use this for the full local stack.
- `just build backend` builds the Go API binary.
- `just test backend` runs env check, `docker compose up --wait`, migrations, and API tests against local Postgres.
- `just run backend` starts local Postgres via compose, applies migrations, and runs only the API with `.env.local`.
- `just build mobile` builds the SwiftUI app for the resolved simulator (`scripts/resolve-ios-sim.sh`).
- `just test mobile` runs host `swift test` in `packages/mobile-core` (no simulator).
- `just run mobile` builds and launches only the app on that sim, with Privy env injected. Start the API separately or use `just run`.
- `just encrypt` / `just decrypt` run `dotenvx encrypt` / `dotenvx decrypt` on `.env.local` (and `.env.production` when present).
- `just show-env` prints decrypted keys/values from `.env.local` only (`.env.production` omitted).

Never `simctl erase` for QA. Agent-driven QA must export `SIMSLIM_UDID` and pass `--simulator-id` from `scripts/gold-sim-udid.sh`. Human `just run` uses `scripts/resolve-ios-sim.sh` and may use a stock sim.

## Open decisions

[`docs/product.md`](product.md) leaves these unlocked. Do not pick them in code. M4 records a ticket for each.

- Who may propose a buy. Any member, voter set only, or creator only.
- Failed Jupiter `/execute` after a passed vote.
- Creator leave and group dissolve.

## Product and ops

- [Product and architecture](product.md)
- [Sweep USDC out of Privy wallets](ops-sweep-wallets.md) (`./scripts/sweep-wallets.sh`)
- [Observability: logs, metrics, alerts, health](ops-observability.md)
- [HTTP API reference](api.md)
- [Agent trading: intents, budgets, safety model](agent-trading.md)
- [Profile photo storage](ops-profile-photos.md)
- [Multi-user verification: what the authz and flow tests cover](multi-user-verification.md)
- [Hackathon submission notes](submission/README.md)

## How-to

- [Connect a trading agent](how-to/connect-an-agent.md)
- [Run on the local simulator](how-to/local-simulator.md)
- [Debug login](how-to/debug-login.md)
- TestFlight: [`apps/mobile/TestFlight.md`](../apps/mobile/TestFlight.md)

## Historical records

`milestones/`, `architect/`, `superpowers/`, `qa/` and the `m3`–`m5` handoff files are kept for context and not maintained.

## Milestone files

- [M0. Scaffold the monorepo](milestones/m0-scaffold.md)
- [M1. Auth and Privy wallets](milestones/m1-auth-wallets.md)
- [M2. Deposits and sweeps](milestones/m2-deposits.md)
- [M3. Jupiter buy and sell](milestones/m3-jupiter.md)
- [M4. Domain logic](milestones/m4-domain.md)
- [M5. Mobile product UI](milestones/m5-mobile.md)
- [Tessera. Pre-IPO tokens](milestones/tessera-pre-ipo.md)
- [PreStocks. Pre-IPO tokens](milestones/prestocks-pre-ipo.md)

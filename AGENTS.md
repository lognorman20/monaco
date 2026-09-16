## Learned User Preferences

- Keep `just` recipes minimal: `just build`, `just test`, and `just run` per app (`backend` or `mobile`), plus root `just run` to start backend and mobile together.
- When the user says "test mobile" or runs `just test mobile`, run host `swift test` in `packages/mobile-core` (fast, deterministic; no iOS Simulator or `xcodebuild test`). `just build mobile` is the iOS compile gate (`xcodebuild build` on gold sim UDID `7B30D45E-62FD-42E2-871A-787B19D38CCF`). Do not boot a sim for unit tests.
- Gold slim sim QA: reuse UDID `7B30D45E-62FD-42E2-871A-787B19D38CCF`; never `simctl erase` or destination by name. Sim smoke / agent tap-through: `.cursor/skills/ios-simslim-fast-qa/SKILL.md`. XcodeBuildMCP: `--simulator-id 7B30D45E-62FD-42E2-871A-787B19D38CCF`. Details in README **Gold slim simulator**.
- App directories are `apps/backend` and `apps/mobile` by intentional rename; do not refer to or recreate `apps/api` or `apps/ios`. The iOS Xcode project/scheme may stay named `Monaco` under `apps/mobile`.
- For M1 testing, support email OTP login in addition to SMS auth.
- Backend is Go over Rust for faster compile times and iteration speed.
- Use dotenvx for repo secrets (`.env.local` for dev, `.env.production` for prod); wrap `just` with `dotenvx run -f .env.local --` because Justfile `dotenv-load` only reads plain `.env`.
- GitHub issue and milestone ticket bodies should follow the write-ticket format (Context, Problem, Proposal with Implement exactly, acceptance criteria, verification commands, Done when) with minimal agent discretion.
- Parallel milestone implementation should use worktree-orchestrate with Composer 2.5 subagents unless another model is explicitly requested.

## Learned Workspace Facts

- Monaco is a mobile app for group treasury investing in tokenized stocks (xStock via Jupiter swaps).
- Apps live at `apps/backend` (Go API) and `apps/mobile` (Swift/iOS). Host-runnable Swift unit tests live in `packages/mobile-core`.
- Go and Swift integrate via HTTP API contract only; no shared compiled API package in `packages/`.
- Local development database is Docker Compose Postgres; do not use hosted or production Supabase for local testing.
- Privy handles auth and per-user wallets; deposits flow user wallet → Privy wallet → group treasury.
- Leaderboard and P&L (within a group and across groups) are core product focus, with portfolio views and partial USDC redeem.
- Mobile never calls xStocks, Jupiter, Pyth Hermes, or Solana RPC for product flows. Swift uses HTTP to the Go API only (plus Privy Swift for auth/wallets).
- Secrets use dotenvx; pre-commit runs `dotenvx precommit` to block plaintext `.env*` from being committed.
- `PHANTOM_APP_ID` belongs in Cursor Phantom MCP config, not Monaco `.env` — Phantom MCP is an agent test harness, separate from Privy product wallets.
- Product uses Solana mainnet (not devnet) for RPC, USDC, and chain operations.
- M0 only needs `DATABASE_URL` (compose defaults suffice); Privy, relayer, and RPC env vars matter from M1 onward.
- M2+ money movement uses Deposit, Withdrawal, Transaction, and Position domain tables—not `sweep_tx_log` or `share_ledger`.

## Privy (M1)

- iOS sim launch: `./scripts/ios-sim` or `just run mobile` from repo root. Decrypts `.env.local` via dotenvx and injects `SIMCTL_CHILD_PRIVY_*` at `simctl launch`. Mobile needs `PRIVY_APP_ID` + `PRIVY_APP_CLIENT_ID` (or legacy `PRIVY_AUTH_ID`); backend uses `PRIVY_APP_SECRET`.
- App: Monaco. App id `cmu26uw5s00mp0cl81v6dud1n` (public).
- Login methods (Authentication → Login methods, verified 2026-09-15): Email **ON** (OTP to inbox; Privy has no separate email+password toggle), SMS **ON** (US/Canada). OAuth/wallets/passkeys off.
- Test accounts (Dashboard → Test accounts **ON**). Login via email + OTP or phone + OTP:
  - Alfred — `test-8081@privy.io` — `+1 555 555 7177` — OTP `465354`
  - Bartholomez — `test-4952@privy.io` — `+1 555 555 9638` — OTP `648588`
  - Cayman — `test-3510@privy.io` — `+1 555 555 8215` — OTP `115543`

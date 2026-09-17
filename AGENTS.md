## Learned User Preferences

- Keep `just` recipes minimal: `just build`, `just test`, `just run`, `just stop`, and `just reset` per app (`backend` or `mobile`), plus root variants. `just reset` = stop all + wipe local Postgres; `just reset db` = DB only. `just killports` kills API listeners (8080), not Compose Postgres (54322).
- When the user says "test mobile" or runs `just test mobile`, run host `swift test` in `packages/mobile-core` (fast, deterministic; no iOS Simulator or `xcodebuild test`). `just build mobile` is the iOS compile gate (`xcodebuild build` on gold sim UDID `7B30D45E-62FD-42E2-871A-787B19D38CCF`). Do not boot a sim for unit tests. For `just test backend`, use stubs/mocks only—never hit the live Jupiter API; trim property tests that run longer than ~2 minutes.
- Gold slim sim QA: reuse UDID `7B30D45E-62FD-42E2-871A-787B19D38CCF`; never `simctl erase` or destination by name. Sim smoke / agent tap-through: `.cursor/skills/ios-simslim-fast-qa/SKILL.md` or XcodeBuildMCP with `--simulator-id 7B30D45E-62FD-42E2-871A-787B19D38CCF`; do not drive taps with AppleScript, CGEvent, or coordinate hacks.
- App directories are `apps/backend` and `apps/mobile` by intentional rename; do not refer to or recreate `apps/api` or `apps/ios`. The iOS Xcode project/scheme may stay named `Monaco` under `apps/mobile`.
- For M1 testing, support email OTP login in addition to SMS auth.
- Backend is Go over Rust for faster compile times and iteration speed.
- Use dotenvx for repo secrets (`.env.local` for dev, `.env.production` for prod). Backend `just run` / `just test backend` re-exec under `scripts/with-dotenv-local.sh`. Mobile Privy: `ensure-ios-privy-config.sh` → xcconfig; `with-ios-privy-env.sh` / `./scripts/ios-sim` → `SIMCTL_CHILD_*`. Justfile `dotenv-load` only reads plain `.env`.
- GitHub issue and milestone ticket bodies should follow the write-ticket format (Context, Problem, Proposal with Implement exactly, acceptance criteria, verification commands, Done when) with minimal agent discretion.
- Parallel milestone implementation should use worktree-orchestrate with Composer 2.5 subagents unless another model is explicitly requested; each implementer gets its own worktree; parent orchestrator periodically checks subagents for progress, blockers, and stalls.
- When multiple agents overlap on the same task, run one agent only and keep whichever is furthest along; when orchestrating parallel lanes, split mobile vs backend tickets to separate agents; for milestone orchestration the parent chat owns the queue and must not spawn nested orchestrator subagents.
- For Privy dashboard verification or client settings during agent QA, use the attached Privy browser tab; do not ask the user to make dashboard changes. Prefer Privy API/dashboard ops over ad-hoc product PATCH-in-loop for legacy wallet signer fixes.
- Never skip verification steps (ticket Done-when, milestone manual verification, `just test`/`just build`, gold-sim QA) unless the user explicitly says to skip in the current message.

## Learned Workspace Facts

- Monaco is a mobile app for group treasury investing in tokenized stocks (xStock via Jupiter swaps).
- Apps live at `apps/backend` (Go API) and `apps/mobile` (Swift/iOS). Host-runnable Swift unit tests live in `packages/mobile-core`.
- Go and Swift integrate via HTTP API contract only; mobile never calls xStocks, Jupiter, Pyth Hermes, or Solana RPC for product flows (plus Privy Swift for auth/wallets).
- Local development database is Docker Compose Postgres; do not use hosted or production Supabase for local testing. App `DATABASE_URL` is `monaco`; `just test backend` / Go tests auto-use sibling `monaco_test` (derived, no extra env).
- Privy handles auth and per-user wallets; deposits flow user wallet → Privy wallet → group treasury. No `FAKE*` wallet address rows in local DB—they break the deposit poller.
- Leaderboard and P&L (within a group and across groups) are core product focus, with portfolio views and partial USDC redeem.
- `PHANTOM_APP_ID` belongs in Cursor Phantom MCP config, not Monaco `.env` — Phantom MCP is an agent test harness with a separate wallet from Privy product wallets. Refund leftover agent-test USDC to Phantom when QA funding runs finish.
- Milestone product work merges to integration branches (`milestone-1`, `milestone-2`, …); start each milestone branch from the previous milestone branch, not `main`.
- Product uses Solana mainnet (not devnet) for RPC, USDC, and chain operations.
- M0 only needs `DATABASE_URL`; M1+ needs Privy app id/secret/client id; M2+ also needs `PRIVY_AUTHORIZATION_PRIVATE_KEY`, `PRIVY_AUTHORIZATION_KEY_ID` (Privy dashboard quorum id, not derivable from the private key), `RELAYER_PRIVATE_KEY`, and RPC.
- M2 Privy sweeps use one app authorization keypair in env; member wallet create must set `owner` plus `additional_signers` with that quorum id at create time; treasuries are app-owned. iOS bundle `com.monaco.app` must be on the Privy iOS client or OTP `sendCode` returns 403 `invalid_native_app_id`. Pre-fix wallets without signers need one-time recreate or owner-signed update — not OTP PATCH as the product path.
- M2+ money movement uses Deposit, Withdrawal, Transaction, and Position domain tables—not `sweep_tx_log` or `share_ledger`. M3 adds Jupiter Swap API v2 on mainnet (backend-only; treasury signs with app-owned Privy wallet + `PRIVY_AUTHORIZATION_*`). `transactions` rows track swaps, idempotent on `tx_signature` / `execute_request_id`; xStocks mints resolve via `api.xstocks.fi` public assets API.

## Learned User Preferences

- Keep `just` recipes minimal: `just build`, `just test`, and `just run` per app (`backend` or `mobile`), plus root `just run` to start backend and mobile together.
- When the user says "test mobile" or runs `just test mobile`, run host `swift test` in `packages/mobile-core` (fast, deterministic; no iOS Simulator or `xcodebuild test`). `just build mobile` is the iOS compile gate (`xcodebuild build` on gold sim UDID `7B30D45E-62FD-42E2-871A-787B19D38CCF`). Do not boot a sim for unit tests.
- App directories are `apps/backend` and `apps/mobile` by intentional rename; do not refer to or recreate `apps/api` or `apps/ios`. The iOS Xcode project/scheme may stay named `Monaco` under `apps/mobile`.
- For M1 testing, support email-and-password login in addition to SMS auth.
- Backend is Go over Rust for faster compile times and iteration speed.
- Use dotenvx for repo secrets (`.env.local` for dev, `.env.production` for prod); wrap `just` with `dotenvx run -f .env.local --` because Justfile `dotenv-load` only reads plain `.env`.

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

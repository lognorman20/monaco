## Learned User Preferences

- Keep `just` recipes minimal: `just build`, `just test`, and `just run` per app (`backend` or `mobile`), plus root `just run` to start backend and mobile together.
- App directories are `apps/backend` and `apps/mobile` by intentional rename; do not refer to or recreate `apps/api` or `apps/ios`.
- For M1 testing, support email-and-password login in addition to SMS auth.
- Backend is Go over Rust for faster compile times and iteration speed.

## Learned Workspace Facts

- Monaco is a mobile app for group treasury investing in tokenized stocks (xStock via Jupiter swaps).
- Apps live at `apps/backend` (Go API) and `apps/mobile` (Swift/iOS).
- Go and Swift integrate via HTTP API contract only; no shared compiled API package in `packages/`.
- Local development database is Docker Compose Postgres; do not use hosted or production Supabase for local testing.
- Privy handles auth and per-user wallets; deposits flow user wallet → Privy wallet → group treasury.
- Leaderboard and P&L (within a group and across groups) are core product focus, with portfolio views and partial USDC redeem.
- Mobile never calls xStocks, Jupiter, Pyth Hermes, or Solana RPC for product flows. Swift uses HTTP to the Go API only (plus Privy Swift for auth/wallets).

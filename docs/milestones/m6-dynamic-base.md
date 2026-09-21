# M6. Dynamic auth and wallets on Base

**Goal.** Replace Privy with Dynamic for OTP auth and server wallets, replace Solana with Base (chain id `8453`), and replace Jupiter/xStocks/Pyth pot marks with a Base DEX aggregator, the Coinbase B20 tokenized-stock registry, and Chainlink total-return feeds. Product behaviour is unchanged: OTP login with no Monaco API traffic until `POST /v1/auth/session`, one copiable deposit address per user, fund-this-cabal as an exact backend sweep, propose → vote → backend executes, backend signs every transfer and swap, users never see keys or gas.

**Branch.** `feat/dynamic-base`, cut from `main`. `main` stays Privy + Solana. This branch is a parallel stack, not a data migration: local databases are wiped (`just reset db`); there is no Solana → Base balance move.

**Depends on.** M5 complete on `main`.

**Owns.** Everything under `apps/backend/internal/{auth,wallets,signer,evm,dex,b20,marks,chainlink}`, `apps/signer/`, migration `000013`, mobile auth and chain-facing DTOs, env, scripts, docs.

## Decisions (locked — do not reopen in code)

| Topic | Decision |
|-------|----------|
| Auth | Dynamic Swift SDK headless OTP (`sdk.auth.email` / `sdk.auth.sms`), plus device-registration and step-up screens (required for headless on API `2026_04_01`). Go verifies the Dynamic JWT: RS256 via JWKS `https://app.dynamicauth.com/api/v0/sdk/{DYNAMIC_ENVIRONMENT_ID}/.well-known/jwks`, `iss == app.dynamicauth.com/{DYNAMIC_ENVIRONMENT_ID}`, `exp`/`iat` valid, `scope` contains `user:basic`, `sub` = Dynamic user id. JWKS cached, refreshed on unknown `kid`. |
| Client wallets | **Off.** Dynamic client-side embedded wallets are disabled in the dashboard. The phone never holds or signs with a product key. |
| Server wallets | Dynamic Server Wallets (TSS-MPC, `TWO_OF_TWO`, `backUpToDynamic: true` with `DYNAMIC_WALLET_PASSWORD`). One member wallet per user (deposit inbox / platform balance) and one treasury per cabal, both created and signed by the backend. Go persists `wallet_metadata` (jsonb) and `key_shares_enc` (AES-256-GCM under `WALLET_SHARES_KEY`). |
| Signing runtime | The Dynamic MPC SDK is Node-only. A sidecar `apps/signer` (TypeScript, Node ≥ 20) owns every private-key operation: Dynamic wallet create / sign / send and the relayer EOA. Go owns policy, calldata, receipts, balances, idempotency. Sidecar binds `127.0.0.1:8081`, requires header `x-signer-secret: $SIGNER_SHARED_SECRET`. |
| Chain | Base mainnet. RPC `BASE_RPC_URL` (default `https://mainnet.base.org`). USDC `0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913` (6 decimals). Addresses stored **lowercase** `0x` + 40 hex; compared lowercase; displayed as returned by the API. |
| Gas — member wallets | Member wallets never hold ETH. USDC leaves a member wallet only via EIP-3009 `transferWithAuthorization`: the member wallet signs EIP-712 typed data (`name: "USD Coin"`, `version: "2"`, `chainId: 8453`, `verifyingContract: USDC`), the relayer submits and pays gas. Authorization `nonce = keccak256("monaco:" + intentID)` so retries reuse one authorization and USDC rejects replays. `validAfter = 0`, `validBefore = now + 1h`. |
| Gas — treasuries | Treasuries hold ETH. Before any treasury-sent tx, `ensureTreasuryGas` tops up from the relayer when balance < `TreasuryGasFloorWei` (0.0003 ETH) by `TreasuryGasTopUpWei` (0.001 ETH); records `treasuries.gas_topped_up_at`. |
| Relayer | One secp256k1 EOA, `RELAYER_PRIVATE_KEY` as `0x`-prefixed 32-byte hex. Go derives the address locally (`just relayer balance`); the sidecar signs with it. Boot fails if relayer ETH < `FeePayerMinWei` (0.002 ETH). Relayer sends are serialized in the sidecar (single nonce queue). |
| Confirmation | A tx is confirmed when `eth_getTransactionReceipt.status == 0x1`. Failed receipt (`0x0`) is terminal failure. `tx_hash` is the idempotency key everywhere `tx_signature` was. |
| Trading venue | KyberSwap Aggregator on Base (keyless; `x-client-id: monaco`). `GET https://aggregator-api.kyberswap.com/base/api/v1/routes` for quotes, `POST …/api/v1/route/build` for calldata. Slippage 50 bps. No route (`code != 0`, empty `routeSummary`, HTTP error) → refuse, no `transactions` row. The venue sits behind `dex.Client` so 0x can replace it later. |
| Stock catalog | Coinbase B20 tokens on Base (8 decimals; verified `AAPLc.decimals() == 8`). Pinned list of 14 (`AAPLc, AMZNc, COINc, CRCLc, GOOGLc, INTCc, METAc, MSFTc, MSTRc, NVDAc, SNDKc, SPCXc, TSLAc`, plus any listed in `docs.base.org` table at implementation time) with contract + Chainlink feed. Onchain Registry `0x3f3E8cf41cdd3b1D118c16471aB0113DfDDd5CaD` is documented, not read at runtime. Symbols shown to users strip the trailing `c` for display names only via existing display helpers; API `symbol` stays `AAPLc`. |
| Pot marks | Chainlink total-return feeds on Base via `latestRoundData()` (8 decimals). These already include the B20 multiplier (splits, dividends). **Pyth is not used for NAV.** `AfterHours = updatedAt older than 25h` (feeds hold last close on weekends/holidays). Pyth for charts and display; never NAV. On the stock screens Pyth serves the underlying equity (`AAPLc` → `Equity.US.AAPL/USD`, one lowercase `c` stripped before upper-casing, so `SPCXc` → `SPCX`): Benchmarks chart history for 1D/1W/1M/3M/1Y/ALL, the stats grid, the day change against the previous regular-session close, and a labelled equity reference line. The hero price stays the Chainlink total-return mark, the same per-token mark NAV uses. The stock-vs-token premium is the Kyber quote-implied mid against that mark; Pyth publishes no feed for a B20 token. |
| Swap execution | `app/swap.go` orchestrates: ensure treasury gas → `allowance(treasury, router)` → `approve(router, amountIn)` if short (wait receipt) → send swap calldata from treasury (wait receipt) → parse ERC-20 `Transfer` logs to the treasury for the output token to get the fill → `transactions` row. Sell is the mirror. |
| Cabal cash-out target | Redeem pays the **member wallet only** (existing withdraw-to-balance path). The external payout address and payout proof are removed from the redeem API and UI; `VerifyPayoutProof` is deleted. External exit is Settings → Withdraw (platform withdraw to any pasted Base address). `payout_proofs` table is dropped in `000013`. |
| Deposits | Unchanged model: inbound Base USDC sits in the member wallet; platform balance = on-chain USDC minus in-flight intents. No log indexer. |
| HTTP contract | Renames only: `solanaMint` → `tokenAddress`, `mint` → `tokenAddress`, `txSignature` → `txHash`, `inputMint`/`outputMint` → `inputToken`/`outputToken`. `memberWalletAddress`, `treasuryAddress`, `fromAddress` unchanged. |
| Copy | "Base" replaces "Solana" where the word appears at all (Settings → Advanced only). Explorer links go to `https://basescan.org/address/{addr}` and `/tx/{hash}`. Main flow still shows no addresses, contract addresses, gas, or explorer links. |
| Package names | Go packages `privy`, `jupiter`, `xstocks`, `solana/*` are **deleted**, not aliased. New packages listed under Structure. No `Privy`/`Solana`/`Jupiter` identifiers remain in `apps/backend` or `apps/mobile` except in `docs/` history. |

## Structure

```text
apps/signer/                       Node sidecar (TypeScript). Only process that touches private keys.
  package.json                     deps: @dynamic-labs-wallet/node, @dynamic-labs-wallet/node-evm, viem; dev: typescript, tsx, vitest
  src/server.ts                    http server, shared-secret check, routes below
  src/dynamic.ts                   DynamicEvmWalletClient auth + createWalletAccount / signTypedData / getWalletClient
  src/relayer.ts                   viem privateKeyToAccount(RELAYER_PRIVATE_KEY), serialized sendTransaction
  src/routes.ts                    request/response zod-free validation, handlers
  test/*.test.ts                   vitest: validation, secret gate, fake Dynamic client injection

apps/backend/
  internal/auth/                   Verifier interface, Identity{DynamicUserID, SessionID}; dynamic.go JWKS verifier; fake.go
  internal/wallets/                Client interface (was privy.Client minus VerifySession/VerifyPayoutProof, plus SendTreasuryTransaction);
                                   signer_client.go implementation over signer + evm; shares.go AES-GCM; fake.go
  internal/signer/                 HTTP client for apps/signer; types; fake.go
  internal/evm/                    JSON-RPC reader (Call, ERC20Balance, ETHBalance, Allowance, Receipt, IsConfirmed, ChainlinkLatestRoundData),
                                   abi.go encoders (transfer, approve, transferWithAuthorization, allowance, latestRoundData, decimals),
                                   address.go NormalizeAddress/IsAddress, eip3009.go typed-data builder, constants.go; fake.go
  internal/dex/                    Client interface (QuoteBuy, QuoteSell, BuildSwap); kyber.go; fake.go
  internal/b20/                    Catalog interface (ResolveTokenAddress, Search, LookupByAddress, Popular, Feed); pinned.go; fake.go
  internal/marks/                  TreasuryRef, CostBasis, MarkedHolding, NavInput, Client interface (moved from pyth)
  internal/chainlink/              marks.Client over evm.ChainlinkLatestRoundData; AssetMark for Assets tab; fake.go
  internal/pyth/                   Display only, never NAV: Benchmarks history, Hermes equity price, stats. EquityQuerySymbol strips one trailing lowercase "c".
  internal/worker/                 Confirmer = evm.IsConfirmed; sweep poller unchanged in shape
  internal/app/                    swap.go on dex + wallets + evm; redeem.go member-wallet payout only; treasury gas
  cmd/api/main.go                  wires signer, evm, wallets, auth, dex, b20, chainlink; boot: relayer ETH check, signer /healthz
  cmd/print-relayer-pubkey         prints 0x address + Base ETH (just relayer balance)
  cmd/sweep-member-to-address      EIP-3009 sweep via signer (ops)

supabase/migrations/000013_base_dynamic.sql

apps/mobile/Monaco/
  MonacoApp.swift                  DynamicSDK.initialize(ClientProps(...)) in init()
  Features/Auth/DynamicAuthService.swift   replaces PrivyAuthService (same published surface)
  Features/Auth/DeviceRegistrationView.swift, StepUpAuthView.swift
  Config.swift                     DynamicAuthSettings
  Config/Dynamic.xcconfig, Dynamic.local.xcconfig (generated, gitignored)
packages/mobile-core/Sources/MonacoCore/DynamicAuthConfig.swift

scripts/ensure-ios-dynamic-config.sh, with-ios-dynamic-env.sh, export-ios-dynamic-env.sh (renamed from *privy*)
```

## Flow

### Auth
1. iOS: `sdk.auth.email.sendOTP` / `sdk.auth.sms.sendOTP` → `verifyOTP`. If `sdk.deviceRegistration.isDeviceRegistrationRequired`, show `DeviceRegistrationView`; if a call throws a step-up error, show `StepUpAuthView`. No Monaco API calls until the JWT exists.
2. `POST /v1/auth/session {accessToken}` → `auth.Verifier.VerifySession` → `UpsertUser(dynamic_user_id)` → `wallets.EnsureMemberWallet` (sidecar `POST /v1/wallets`, persist metadata + encrypted shares + lowercase address).

### Deposit → fund
3. User sends Base USDC to `memberWalletAddress`. `GET /v1/me/balance` = `evm.ERC20Balance(USDC, member)` minus in-flight.
4. `POST /v1/groups/{id}/fund` inserts a pending deposit. Worker tick: `wallets.SubmitSweep` builds EIP-3009 typed data (`nonce = keccak256("monaco:deposit:" + depositID)`), sidecar signs with the member wallet, sidecar relayer submits `transferWithAuthorization` → `tx_hash`. Worker confirms via receipt → `ObserveSweep` credits shares at current NAV.

### Buy / sell
5. Proposal passes → `ExecuteOnPass` → `swap.go`: `b20.ResolveTokenAddress(symbol)` → `dex.QuoteBuy` (refuse on no route) → `ensureTreasuryGas` → allowance/approve → `dex.BuildSwap(recipient = treasury)` → `wallets.SendTreasuryTransaction(to = router, data)` → receipt → fill from `Transfer` logs → `transactions` row (`input_token`, `output_token`, `tx_hash`, cost basis).
6. Sell: `dex.QuoteSell` / `BuildSwap` from B20 token to USDC; same path; `action = sell`.

### Cash out
7. Redeem (`POST /v1/groups/{id}/redeem` and withdraw-to-balance) debits share units, sells shortfall, then `wallets.PayUSDC` from treasury to the member wallet (plain ERC-20 `transfer`, treasury pays gas).
8. Platform withdraw (`POST /v1/me/withdrawals`) uses `wallets.SubmitMemberUSDCTransfer` = EIP-3009 to the pasted address via relayer.

### Marks
9. `chainlink.MarkedPot` reads `latestRoundData` per held token, converts 8-decimal answer to USDC micros, sets `AfterHours` on stale `updatedAt`. Callers (`home`, `group_view`, `deposit_credit`, `redeem`) are unchanged apart from the `marks` import.

## Data models

`supabase/migrations/000013_base_dynamic.sql` (renames, additive columns, one drop):

| Table | Change |
|-------|--------|
| `users` | `privy_user_id` → `dynamic_user_id` |
| `member_wallets` | `privy_wallet_id` → `wallet_id`; `solana_address` → `address`; add `wallet_metadata jsonb NOT NULL DEFAULT '{}'::jsonb`, `key_shares_enc text` |
| `treasuries` | same as member_wallets; add `gas_topped_up_at timestamptz` |
| `deposits`, `withdrawals`, `platform_withdrawals`, `transactions` | `tx_signature` → `tx_hash` (unique constraints preserved) |
| `proposals` | `fill_tx_signature` → `fill_tx_hash` |
| `transactions` | `input_mint` → `input_token`, `output_mint` → `output_token` |
| `redeem_jobs` | `payout_address` kept (always the member wallet) |
| `payout_proofs` | `DROP TABLE` |

Amount columns are unchanged: USDC micros (6 dp) and token atomics (8 dp) both fit `bigint`.

## Environment

Removed: every `PRIVY_*`, `SOLANA_RPC_URL`, `JUPITER_API_KEY`.

| Key | Used by | Notes |
|-----|---------|-------|
| `DYNAMIC_ENVIRONMENT_ID` | Go verifier, iOS xcconfig, signer | dashboard → Developers → SDK & API Keys |
| `DYNAMIC_API_TOKEN` | signer only | dashboard API token |
| `DYNAMIC_WALLET_PASSWORD` | signer only | wraps the Dynamic backup share |
| `WALLET_SHARES_KEY` | Go | 32-byte hex, AES-256-GCM for `key_shares_enc` |
| `SIGNER_URL` | Go | default `http://127.0.0.1:8081` |
| `SIGNER_SHARED_SECRET` | Go, signer | random string |
| `SIGNER_PORT` | signer | default `8081` |
| `BASE_RPC_URL` | Go, signer | default `https://mainnet.base.org` |
| `RELAYER_PRIVATE_KEY` | signer (sign), Go (derive address) | `0x` + 64 hex |
| `KYBER_CLIENT_ID` | Go | optional, default `monaco` |
| `PYTH_API_KEY`, `PYTH_HERMES_BASE_URL`, `PYTH_BENCHMARKS_BASE_URL` | Go | Pyth for charts and display; never NAV. Benchmarks is keyless; the key gates only Hermes |
| `AUTH_SMS_LOGIN_ENABLED`, `AUTH_EMAIL_LOGIN_ENABLED` | iOS | replace `PRIVY_*_LOGIN_ENABLED` |

Dynamic dashboard (manual, once): enable Email OTP and SMS OTP; enable EVM + Base; **disable** embedded wallets for end users; enable server wallets; create API token; whitelist deeplink `monaco://`; set minimum API version so headless device registration + step-up apply.

## Parallelization

| Wave | Tracks (parallel within the wave) | Gate |
|------|-----------------------------------|------|
| **1** | M6-T1 foundation (schema, package rename, interfaces, fakes, evm package, HTTP field renames, config) | — |
| **2** | **Backend auth** M6-T2 · **Mobile auth** M6-T3 · **Signer + wallets** M6-T4 · **Trading** M6-T5 · **Marks** M6-T6 · **Mobile product** M6-T7 | M6-T1 |
| **3** | **Money flows + wiring** M6-T8 | M6-T4 (+ T2, T5, T6 merged) |
| **4** | **Ops, env, Justfile, docs** M6-T9 | Wave 3 |
| **5** | **Workspace gate + manual verification** M6-T10 | Wave 4 |

One agent at a time on `Justfile`, `.env.example`, `docker-compose.yml`, `cmd/api/main.go` (T1 first, then T8, then T9).

## Tickets

1. M6-T1 Foundation: schema `000013`, chain-neutral Go packages and fakes, `internal/evm`, HTTP field renames, config keys.
2. M6-T2 Dynamic JWT verifier in Go. Depends on: M6-T1
3. M6-T3 Dynamic Swift SDK auth on iOS (headless OTP, device registration, step-up, config, scripts). Depends on: M6-T1
4. M6-T4 `apps/signer` sidecar, `internal/signer` client, `wallets.SignerClient`. Depends on: M6-T1
5. M6-T5 B20 catalog, Kyber DEX client, swap/sell/agent execution on Base. Depends on: M6-T1
6. M6-T6 Chainlink total-return marks; Pyth charts symbol map. Depends on: M6-T1
7. M6-T7 Mobile product changes: DTO renames, redeem to balance only, copy, explorer links, boundary scanners. Depends on: M6-T1
8. M6-T8 Money flows on Base and `main.go` wiring: EIP-3009 sweeps and withdrawals, treasury payout and gas, receipts, boot checks. Depends on: M6-T2, M6-T4, M6-T5, M6-T6
9. M6-T9 Ops and docs: Justfile signer wiring, `.env.example`, scripts, relayer and sweep commands, README, `docs/product.md`, `docs/index.md`. Depends on: M6-T8
10. M6-T10 Workspace gate and manual verification. Depends on: M6-T9

## Ticket details

#### M6-T1: Foundation — schema, packages, interfaces, fakes, evm, contract renames

**Context**
Every later lane consumes the interfaces and names fixed here. This is the only ticket allowed to touch the whole tree at once.

**Problem**
`apps/backend` is organised around `privy.Client`, `jupiter.Client`, `xstocks.*`, `pyth.Client`, `worker.SolanaRPC`, and `solana/*`. Column and JSON names encode Solana.

**Proposal — implement exactly**
1. Add `supabase/migrations/000013_base_dynamic.sql` per **Data models**. Update `apps/backend/internal/postgres/*` queries and structs for every rename. Go struct fields: `DynamicUserID`, `WalletID`, `Address`, `WalletMetadata json.RawMessage`, `KeySharesEnc string`, `TxHash`, `InputToken`, `OutputToken`, `FillTxHash`, `GasToppedUpAt *time.Time`.
2. Create `internal/auth`: `type AccessToken string`; `type Identity struct{ DynamicUserID, SessionID, DisplayName string }`; `type Verifier interface{ VerifySession(ctx, AccessToken) (Identity, error) }`; `NewFakeVerifier()` with `RegisterToken(token, Identity)`. Implementation file `dynamic.go` is a stub returning `ErrNotConfigured` (T2 fills it).
3. Create `internal/wallets` by moving `internal/privy` and renaming: package `wallets`; `WalletRef{UserID, WalletID, Address}`, `TreasuryRef{GroupID, WalletID, Address}`; `SweepRequest{MemberAddress, TreasuryAddress, Amount int64, IntentID string}`; `SweepResult{TxHash}`; `TransferRequest{..., IntentID}`; `TransferResult{TxHash}`; `PayUSDCRequest{TreasuryRef, ToAddress, Amount}`; `PayUSDCResult{TxHash}`. Interface:
   `EnsureMemberWallet(ctx, dynamicUserID string, userID UserID) (WalletRef, error)`, `EnsureTreasury(ctx, GroupID) (TreasuryRef, error)`, `MemberUSDCBalance`, `TreasuryUSDCBalance`, `SubmitSweep`, `SubmitMemberUSDCTransfer`, `PayUSDC`, `SendTreasuryTransaction(ctx, treasury TreasuryRef, to string, data []byte, valueWei *big.Int) (txHash string, err error)`. Delete `VerifySession`, `VerifyPayoutProof`, `PayoutProof`, `PayoutMessage`, `ListAppSolanaWallets`, `auth_sign.go`, `http.go` Privy transport. `NewFakeClient()` keeps the existing register/set helpers under the new names; fake addresses are lowercase `0x` + 40 hex derived deterministically (no `FAKE` prefix strings).
4. Create `internal/evm`: `const ChainID = 8453`, `USDCAddress = "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"`, `USDCDecimals = 6`, `TreasuryGasFloorWei`, `TreasuryGasTopUpWei`, `FeePayerMinWei`. `NormalizeAddress(string) (string, error)` (lowercase, validates 0x + 40 hex). `Client` interface: `Call(ctx, to string, data []byte) ([]byte, error)`, `ERC20Balance(ctx, token, holder string) (*big.Int, error)`, `ETHBalance(ctx, addr string) (*big.Int, error)`, `Allowance(ctx, token, owner, spender string) (*big.Int, error)`, `Receipt(ctx, txHash string) (Receipt, error)` with `Receipt{Status uint64, Logs []Log, Found bool}`, `IsConfirmed(ctx, txHash string) (bool, error)` (found && status==1), `ChainlinkLatestRoundData(ctx, feed string) (RoundData{Answer *big.Int, UpdatedAt time.Time}, error)`. `NewJSONRPCClient(url)`; `NewFakeClient()` with setters. `abi.go`: `EncodeTransfer(to, amount)`, `EncodeApprove(spender, amount)`, `EncodeTransferWithAuthorization(from, to, value, validAfter, validBefore *big.Int, nonce [32]byte, v uint8, r, s [32]byte)`, `DecodeERC20TransferLogs(logs, token, to) *big.Int`. `eip3009.go`: `TransferAuthorizationTypedData(from, to string, value *big.Int, validBefore int64, nonce [32]byte) TypedData` (JSON-serialisable, EIP-712 for USDC on Base) and `AuthorizationNonce(intentID string) [32]byte` = keccak256(`"monaco:" + intentID`). Use `github.com/ethereum/go-ethereum` for `common`, `crypto`, `accounts/abi`.
5. Create `internal/signer`: `Client` interface `Health(ctx) (HealthInfo{RelayerAddress string, ChainID int64}, error)`, `CreateWallet(ctx) (CreatedWallet{WalletID, Address string, Metadata json.RawMessage, KeyShares json.RawMessage}, error)`, `SignTypedData(ctx, SignRequest{Metadata, KeyShares json.RawMessage, TypedData any}) (string, error)`, `SendTransaction(ctx, SendRequest{Metadata, KeyShares json.RawMessage, To string, Data []byte, ValueWei *big.Int}) (string, error)`, `RelayerSend(ctx, RelayerSendRequest{To string, Data []byte, ValueWei *big.Int}) (string, error)`. `NewHTTPClient(baseURL, secret)` and `NewFakeClient()` (T4 fills the HTTP body; the fake is complete here).
6. Create `internal/dex` replacing `internal/jupiter`: `Quote{TokenIn, TokenOut string, AmountIn, AmountOut *big.Int, Routable bool, RouteSummary json.RawMessage}`; `SwapCall{Router string, Data []byte, AmountOutMin *big.Int}`; interface `QuoteBuy(ctx, tokenOut string, usdcIn *big.Int) (Quote, error)`, `QuoteSell(ctx, tokenIn string, amountIn *big.Int) (Quote, error)`, `BuildSwap(ctx, q Quote, sender, recipient string) (SwapCall, error)`. `NewFakeClient()` with `RegisterQuote`, `RegisterNoRoute`, `RegisterSwapCall`. `kyber.go` stub returns `ErrNotConfigured` (T5 fills).
7. Create `internal/b20` replacing `internal/xstocks`: `Asset{Symbol, Name, TokenAddress, FeedAddress string, Decimals int}`; `Catalog` interface `ResolveTokenAddress(ctx, symbol) (string, error)`, `Search(ctx, query string, limit, offset int) (SearchPage, error)`, `LookupByAddress(ctx, addr) (Asset, bool, error)`, `Popular(ctx) ([]Asset, error)`, `Feed(ctx, symbol) (string, error)`. `pinned.go` holds the B20 table from `docs.base.org/base-chain/asset-issuance/tokenized-stocks-on-base` (contract + Chainlink feed per ticker). `NewPinnedCatalog()`, `NewFakeCatalog()`.
8. Create `internal/marks` with the types and `Client` interface moved out of `pyth` (`USDCOnlyPot`, `MarkedPot`). `pyth` keeps `AssetPriceClient` and Hermes charts; `pyth.Client` is deleted; `EquityQuerySymbol` strips a trailing `c`/`C` instead of `x`/`X`. `internal/chainlink` is created with `NewFakeClient()` and a stub `NewClient(evm.Client, b20.Catalog)` (T6 fills).
9. Delete `internal/solana/*`, `internal/jupiter`, `internal/xstocks`, `internal/privy`. `worker.SolanaRPC` → `worker.Confirmer` (`IsConfirmed(ctx, txHash)`); `app.SolanaConfirmer` → `app.Confirmer`; `app.TreasurySigner` deleted (swap uses `wallets.SendTreasuryTransaction`). `app/swap.go`, `start_buy.go`, `agent_intent.go` compile against `dex` + `b20` + `wallets` + `evm` with the orchestration described under **Flow 5–6** implemented against fakes (T5 only supplies the live Kyber client).
10. Redeem: delete `PayoutProof` from `RedeemRequest` and the HTTP request; `Redeem` always pays `memberWallet.Address`; delete `isPlatformPayoutAddress`; remove `payoutAddress` from `httpapi/groups.go` request/response.
11. `config`: remove Privy/Solana/Jupiter keys; add the table under **Environment** (Go-side keys only). `RelayerAddress()` derived with go-ethereum `crypto`. `config_test.go` updated.
12. HTTP renames per **Decisions** across `httpapi/*.go` and tests. `packages/mobile-core` and `apps/mobile` DTOs are **not** touched here (T7).
13. Scripts asserting env keys (`scripts/install-dev.sh`, `apps/backend/internal/config/config_test.go`) updated for the new keys so `just test backend` runs.

**Scope**
`supabase/migrations/000013_base_dynamic.sql`, `apps/backend/**` (all), `scripts/install-dev.sh`, `.env.example`.

**Out of scope**
Live Dynamic, Kyber, Chainlink, signer HTTP bodies; mobile; docs; Justfile.

**Acceptance Criteria**
- [ ] `rg -n "privy|Privy|solana|Solana|jupiter|Jupiter|xstocks|xStocks|Pyth.*MarkedPot" apps/backend` returns no matches outside test fixture names that are being asserted absent.
- [ ] `TestMigration000013_renamesAndDrops` (in `internal/postgres`) asserts `users.dynamic_user_id`, `member_wallets.address`, `transactions.tx_hash`, `transactions.input_token` exist and `payout_proofs` does not.
- [ ] `TestNormalizeAddress_lowercasesAndValidates`, `TestAuthorizationNonce_isDeterministic`, `TestTransferAuthorizationTypedData_matchesUSDCDomain`, `TestEncodeTransferWithAuthorization_selector` pass in `internal/evm`.
- [ ] `TestRedeem_paysMemberWalletOnly` passes; `payoutAddress` is absent from `httpapi/groups.go`.
- [ ] `just test backend` exits 0.

**Verification**
`just test backend`; the `rg` above.

**Done when**
All AC boxes checked; `git grep -n tx_signature apps/backend supabase/migrations/000013*` empty except historical migrations.

- **Wave:** 1

#### M6-T2: Dynamic JWT verifier

**Context / Problem**
`auth.Verifier` is a stub after T1. Sessions must verify Dynamic JWTs.

**Proposal — implement exactly**
1. `internal/auth/dynamic.go`: `NewDynamicVerifier(environmentID string, httpClient *http.Client) *DynamicVerifier`. JWKS URL `https://app.dynamicauth.com/api/v0/sdk/{env}/.well-known/jwks`; cache keys by `kid`; refetch once on unknown `kid`; RS256 only; check `iss == "app.dynamicauth.com/{env}"`, `exp` future, `iat` not > 5 min future, `scope` split on space contains `user:basic`; `sub` → `DynamicUserID`, `sid` → `SessionID`, optional `email`/`given_name` → `DisplayName` when present. Use `github.com/golang-jwt/jwt/v5` (already a dependency).
2. Errors map to `auth.ErrUnauthorized` so `httpapi` returns 401.
3. Tests with an in-test RSA key and `httptest` JWKS server.

**Scope** `apps/backend/internal/auth/`.

**Acceptance Criteria**
- [ ] `TestDynamicVerifier_validToken_returnsIdentity`, `TestDynamicVerifier_wrongIssuer_rejects`, `TestDynamicVerifier_missingUserBasicScope_rejects`, `TestDynamicVerifier_unknownKid_refetchesJWKSOnce`, `TestDynamicVerifier_expired_rejects` pass.
- [ ] `just test backend` exits 0.

- **Wave:** 2 · Depends on: M6-T1

#### M6-T3: Dynamic Swift SDK auth on iOS

**Context / Problem**
`PrivyAuthService` and the `privy-ios` SPM package are the only mobile auth path. Dynamic headless auth also requires device registration and step-up screens.

**Proposal — implement exactly**
1. `apps/mobile/Monaco.xcodeproj/project.pbxproj`: remove `privy-ios`; add `https://github.com/dynamic-labs/swift-sdk-and-sample-app.git` from `1.3.0`, product `DynamicSDKSwift`. Info.plist: `CFBundleURLSchemes` `monaco`.
2. `MonacoApp.swift` `init()`: `DynamicSDK.initialize(props: ClientProps(environmentId: Config.dynamic.environmentID, appLogoUrl: "https://monaco.app/logo.png", appName: "Monaco", redirectUrl: "monaco://", appOrigin: "https://monaco.app"))`.
3. `Features/Auth/DynamicAuthService.swift` replaces `PrivyAuthService.swift` with the same published surface (`phase`, `accessToken`, `lastSignOutReason`, `restoreSessionIfNeeded`, `sendSMSCode`, `loginWithSMSCode`, `sendEmailCode`, `loginWithEmailCode`, `logout`, `resetLoginFlow`, `recordBackendSession`, `shouldInvalidateBackendSession`). OTP via `sdk.auth.sms.sendOTP(phoneData:)` / `verifyOTP(token:)` and `sdk.auth.email.sendOTP(email:)` / `verifyOTP(token:)`. Access token from the SDK's JWT accessor on `sdk.auth` (check the SDK reference for the exact property; do not use the built-in `sdk.ui`). Observe `sdk.auth.authenticatedUserChanges`. Every view that took `PrivyAuthService` takes `DynamicAuthService`.
4. Add `DeviceRegistrationView` (shown when `sdk.deviceRegistration.isDeviceRegistrationRequired`; OTP entry; completes registration) and `StepUpAuthView` (uses `sdk.stepUpAuth.sendOtp()` / `verifyOtp(verificationToken:requestedScopes:)`). Both use the existing design system; no addresses shown.
5. `Config.swift`: `DynamicAuthSettings { environmentID, smsLoginEnabled, emailLoginEnabled }` from `DYNAMIC_ENVIRONMENT_ID`, `AUTH_SMS_LOGIN_ENABLED`, `AUTH_EMAIL_LOGIN_ENABLED`. `Config/Dynamic.xcconfig` template; `Dynamic.local.xcconfig` generated and gitignored. `packages/mobile-core/.../DynamicAuthConfig.swift` mirrors it (replaces `PrivyAuthConfig.swift`) with tests.
6. Scripts: rename `ensure-ios-privy-config.sh` → `ensure-ios-dynamic-config.sh`, `with-ios-privy-env.sh` → `with-ios-dynamic-env.sh`, `export-ios-privy-env.sh` → `export-ios-dynamic-env.sh`; they read `DYNAMIC_ENVIRONMENT_ID` and the two `AUTH_*` flags; `SIMCTL_CHILD_DYNAMIC_*`. Update `scripts/ios-sim`, `scripts/ios-build`, and the Justfile lines that call them (only those lines).
7. `MainFlowCopyAudit` / `ProductBoundaryScanner`: replace Solana/Privy terms with Base/Dynamic terms; still block raw addresses and explorer hosts in main-flow copy.

**Scope** `apps/mobile/**`, `packages/mobile-core/**` (auth config + tests), `scripts/*ios*`, `scripts/*dynamic*`, Justfile mobile lines only.

**Acceptance Criteria**
- [ ] `rg -n "Privy|privy" apps/mobile packages/mobile-core scripts` empty.
- [ ] `testDynamicAuthConfig_readsEnvironmentIDAndFlags` and `testDynamicAuthConfig_missingEnvironmentID_fails` pass in `packages/mobile-core`.
- [ ] `just test mobile` exits 0; `just build mobile` exits 0 with `DYNAMIC_ENVIRONMENT_ID` set in `.env.local`.

- **Wave:** 2 · Depends on: M6-T1

#### M6-T4: Signer sidecar, signer client, wallets implementation

**Context / Problem**
Nothing signs yet. Dynamic MPC only runs in Node.

**Proposal — implement exactly**
1. `apps/signer` per **Structure**. `npm run build` (tsc), `npm run dev` (tsx watch), `npm test` (vitest). Node 20 `engines`. Routes: `GET /healthz`, `POST /v1/wallets`, `POST /v1/wallets/sign-typed-data`, `POST /v1/wallets/send`, `POST /v1/relayer/send`. Reject missing/incorrect `x-signer-secret` with 401. Listen on `127.0.0.1:${SIGNER_PORT:-8081}`.
2. `src/dynamic.ts`: `DynamicEvmWalletClient({environmentId})` + `authenticateApiToken(DYNAMIC_API_TOKEN)`; `createWalletAccount({ thresholdSignatureScheme: TWO_OF_TWO, password: DYNAMIC_WALLET_PASSWORD, backUpToDynamic: true })` → return `walletMetadata`, `externalServerKeyShares`, `accountAddress` lowercased; `signTypedData({walletMetadata, typedData, password, externalServerKeyShares})`; `send` via `getWalletClient` (viem, chain `base`, transport `http(BASE_RPC_URL)`) `sendTransaction({to, data, value})` → hash.
3. `src/relayer.ts`: `privateKeyToAccount(RELAYER_PRIVATE_KEY)`, viem `walletClient.sendTransaction`, promise-chained queue so nonces are sequential.
4. Tests inject a fake Dynamic client; no network.
5. `internal/signer/http.go`: JSON client for the routes above with the shared-secret header, 30 s timeout, structured errors (`ErrUnauthorized`, `ErrSignerUnavailable`).
6. `internal/wallets/signer_client.go`: `NewSignerClient(signer signer.Client, chain evm.Client, store WalletStore, sharesKey []byte, relayerAddress string)`. `EnsureMemberWallet` / `EnsureTreasury` are idempotent on the store row; on create they call `signer.CreateWallet`, encrypt shares (`shares.go`: AES-256-GCM, random 12-byte nonce, base64 `nonce||ciphertext`), persist metadata + address. `SubmitSweep` and `SubmitMemberUSDCTransfer`: `evm.TransferAuthorizationTypedData` → `signer.SignTypedData` (member wallet) → split signature into v,r,s → `evm.EncodeTransferWithAuthorization` → `signer.RelayerSend(to = USDC)`. `PayUSDC`: `ensureTreasuryGas` → `SendTreasuryTransaction(to = USDC, data = EncodeTransfer)`. `SendTreasuryTransaction`: `ensureTreasuryGas` then `signer.SendTransaction` with the treasury's metadata + decrypted shares. `ensureTreasuryGas`: `evm.ETHBalance(treasury) < TreasuryGasFloorWei` → `signer.RelayerSend(to = treasury, value = TreasuryGasTopUpWei)`, wait for receipt, set `gas_topped_up_at`. Balances via `evm.ERC20Balance(USDC, …)`.

**Scope** `apps/signer/**`, `apps/backend/internal/signer/`, `apps/backend/internal/wallets/` (implementation files only; interface fixed in T1).

**Acceptance Criteria**
- [ ] vitest: `rejects requests without the shared secret`, `creates a wallet and returns lowercase address + metadata + shares`, `signs typed data with stored shares`, `serialises relayer sends` pass.
- [ ] Go: `TestSignerClient_EnsureMemberWallet_isIdempotent`, `TestSignerClient_SubmitSweep_buildsEIP3009AndRelays`, `TestSignerClient_PayUSDC_topsUpGasWhenBelowFloor`, `TestShares_roundTrip` pass with `signer.NewFakeClient()` and `evm.NewFakeClient()`.
- [ ] `just test backend` exits 0; `cd apps/signer && npm ci && npm test && npm run build` exit 0.

- **Wave:** 2 · Depends on: M6-T1

#### M6-T5: B20 catalog, Kyber DEX, execution on Base

**Context / Problem**
`dex.kyber.go` and `b20` live data are stubs; `swap.go` orchestration exists against fakes.

**Proposal — implement exactly**
1. `internal/dex/kyber.go`: `NewKyberClient(httpClient, clientID)`. `QuoteBuy` / `QuoteSell` → `GET https://aggregator-api.kyberswap.com/base/api/v1/routes?tokenIn&tokenOut&amountIn` with `x-client-id`; `Routable = code == 0 && routeSummary present`; keep `routeSummary` raw. `BuildSwap` → `POST …/api/v1/route/build` with `{routeSummary, sender, recipient, slippageTolerance: 50, deadline: now+20m, source: clientID}` → `SwapCall{Router: routerAddress, Data, AmountOutMin}`.
2. `internal/b20/pinned.go`: fill the full table (symbol, name, token address, Chainlink feed address, decimals 8) from the Base docs page; `Search` is case-insensitive prefix/substring over symbol and name; `Popular` returns a fixed ordered subset (`AAPLc, NVDAc, TSLAc, MSFTc, AMZNc, GOOGLc, METAc, COINc`).
3. `httpapi/catalog.go`, `httpapi/assets.go`: serve from `b20.Catalog`; routability via `dex.QuoteBuy` with 1 USDC probe (cached 60 s per symbol); field `tokenAddress`.
4. `app/swap.go`: confirm the T1 orchestration handles: approve only when `Allowance < AmountIn`; wait for approve receipt before swap; fill = `DecodeERC20TransferLogs(receipt.Logs, tokenOut, treasury)`; on receipt status 0 mark the transaction `failed` (no retry); `execute_request_id` = proposal id or agent intent id.
5. Agent intents (`agent_intent.go`) use the same path; no changes beyond compile.

**Scope** `apps/backend/internal/dex/`, `internal/b20/`, `internal/app/swap.go`, `start_buy.go`, `httpapi/catalog.go`, `httpapi/assets.go`, their tests.

**Acceptance Criteria**
- [ ] `TestKyber_QuoteBuy_parsesRouteSummary`, `TestKyber_QuoteBuy_noRoute_returnsRoutableFalse`, `TestKyber_BuildSwap_returnsRouterAndCalldata` pass against `httptest` fixtures.
- [ ] `TestPinnedCatalog_containsAAPLcWithFeed`, `TestPinnedCatalog_Search_caseInsensitive` pass.
- [ ] `TestExecuteBuy_approvesWhenAllowanceShort_thenSwaps`, `TestExecuteBuy_skipsApproveWhenAllowanceSufficient`, `TestExecuteBuy_failedReceipt_marksTransactionFailed`, `TestExecuteBuy_duplicateTxHash_isIdempotent`, `TestSellToUSDC_happyPath_reducesTokenAndIncreasesTreasuryUsdc` pass.
- [ ] `just test backend` exits 0. No live HTTP in tests.

- **Wave:** 2 · Depends on: M6-T1

#### M6-T6: Chainlink marks

**Proposal — implement exactly**
1. `internal/chainlink/client.go`: `NewClient(chain evm.Client, catalog b20.Catalog, now func() time.Time)` implementing `marks.Client`. For each holding: `feed = catalog.Feed(symbol)`; `RoundData` → price micros = `answer * 1e6 / 1e8`; value = `units * price / 1e8`; `AfterHours = any feed updatedAt older than 25h`. `USDCOnlyPot` uses `evm.ERC20Balance(USDC, treasury)`.
2. `AssetMark(ctx, symbol)` for `httpapi/assets.go` from the same feed; `ChartSeries` stays on `pyth` with `EquityQuerySymbol("AAPLc") == "Equity.US.AAPL/USD"`.
3. Stale/zero answer → `marks.ErrMarkUnavailable`; callers keep the existing cost-basis fallback.

**Scope** `apps/backend/internal/chainlink/`, `internal/pyth/feeds.go` (+ tests), `httpapi/assets.go` wiring for marks only.

**Acceptance Criteria**
- [ ] `TestChainlink_MarkedPot_convertsEightDecimalsToMicros`, `TestChainlink_MarkedPot_staleFeedSetsAfterHours`, `TestChainlink_zeroAnswer_returnsErrMarkUnavailable`, `TestEquityQuerySymbol_stripsTrailingC` pass.
- [ ] `just test backend` exits 0.

- **Wave:** 2 · Depends on: M6-T1

#### M6-T7: Mobile product changes

**Proposal — implement exactly**
1. DTOs in `packages/mobile-core` and `apps/mobile`: `solanaMint` → `tokenAddress`, `txSignature` → `txHash`, `inputMint`/`outputMint` → `inputToken`/`outputToken`, `mint` → `tokenAddress`. Update fixtures and `MockURLProtocol` tests.
2. Redeem: delete `PayoutProofCollector.swift`; `RedeemView` has no payout address field; request body has no `payoutAddress`/proof; copy "Sell to balance" / "Sold to your balance" via existing copy tables.
3. `SettingsAdvancedLinks` (both copies): Basescan `address/` and `tx/` URLs; labels "View on Basescan".
4. Address rendering: full lowercase-or-checksum string as returned; copy button unchanged; middle-ellipsis where existing Solana truncation existed; never hyphenate.
5. Any "Solana"/"SOL"/"Solscan" copy → "Base"/"ETH"/"Basescan" (Settings → Advanced only). Deposit sheet subtitle: "Send USDC on Base to this address".
6. `ProductBoundaryScanner`: block `0x[0-9a-fA-F]{40}` and `basescan.org`/`etherscan.io` in main-flow copy; allow in Settings → Advanced.

**Scope** `apps/mobile/**` except `Features/Auth` (T3), `packages/mobile-core/**` except auth config.

**Acceptance Criteria**
- [ ] `rg -n "solanaMint|txSignature|inputMint|outputMint|Solscan|solana" apps/mobile packages/mobile-core` empty.
- [ ] `testMarketAssetDTO_decodesTokenAddress`, `testRedeemRequest_hasNoPayoutFields`, `testSettingsAdvancedLinks_basescan`, `testProductBoundaryScanner_blocksEvmAddressInMainFlow` pass.
- [ ] `just test mobile` exits 0.

- **Wave:** 2 · Depends on: M6-T1

#### M6-T8: Money flows on Base and API wiring

**Proposal — implement exactly**
1. `cmd/api/main.go`: build `evm.NewJSONRPCClient(cfg.BaseRPCURL)`, `signer.NewHTTPClient(cfg.SignerURL, cfg.SignerSharedSecret)`, `wallets.NewSignerClient(...)`, `auth.NewDynamicVerifier(cfg.DynamicEnvironmentID, http.DefaultClient)`, `dex.NewKyberClient`, `b20.NewPinnedCatalog`, `chainlink.NewClient`, `pyth` charts. Boot: `signer.Health` must succeed and report `ChainID == 8453`; `evm.ETHBalance(cfg.RelayerAddress()) >= FeePayerMinWei` else exit with a clear message.
2. Worker: `NewSweepPoller(..., evm.Client as Confirmer, ...)`; `SubmitSweep` gets `IntentID = deposit.ID`; confirm by receipt; a receipt with status 0 marks the deposit `failed` and clears in-flight.
3. `platform_withdraw.go`: `IntentID = withdrawal.ID`; confirm by receipt.
4. `redeem.go`: `PayUSDC` to the member wallet; `deposit_reconcile.go` and `CreditUncreditedTreasuryUSDC` on `evm.ERC20Balance`.
5. `cmd/print-relayer-pubkey`: print `RelayerAddress()` and Base ETH balance (wei → ETH string). `cmd/sweep-member-to-address`: EIP-3009 via `wallets.SubmitMemberUSDCTransfer` for ops.
6. Structured logs at sweep sign / relay / receipt, approve / swap / receipt, gas top-up.

**Scope** `cmd/**`, `internal/worker/`, `internal/app/{deposit*,platform_withdraw,redeem}.go`, `internal/config` (only if a key is missing).

**Acceptance Criteria**
- [ ] `TestSweepPoller_confirmsByReceipt_thenCredits`, `TestSweepPoller_failedReceipt_marksDepositFailed`, `TestPlatformWithdraw_usesEIP3009WithWithdrawalIDNonce`, `TestBoot_failsWhenRelayerETHBelowMin` pass.
- [ ] `just test backend` exits 0; `just build backend` exits 0.

- **Wave:** 3 · Depends on: M6-T2, M6-T4, M6-T5, M6-T6

#### M6-T9: Ops, env, Justfile, docs

**Proposal — implement exactly**
1. Justfile (existing recipes only): `just run backend` starts the signer (`npm ci` when `node_modules` missing, then `npm run dev` under `scripts/with-dotenv-local.sh`, log via `run-with-logs.sh`) before the API; `just stop backend` and `just killports` also kill `8081`; `just test backend` runs `npm test` in `apps/signer` after Go tests; `just build backend` runs `npm ci && npm run build` in `apps/signer` after the Go build; `just relayer balance` prints the 0x address and ETH.
2. `.env.example`: replace the Privy/Solana block with the **Environment** table (comments explain each, no values). `scripts/install-dev.sh` asserts `DYNAMIC_ENVIRONMENT_ID`, `SIGNER_SHARED_SECRET`, `WALLET_SHARES_KEY`, `RELAYER_PRIVATE_KEY`.
3. `scripts/sweep-wallets.sh` → new sweep command; `docs/ops-sweep-wallets.md` rewritten for Base.
4. `README.md`: clone/run for Dynamic + Base (dashboard checklist, Node 20 for the signer, relayer funding with ETH on Base, deposit test with Base USDC). Remove Solana/Privy/Phantom-MCP instructions.
5. `docs/product.md` and `docs/index.md`: Solana/Privy/Jupiter → Base/Dynamic/Kyber + B20 + Chainlink, constants replaced, wallet table updated (member wallets hold no ETH; treasuries hold gas; relayer). `docs/milestones/m6-dynamic-base.md` listed in the index.
6. `.gitignore`: `apps/signer/node_modules`, `apps/signer/dist`, `apps/mobile/Config/Dynamic.local.xcconfig`.

**Scope** `Justfile`, `.env.example`, `.gitignore`, `scripts/`, `README.md`, `docs/product.md`, `docs/index.md`, `docs/ops-sweep-wallets.md`.

**Acceptance Criteria**
- [ ] `rg -n "Privy|Solana|Jupiter|xStocks|Phantom" README.md docs/product.md docs/index.md Justfile .env.example scripts` empty.
- [ ] `just test backend`, `just test mobile`, `just build backend`, `just build mobile` exit 0.

- **Wave:** 4 · Depends on: M6-T8

#### M6-T10: Workspace gate and manual verification

Run the **Automated verification** commands, then the **Manual verification** section. Anything blocked on credentials or funds is reported, not marked done.

- **Wave:** 5 · Depends on: M6-T9

## Automated verification

**Commands.** `just test backend` (Go + signer vitest), `just test mobile`, `just build backend`, `just build mobile`.

| Layer | Proves |
|-------|--------|
| Go unit | JWT verify (JWKS httptest), EIP-3009 typed data + calldata, address normalisation, Kyber parsing + no-route refusal, B20 pinned catalog, Chainlink conversion + staleness, share encryption |
| Go integration (local Postgres, all fakes) | session → member wallet row; fund → sweep → receipt → share credit; withdraw; redeem to member wallet; execute-on-pass approve + swap + fill; sell; agent intent; idempotency on `tx_hash` |
| Signer vitest | secret gate, wallet create shape, typed-data sign, relayer queue — fake Dynamic, no network |
| Swift (`packages/mobile-core`) | DTO decode with new fields, Dynamic config, boundary scanner, Basescan links |

No test calls Dynamic, Kyber, Chainlink, Base RPC, or Pyth.

## Manual verification

Requires `.env.local` with a Dynamic environment (Email + SMS OTP enabled, EVM/Base enabled, client embedded wallets off, server wallets on, API token, deeplink `monaco://`), a relayer with ≥ 0.005 ETH on Base, and a small amount of Base USDC.

1. `just run backend`: signer `/healthz` logged with the relayer address; API boot passes the ETH check.
2. `just run mobile` on the resolved sim: email OTP → session. Confirm zero Monaco API traffic before `POST /v1/auth/session`. Confirm a `member_wallets` row with a lowercase `0x` address and non-empty `wallet_metadata`.
3. Dynamic dashboard shows the server wallet; no end-user embedded wallet was created.
4. Send 2 USDC on Base to the deposit address. Balance appears in the app without a sweep.
5. Create a cabal; a treasury row exists; fund 1 USDC → deposit row gets a `tx_hash`; Basescan shows `transferWithAuthorization` sent by the relayer; shares credited.
6. Propose `AAPLc` for 1 USDC, pass the vote: logs show gas top-up (first time), approve, swap, receipt; Basescan shows AAPLc in the treasury; pot view shows the position marked from Chainlink.
7. Sell half back to balance: treasury USDC rises, position falls, member balance rises.
8. Settings → Withdraw 1 USDC to an external Base address: `transferWithAuthorization` from the member wallet, relayer-paid.
9. Settings → Advanced explorer links open Basescan.
10. Sign out, sign in on a second device or after reinstall: device registration screen appears and completes.

## Out of scope

Migrating Solana balances; ERC-4337 / paymasters; 0x integration (interface only); on-chain voting; Dinari or Ondo venues; Android or web.

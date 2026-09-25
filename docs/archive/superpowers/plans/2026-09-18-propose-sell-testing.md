# Propose sell (#146) testing plan

Run from the clone root on this machine only. Sign iOS with personal team `8VKB4AC8F9` (`andrescamp_ac@icloud.com`). Never `andres@series.so`.

## 1. Backend (required)

```bash
docker compose up -d --wait
./scripts/with-dotenv-local.sh scripts/apply-migrations.sh
just test backend
```

Focused checks if the full suite fails:

```bash
go test -C packages/domain -count=1 ./...
./scripts/with-dotenv-local.sh bash -c 'cd apps/backend && go test -count=1 ./internal/app ./internal/postgres ./internal/httpapi ./internal/jupiter ./internal/worker'
```

## 2. Mobile host tests (required)

```bash
just test mobile
```

## 3. iOS compile (required)

```bash
export MONACO_IOS_DEVELOPMENT_TEAM=8VKB4AC8F9
export CODE_SIGN_IDENTITY='Apple Development: andrescamp_ac@icloud.com (8VKB4AC8F9)'
just build mobile
```

## 4. Gold-sim tap-through (required unless the current user message skips it)

UDID from `scripts/gold-sim-udid.sh` (fail if `SIMSLIM_UDID` unset). Sign with the same personal team env as step 3.

1. Sign in (email OTP).
2. Open a cabal with a stock holding.
3. Actions → Propose → Sell.
4. Pick a held symbol, enter a decimal amount at or under the ceiling, Get quote, Submit proposal.
5. Confirm toast, proposal history shows Sell + Open.
6. Vote the sell through; activity shows pending then confirmed sell (stock amount + USDC proceeds).
7. Repeat Propose → Buy on the same cabal; buy flow still works.
8. Sell amount above holding: quote/create 400, no proposal created.

## Pass / fail

The feature is green only when steps 1–3 pass and step 4 completes without product errors. Do not push until then.

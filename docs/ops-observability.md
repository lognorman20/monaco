# Ops: observability

What the API and the app record, where it lands, and what to alert on. Setup is env only; with
nothing set the API behaves as before (text logs on stderr, alerts as log lines, `/metrics` on
loopback).

## Turn it on

| Env | What it does | Unset |
| --- | --- | --- |
| `LOG_FILE` | Appends every log line as JSON to this path. | Text on stderr only. |
| `SENTRY_DSN` | Reports handler panics, poller panics and alerts to Sentry. One DSN per environment. | Off. |
| `APP_ENV`, `RELEASE` | Environment name and build id (git sha) on Sentry events. | Blank. |
| `ALERT_WEBHOOK_URL` | Slack- or Discord-compatible incoming webhook that receives alerts. | Alerts stay in the log and Sentry. |
| `METRICS_TOKEN` | Bearer token a Prometheus scraper presents to `GET /metrics`. | `/metrics` answers loopback only, 404 to everyone else. |

A malformed `SENTRY_DSN` or `ALERT_WEBHOOK_URL` fails boot. A typo should not silently turn
reporting off.

## Alerts

Alerts go to the log (`alert=<kind>`), Sentry, and the webhook. The same alert key is sent once
per 15 minutes; repeats are counted in `monaco_alerts_total{delivery="suppressed"}`. Delivery is
async with a bounded queue, so a slow or dead webhook never blocks a money path.

| Kind | Severity | Means | Do |
| --- | --- | --- | --- |
| `redeem_wedged` | critical | A cash out burnt share units but the USDC payout could not be verified on chain. Nothing automatic is safe. | Look up `job_id` in `redeem_jobs`, check the treasury's transfers on an explorer, then settle or roll back by hand. |
| `swap_unresolved` | critical | A treasury swap was handed to the venue 30+ minutes ago and neither the venue nor the chain can say whether it landed. The row stays `pending` and its proposal is not retried, so nothing is bought or sold twice. | Look up `tx_signature` on an explorer (Jupiter) or the order in the Flash dashboard (`request_id`). Landed: set the row `confirmed` with the fill amounts. Never landed: set it `failed`; the execute poller retries the proposal. |
| `relayer_low_balance` | critical | The fee-paying relayer is at or under 0.001 SOL. At zero, every sweep, trade and cash out fails, and the API will not boot. | Send SOL to `relayer_pubkey`. |
| `poller_panic` | critical | A background poller tick panicked. The loop survives and keeps ticking; the stack is in the log and Sentry. | Read the stack. A repeat every tick means one poisoned row. |
| `price_source_down` | warning | The Jupiter price breaker opened. Pots fall back to cost basis, so P&L stops moving until it recovers. | Check Jupiter status and `JUPITER_API_KEY` rate limits. |

## `GET /health`

`200` with `ok` or `degraded`, `503` with `down`. Only `database` is critical. A restart does not
refill a wallet or fix an upstream, and restarting mid-payout is worse than running degraded.

| Check | Fails when |
| --- | --- |
| `database` (critical) | Postgres does not answer a ping. |
| `solana_rpc` | The RPC does not answer a balance read. |
| `relayer_balance` | The relayer holds 0.001 SOL or less. Also raises `relayer_low_balance`. |
| `pollers` | A poller has not finished a tick in 3 intervals (2 minutes minimum). |
| `privy` | Privy's API is unreachable. |
| `price_source` | The Jupiter price API errors. |

Point the uptime monitor at `/health` and alert on anything other than `200` + `"status":"ok"`
for more than a few minutes.

## Metrics

Prometheus text format at `GET /metrics`. Route labels are mux patterns (`GET /v1/groups/{id}`),
never raw paths, so ids do not become time series.

| Metric | Labels | Use |
| --- | --- | --- |
| `monaco_http_requests_total` | `route`, `method`, `status` | Error rate per route. |
| `monaco_http_request_duration_seconds` | `route`, `method` | Latency. Trade and cash out routes confirm on chain inside the request, so tens of seconds is normal there. |
| `monaco_money_events_total` | `event`, `outcome` | `event`: `deposit_sweep`, `swap_buy`, `swap_sell`, `redeem`, `redeem_recovery`, `agent_intent`. `outcome`: `ok`, `rejected` (caller's fault: bad input, over budget, paused bot), `error` (ours or an upstream's), `canceled`, `replayed` (idempotent retry of a swap that already landed), `pending` (swap submitted but not observed; the swap reconcile poller counts it as `ok` or `error` when it settles). |
| `monaco_money_volume_usdc_micros_total` | `event` | USDC moved by successful events. Replays are not counted. |
| `monaco_upstream_requests_total` | `service`, `outcome` | `service`: `jupiter`, `pyth`, `privy`, `solana_rpc`, `xstocks`, `flash`, `supabase_storage`. `outcome`: `ok`, `client_error`, `rate_limited` (429), `server_error`, `transport_error`. |
| `monaco_upstream_request_duration_seconds` | `service` | Upstream latency. |
| `monaco_poller_ticks_total` | `poller`, `outcome` | `outcome`: `ok`, `error`, `panic`. |
| `monaco_poller_last_tick_timestamp_seconds` | `poller` | Alert when `time() - value` keeps growing. |
| `monaco_poller_tick_duration_seconds` | `poller` | Tick cost. |
| `monaco_relayer_balance_lamports` | | Updated on each `/health` probe round. |
| `monaco_price_breaker_opens_total` | `source` | One per outage, not per failed probe. |
| `monaco_price_fallbacks_total` | `tier` | Holdings valued at cost basis because no live source answered. |
| `monaco_alerts_total` | `kind`, `delivery` | `delivery`: `sent`, `failed`, `dropped`, `suppressed`, `log_only`. |

Plus the standard Go runtime and process collectors.

Starter alert rules:

```
# Money operations failing for our reasons, not the caller's
sum(rate(monaco_money_events_total{outcome="error"}[10m])) by (event) > 0

# Jupiter is rate limiting us
sum(rate(monaco_upstream_requests_total{service="jupiter",outcome="rate_limited"}[5m])) > 0.2

# A poller stopped
time() - monaco_poller_last_tick_timestamp_seconds > 300

# Relayer under 0.005 SOL: top up before the hard floor
monaco_relayer_balance_lamports < 5000000

# Alerts are being raised but not delivered
sum(rate(monaco_alerts_total{delivery=~"failed|dropped"}[15m])) > 0
```

## Logs

Every request gets an `X-Request-Id` (the client's, or one the API mints). It is echoed on the
response, returned as `requestId` in every error body, and stamped as `request_id` on log lines
written with a request context. `http response` lines carry `route`, `status` and `duration_ms`.

Every agent intent writes one service-layer line: `agent intent executed`, `agent intent rejected`
or `agent intent failed`, with `group_id`, `intent_id`, `side`, `symbol`, amounts and the outcome.
The agent key is never logged.

Known gap: service and poller log lines below the HTTP layer do not carry `request_id` yet,
because those helpers do not take a context. Correlate through `group_id`, `deposit_id`, `job_id`
or `tx_signature` until they do.

## iOS app

- Every API request sends a fresh `X-Request-Id`, so a line in Console.app matches a line in the
  API log.
- `os.Logger` category `api` (subsystem = bundle id): failures at error, requests over 2 s at
  notice, the rest at debug. It records method, route template, status or transport category
  (`offline`, `timeout`, `cancelled`, `tls`, `other`), duration and the ids. Never tokens, bodies,
  query values or wallet addresses.
- API errors expose `apiRequestID` and `apiSupportReference` ("ref: 3f9a1c20") for support.
- MetricKit crash, hang, CPU and disk-write diagnostics are written as JSON to
  `Application Support/Diagnostics`, newest 20 kept. They stay on the device: pull them from a
  device container or a sysdiagnose.

Not built: uploading those diagnostics anywhere. The iOS app has no remote crash reporting yet;
adding the Sentry SDK through Xcode's package manager is the next step.

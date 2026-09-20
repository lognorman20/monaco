# Observability: logs, crash reports, metrics, alerts

The Go API moves member money inside one process: HTTP handlers plus three pollers (deposit sweeps, execute-on-pass, cash-out recovery). This page is how to see what it is doing and what should wake someone up.

## Environment

| Variable | Default | What it does |
| --- | --- | --- |
| `APP_ENV` | `local` | `local`, `staging` or `production`. Tags crash reports, picks the log format. Anything else fails boot. |
| `APP_RELEASE` | git revision from `go build` | Release tag on crash reports. Set it in CI when the binary is built without VCS info. |
| `SENTRY_DSN` | empty (off) | Turns on crash and error reporting. One Sentry project (DSN) per environment. |
| `LOG_FORMAT` | `text` for local, `json` otherwise | Override for stderr. |
| `LOG_FILE` | unset | Also append JSON lines to this file (stderr switches to JSON too). |
| `METRICS_ADDR` | `127.0.0.1:9090` | Listener for `GET /metrics`. `off` disables it. |
| `METRICS_TOKEN` | unset | Bearer token for `/metrics`, 24+ chars. Required when `METRICS_ADDR` is not loopback. |
| `DB_MAX_OPEN_CONNS` / `DB_MAX_IDLE_CONNS` | `20` / `10` | Postgres pool caps. |
| `DB_CONN_MAX_LIFETIME` / `DB_CONN_MAX_IDLE_TIME` | `30m` / `5m` | Connection recycling. |

## Where logs land

- The API writes every line to **stderr**. With `APP_ENV=local` it is slog text; in `staging` and `production` it is one JSON object per line with `time` (UTC), `level`, `msg`, `request_id` and the money-path ids (`deposit_id`, `job_id`, `proposal_id`, `tx_signature`, ...).
- stderr is not durable by itself. The host must keep it: systemd/journald, Docker's log driver, or the platform's log drain shipping to your log store. Pick one before the first production deploy and check a line shows up there.
- `LOG_FILE=/var/log/monaco/api.log` adds an appended JSON copy on disk (mode 0600). Rotate it with logrotate `copytruncate`; the API does not rotate.
- Locally nothing changes: `just run` / `just run backend` tee stderr to `.logs/<timestamp>/backend.log`.

## Crash and error reporting (Sentry)

Off until `SENTRY_DSN` is set. When on, three things are reported, tagged with `environment` (`APP_ENV`) and `release`:

1. **Handler panics**: the recovery middleware returns the JSON 500 and reports the panic with its stack, route pattern, method and request id.
2. **Worker panics**: the supervisor (below) reports the panic with its stack and a `worker` tag.
3. **Every error-level log line.** The money paths already log each failure at error level with its ids (`sweep poller tick failed`, `redeem recovery failed`, `redeem job wedged in paying...`, `proposal execute failed`, any 5xx `http response`), so they reach Sentry without per-call wiring. Events group by log message, not by error text.

Before an event leaves the process it is scrubbed: `Authorization`, `Cookie` and `X-Monaco-Agent-Key` values, bearer tokens and Privy JWTs, `wallet-auth:` authorization keys, base58 or byte-array Solana secret keys, PEM private keys, URL passwords and `?api-key=` style query values are replaced with `[redacted]`. Request bodies, headers, IPs and source-code context are never attached. Transaction signatures are kept only under `tx_signature` / `signature` attributes (a signature is public, but is the same shape as a secret key, so anywhere else it is redacted).

Queued reports are flushed (up to 5s) on graceful shutdown and on a fatal boot or server error.

To swap Sentry out, implement `errreport.Reporter` (`apps/backend/internal/errreport`) and return it from `errreport.New`; nothing else imports the SDK.

## Worker supervisor

Each poller runs under `worker.Supervisor`. A panic inside a tick used to kill the whole API, possibly between a Solana submit and the database write recording it, and nothing reported it. Now the panic is recovered, logged (`worker panic`, with stack), reported, and the loop restarts after a backoff of 1s doubling to 1m (reset once a run stays up 5 minutes). Shutdown cancels the backoff wait, so SIGTERM is never delayed by it. Each restart increments `monaco_worker_restarts_total`.

A restart is not a fix: the pollers are idempotent, so the same row will usually panic again. Treat any restart as a page.

## Metrics

`GET /metrics` (Prometheus text format) is served by its **own listener**, never the public API port:

- Default `127.0.0.1:9090`: scrape from the same host, a sidecar, or an SSH tunnel.
- To scrape over the network set `METRICS_ADDR=0.0.0.0:9090` **and** `METRICS_TOKEN`; boot fails if the address is not loopback and the token is missing. Prometheus: `authorization: { credentials: <token> }`. Firewall the port to the scraper anyway.

```bash
curl -s localhost:9090/metrics | grep '^monaco_'
```

| Metric | Type | Meaning |
| --- | --- | --- |
| `monaco_worker_ticks_total{worker,result}` | counter | Ticks by `success` / `failure`. Workers: `sweep_poller`, `proposal_execute_poller`, `redeem_recovery_poller`. |
| `monaco_worker_tick_duration_seconds{worker}` | histogram | Tick duration. |
| `monaco_worker_last_success_timestamp_seconds{worker}` | gauge | Unix time of the last clean tick. |
| `monaco_worker_restarts_total{worker}` | counter | Supervisor restarts (panic or unexpected exit). |
| `monaco_pending_deposits`, `monaco_pending_deposit_oldest_age_seconds` | gauge | Deposits waiting for their sweep; age of the oldest. |
| `monaco_pending_swaps`, `monaco_pending_swap_oldest_age_seconds` | gauge | Treasury swaps still `pending`. |
| `monaco_redeem_jobs{status}`, `monaco_redeem_job_oldest_age_seconds{status}` | gauge | Unsettled cash-out jobs by `debited` / `selling` / `paying`; time since the longest-waiting one last changed status. |
| `monaco_fee_payer_lamports` | gauge | Relayer SOL balance (1 SOL = 1e9 lamports). |
| `monaco_ops_source_up{source}` | gauge | `database` / `solana_rpc`: 0 when the source of the gauges above failed on this scrape (those gauges are then omitted, not zeroed). |
| `monaco_http_requests_total{route,method,status}` | counter | By route pattern (`POST /v1/groups/{id}/fund`), never raw path; unknown paths are `unmatched`. |
| `monaco_http_request_duration_seconds{route,method}` | histogram | Latency. Trade and cash-out routes confirm on Solana in-request, so seconds are normal there. |
| `go_sql_*{db_name="monaco"}`, `go_*`, `process_*` | | `database/sql` pool stats (`go_sql_wait_count_total`, `go_sql_in_use_connections`), Go runtime, process. |

Backlog gauges are read from Postgres and the RPC at scrape time, cached 10s. Faker (demo) users and groups are excluded: their rows never move.

## Alerts that should page

Thresholds are starting points; `N` values are yours to tune once there is a week of data.

| Alert | Rule | Why it pages |
| --- | --- | --- |
| Worker restarted | `increase(monaco_worker_restarts_total[10m]) > 0` | A poller panicked mid-operation. Read the Sentry event, check the row it was on. |
| Worker not ticking | `time() - monaco_worker_last_success_timestamp_seconds > 300` (900 for `redeem_recovery_poller`), or `rate(monaco_worker_ticks_total{result="failure"}[10m]) > 0` with no successes. The timestamp series only exists after a worker's first clean tick, so pair it with `absent(monaco_worker_last_success_timestamp_seconds{worker="sweep_poller"})` for 5m | Alive but moving no money: RPC, Privy or database trouble. |
| Oldest pending deposit | `monaco_pending_deposit_oldest_age_seconds > 600` (N = 10 min) | A member funded a cabal and the sweep has not confirmed. |
| Redeem job stuck in paying | `monaco_redeem_job_oldest_age_seconds{status="paying"} > 900` | Share units are burnt and the member has no USDC. The recovery poller retries from 10 min; if it logs `redeem job wedged in paying`, it needs a person. Also alert on `debited`/`selling` > 1800. |
| Pending swap stuck | `monaco_pending_swap_oldest_age_seconds > 600` | A treasury trade was recorded but never confirmed or failed. |
| Fee payer SOL low | `monaco_fee_payer_lamports < 50000000` (0.05 SOL) | At zero every sweep, swap and payout fails; the API refuses to boot under 0.001 SOL. Top up: see README, Relayer. |
| 5xx rate | `sum(rate(monaco_http_requests_total{status=~"5.."}[5m])) / sum(rate(monaco_http_requests_total[5m])) > 0.02` for 5m | Members are seeing errors. |
| Metrics blind | `monaco_ops_source_up == 0` for 5m, or the scrape target down | The gauges above are missing, so their alerts cannot fire. |
| Health down | `GET /health` returns 503 (`database` or `auth_verifier` down) | Nothing authenticated can be served. `degraded` (200) is a ticket, not a page. |
| DB pool saturated | `rate(go_sql_wait_count_total[5m]) > 0` sustained | Requests queue for connections; raise `DB_MAX_OPEN_CONNS` within the database's limit or find the slow query. |

Route alerts to a channel someone watches (PagerDuty, Opsgenie, a Slack channel with on-call). An alert nobody receives is the same as no alert.

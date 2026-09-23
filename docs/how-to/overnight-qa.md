# Overnight QA

One runner, `scripts/qa/night.sh`, runs the same way on a laptop (`just qa-night`) and in the
nightly GitHub workflow. Per-PR CI builds the app too.

## What runs where

| When | Workflow / job | Runs |
| --- | --- | --- |
| Every PR, push to main | `ci.yml` · `go` | backend, domain and reference bot: vet + test against Postgres |
| Every PR, push to main | `ci.yml` · `swift` | `swift test` in `packages/mobile-core` |
| PRs touching `apps/mobile/**`, `packages/mobile-core/**`, `ci.yml` or the scripts it uses | `ci.yml` · `ios` | app + test `build-for-testing`, `MonacoTests`, sample-screen manifest check |
| Nightly 07:00 UTC (03:00 EDT / 02:00 EST), manual dispatch, PRs touching `nightly.yml` or `scripts/qa/**` | `nightly.yml` · `qa` | `night.sh --screenshots`: backend, domain, mobile-core, app build, `MonacoTests`, each sample UI test class, screenshot gallery |

A Go-only PR skips the `ios` job. Both app builds use the placeholder config below, so CI needs
no secret.

## Running it locally

```sh
just qa-night                     # one round
just qa-night --until 07:30       # loop until 07:30 local time
just qa-night --rounds 4 --skip-backend
just qa-night --only-ui CabalsTabSampleUITests,ProposalFeedSampleUITests
just qa-night --screenshots       # also shoot every sample screen (round 1)
MONACO_QA_IOS_CONFIG=placeholder just qa-night   # what the nightly does
```

| Setting | Default | Meaning |
| --- | --- | --- |
| `MONACO_QA_IOS_CONFIG` | `generate` | `generate`: Dynamic config from `.env.local`. `placeholder`: compile-only config, no environment ID |
| `MONACO_QA_DATABASE_URL` | unset | Postgres for backend tests; unset uses `just test backend` (Docker) |
| `MONACO_QA_SIM` / `--sim` | "Monaco Night QA" | Simulator UDID; without either, `scripts/resolve-ios-sim.sh` picks one |
| `MONACO_QA_SCREENSHOTS` / `--screenshots` | off | Shoot the screenshot gallery |
| `MONACO_QA_CLASS_TIMEOUT` | 1500 s | Watchdog per UI class |
| `MONACO_QA_BOOT_TIMEOUT` | 180 s | Wait for the simulator to report booted |
| `MONACO_QA_MIN_FREE_SWAP_MB` | 256 | Stop starting UI classes below this free swap |

A round runs one heavy job at a time: backend, domain + mobile-core, app build, `MonacoTests`,
then each sample-data UI class on its own with a screen recording and one retry if the test
runner itself was killed (reported as flaky, not failing).

Only sample-data classes run. They need no backend and no sign-in, and they never move money.
The live-login tests in `MonacoUITests/MonacoUITests.swift` are never run: Dynamic has no fixed
test codes, so they cannot pass unattended.

## App config

The app's xcconfig includes a generated, gitignored `apps/mobile/Config/Dynamic.local.xcconfig`
(plus `Dynamic.local.Info.plist`).

| Mode | Command | Result |
| --- | --- | --- |
| `generate` | `scripts/ensure-ios-dynamic-config.sh generate` | Real config from `DYNAMIC_ENVIRONMENT_ID` in `.env.local` |
| `placeholder` | `scripts/ensure-ios-dynamic-config.sh placeholder` | Empty environment ID: builds, sample screens work, sign-in shows "Dynamic not configured" |

`placeholder` refuses to overwrite a real config; delete the file first (`generate` recreates
it). `generate` always replaces a placeholder. Without a config the round records
`ios-config: fail` and skips the app steps.

## Simulator

Locally the runner uses **Monaco Night QA**, slims it with SimSlim and keeps the Mac awake with
`caffeinate`. All three are optional: on a CI runner it logs the simulator
`resolve-ios-sim.sh` picked (newest iOS runtime) and that slimming was skipped.

```sh
udid=$(xcrun simctl create "Monaco Night QA" "iPhone 17" com.apple.CoreSimulator.SimRuntime.iOS-26-3)
simslim on "$udid" --profile scripts/simslim-profile.json
```

Create it on an iOS 18.5+ runtime: on 18.0 SimSlim cannot persist, so the runner re-applies the
profile before every round.

## Screenshot gallery

`scripts/qa/sample-screens.txt` lists every Debug sample-harness screen and its launch flags
(`-Monaco…Sample`, `-MonacoDesignGallery`). `scripts/qa/screens.sh` launches each and saves a
PNG. `screens.sh --check` (run by the `ios` job and by every capture) fails when a harness flag
or scenario in `apps/mobile/Monaco` has no manifest line: add the line when you add a harness.

## Results

| Where | What |
| --- | --- |
| `.logs/qa/<UTC stamp>/report.md` | One row per step, first failing lines, flaky steps |
| `.logs/qa/<UTC stamp>/` | `*.log`, `clips/*.mp4` per UI class, `screens/*.png`, `failures.tsv` |
| Nightly run page | `report.md` in the job summary |
| Nightly artifacts | `nightly-qa-report` (report, logs, recordings), `nightly-qa-screens` (gallery); 14 days |

`night.sh` exits 1 when a step failed without a passing retry.

## Failure alerts

| Nightly result | What happens |
| --- | --- |
| Fails, no open `nightly-failure` issue | Opens one: run link, failing steps, first failing lines of `report.md` |
| Fails, issue already open | Comments on it with the same details |
| Passes, issue open | Comments "Recovered" with the run link and closes it |

Only the scheduled run and manual runs on `main` touch issues; PR runs print what they would do.
The alert job uses `GITHUB_TOKEN` with `contents: read` and `issues: write`. GitHub disables
schedules after 60 days without repository activity; re-enable from the Actions tab.

## One build at a time

`scripts/qa/xcode-lock.sh <command>` holds a machine-wide lock around a build. Wrap any
`xcodebuild` or `swift test` you start by hand (or from an agent) while the overnight run is
going, so two builds never compete for memory:

```sh
scripts/qa/xcode-lock.sh xcodebuild -project apps/mobile/Monaco.xcodeproj -scheme Monaco build
```

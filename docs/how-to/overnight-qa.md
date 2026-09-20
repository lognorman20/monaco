# Overnight QA

`just qa-night` runs every automated check in a loop while nobody is watching and leaves a
report. It is built for a 16 GB Mac: one heavy job at a time, one slimmed simulator, and it
stops starting UI classes when free swap runs low.

```sh
just qa-night                     # one round
just qa-night --until 07:30       # loop until 07:30 local time
just qa-night --rounds 4 --skip-backend
just qa-night --only-ui CabalsTabSampleUITests,ProposeFlowSampleUITests
```

## What a round runs

1. Backend: `go vet` and `go test -p 1 ./...`. Uses `just test backend` unless
   `MONACO_QA_DATABASE_URL` points at a local Postgres test database.
2. `packages/domain` and `packages/mobile-core` host tests.
3. App build, app unit tests, then each sample-data UI test class on its own. Sample classes
   need no backend and no sign-in, and they never move money. Live-login tests are not run.

Each UI class gets a watchdog timeout (`MONACO_QA_CLASS_TIMEOUT`, default 1500 s), a screen
recording in `clips/`, and one retry when the test runner itself was killed. A step that
fails and then passes on retry is reported as flaky, not failing.

## Simulator

The runner uses the simulator named **Monaco Night QA** (or `--sim <udid>` / `MONACO_QA_SIM`).
Create it once on an iOS 18.5+ runtime so SimSlim's settings persist across reboots:

```sh
udid=$(xcrun simctl create "Monaco Night QA" "iPhone 17" com.apple.CoreSimulator.SimRuntime.iOS-26-3)
simslim on "$udid" --profile scripts/simslim-profile.json
```

On iOS 18.0 SimSlim cannot persist; the runner re-applies the profile before every round.

## One build at a time

`scripts/qa/xcode-lock.sh <command>` holds a machine-wide lock around a build. Wrap any
`xcodebuild` you start by hand (or from an agent) while the overnight run is going, so two
builds never compete for memory:

```sh
scripts/qa/xcode-lock.sh xcodebuild -project apps/mobile/Monaco.xcodeproj -scheme Monaco build
```

## Reading the result

`.logs/qa/<UTC timestamp>/report.md` has one row per step per round, the first failing lines
of each failed log, and the flaky steps. The command exits 1 when a step failed without a
passing retry, so it can gate a morning merge.

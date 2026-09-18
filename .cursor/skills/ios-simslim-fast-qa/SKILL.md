---
name: ios-simslim-fast-qa
description: >-
  Fast-pass iOS simulator QA loop with SimSlim (RAM-thin simulators) and MobAI
  (tap/observe automation). Covers repo discovery, slim profiles, build/install,
  claim/bridge/drive/release. Use when the user wants fast iOS QA, sim smoke
  tests, agent-driven tap-through, SimSlim, MobAI, or slim-simulator QA — not
  full device farms or multi-sim regression at scale.
---

# iOS SimSlim Fast QA

Repo-agnostic fast smoke loop: **one slim simulator**, **one MobAI claim**, tight iterate cycle.

| Layer | Job | Not its job |
| --- | --- | --- |
| SimSlim | Disable unused sim daemons; verify/doctor | Tap UI, build, screenshot |
| MobAI + mobai-mcp | claim, start_bridge, execute_dsl, install_app | Compile Swift; slim RAM |
| xcodebuild / XcodeBuildMCP | Build, install, optional snapshot-ui | Cut sim RAM or manage fleet |
| Agent | discover → profile → build → drive → fix | Stock sim forever; parallel fleet |

**Default:** 1 sim (~0.9 GB slim vs ~4 GB stock). **Rare:** 2–3 sims (clone gold after slim-once). No 12-sim farm.

---

## When to use

- User asks for fast iOS QA, sim smoke, agent tap-through, SimSlim, or MobAI.
- Day-1 smoke after a feature: launch, primary nav, one critical path — not full regression.
- Unit tests first (no sim). Expand tiers only after smoke is green.

**Skip this skill** when user needs true parallel regression at scale, Pro-first multi-agent fleet, or web automation (not available on iOS sim).

---

## Machine setup (once)

### SimSlim

```bash
brew install mobai-app/tap/simslim
```

### MobAI desktop

Run MobAI desktop app. API at `127.0.0.1:8686`.

### MCP (user or repo `.cursor/mcp.json`)

```json
{
  "mcpServers": {
    "mobai": {
      "command": "npx",
      "args": ["-y", "mobai-mcp"]
    }
  }
}
```

Add XcodeBuildMCP only if the repo already wires it — optional, not required.

### Gold simulator (once per machine, slow)

Pick one iOS **18.5+** runtime (slim persist minimum). Boot it, note UDID:

```bash
xcrun simctl list devices available
```

Slim it once with base profile:

```bash
simslim on <GOLD_UDID> --profile ci/profiles/base-slim.json --json
```

Keep gold **booted between agent sessions** when possible. For 2–3 sims: clone gold after slim-once (clone inherits slim + apps).

**Monaco:** UDID is **per machine**. Never copy a UUID from this repo or another laptop. After slim-once, `export SIMSLIM_UDID=<udid>` (shell rc and/or plain `.env`). `scripts/gold-sim-udid.sh` prints it or exits 1. `just build mobile` / `just reset mobile` / `./scripts/ios-sim` use `scripts/resolve-ios-sim.sh` (stock fallback). `scripts/stop-mobile.sh` uninstalls `com.monaco.app` on every available sim. XcodeBuildMCP: `--simulator-id "$SIMSLIM_UDID"`. Stock Simulator without slim: README **Without slim sim**.

**Every session** before driving UI:

```bash
simslim verify <UDID> --profile ci/profiles/<profile>.json \
  || simslim on <UDID> --profile ci/profiles/<profile>.json
simslim doctor --requires ci/profiles/<profile>.json
```

`except` in profile = **KEEP** that daemon category ON (less slim, more capability).

---

## Repo discovery

Run before first build. Works on any Swift/UIKit/SwiftUI app.

| Target | How | Why |
| --- | --- | --- |
| Scheme | `xcodebuild -list -json` — pick app scheme, not test-only | Build target |
| Bundle ID | `xcodebuild -showBuildSettings -scheme <SCHEME> \| grep PRODUCT_BUNDLE_IDENTIFIER` or Info.plist | `open_app` / `install_app` |
| Deploy target | `IPHONEOS_DEPLOYMENT_TARGET` in pbxproj or build settings | Must fit sim runtime; slim persist needs iOS 18.5+ |
| Entitlements | `*.entitlements`, Signing & Capabilities in pbxproj | Profile except/keep map |
| Info.plist | `*UsageDescription*`, URL schemes, SKAdNetwork | Photos, camera, location, tracking |
| Backends | README, `.env.example`, localhost URLs in code | Start sidecar before UI tier |

```bash
xcodebuild -list -json
xcodebuild -scheme <SCHEME> -showBuildSettings 2>/dev/null | grep -E 'PRODUCT_BUNDLE_IDENTIFIER|IPHONEOS_DEPLOYMENT_TARGET'
```

---

## Capability → profile map

Start from **base-slim** (maximum cut). Add `except` / `keep` only for QA paths that touch that subsystem.

**Rule:** `except: ["photos"]` means keep the **photos** category ON.

| Signal in repo | Profile tweak | Category |
| --- | --- | --- |
| SwiftUI tabs only, no system pickers | `except: []` | Full slim |
| PHPicker / Photos / camera | `except: ["photos"]` | photos |
| Push notifications QA | `keep: ["apsd"]` or `except: ["push"]` | push |
| StoreKit / IAP | `keep: ["storekitd"]` | storekit |
| Universal links / handoff | `keep: ["swcd"]` | links |
| HealthKit | `except: ["health"]` | health |
| Location / maps | `except: ["location"]` | location |
| iCloud / CloudKit | `except: ["icloud"]` | icloud |

Example `ci/profiles/base-slim.json`:

```json
{
  "except": []
}
```

Example `ci/profiles/photos-qa.json`:

```json
{
  "except": ["photos"]
}
```

Re-pick profile when entitlements or smoke scope changes.

---

## Inner loop

```
discover → profile → verify||on → build → install → claim → start_bridge → drive → report → (fix) → loop
```

### Checklist (copy per iteration)

```
- [ ] Discover scheme, bundle ID, entitlements, deploy target
- [ ] Pick slim profile; verify || on; doctor
- [ ] Build .app
- [ ] Install on claimed sim UDID
- [ ] MobAI: claim → start_bridge → open_app → execute_dsl
- [ ] On fail: save_screenshot + ui_tree; fix code; loop
- [ ] release_device when done
```

### Build + install (xcodebuild — always available)

```bash
xcodebuild -scheme <SCHEME> \
  -destination 'platform=iOS Simulator,id=<UDID>' \
  -derivedDataPath /tmp/dd-<SCHEME> \
  build

xcrun simctl install <UDID> \
  /tmp/dd-<SCHEME>/Build/Products/Debug-iphonesimulator/<App>.app
```

### Build + install (XcodeBuildMCP — if present)

Use same UDID MobAI will claim. `build-and-run`, `snapshot-ui`, `screenshot` — optional shortcuts, not assumed.

---

## MobAI protocol

**Read first:** fetch MCP resource `mobai://reference/device-automation` (and testing docs if driving flows).

| Step | Action |
| --- | --- |
| Reserve | `list_devices` → `claim_device(holder=<task-id>)` |
| Bridge | `start_bridge` — iOS cold start ~60s; wait for ready |
| Fresh launch | `open_app` with `bundleId`, `fresh=true` |
| Drive | `execute_dsl` batch or replay `mobai/flows/*.mob` if repo has them |
| Fail | `save_screenshot` + `ui_tree`; `debug_attach` if crash |
| Done | `release_device` |

**Concurrency:** MobAI Free = **1 concurrent device** — enough for default loop. For 2–3 flows on Free: **sequential** claim → drive → release per task. Pro allows 2–3+ parallel on separate sims.

Example DSL sketch (adapt to actual MobAI schema):

```json
{
  "steps": [
    { "action": "tap", "label": "Home" },
    { "action": "wait", "ms": 500 },
    { "action": "assert_visible", "label": "Settings" }
  ]
}
```

---

## Day-1 smoke (not full suite)

Three checks on the one slim sim:

| Check | Pass |
| --- | --- |
| Launch | App opens, splash dismisses, no crash |
| Primary nav | Tabs / sidebar / root flows reachable |
| One critical path | Single highest-risk user flow end-to-end |

Write or replay flows under `mobai/flows/*.mob` only when the repo already uses them — do not require per-repo artifacts to run this skill.

---

## Per-repo artifacts (optional)

| Path | Purpose |
| --- | --- |
| `ci/profiles/*.json` | Capability-specific slim profiles |
| `.cursor/mcp.json` | mobai-mcp (+ optional XcodeBuildMCP) |
| `mobai/flows/*.mob` | Replayable smokes for this app |
| `.cursor/skills/<app>-qa/SKILL.md` | App-specific scheme, bundle, backends, smoke tiers |

Skill is **global**. Per-repo files speed repeat runs; absence is fine — discover each time.

---

## Gotchas

| Issue | Mitigation |
| --- | --- |
| `simslim erase` / recreate sim | Wipes slim; re-run `simslim on` on gold |
| Runtime below iOS 18.5 | Slim does not persist across reboot |
| `start_bridge` cold | Budget ~60s before first tap |
| No launch args for test state | Seed data via backend or debug menu; document blocker |
| Missing accessibility ids | Add ids or use label/text taps; fragile |
| PHPicker / system pickers | Use `except: ["photos"]`; PhotosPicker hard to automate |
| Backend not running | Start sidecar before UI smoke |
| Web automation on iOS sim | Not supported — native MobAI DSL only |

---

## Agent output format

After a smoke pass, report:

```markdown
## iOS fast QA — <scheme>

- Sim: <name> (<UDID>), profile: <profile>.json
- Build: pass | fail
- Smoke: launch / nav / critical path — pass | fail | blocked
- Blockers: [list or none]
- Screenshot: [MobAI save path or upload if user needs link]
```

On fail: screenshot + ui_tree summary, one concrete fix, then re-loop — do not claim a second sim on Free tier.

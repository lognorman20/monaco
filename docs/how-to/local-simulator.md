# Running Monaco on a local iOS Simulator

## Signing

`scripts/ios-build` and `scripts/ios-sim` (used by `just build mobile` and
`just run mobile`) build the Debug simulator app with:

```
CODE_SIGN_IDENTITY="-" CODE_SIGNING_REQUIRED=NO CODE_SIGNING_ALLOWED=YES
```

This is **ad-hoc signing** ("sign to run locally") — it self-signs the app
without needing the project's own Apple Developer team certificate
(`DEVELOPMENT_TEAM = JSF53DFS29` in `apps/mobile/Monaco.xcodeproj`), so the
build works on any contributor's machine regardless of whether they have
that team's signing identity installed.

**Do not build the simulator app with `CODE_SIGNING_ALLOWED=NO`.** That skips
codesigning entirely and produces a fully unsigned binary. iOS ties a Keychain
item's access group to the signature of the app that created it — with no
signature, the app can't consistently reclaim its own keychain entries, so
the Dynamic session gets silently dropped on every relaunch or reinstall (the
user looks signed out for no reason). Ad-hoc signing (`CODE_SIGN_IDENTITY="-"`)
still produces a real, consistent signature, so the keychain entry persists
across relaunches and reinstalls, while `CODE_SIGNING_REQUIRED=NO` keeps the
build from failing if a stricter signing requirement is inherited from the
project or environment.

Some older QA docs (`docs/qa/148`, `docs/qa/160`) used to show
`CODE_SIGNING_ALLOWED=NO` in their reproduction commands — that's a copy/paste
trap for exactly this bug, and both have been updated to the ad-hoc flags
above. If you're pasting an `xcodebuild` command from an old doc or PR,
double check it doesn't carry `CODE_SIGNING_ALLOWED=NO` forward.

## SimSlim profiles

`scripts/simslim-profile.json` is Monaco's SimSlim capability profile
(see `.cursor/skills/ios-simslim-fast-qa`). `except` lists the daemon
categories that stay **on** (i.e. *not* slimmed away):

```json
{
  "except": ["icloud", "web", "messaging", "store", "telemetry", "photos"]
}
```

`photos` must stay in `except`. Slimming it away disables the photo-picker
daemons the profile-photo flow (`PHPicker`) depends on, which breaks that QA
path in a way that looks unrelated to SimSlim. If you create a narrower
profile for a specific QA pass, keep `photos` in its `except` list too unless
you're specifically testing the no-photos degraded path.

### Runtime requirement

SimSlim settings only **persist** across a simulator reboot on **iOS 18.5+**
or **iOS 26.x** runtimes. An **iOS 18.0** runtime can be slimmed for the
current boot, but the settings do not survive a reboot or `simslim erase`, so
you'll silently fall back to a stock (unslimmed, higher-RAM) simulator after a
restart. Use an 18.5+ or 26.x runtime for your gold simulator.

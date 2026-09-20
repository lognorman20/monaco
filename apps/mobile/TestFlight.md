# M5-T25 — TestFlight internal distribution

Optional upload path for hackathon demo builds. Requires Apple Developer account with App Store Connect access.

## Prerequisites

- Xcode signed in with team that owns bundle ID `com.monaco.app`
- Privy iOS client includes `com.monaco.app` (OTP `sendCode` otherwise returns 403)
- Wave 4 green: `just test mobile` and `just build mobile`
- A reachable **https** backend for the archive's environment (next section). A Release build cannot use `localhost` or `http`

## Pick the API environment

Archives are Release builds, and Release defaults to `MONACO_ENVIRONMENT = production` (`Config/Monaco.xcconfig`). The staging and production URLs are **placeholders, empty in git**, because no hosted backend is committed to this repo. Until you set one, the archive builds but refuses to launch with `MONACO_API_BASE_URL is empty for the selected environment`.

1. Set the URL once (not a secret; must be `https://`, a tunnel URL is fine for a demo build):

   ```bash
   dotenvx set MONACO_STAGING_API_BASE_URL https://<staging-or-tunnel-host> -f .env.local --plain
   dotenvx set MONACO_PRODUCTION_API_BASE_URL https://<production-host> -f .env.local --plain
   ```

2. Regenerate the gitignored config (`just build mobile` does this too): `./scripts/ensure-ios-privy-config.sh generate`. It writes `Config/Environment.local.xcconfig` and fails on a non-https value.
3. Archive. Production is the default, so the Xcode **Product → Archive** path always targets production. For a staging archive use the CLI alternative below and add `MONACO_ENVIRONMENT=staging` to the `xcodebuild archive` command.
4. Check what went into the archive before uploading:

   ```bash
   plutil -p /tmp/Monaco.xcarchive/Products/Applications/Monaco.app/Info.plist | grep MONACO_
   ```

Release rejects at launch: an empty or malformed URL, `http`, `localhost` / loopback / `.local` hosts, and the `local` environment. Release also ignores `SIMCTL_CHILD_MONACO_API_BASE_URL`; that override is Debug-only.

## Build archive

From repo root:

```bash
just test mobile
just build mobile
```

In Xcode (`apps/mobile/Monaco.xcodeproj`):

1. Scheme **Monaco**, destination **Any iOS Device (arm64)**
2. **Product → Archive**
3. Organizer → **Distribute App → TestFlight & App Store → Upload**
4. Keep default bitcode/symbol settings; wait for processing in App Store Connect

CLI alternative (same signing team as Xcode):

```bash
xcodebuild archive \
  -project apps/mobile/Monaco.xcodeproj \
  -scheme Monaco \
  -archivePath /tmp/Monaco.xcarchive

xcodebuild -exportArchive \
  -archivePath /tmp/Monaco.xcarchive \
  -exportOptionsPlist apps/mobile/ExportOptions-testflight.plist \
  -exportPath /tmp/Monaco-export
```

## Invite internal testers

1. App Store Connect → **Monaco** → **TestFlight**
2. Select the uploaded build after processing completes
3. **Internal Testing** group → add team members by Apple ID email
4. Share the TestFlight invite link; testers install via TestFlight app

## Smoke after install

Follow [`docs/m5-demo-script.md`](../../docs/m5-demo-script.md) on a physical device. The backend at the archive's `MONACO_API_BASE_URL` must be reachable over https for live JSON flows.

## Done when

- [ ] Build uploaded and processed in TestFlight
- [ ] At least one internal tester invited and can install
- [ ] `just test mobile` still exits 0 on the release branch

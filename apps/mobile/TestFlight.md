# M5-T25 — TestFlight internal distribution

Optional upload path for hackathon demo builds. Requires Apple Developer account with App Store Connect access.

## Prerequisites

- Xcode signed in with team that owns bundle ID `com.monaco.app`
- Privy iOS client includes `com.monaco.app` (OTP `sendCode` otherwise returns 403)
- Wave 4 green: `just test mobile` and `just build mobile` on this machine’s gold slim sim (`SIMSLIM_UDID`)

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

Follow [`docs/m5-demo-script.md`](../../docs/m5-demo-script.md) on a physical device. Backend must be reachable (staging or tunnel) for live JSON flows.

## Done when

- [ ] Build uploaded and processed in TestFlight
- [ ] At least one internal tester invited and can install
- [ ] `just test mobile` still exits 0 on the release branch

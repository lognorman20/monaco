# #160 Profile — QA evidence

Screenshots come from the Debug-only sample harness, which renders the real `ProfileTabView` from canned `AppSessionStore` data. There is no Privy session and no backend. Build and launch on a simulator:

```bash
dotenvx run -q -f .env.local -- bash scripts/ensure-ios-privy-config.sh
xcodebuild -project apps/mobile/Monaco.xcodeproj -scheme Monaco -configuration Debug \
  -destination 'platform=iOS Simulator,id=<sim udid>' -derivedDataPath <dd> \
  CODE_SIGN_IDENTITY="-" CODE_SIGNING_REQUIRED=NO CODE_SIGNING_ALLOWED=YES build
xcrun simctl install <sim udid> <dd>/Build/Products/Debug-iphonesimulator/Monaco.app
xcrun simctl launch <sim udid> com.monaco.app -MonacoProfileSample <scenario>
```

| Scenario | File | Shows |
| --- | --- | --- |
| `placeholder` | `profile-placeholder.png` | Initials avatar, name, member since, balance, deposit address wrapped without a hyphen |
| `photo` | `profile-photo.png`, `profile-photo-dark.png` | Photo in the avatar (a generated image loaded from a file URL), light and dark |
| `validation` | `profile-validation.png` | 37-character draft: red count and inline "32 characters or fewer" error, Save disabled |
| `cabals` | `profile-cabals.png` | Scrolled to the cabal list: pot, your position, dollar P&L with percent (a loss shows -3.6%), and a cabal where you hold no stake |
| `empty` | `profile-empty.png` | Scrolled to the empty cabal state |
| `loading` | `profile-loading.png` | First load |
| `error` | `profile-error.png` | Session failed to load, with Try again |

The camera badge looks dimmed in these shots because the harness has no sign-in token, so the picker is disabled. `cabals` and `empty` open scrolled to the bottom; the navigation bar is transparent app-wide, so content shows under the title.

## Automated coverage

- Backend: `internal/httpapi/me_patch_test.go` (PATCH /v1/me contract, validation, malformed and oversized bodies, 401, 429, session shape, photo upload limit and size, and the renamed user showing with their photo on the member board, `/v1/home` people board, and dashboard leaderboard), `internal/app/display_name_test.go`, `internal/postgres/users_profile_test.go`, `internal/ratelimit/ratelimit_test.go`.
- mobile-core: `ProfileTests.swift` (PATCH and multipart request encoding, error mapping, MeDTO decoding, name rules, initials, member-since).

## Not verified here

- A real photo upload from the simulator to Supabase Storage and a real rename against a live backend. Both need a signed-in Privy session on the shared backend. Manual check: sign in, open Profile, change the name and photo, then confirm Home leaderboard and a cabal member board show both after refresh, and that they survive sign out and sign in.

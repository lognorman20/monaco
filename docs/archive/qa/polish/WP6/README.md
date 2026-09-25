# WP6 brand, login and seed: acceptance

| File | Shows |
|---|---|
| `springboard-light.png`, `springboard-dark.png` | App icon on the home screen (iOS 18 simulator) |
| `icon-master-light.png` | 1024² icon master, rendered by `scripts/design/render-app-icon.swift` |
| `login-light.png`, `login-dark.png` | Login: 20pt gutters, live `MonacoMark`, wordmark, headline, sunken fields, full-width CTA |
| `faker-demo-output.json` | `just faker demo <group_id> <proposal_id>` against a local database (6 messages, 3 comments) |

Regenerate the icon and launch mark: `swift scripts/design/render-app-icon.swift`.

Launch screen: `LaunchScreen.storyboard` (canvas `LaunchBackground` colour, 96pt `LaunchMark`, light and
dark variants). The plan's `INFOPLIST_KEY_UILaunchScreen_BackgroundColor` / `_ImageName` build settings
are not honoured by Xcode's generated Info.plist (the built `UILaunchScreen` dict came out empty), so the
launch screen is a storyboard referenced by `INFOPLIST_KEY_UILaunchStoryboardName`. The launch frame is
on screen for well under a frame burst on the simulator, so it is not captured here; the burst shows the
canvas colour fading in before login.

Seed tests (`bt.sh`, dedicated database): `go test ./internal/faker/... ./internal/httpapi/...` pass; the
full backend suite passes except `TestOpenTestDB_connectsToDerivedDatabase` (known, environment).

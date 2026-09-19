# #148 Cabals tab: QA evidence

Simulator: iPhone SE (3rd generation), iOS 18.0, slimmed with SimSlim. Debug build.

## How to reproduce

The Debug-only launch argument `-MonacoCabalsTabSample` opens the Cabals tab on built-in sample data. It skips sign-in and needs no backend (`apps/mobile/Monaco/Features/Debug/CabalsTabSampleData.swift`). Release builds don't compile it.

```bash
xcodebuild -project apps/mobile/Monaco.xcodeproj -scheme Monaco -configuration Debug \
  -destination 'platform=iOS Simulator,id=<sim udid>' CODE_SIGNING_ALLOWED=NO \
  test -only-testing:MonacoTests -only-testing:MonacoUITests/CabalsTabSampleUITests
```

Screenshots 01 to 05 are XCTAttachments exported from that run. Screenshot 06 was taken with `xcrun simctl io ... screenshot` after `xcrun simctl ui ... appearance dark`.

## Screenshots

| File | Shows |
| --- | --- |
| `01-overview.png` | Search field, P&L chart with one line per joined cabal (tone and dash pattern tell the lines apart; green and red appear only on P&L figures), 1D/1W/1M/3M range chips, legend with the latest P&L, and the start of the "Your cabals" strip |
| `02-leaderboard.png` | Platform board: rank, name, member count with join status (Open, Approval, Joined), pot value, dollar P&L, and percent return |
| `03-search-results.png` | Search for "week" after the 300ms debounce: a joined cabal and an open cabal |
| `04-search-empty.png` | Empty state for a query with no match |
| `05-join-approval.png` | Tapping an approval-only cabal opens the ask-to-join screen with the cabal's name |
| `06-overview-dark.png` | Overview in dark mode |

## Results

- `CabalsTabSampleUITests`: 7 of 7 passed. Covered: overview (chart, 3 legend rows, strip, board), board rows after scrolling, search results, empty search, too-short query hint, ask-to-join routing, and a strip card pushing the cabal screen.
- `MonacoTests` (`CabalsTabModelTests`): 7 of 7 passed. Covered: debounce sends only the last query, a single character never hits the server, clearing cancels a pending search, empty and error states, 401 signs out, and a range change reloads the chart.

## Not covered here

- Live backend data. The server contract is covered by Go handler tests in `apps/backend/internal/httpapi/groups_tab_test.go`.
- The join submit button. The harness has no backend, so the tests stop at the join screen.
- The navigation bar is transparent app-wide. Content that scrolls under the title shows through it (visible at the top of `02-leaderboard.png`). This was already the case before this change.

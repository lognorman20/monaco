# WP5 Discovery — acceptance screenshots

Captured from `-MonacoCabalsTabSample` (no backend, fixed sample data) on the
WP5 simulator, plus `xcresult` attachments from `CabalsTabSampleUITests`.

- `cabals-overview-light.png` / `cabals-overview-dark.png` — Cabals tab: search, tinted "Your cabals" strip, P&L chart.
- `cabals-overview-axl.png` — same screen at AX-XL Dynamic Type.
- `cabals-leaderboard-light.png` — "Top cabals" scrolled into view: ranked rows with a CabalMark + rank badge, percent-first trailing figures.
- `cabals-search-results-light.png` — search for "week": two matches in the same row style as the leaderboard.
- `cabals-search-empty-light.png` — search for "zzzz": no-icon empty state ("No cabal called "zzzz".").
- `join-cabal-request-light.png` — tapping an approval-required search result ("Tesla or bust") opens Ask to join.

Not captured here (need a signed-in session / backend, no debug harness for
these screens): Stocks list/detail, Create cabal preview, Join (open).
Verified by build + `CabalsTabModelTests` + `CabalsTabSampleUITests` instead.

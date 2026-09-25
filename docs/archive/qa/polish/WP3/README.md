# WP3 acceptance screenshots: Group detail, Cash out, Chat, Transaction detail

Taken on an iPhone SE-class simulator (375×667 pt, iOS 18.0) from the Debug sample harnesses. None of these screens need sign-in or a backend.

| File | Launch arguments |
|---|---|
| `populated-{light,dark}.png` | `-MonacoGroupDetailSample populated` |
| `empty-{light,dark}.png` | `-MonacoGroupDetailSample empty` |
| `loading-{light,dark}.png` | `-MonacoGroupDetailSample loading` |
| `details-{light,dark}.png` | `-MonacoGroupDetailSample details` (Cabal details sheet) |
| `propose-{light,dark}.png` | `-MonacoGroupDetailSample propose` (chooser sheet over the group screen) |
| `cashOut-{light,dark}.png` | `-MonacoGroupDetailSample cashOut` |
| `receipt-{light,dark}.png`, `receiptFailed-{light,dark}.png` | `-MonacoGroupDetailSample receipt` / `receiptFailed` |
| `activity-{light,dark}.png` | `-MonacoGroupDetailSample activity` (full list from "See all") |
| `populated-scrolled-light.png` | populated, scrolled to holdings and the leaderboard |
| `populated-axl.png` | populated at AX-L (`xcrun simctl ui <sim> content_size extra-extra-large`) |
| `chat-{light,dark}.png`, `chat-empty-{light,dark}.png` | `-MonacoChatSampleQA` [`-MonacoChatSampleEmpty`] |

Dark mode: `xcrun simctl ui <sim> appearance dark`.

The proposal cards inside "Needs your vote" and the chooser sheet belong to WP4 and show their pre-WP4 styling here.

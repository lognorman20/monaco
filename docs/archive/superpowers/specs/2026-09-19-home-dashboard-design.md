# Home dashboard design (#157)

**Date:** 2026-09-19
**Issue:** [#157](https://github.com/lognorman20/monaco/issues/157)
**Depends on:** five-tab Orbix shell (#161 / PR #197)

Home is the first post-auth screen. It should answer four questions without leaving the tab: how much of me is in cabals, how those cabals are doing, who is winning, and what I still need to vote on.

Do not re-home cabal discovery here. The Cabals tab already lists every cabal (joined and not). Home shows the viewer’s money and the social board.

## Visual

Reuse #161 tokens. Do not invent a second palette.

| Token | Role on Home |
| --- | --- |
| Paper `#FDFCFB` + 128pt kiln wash `#FAE3CC` | Screen canvas |
| Avenir Next Bold 28 | Net worth figure |
| Ink `#1C1814` | Headings, ranks |
| Kiln `#C45C26` | Chart stroke, “Vote” |
| Success / destructive | Signed P&L only |
| `MonacoChip` | Leaderboard ranges |
| `MonacoRowCard` | Positions, people, votes |
| `MonacoHeroHeader` | Net worth (not a boxed card) |

Spend the boldness on the number sitting in the wash. Do not wrap the whole page in nested surface cards (that is what `feat/157-home-tab` did). Sections are a caption + rows.

```
┌─────────────────────────────────┐
│  (peach wash, 128pt)            │
│                                 │
│  Your cabals                    │
│  $1,248.50                      │
│  +$48.20              +4.0%     │
│                                 │
│  Account $12.00     [Deposit]   │  ← idle USDC; not net worth
│                                 │
│  Your positions                 │
│  ┌───────────────────────────┐  │
│  │ Brainers                  │  │
│  │ $3.35 · 12% of pot        │  │
│  │              +$0.10  +3.1%│  │
│  └───────────────────────────┘  │
│                                 │
│  P&L · last hour                │
│  [kiln area chart, 160pt]       │
│                                 │
│  Leaderboard                    │
│  [1H] [1D] [1W] [1M] [All]      │
│  Alfred           +12.4%  +$48  │
│                                 │
│  Needs your vote                │
│  Brainers · Apple               │
│  Vote by 4:00 PM                │
└─────────────────────────────────┘
```

Empty copy (cabal, never group/club/NAV/xStock):

- Positions: `Join a cabal to see your positions here.`
- Chart: `P&L history shows up after you fund a cabal.`
- Leaderboard: `Join a cabal to see members on the leaderboard.`
- Votes: `You're caught up.`

Toolbar plus menu (Create cabal / Join cabal) stays on Home so an empty dashboard still has an action. Cabals tab keeps the same menu.

## Money model

Two different piles. Keep them apart.

| Label | Meaning | Source |
| --- | --- | --- |
| **Your cabals** (net worth) | Sum of viewer equity across joined cabals: `(shares / total shares) × pot NAV` | `GET /v1/home/dashboard` |
| **Account** | Idle USDC in the Privy member wallet minus in-flight fund jobs | `GET /v1/me/balance` (already on `AppSessionStore`) |

Do not add account USDC into net worth. Product copy already calls that pile “account balance”; mixing it into “your cabals” hides how much is still undeployed.

Per-cabal row uses viewer equity, slice of that pot, dollar P&L, percent return vs net USDC in **that** cabal. Not pot NAV (that stays on the Cabals tab).

Lifetime percent on the hero: `sum(equity) / sum(net USDC in) − 1`. Skip the percent when net USDC in is 0.

## API

Keep `GET /v1/home` as the Cabals-tab discovery board (all cabals, pot P&L, people unused by that tab). Add dashboard routes. Do not stuff chart + missed votes into the discovery payload.

Port and restyle the already-merged-on-`oh-yea` contract from `origin/feat/157-home-tab` onto current main + #161. Do not merge `oh-yea`.

```
GET /v1/home/dashboard?leaderboardRange=ALL|1H|1D|1W|1M
GET /v1/home/pnl-series?range=1H|1D|1W|1M
GET /v1/home/missed-proposals
```

Dashboard JSON:

```
{
  "netWorthUsd": "1248.50",
  "netWorthDollarPnl": "+48.20",
  "netWorthPercentReturn": "0.040",
  "myGroups": [
    {
      "groupId": "...",
      "name": "Brainers",
      "equityUsd": "3.35",
      "slicePercent": "0.12",
      "dollarPnl": "+0.10",
      "percentReturn": "0.031"
    }
  ],
  "pnlSeries1H": [{ "ts": "...", "equityUsd": "...", "dollarPnl": "..." }],
  "leaderboard": { "range": "ALL", "people": [{ "userId", "displayName", "percentReturn", "dollarPnl" }] },
  "missedProposals": [
    {
      "groupId", "groupName", "proposalId", "symbol",
      "status", "createdAt", "expiresAt"
    }
  ]
}
```

JSON keys stay `groupId` / `myGroups` (API types are `groups`). UI copy is cabal.

Default leaderboard range is `ALL` (today’s people board). Changing a chip refetches `GET /v1/home/dashboard?leaderboardRange=`. Chart stays 1H on first paint; `pnl-series` exists if we later add chart chips (out of v1).

Missed list: open proposals in viewer’s cabals with no `proposal_votes` row for the viewer, newest first, cap 20. Tap → existing `ProposalDetailView(proposalId:)`. After a successful vote, drop that row locally and refresh dashboard.

## Time series and ranged leaderboard

`nav_snapshots` today write only on deposit, swap confirm, and redeem. There is no hourly job.

v1 replay:

1. Load snapshots for the viewer’s cabals in the window.
2. At each distinct snapshot timestamp, equity = current share units × that snapshot’s NAV per share, summed across cabals.
3. Always append **now** using live marked equity so a funded user with one deposit snapshot has two points (meets the “at least two points” AC).
4. Dollar P&L on a point = equity − **current** net USDC in (same as `feat/157-home-tab`). Document: deposits mid-window make the left side of the chart look worse; do not invent historical net-in.

Empty chart when the viewer has no snapshots and no live equity.

Ranged people board (1H/1D/1W/1M): percent return = `equity_now / equity_at_or_before(window start) − 1`. Rank by that percent. Skip people with no snapshot at window start or zero start equity. `ALL` reuses today’s lifetime people board (`equity / net USDC in − 1`).

Accuracy limits (write in the handler comment, not the UI): window math uses **current** share units against a historical NAV snapshot, so a mid-window fund or redeem is approximate. Good enough for v1; no daily rollup table.

No periodic snapshot worker in this ticket. Event snapshots plus the live “now” point satisfy the chart AC without extra Pyth/Privy load every 15 minutes.

## Mobile structure

| File | Job |
| --- | --- |
| `AppSessionStore` | Keep `home` (Cabals tab) + `platformBalance`. Add `dashboard`. Refresh all three in parallel. Leaderboard range lives on Home, not the store, so chip changes do not rebuild Cabals. |
| `HomeView` | Vertical `ScrollView` of the five sections. Drop the Cabals/People `Picker`. |
| `HomeNetWorthSection` | Hero + compact account row + Deposit. |
| `HomePositionsSection` | Joined cabals with viewer P&L → `GroupDetailView`. |
| `HomePnLChartSection` | Swift Charts area/line. |
| `HomeLeaderboardSection` | Chips + people rows → `UserProfileGroupsView`. |
| `HomeMissedVotesSection` | Inbox → `ProposalDetailView`. |
| `packages/mobile-core` `HomeDashboardDTO` | Decode + copy-audit strings. |

Ignore `URLError.cancelled` the same way proposals already do. Do not put dashboard fetch in a child `.task` that dies when the tab is not selected if `AppSessionStore.refresh` already loaded it.

Formatters: `UsdAmountFormatter`, `PercentReturnFormatter`, `CatalogAssetNameFormatter` / `AssetSymbolFormatter` (never mint, never “xStock”).

## Out of scope

- Cabals tab search/charts (#148)
- Assets tab holdings (#156)
- Including account USDC in net worth
- Periodic NAV worker
- Chart range chips
- Changing `GET /v1/home` shape (Cabals tab still needs it)

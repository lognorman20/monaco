# Demo polish on main: reconciled screens

Captured from the Debug harnesses on the rebased `feat/polish` branch (iPhone SE 3rd gen sim, light mode).

| File | Harness | What to check |
|---|---|---|
| `home-corner-avatar.png` | `-MonacoHomeSample populated` | Profile photo in the Home corner; hero, balance row, needs-your-vote, cabals |
| `group-hero.png` | `-MonacoGroupDetailSample populated` | Tinted hero with the pot, round actions (Add money, Propose, Cash out, Chat) |
| `group-activity.png` | same, scrolled | Latest five transactions with "See all", retry on a failed sell |
| `group-details-sheet.png` | `-MonacoGroupDetailSample details` | Cabal account address, Solscan, invite code, Leave |
| `profile-account.png` | `-MonacoProfileSample cabals` | Balance card (Add money / Cash out), cabals, Account section (Advanced, Sign out) from #210 |
| `proposal-feed.png` | `-MonacoProposalFeedSample` | Card with thesis excerpt, vote dots, Yes / No |
| `propose-amount-thesis.png` | `ProposeFlowSampleUITests` | Buy step 2 with the optional reason (thesis) field |
| `propose-review.png` | `ProposeFlowSampleUITests` | Buy step 3 receipt |

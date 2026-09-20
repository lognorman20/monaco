# WP4 proposals and propose flows

Simulator: iPhone SE-class (375×667 pt). All captures use the Debug sample harness, no backend.

- `01`–`06`: `ProposalFeedSampleUITests` (feed, vote from card → "You voted yes", detail thread, comment, reply).
- `10`–`15`: `ProposeFlowSampleUITests.testBuy_threeSteps_sendsToCabal` (chooser sheet, pick stock, $50 preset, over-limit state, review, success toast).
- `20`–`22`: sell in three steps (holdings, 50% preset, review with estimate).
- `*-light.png` / `*-dark.png`: feed, chooser, detail at Voting, read-only (seeded) proposal, and the tracker at Buying and Done (the detail polls every 5s; `sample-22` moves from Buying to Done).

Launch arguments: `-MonacoProposalFeedSample` (feed), add `-MonacoProposeSample` (cabal stand-in with the Propose sheet, `-MonacoProposeSampleOpen` to open it) or `-MonacoProposalSampleDetail <id>`.

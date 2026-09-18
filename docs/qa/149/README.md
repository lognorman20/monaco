# Proposal feed and comments verification

Issue #149. Base: `oh-yea`. Adds `proposal_comments` (migration 000009), `GET/POST /v1/proposals/{id}/comments`, feed card fields on the group proposal list (`canVote`, `voteSummary`, `commentCount`), `commentCount` on detail, and the SwiftUI card feed, detail thread, and composer.

## Verified

- Backend runtime suite passes with `go test -p 1 ./...` on a dedicated local Compose Postgres database (`monaco_149` / `monaco_149_test`) and fake Privy/Jupiter providers, except two failures that are not from this change: the Jupiter package vet error (`testing.B.Context` needs Go 1.24; module declares 1.23, fixed separately by PR #183; the package passes with `-vet=off`) and `TestOpenTestDB_connectsToDerivedDatabase`, which hard-codes the shared `monaco_test` name. Domain tests pass.
- Comment HTTP tests cover: member post and reply, oldest-first thread with `parentId`, empty thread, non-member 404 on list and post (no text leaked), missing auth 401, whitespace-only / missing / over 1,000 code points / over the 16 KiB byte cap / NUL / malformed JSON / malformed or unknown parent id bodies (400, nothing stored), exactly 1,000 code points accepted, reply to a comment on another proposal 400, unknown or malformed proposal id 404, 11th comment in a minute 429 with `Retry-After`, and list/detail card fields before and after voting.
- 73 host mobile-core tests pass (`swift test`): DTO decoding (feed card, legacy list payload, detail), comment decode, thread nesting, orphan and cycle handling, draft length rule, formatters, feed copy audit, client paths, bodies, and error statuses.
- Native Debug simulator build passes with a public compile-only Privy configuration.
- XCUITest `ProposalFeedSampleUITests` passes on the SimSlim-prepared simulator (iOS 18.0) using the Debug-only `-MonacoProposalFeedSample` launch argument, which swaps in an in-memory service. It votes yes from a card, scrolls through 20 open cards, opens the thread, posts a comment, replies to a comment, and checks that Post stays disabled for a whitespace-only draft. Screenshots below come from that run.

## Still required before merge

Live authenticated walkthrough on the local backend: two members, A proposes, B comments, A replies, both vote, feed counts update. The current environment key does not decrypt the shared environment, so no Privy sign-in was possible. Sample data proves the SwiftUI flow and the shared vote path, not the live HTTP contract; the contract is covered by the Go handler tests and the mobile-core client tests.

## Screens (sample data)

| | |
|---|---|
| ![Feed](01-feed.png) | ![After card vote](02-after-card-vote.png) |
| ![Scrolled feed](03-feed-scrolled.png) | ![Thread](04-detail-thread.png) |
| ![Comment posted](05-comment-posted.png) | ![Reply posted](06-reply-posted.png) |

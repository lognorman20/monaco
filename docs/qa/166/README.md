# Cabal chat verification

Issue #166. Base: `oh-yea`. Members of a cabal can read and post plain-text messages from the cabal screen (Actions → Cabal chat).

## API

- `GET /v1/groups/{id}/messages?before=<cursor>&limit=<1-100, default 30>`: newest first. `nextCursor` is set only when older messages exist.
- `POST /v1/groups/{id}/messages` with `{"body": "..."}`: returns 201 and the stored message.
- Each message has `id`, `groupId`, `authorId`, `authorName`, `body`, `createdAt` (fixed-width microsecond UTC) and `mine`.
- Non-members get 403 and unknown or malformed cabal ids get 404. A blank body, a body over 2,000 characters, a bad cursor or a bad limit gets 400. A missing or invalid token gets 401.
- Posting is limited per user to a burst of 10, then one message per second. Past that the API returns 429 with `Retry-After`. The limiter lives in process memory, so it resets on restart and isn't shared across instances.
- Migration `000015_group_messages.sql`: table `group_messages` and index `(group_id, created_at DESC, id DESC)`.

## Verified

- Backend HTTP integration tests against an isolated local Postgres (`monaco_166_test`) with the fake Privy provider. They cover:
  - posting and trimming, author name, and `mine` from each member's point of view
  - an empty cabal, and cursor paging through 5 messages 2 at a time
  - non-member 403 with nothing stored, and unknown or malformed cabal 404
  - blank, missing, too-long and malformed bodies, an oversized payload, and exactly 2,000 two-byte characters
  - a bad cursor, and zero, negative, too-large and non-numeric limits
  - 401 when the token is missing or invalid
  - 429 with `Retry-After`, where the limit applies to each user separately
- Backend unit tests cover the token bucket (burst, refill, cap and per-key limits) and cursor round-trip and rejection.
- mobile-core host tests: 67 total, 28 of them chat tests. They cover DTO decoding, draft validation, timeline merge, poll de-duplication, same-second ordering, older-page and gap handling, copy mapping and the copy audit. They also cover client requests, 403 and 429, malformed JSON and an offline network failure.
- The Debug app builds for the simulator with the public compile-only Privy config.
- SimSlim simulator check (iOS 18.0, slimmed with `scripts/prepare-simulator.sh`) using the Debug-only sample-data harness `-MonacoChatSampleQA`. It uses in-memory data and no backend:
  - `01-thread.png`: other members' bubbles on the left with author names, own bubbles on the right, and local timestamps.
  - `02-sent.png`: after a message is typed and sent, it appears at the bottom and the composer clears.
  - `03-empty.png` (`-MonacoChatSampleEmpty`): the empty state tells the member what to do next.
  - `04-send-failed-toast.png` (`-MonacoChatSampleOffline`): the send failure appears as a `monacoToast` and the draft is kept for retry.

## Not verified live

- Signed-in path: Cabal screen → Cabal chat → post, then a second member sees it within one poll (4 s). This wasn't run because the `.env.keys` on this machine doesn't decrypt `.env.local`, so there was no Privy OTP sign-in and no backend boot (the backend needs real Privy config and a funded relayer).
- The "Cabal chat" row on the cabal screen compiles but wasn't tapped on the simulator for the same reason.
- The backend wasn't started on port 8092, and no curl run against a live server was done.

## Notes

- Full backend suite (`go test -vet=off -p 1 ./...` with `DATABASE_URL` pointed at `monaco_166`) has two failures that are unrelated to this change:
  - `TestOpenTestDB_connectsToDerivedDatabase` expects the shared `monaco_test` name.
  - `TestGET_groups_search_returnsMatchingClub` is a pre-existing flake. It runs in parallel with tests that create "Alpha Fund" and searches for "alpha".

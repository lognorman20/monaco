# Multi-user verification

Rerun this after you change auth, membership, account balance or cabal funding, governance, the member board, or Home P&L. It pairs the automated backend suite with a two-account pass on the gold simulator. The style follows [`m5-demo-script.md`](m5-demo-script.md).

Never `simctl erase`. Never hardcode another laptop's simulator UDID.

## Automated coverage (run first)

```bash
just test backend   # includes the multi-user suites below
just test mobile
just build mobile
```

| Test | What it proves |
| --- | --- |
| `apps/backend/internal/httpapi/multi_user_flow_test.go` `TestMultiUserFlow_joinDepositProposeVote_boardsShowEachMembersPnL` | A creates, B joins by cabal id, and both fund from their account balance (`POST /v1/groups/{id}/fund`, then the sweep is confirmed through `ObserveSweep`). A joined member can load the cabal and its treasury, search assets and get a quote. A proposes, B's vote is recorded, and both votes pass the proposal. `/view`, `/v1/home` and `/v1/home/dashboard` return each member's own P&L to both tokens. |
| `…TestMultiUserFlow_onlyJoinerFunded_creatorPnLStaysFlat` | When only B funds and the pot gains, A's slice, People row, net worth and leaderboard row stay at `+0.00` with a nil percent. B's rows carry the whole gain. |
| `…TestMultiUserFlow_peopleBoard_excludesMembersOtherClubs` | A's People board shows B's P&L only from the cabal they share. B's own row adds up all of B's cabals. |
| `…TestMultiUserFlow_pendingJoinerDeposit_isNotCreditedToFundedMembers` | While B's funding is still pending, the on-read treasury reconcile does not credit B's USDC to A. After confirmation the credit goes to B. |
| `apps/backend/internal/httpapi/multi_user_authz_test.go` `TestMultiUserAuthz_nonMember_cannotReadOrMutateClub` | Non-member C gets 404 or 403 on every cabal-scoped read and write: view, cabal, proposals, proposal detail, deposit detail, treasury, assets, quotes, fund, propose, vote and join-requests. C has enough account balance to fund, so membership alone is what stops C. C's refused vote records no ballot, and C's refused fund leaves no pending deposit. C's home and dashboard contain nothing from the cabal. |
| `…TestMultiUserAuthz_member_cannotActOutsideClubRules` | A member outside a named voter set cannot vote or propose, and a non-admin cannot list join requests. |

The tests run in-process against the `*_test` database only. The fake wallets (`FAKE…` addresses) and their balances exist only there, and `TestIsolation` deletes them when the tests finish. Do not seed `FAKE*` `member_wallets` rows into the dev database, because the sweep poller would try to sweep them.

## Prerequisites (manual pass)

```bash
dotenvx run -f .env.local -- just run backend        # API on 127.0.0.1:8080
curl -s http://127.0.0.1:8080/health
just build mobile
```

Simulator (gold, SimSlim):

1. Find your own gold UDID with `./scripts/gold-sim-udid.sh`. It prints `SIMSLIM_UDID` or exits 1. UDIDs are per machine, so export yours in your shell rc or in plain `.env`.
2. Slim the simulator with [SimSlim](https://github.com/MobAI-App/simslim) without erasing it: `bash scripts/prepare-simulator.sh "$(./scripts/gold-sim-udid.sh)"`. Set `SIMSLIM_BIN=<path>/simslim` if the binary is not on `PATH` or in `.tools/bin`. The script runs `simslim on --no-reboot`, then `verify` and `doctor`, with `scripts/simslim-profile.json`. On iOS 18.0 the overrides last only for the current boot, so run it again after each boot. If your checkout does not have the script yet, run `simslim on <udid> --no-reboot --profile <profile>.json`, then `simslim verify <udid> --profile <profile>.json` and `simslim doctor <udid>` (see the README SimSlim section).
3. Agents drive taps with [`.cursor/skills/ios-simslim-fast-qa/SKILL.md`](../.cursor/skills/ios-simslim-fast-qa/SKILL.md) (MobAI claim → bridge → DSL) or XcodeBuildMCP with `--simulator-id "$(./scripts/gold-sim-udid.sh)"`. Do not drive taps with AppleScript, CGEvent or coordinates.

Accounts:

| Role | Login | Notes |
| --- | --- | --- |
| User A (creator) | A real Dynamic email or SMS | Same account you use to develop |
| User B (joiner) | A second Dynamic login (different phone or email) | Different `users.dynamic_user_id` and member wallet, so a separate account balance |
| User C (outsider, optional) | Any third Dynamic login | Only needed for step 11 |

To switch accounts on one simulator, use **Settings → Sign out**, then sign in again. `just stop mobile` also uninstalls `com.monaco.app` to clear the Dynamic session.

## Checklist

1. **Sign in as A.** Use the email OTP. The session gate loads Home. Check the network log for `POST /v1/auth/session` → 200 with A's `userId`.
2. **Create a cabal as A.** Go to Cabals → Create cabal and keep the defaults (open join, all members vote, majority). Copy the cabal id from the cabal screen. API: `POST /v1/groups`.
3. **Fund A.** Go to Home → Add money and send USDC on Base to A's deposit address. The Account balance card updates (`GET /v1/me/balance`). Open the cabal → **Fund this cabal**, then enter an amount up to the available balance (`POST /v1/groups/{id}/fund`). Wait for the sweep: the pending "funding a cabal" line clears. A's "You" slice shows the funded amount with `+0.00`. API: `GET /v1/groups/{id}/view`.
4. **Switch to B.** Go to Settings → Sign out, then sign in as B. **Auth isolation check:** before any pull-to-refresh, Home, Cabals and Profile must not show A's net worth, account balance, cabals or name. B starts with no cabals, a `0.00` net worth and B's own (empty) account balance. API: `GET /v1/home/dashboard` and `GET /v1/me/balance` with B's token return B's projection only.
5. **Join by id as B.** Go to Cabals → Join cabal, paste the id, and join. API: `POST /v1/groups/{id}/join` → 204. The cabal opens, and the member board lists A and B. B shows `+0.00` and no percent. B can open the cabal treasury (`GET /v1/groups/{id}`, `GET /v1/groups/{id}/treasury/usdc` → 200, not 404).
6. **Check isolation with only A funded.** On B's device, B's "You" slice is `0.00` and B's Home net worth is `0.00`. B's row on the Home leaderboard shows `+0.00` with no percent. A's row shows A's own P&L.
7. **Fund B.** Go to Add money and send USDC to B's own deposit address (it differs from A's). After B's balance updates, open the cabal → Fund this cabal. Wait until the funding clears. The member board shows both members with their own dollar P&L. API: `GET /v1/groups/{id}/view` → two `members` rows with distinct `userId` values.
8. **Propose as A.** Sign out, then sign in as A. In the cabal, go to Propose → search `AAPL` → get a quote → propose. API: `GET /v1/groups/{id}/assets`, `POST /v1/groups/{id}/quotes`, then `POST /v1/groups/{id}/proposals`.
9. **Vote as B.** Sign out, then sign in as B. The proposal appears under Home → Needs your vote (`GET /v1/home/dashboard` `missedProposals`) and in the cabal's Proposals list (`GET /v1/groups/{id}/proposals`). Vote yes (`POST /v1/proposals/{id}/votes` → 204) and confirm the toast. The proposal detail (`GET /v1/proposals/{id}`) lists B's ballot, the Vote button disappears, and the proposal leaves Needs your vote. A second vote does not add a ballot.
10. **Refresh boards.** After the proposal passes and executes, pull to refresh on both accounts. The cabal pot shows the AAPL row. Each account's "You" slice matches its own share. Home "Your cabals", net worth and the Leaderboard (ALL) update. A's and B's dollar P&L split by share, and neither account's row shows the other's funding.
11. **Outsider (optional, C).** Signed in as C with some account balance, the cabal is not in C's cabals and A and B do not appear on C's Home leaderboard. Pasting the cabal id into Join cabal offers to join. Without joining, C must not see the pot, the member board or the proposals, and cannot fund the cabal.
12. **Leave with withdraw (optional).** As B, go to Leave cabal → Withdraw and leave (`POST /v1/groups/{id}/leave` with `{"withdrawStake": true}`; a partial Withdraw without leaving uses `POST /v1/groups/{id}/withdraw-to-balance`). B's stake returns to B's account balance only. A's slice and account balance do not change.

## Expected API surfaces

| Surface | Who | Expect |
| --- | --- | --- |
| `GET /v1/me/balance` | the caller | The caller's own account balance and pending funding, never another user's |
| `GET /v1/home` | any member | `groups[]` lists every cabal (`isJoined` per viewer). `people[]` comes only from the viewer's cabals, one row per user with their own `dollarPnl` |
| `GET /v1/home/dashboard?leaderboardRange=ALL` | any user | `netWorthUsd`, `myGroups[]` and `missedProposals[]` belong to the viewer. `leaderboard.people[]` covers the members of the viewer's cabals |
| `GET /v1/groups/{id}`, `/view`, `/treasury/usdc`, `/treasury/tokens` | members only (404 otherwise) | `members[]` lists every member. `you` is the caller's slice |
| `GET /v1/groups/{id}/proposals` | members only (404) | Open/history proposals with `proposerId` |
| `POST /v1/groups/{id}/fund`, `/quotes`, `GET /assets` | members only (403) | Any joined member, not only the creator. `/fund` also caps at the caller's available balance (400) |
| `POST /v1/groups/{id}/proposals` | eligible proposers only (403) | 200 with `proposalId` |
| `POST /v1/proposals/{id}/votes` | eligible voters only (403) | 204. The ballot is visible in `GET /v1/proposals/{id}` |
| `POST /v1/groups/{id}/deposits` | anyone | 410 Gone. Funding goes through the account balance and `/fund` |

## Done when

Every checklist step passes on the gold simulator with two distinct Dynamic accounts, and `just test backend` is green. Record failures with a screenshot, the MobAI `ui_tree`, and the request/response of the failing call.

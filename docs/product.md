# Monaco product

This is the canonical product and architecture brief. It is not legal advice.

**Monaco** is an iOS app where friends form a group, pool USDC, and buy tokenized US stocks on Solana.

How to clone, run, and operate the repo: [README](../README.md). Milestone backlog: [docs/index.md](index.md).

Prize target is the general Stocklana pool. Judges ask whether this could be a real app people use. The build does **not** use Meteora DBC or Clawpump. Execution is Jupiter Swap API v2 on **Solana mainnet** with small real USDC.

## Goals

- **Social investing, not crypto.** Copy is "invite friends", "add money", "buy Apple". No wallets, gas, seed phrases, or "mint" in user-facing copy.
- **Leaderboard and P&L first.** Two boards, both percent return. Inside a group: who in this pot is winning. Across the app: which groups and which people are winning. Buys and cash-out serve those screens.
- **Real on-chain execution.** Tokenized stocks land in the group treasury via Jupiter. A fiat-only mock does not meet the bar.
- **Fair equity.** Members hold share units (claim tickets on the pot), not dollar IOUs. A redeem pays that member's slice of what the pot is worth now, in USDC, not a refund of what they put in.
- **Cash out is a primary flow.** Partial redeem to USDC at a payout address the user proved they own. Full exit is the same path with the amount at max.
- **Shippable Friday scope.** Native SwiftUI, Go API, Supabase Postgres, Privy auth and wallets, Jupiter swaps. No custom Solana program.

## How it works

1. Sign in with SMS or email OTP via Privy.
2. Create a group or join one of many. One user belongs to many groups. App home ranks groups and people across the whole app.
3. Deposit USDC into the member wallet. It appears as **account balance** (chain USDC in the Privy member wallet). The user picks a cabal and amount to fund; the backend sweeps that exact amount into the group treasury and credits share units at the current share price.
4. Propose a buy from the xStocks catalog. The group's voter set must pass it under the creator's threshold and expiry. Then the backend swaps treasury USDC for the token on Jupiter.
5. Live on the group screen: pot composition, your slice, dollar P&L, percent return, and the in-group member leaderboard.
6. Redeem some or all share units whenever you want. The backend sells that slice to USDC and pays a verified payout address.

## Groups and invites

Each group has one shared portfolio and one Privy Solana server wallet as the treasury.

At create, the **group creator** sets:

- **Join policy.** Anyone may join, or a join password is required.
- **Voter set.** Either a named subset of members (minimum size 1, which may be only the creator) or every member.
- **Vote threshold.** Unanimous among the voter set, or majority among the voter set.
- **Vote expiry.** A duration the creator chooses. If the proposal does not pass before expiry, it dies and no swap runs.

## Votes and buys

On-chain governance is out of scope. Votes live in Postgres. The Go API is the source of truth.

1. A buy proposal names an xStock from the **full public catalog** (search, not a fixed two-ticker list).
2. If Jupiter cannot quote a route, the UI refuses the proposal. Do not offer names the Meta-Aggregator cannot fill.
3. Members in the voter set vote yes or no before expiry.
4. On pass, the backend builds a Jupiter v2 order (`inputMint` = USDC, `outputMint` = xStock mint), signs with the treasury via Privy, and `POST`s `/execute`. Confirm `status: Success`, `code: 0`.
5. The token lands in the **group treasury**.

Constants:

- USDC mint: `EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v`
- xStock mints: `GET https://api.xstocks.fi/api/v2/public/assets/{symbol}` then `deployments` where `network == Solana` then `address`. Examples: `AAPLx` is `XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp`. `TSLAx` is `XsDoVfqeBukxuZHWhdvWHBhgEHjGNst4MLodqsJHzoB`.

The xStocks public API is mint metadata only. It is not an execution rail. Poll `/execute` for confirmation. Do not use a Jupiter WebSocket. Do not use Privy production webhooks (Enterprise-only).

## Architecture

No custom on-chain vault. Privy server wallets hold assets. Supabase Postgres holds member share units, votes, NAV snapshots, and P&L inputs (net USDC in). A Go API talks to Privy and Jupiter.

Solana transaction fees are paid by an **app relayer**. Treasuries may hold no SOL. Users never see gas. Relayer env and funding: [README](../README.md#relayer-fee-payer).

The backend can sign the treasury. That custodial fact is accepted for the hackathon. Demo the buy. Do not spend UX on a trust explainer.

```
SwiftUI (iOS 17+)
  → Privy Swift (auth, member wallets)
  → Go API (groups, invites, votes, share ledger, sweeps, swaps, P&L)
  → Supabase Postgres DB
  → Privy Solana server wallet per group (treasury)
  → App fee payer (SOL fees)
  → Jupiter Swap API v2 (USDC → xStocks)
```

### Wallets

A **wallet** is a keypair on a chain. On Solana the public key is the **address** (base58). The private key **signs** transactions. The address holds:

- **SOL** — native token. Every tx burns a tiny amount as a fee. No SOL → send fails even if you hold USDC.
- **SPL tokens** — e.g. USDC. Same address, different mint. Monaco USDC mint: `EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v` ([Solana mainnet USDC](https://solscan.io/token/EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v)). USDC on Ethereum or Base is a different token; the deposit poller will not see it.

You do not “log into Solana.” You hold keys that can move whatever sits at that address. Whoever can sign, spends.

Product copy hides this. Devs still need it for QA.

| Wallet                   | Owner                            | Role                                                                                                                                    |
| ------------------------ | -------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| Member wallet            | One per user (Privy)             | Deposit inbox. Unique attribution for who funded. Backend-signable via Privy.                                                           |
| Group treasury (vault)   | One per group (Privy, app-owned) | Holds USDC and tokenized stocks. All group trades execute from here.                                                                    |
| Relayer / fee payer      | App                              | Pays SOL fees so treasury and member wallets need no SOL on the happy path.                                                             |
| Phantom **agent** wallet | Coding-agent harness             | **Not product.** Funds member inboxes for mainnet QA; leftover USDC returns here. Separate keys from Privy and from your phone Phantom. |

Users never manage keys or approve individual Solana transactions in the happy path. The backend signs sweeps, swaps, and payouts.

Do not put `PHANTOM_APP_ID` in Monaco `.env.local`. Agent wallet setup: [README Agent QA](../README.md#agent-qa-phantom-mcp).

### Deposit and fund

1. The user funds **their** Privy member wallet with USDC (onramp or external transfer). Personal Phantom send to the member inbox also works; see [README Deposits](../README.md#deposits). Inbound USDC stays in the member wallet and shows as **account balance** (`GET /v1/me/balance` reads chain USDC minus in-flight fund jobs).
2. To deploy into a cabal, the user calls **`POST /v1/groups/{id}/fund`** with an amount ≤ account balance. The backend creates a pending deposit and sweeps that exact amount member → treasury (server-signed, no second approval sheet).
3. On **confirmed sweep into treasury**, credit share units at the current share price. Idempotent on transaction signature.
4. Do **not** credit shares when USDC only arrives in the member wallet. Do **not** auto-sweep inbound USDC without an explicit fund action.

See **NAV and share units** for the formula.

## NAV and share units

NAV means **net asset value**. It is the dollar value of the whole group pot right now.

Two numbers, keep them distinct:

- **Pot NAV.** USDC sitting in the treasury, plus every tokenized stock marked at its current price. Example: $40 USDC + 0.1 AAPLx worth $60 = $100 pot.
- **NAV per share** (share price). `pot NAV / total shares`. This is what one share unit is worth. On an empty group there are no shares yet, so the first deposit uses a share price of **$1**.

A **share unit** is a claim ticket, not a dollar IOU. The ledger stores how many tickets each member holds, not "Alex is owed $100." Your dollars in the app are:

`your equity = (your shares / total shares) × pot NAV`

When someone deposits, they buy tickets at today's share price:

`shares credited = USDC swept in / NAV per share`

When someone redeems, they return tickets and take that fraction of the pot in USDC. The pot is marked first, then (if needed) that slice of stock is sold to USDC.

**Why not track dollars deposited.** Alex puts in $100 and the group buys Apple. Apple goes up 10%. The pot is $110. If Blair then "deposits $110" as a dollar balance, she would own half of a pot that already includes Alex's gain, or Alex would eat her later losses. Share units fix that. Blair's $110 buys shares at $1.10, so she gets the same number of tickets Alex has, and she does not steal the bounce.

Worked numbers (ignore Jupiter slippage for the story):

1. Empty group. Share price $1.
2. Alex deposits $100. He gets 100 shares. Pot $100. Total shares 100. Share price $1.
3. The group buys AAPLx with the $100. Pot still about $100, now in stock.
4. AAPLx rises 10%. Pot $110. Alex still has 100 shares. His equity is $110. Share price is $1.10.
5. Blair deposits $110. She gets `110 / 1.10 = 100` shares. Pot $220. Total shares 200. Each still owns half.
6. Blair redeems 50 shares. That is `50 / 200` of the pot = $55 USDC. She keeps 50 shares. Alex still has 100.

Marks: Jupiter fill price is cost basis. Ongoing P&L may use Pyth equity feeds. If the token still trades on-chain after the cash equity market closes, show an after-hours label.

**UI copy.** Do not say "NAV" to users. Say the pot value, their slice, and gain or loss in dollars.

## Portfolio, P&L, and leaderboard

P&L is the product. It exists at two scopes. Same math, different rows.

Rank by **percent return**, never by dollars. A small pot can beat a whale. Dollar P&L sits beside the name.

`percent return = equity / net USDC in − 1`

Skip a row when net USDC in is 0 (no divide by zero, no fake 0% clubs).

**Inside a group** (group screen). Trade and cash-out are actions here.

- **Pot.** Holdings list: USDC plus each xStock with units, mark, and dollar value. Cost basis per position from the Jupiter fill. After-hours label when Pyth equity is frozen.
- **You.** Slice in dollars and as a percent of this pot. Dollar P&L and percent return versus **net USDC in this group** (sweeps credited here minus USDC paid out on redeems here).
- **Member board.** Every member with a share balance greater than zero in this group. Ranked by that in-group percent. A full exit from this group drops them off this board only.

**Across groups** (app home, first screen after sign-in). Two lists, both live off the same ledger.

- **Group board.** One row per group with net USDC in greater than 0. Equity is that group's pot NAV. Net USDC in is all member sweeps into that treasury minus all redeems out of it. This is how clubs compete with each other. A join password still hides entry, not the score. The row shows the group name, percent, and dollar P&L of the pot. Tap through to join or open.
- **People board.** One row per user with net USDC in greater than 0 across **all** groups they belong to. Equity is the sum of their slices. Net USDC in is the sum of their per-group net USDC in. Alex in three clubs is one row, not three. Tap through to their profile list of groups.

**Why two boards.** Friends care who is winning this pot. The app-wide loop is which clubs are hot and who is good across clubs. The people board only works if one user can sit in many groups.

Postgres stores NAV snapshots on deposit, fill, and redeem so charts and both boards are replayable. Do not recompute history only from live wallets.

Settings → Advanced may expose explorer links. The main flow never needs them.

## Profile

The Profile tab is the signed-in user's own page: photo, display name, member-since date, account balance, deposit address with copy, and every joined cabal with its pot, the viewer's position, and P&L. It reads the same `/v1/home`, `/v1/home/dashboard`, `/v1/me`, and `/v1/me/balance` payloads Home already loads, so it adds no fetches.

- **Display name** (`PATCH /v1/me` with `{"displayName": "..."}`) is a label, not a handle. Names are not unique; boards key on user id. 1–32 characters after trimming. Letters, numbers, spaces, punctuation, and emoji; no control, zero-width, bidi-override, or blank filler characters, and at least one letter or digit. The app checks the same rules inline. A save shows immediately and rolls back with a toast if the server rejects it.
- **Photo** (`POST /v1/me/profile-photo`) is picked on Profile or Settings through one shared picker. Storage details: [ops-profile-photos.md](ops-profile-photos.md).
- Both writes are limited per user (name: 5 quick edits, then one per 12 s; photo: 3, then one per 20 s) and return 429 with `Retry-After` past that.
- After either write the app refetches Home, so the people board, dashboard leaderboard, and cabal member boards show the new name and photo. Those rows carry `profilePhotoUrl`.

## Withdraw

**Platform withdraw** (Settings → Withdraw) sends idle USDC from the user's Privy **member wallet** to any Solana address they paste. It uses `GET /v1/me/balance` (chain USDC minus in-flight fund jobs and pending platform withdrawals) and `POST /v1/me/withdrawals`. It does not sell cabal holdings, debit share units, or pull from group treasuries. Deployed stake must return to the member wallet first (see leave / withdraw-to-balance flows).

**Cabal cash out** (group screen redeem) is different: it debits share units, may sell pot holdings on Jupiter, and pays USDC from the **group treasury** to a proven payout address.

Partial redeem is a first-class action on the group screen, same weight as buy. Payout is **USDC only**. Never send tokenized stock in kind.

The user picks how many dollars (or how many shares) to take, from a dust minimum up to their full equity. Full exit is the same flow with the slider at max.

1. **Debit share units** first (row-locked in Postgres).
2. Compute the member's slice of the pot (`shares redeemed / total shares × pot NAV`). If the treasury holds stock, **sell that slice to USDC** on Jupiter first.
3. Send USDC only to a **payout address the user proved they own** (signed message). The proof is required on every redeem, including partials. Reject attacker-supplied pubkeys.

They receive USDC equal to their redeemed fraction of the pot at that moment, not a refund of dollars they put in. That group's member board, the global group board, and the global people board all recompute from the new net-USDC-in figure.

## Stack

| Layer            | Choice                                                                                                       |
| ---------------- | ------------------------------------------------------------------------------------------------------------ |
| Mobile           | SwiftUI, iOS 18+ only                                                                                        |
| Auth and wallets | [Privy Swift](https://docs.privy.io/basics/swift/quickstart). Member wallets plus per-group server treasury. |
| API              | Go                                                                                                           |
| Ledger           | [Supabase](https://supabase.com/) Postgres. Share units, votes, NAV snapshots, idempotent tx log.            |
| Execution        | [Jupiter Swap API v2](https://dev.jup.ag/docs/swap) on mainnet                                               |
| Fees             | App relayer (SOL)                                                                                            |
| Asset metadata   | [xStocks public API](https://api.xstocks.fi/api/v2/public/assets) (mints only)                               |
| Marks            | Jupiter fill + [Pyth Hermes](https://docs.pyth.network/price-feeds/core/api-instances-and-providers/hermes)  |

## Hackathon demo checklist

Judges should spend most of the live pass on P&L. Show the in-group member board and the app-wide group and people boards. The buy exists so those numbers are real.

1. Create a group. Set join policy (open or password), voter set, threshold, and expiry.
2. Join from a second account. Two names on the in-group board.
3. Both deposit mainnet USDC → sweep → share credit. Boards show 0% until a mark moves.
4. Search xStocks, propose a buy, pass the vote, Jupiter `/execute` success (prefer `AAPLx` on stage).
5. Group screen: pot composition, both slices, dollar P&L, in-group percent board.
6. App home: this group on the group board, both people on the people board (second group optional if time).
7. One member partial-redeems to USDC at a verified payout address. In-group board, group board, and people board update. The other member still in.

## Out of scope (MVP)

- Custom on-chain vault or share-token program
- On-chain voting
- Meteora DBC, DAMM, and Clawpump prize tracks
- Privy production webhooks (Enterprise)
- Android, web client, copy-trading network
- Primary issuer mint or redeem APIs (Backed client, institutional gates). Secondary Jupiter path only.
- App Store public listing, full KYC and AML, securities licensing. Demo may use TestFlight and geo-labeled test assets.

## Open decisions

These were not locked in the spec session. Do not invent them in code until they are.

- Who may **propose** a buy (any member, voter set only, or creator only).
- Failed `/execute` after a passed vote (mark failed, do not retry forever).
- Creator leave and group dissolve.

## Notes for production (not blockers for demo)

Tokenized stock exposure (`AAPLx`) is on-chain tracker exposure, not DTCC shares. Pooled custody and trade execution trigger broker-dealer, adviser, and money-transmitter questions in the US. Confirm the path with securities and fintech counsel before a consumer launch. Geo-fencing and licensed partner rails may be required for US persons depending on asset issuer terms.

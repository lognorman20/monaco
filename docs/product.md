# Monaco Product Brief

Monaco is a social investing app that lets users start a hedge fund with their friends. Users form groups (cabals) that share one treasury and invest in tokenized stocks on Solana.

A cabal is one treasury, not a feed of separate accounts. Members contribute USDC, get a proportional stake, propose what the treasury should buy, and vote. Approved trades execute for the group. Each member's slice moves with the pot.

The loop is: join, contribute, decide, watch the book, decide again.

How to clone, run, and operate the repo: [README](../README.md). Milestone backlog: [docs/index.md](index.md).

## Cabals

A user creates a cabal and invites people in. Each cabal has one shared portfolio. The creator sets who can join, who votes, the pass threshold, and how long a proposal lives. Detail: [Groups and invites](#groups-and-invites).

One person can belong to many cabals: a friends pot, a coworkers pot, a pot built around one thesis. Home ranks cabals and people across the app. That history is the social graph. It is tied to money in, votes, and returns.

## Shared ownership

You own a fraction of the pot, not a dollar IOU. The first dollars in buy shares at $1. Later dollars buy shares at the current price, so a new member does not take earlier gains. Pot up, your slice up. Pot down, your slice down. Add capital any time, or redeem shares for USDC equal to that fraction now.

Formula and worked numbers: [NAV and share units](#nav-and-share-units).

## Performance

Two boards, both percent return. Inside a cabal: who in this pot is ahead. Across the app: which cabals and which people are ahead. The boards are why people come back and argue about the next trade. Buys and cash-out exist so those numbers are real.

## Agents

A cabal can vote to hand a slice of the treasury to an agent. Example: 10% of the pot, one strategy. That slice sits in the portfolio next to positions members picked and USDC left idle.

Members see capital in each agent, how that slice has done, and vote to raise it, cut it, pause it, or remove it. A cabal can start with every trade as a member vote, delegate after a strategy has a record, and pull the allocation if it does not work. The agent trades only inside the budget the vote set. The cabal remains the decision layer.

## Solana, Privy, and Bankr

Users see cabal, stake, and performance. Under that, three systems split the work: **Solana** settles, **Privy** holds the keys, **Bankr** runs the strategy. Solana is the chain because fees are a fraction of a cent, which keeps small group buys worth making.

### Solana

Settlement is Solana mainnet. There is no custom program.

- Cash is USDC, mint `EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v`. USDC on another chain is a different token. The deposit poller ignores it.
- Positions are xStocks tokenized US stocks. Mints come from the xStocks public API, which is metadata only and does not execute trades. Detail: [Votes and buys](#votes-and-buys).
- A passed member vote or a valid agent intent swaps on Jupiter, from the cabal treasury. The token lands in that treasury.
- Fees are SOL, paid by the app relayer. Member wallets and treasuries need no SOL on the happy path. Funding: [README relayer](../README.md#relayer-fee-payer).

Postgres holds share units, votes, NAV snapshots, and the agent budget. Solana holds the assets.

### Privy

Privy is sign-in and custody.

- Sign-in is email or SMS OTP. No password in the product path.
- Each member gets one Privy wallet. That address is the deposit inbox. Inbound USDC stays there as account balance until the member funds a cabal. `POST /v1/groups/{id}/fund` sweeps that amount into the treasury and credits share units at the current share price. Inbound USDC alone does not credit shares.
- Each cabal gets one app-owned Privy server wallet. That wallet is the treasury. Member buys, agent fills, and redeems all sign from it.
- The Go API signs sweeps, swaps, and payouts. Members do not approve each transaction. Wallet roles: [Wallets](#wallets).

### Bankr

Bankr is the strategy runtime. Monaco does not run the strategy process.

A strategy ships as a [Bankr skill](https://skills.bankr.bot/): a package any agent host can load. The same skill can run on a laptop, a server, Cursor, Claude, the Bankr CLI, or another host. It needs the cabal agent key and a path to the Monaco API. It does not need to live next to the backend or inside the iOS app.

Split of work:

1. **Cabal votes the agent in.** Name plus a USDC allocation. Later votes pause, resume, or revoke. On pass, the proposer gets the agent key. Key rules and error codes: [agent trading](agent-trading.md).
2. **The skill runs wherever it was installed.** It reads the cabal catalog (`GET /v1/groups/{id}/assets`) and posts buy or sell intents with `X-Monaco-Agent-Key`. No member JWT.
3. **Monaco enforces the vote.** A bad or revoked key, a paused agent, an intent over the allocation, and too many wrong keys are each refused.
4. **The fill uses the member-vote path.** The Go API swaps on Jupiter and Privy signs the cabal treasury. The position lands in the shared pot and on the cabal activity feed. It does not land in a Bankr wallet. A skill that spent from its own wallet would split the pot and break share accounting.

Bankr tools (prices, research, other skills in the catalog) can inform the decision. They do not sign the treasury. The only order Monaco fills is an intent inside the voted budget.

`agents/momentum-bot` is the reference shape: read prices, apply one rule, POST an intent, stop on 401. Fork that loop or encode it as a Bankr skill. The API does not care which host sent the request.

Operator steps: [connect an agent](how-to/connect-an-agent.md). HTTP contract: [agent trading](agent-trading.md).

## How it works

1. Sign in with SMS or email OTP via Privy.
2. Create a group or join one of many. One user belongs to many groups. App home ranks groups and people across the whole app.
3. Deposit USDC into the member wallet. It appears as **account balance** (chain USDC in the Privy member wallet). The user picks a cabal and amount to fund; the backend sweeps that exact amount into the group treasury and credits share units at the current share price.
4. Propose a buy from the catalog (tokenized US stocks via xStocks, and pre-IPO tokens via Tessera and PreStocks). The group's voter set must pass it under the creator's threshold and expiry. Then the backend swaps treasury USDC for the token on Jupiter.
5. Live on the group screen: pot composition, your slice, dollar P&L, percent return, and the in-group member leaderboard.
6. Redeem some or all share units whenever you want. The backend sells that slice to USDC and pays a verified payout address.

## Groups and invites

Each group has one shared portfolio and one Privy Solana server wallet as the treasury.

At create, the **group creator** sets:

- **Join policy.** Anyone may join, or a join password is required.
- **Voter set.** Either a named subset of members (minimum size 1, which may be only the creator) or every member.
- **Vote threshold.** Unanimous among the voter set, or majority among the voter set.
- **Vote expiry.** A duration the creator chooses. If the proposal does not pass before expiry, it dies and no swap runs.

**Invites.** Any member shares the cabal with one tap: a link (`trymonaco.xyz/join/<code>`), the eight-character code inside it, or a QR code of the link, from the cabal's details sheet. The link opens the app on the join screen with the code filled in; without the app it opens a page with the cabal's name, member count, and the code. A friend who is signed out signs in first and lands on the same join screen. Joining through a code follows the cabal's join policy: an open cabal takes them in, one by request sends the admin a request. One code is live per cabal; "New code" retires the old one for everyone.

## Votes and buys

On-chain governance is out of scope. Votes live in Postgres. The Go API is the source of truth.

1. A buy proposal names an asset from the catalog (tokenized US stocks via xStocks, and pre-IPO tokens via Tessera and PreStocks). Search the full public catalog, not a fixed ticker list. When SpaceX, OpenAI, or Kalshi exists from both issuers, the buy quote picks the cheaper one and the proposal stores that symbol. A sell keeps the issuer already held.
2. If Jupiter cannot quote a route, the UI refuses the proposal. Do not offer names the Meta-Aggregator cannot fill.
3. Members in the voter set vote yes or no before expiry.
4. On pass, the backend builds a Jupiter v2 order (`inputMint` = USDC, `outputMint` = xStock mint), signs with the treasury via Privy, and `POST`s `/execute`. Confirm `status: Success`, `code: 0`.
5. The token lands in the **group treasury**.

Constants:

- USDC mint: `EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v`
- xStock mints: `GET https://api.xstocks.fi/api/v2/public/assets/{symbol}` then `deployments` where `network == Solana` then `address`. Examples: `AAPLx` is `XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp`. `TSLAx` is `XsDoVfqeBukxuZHWhdvWHBhgEHjGNst4MLodqsJHzoB`.
- Tessera mints: `tSpaceX` `TSPXcLV76s6V2zDiZQ18kBfcbnjaE2ZzNT3ga2Pd99v`, `tKalshi` `TKLSidmLVt3cqGaaodG8tyRzoANfQwoh67AccjmubeZ`, `tOpenAI` `oPAiAikWTaFj9RYoRFD35ccfwhnMcB3ThgBZRHSkjTZ`.
- PreStocks mints: `SPACEX` `PreANxuXjsy2pvisWWMNB6YaJNzr7681wJJr2rHsfTh`, `OPENAI` `PreweJYECqtQwBtpxHL171nL2K6umo692gTm7Q3rpgF`, `KALSHI` `PreLWGkkeqG1s4HEfFZSy9moCrJ7btsHuUtfcCeoRua`, `ANTHROPIC` `Pren1FvFX6J3E4kXhJuCiAD5aDmGEb7qJRncwA8Lkhw`, `ANDURIL` `PresTj4Yc2bAR197Er7wz4UUKSfqt6FryBEdAriBoQB`, `NEURALINK` `PrekqLJvJ3qVdXmBGDiexvwUTF4rLFDa6HWS4HJbw9S`, `FIGUREAI` `PreZad18qfPtbxNpMtMuAuX2zVpvkEU8DnJx56faCWd`, `POLYMARKET` `Pre8AREmFPtoJFT8mQSXQLh56cwJmM7CFDRuoGBZiUP`.

## Pre-IPO tokens

Tessera and PreStocks tokens sit in the same catalog, propose, buy, hold, and sell path as tokenized stocks. The phone never calls those catalogs. The API does.

| Fact | Rule |
| --- | --- |
| Unit | 9 decimals. Quantities are tokens, not shares. PreStocks storage stays in raw atomics. The amount on screen is that raw amount times the mint's UI multiplier. |
| Best price | SpaceX, OpenAI, and Kalshi exist from both issuers. A buy compares Jupiter's executable price with the private-market reference and keeps the cheaper issuer. Anthropic, Anduril, Neuralink, Figure, and Polymarket are PreStocks only. |
| Mark | The Jupiter DEX price is the pot mark. A private-market reference is shown beside it when that reference is fresh. |
| Hours | These tokens trade around the clock. They never show an after-hours label. |
| Copy | Display names drop Tessera's `T-` prefix and PreStocks' ` PreStocks` suffix (SpaceX). The on-chain symbols stay `tSpaceX` and `SPACEX`. Holdings say which issuer they came from. |
| Fees | Tessera withholds 0.2% on transfer. PreStocks withholds 1%. The buy still quotes one mint. |
| Charts | No price history until samples exist. The detail screen says the chart is empty. |

The xStocks public API is mint metadata only. It is not an execution rail. Poll `/execute` for confirmation. Do not use a Jupiter WebSocket. Do not use Privy production webhooks (Enterprise-only).

## News

A member deciding whether to propose a stock wants to know what happened to it today. The stock screen shows the newest three headlines about it (all twelve behind "See all"), and the Stocks tab shows the day's market headlines under the movers. The API reads free public RSS, Yahoo Finance by ticker and Google News by company name for pre-IPO tokens, and caches each list for ten minutes; the app never calls a feed. Articles open in Safari's in-app reader. Contract: [News](api.md#news).

## Architecture

Who does what is in [Solana, Privy, and Bankr](#solana-privy-and-bankr). The API signs the treasury. The product UI does not explain custody.

```
SwiftUI + Privy OTP
Bankr skill (any host, agent key)
        → Go API
            → Postgres (shares, votes, NAV, agent budget)
            → Privy treasury on Solana
                → Jupiter (USDC → xStocks, Tessera, or PreStocks)
                → relayer pays SOL
```

### Retrying money requests

The app sends an `Idempotency-Key` header (one UUID per confirmed action, reused when the same submission is retried) on every user money POST: fund, withdraw, cash out, leave, propose, and transaction retry. The API scopes the key to the signed-in user and keeps it for 24 hours. Same key and same body replays the stored response (`Idempotency-Status: replayed`). Same key with a different body or route returns 422. A duplicate that arrives while the first is still running returns 409 with `Idempotency-Status: in_progress`, which tells the app to keep the key. 5xx responses are never stored, so a retry runs again. The header is optional; requests without it behave as before.

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

**One valuation.** Deposit credit, redeem quote and payout, the surplus reconcile, NAV snapshots, and the group screens all read the same pot valuation (`valuePot` in `apps/backend/internal/app/pot_valuation.go`):

- Pot NAV is treasury USDC that the group's ledger accounts for (net contributed − confirmed buys + confirmed sells) plus holdings at their mark. On-chain USDC above the ledger has landed without being credited yet and is nobody's gain.
- Total shares is every outstanding claim: position share units plus units already debited by a redeem that has not paid out.
- Shares mint 1:1 only while no shares are outstanding. After that every pot, cash-only or not, mints `amount × total shares / pre-credit NAV`. Mints and payouts round down, in favour of the pool.
- Anything that mints shares or pays USDC needs a live mark (Pyth, then Jupiter). With none, the deposit stays pending and is retried, and the redeem fails with the shares returned. Cost basis is never used as a price for money movement; screens and NAV snapshots may fall back to it so they keep rendering.
- The surplus reconcile only credits USDC the ledger cannot explain (a transfer straight to the treasury address). Realized gains stay P&L, and USDC owed to an in-flight redeem stays owed.

Marks: Jupiter fill price is cost basis. A live mark comes from Pyth Hermes first, then Jupiter's price for the mint (rejected under $10,000 of pool liquidity, more than 25% from the last accepted mark, or more than 5× from cost basis). A failing source is skipped by a circuit breaker until its cooldown ends (`apps/backend/internal/pricechain`). If the token still trades on-chain after the cash equity market closes, show an after-hours label.

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
4. **Settle on chain, exactly once.** The signed transfer and its signature are stored before the transfer is broadcast, and the redeem only settles (withdrawal row, NAV snapshot) once Solana confirms that signature. A transfer that fails on chain or expires without landing returns the share units. A redeem never signs a second transfer, so a retry can only finish the first one.
5. If the sale raises less USDC than the slice, the payout is what the treasury holds and **only the share units that payout covers are burned**. The member keeps the rest and can redeem them later.

They receive USDC equal to their redeemed fraction of the pot at that moment, not a refund of dollars they put in. That group's member board, the global group board, and the global people board all recompute from the new net-USDC-in figure.

## Stack

| Layer            | Choice                                                                                                       |
| ---------------- | ------------------------------------------------------------------------------------------------------------ |
| Mobile           | SwiftUI, iOS 18+ only                                                                                        |
| Auth and wallets | [Privy Swift](https://docs.privy.io/basics/swift/quickstart). Member wallets plus per-group server treasury. |
| API              | Go                                                                                                           |
| Ledger           | [Supabase](https://supabase.com/) Postgres. Share units, votes, NAV snapshots, idempotent tx log.            |
| Execution        | [Jupiter Swap API v2](https://dev.jup.ag/docs/swap) on mainnet                                               |
| Agent strategies | [Bankr skills](https://skills.bankr.bot/). Any host. Intents only. Fills stay in the Privy treasury.          |
| Fees             | App relayer (SOL)                                                                                            |
| Asset metadata   | [xStocks public API](https://api.xstocks.fi/api/v2/public/assets) (mints only)                               |
| Marks            | [Pyth Hermes](https://docs.pyth.network/price-feeds/core/api-instances-and-providers/hermes), then Jupiter Price API; cost basis for display only |

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

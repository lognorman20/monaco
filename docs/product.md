# Product

What Monaco does and the rules it follows, from the user's side. For how it is built, see [architecture.md](architecture.md).

## What Monaco is

Monaco lets friends run a small hedge fund together. A group, called a **cabal**, pools USDC into one shared pot. Members propose trades in tokenized US stocks, vote on them, and passed trades execute for the whole pot. Each member owns a share of the pot, so when the pot gains, every member's stake gains with it. A cabal can also vote in a trading agent with a capped budget.

The loop is: join, put money in, decide, watch the pot, decide again. Leaderboards inside each cabal and across the app are what bring people back.

## Words

| Word | Meaning |
| --- | --- |
| **Cabal** | A group with one shared pot. In code and the API it is a `group`. |
| **Account balance** | USDC in the user's own wallet, not yet in any cabal. |
| **Deposit** | Sending USDC into your account balance from outside Monaco. |
| **Fund** | Moving USDC from your account balance into a cabal's pot. This is what buys you shares. |
| **Pot** | Everything a cabal owns: its USDC plus its stock holdings. It sits in the cabal's treasury wallet. |
| **Share units** | Your claim on a fraction of the pot. Their value moves with the pot. |
| **Proposal** | A request the cabal votes on: buy, sell, or add, pause, resume or remove an agent. |
| **Voter set** | The members allowed to propose and vote. Either everyone or a named list. |
| **Cash out** | Selling some or all of your share units back to the cabal for USDC in your account balance. |
| **Withdraw** | Sending USDC from your account balance to an outside Solana address. |
| **Agent** | A program the cabal voted in to trade a slice of the pot. |

User-facing copy says "cabal", "pot value", "your slice" and "gain or loss". It never says "NAV", "P&L", "wallet", "gas", "mint" or "xStock", and it never shows a raw token address.

## Cabals

Anyone can create a cabal and invite people. One person can be in many cabals: a friends pot, a work pot, a pot built around one idea.

The creator sets the rules at creation:

- **Join mode.** Open (anyone can join) or by request (the creator approves each request).
- **Voter set.** Every member, or a named list (at least one person, which may be only the creator).
- **Threshold.** Majority of the voter set, or unanimous.
- **Vote expiry.** How long a proposal stays open. A proposal that has not passed by then dies and nothing trades.

Rules for membership:

- Only voter-set members can propose and vote.
- The creator cannot leave while other members remain.
- A member cannot leave while they still hold share units, unless they cash out as they leave. The last member cannot leave while the pot holds money.

Each cabal also has a chat, and each proposal has a comment thread.

## Money in

Money reaches a cabal in two steps, on purpose:

1. **Deposit.** Each user has one deposit address, shown in the app with a copy button. They send Solana USDC to it from anywhere. It shows up as account balance. There is no amount to type in the app.
2. **Fund.** The user picks a cabal and an amount up to their account balance. Monaco moves exactly that amount into the cabal's pot and credits share units at today's share price once the transfer confirms.

USDC sitting in the account balance is never swept into a cabal on its own, and never earns shares.

## Shares and pot value

You own a fraction of the pot, not a fixed number of dollars.

- **Pot value** = the pot's USDC plus each stock holding at its current price. Example: $40 USDC + 0.1 AAPLx worth $60 = $100.
- **Share price** = pot value ÷ total share units. In an empty cabal the first dollar buys one share at $1.
- **Funding** buys shares at today's price: `shares = USDC in ÷ share price`.
- **Your stake** = `your shares ÷ total shares × pot value`.
- **Cashing out** returns shares for that fraction of the pot in USDC.

**Why shares and not dollars.** Alex puts in $100 and the cabal buys Apple. Apple rises 10%, so the pot is $110. If Blair then put in $110 and was simply "owed $110", she would own half of a pot that includes Alex's gain. With shares, her $110 buys at $1.10 a share, so she gets the same 100 shares Alex has and does not take his gain.

Worked through (ignoring swap fees):

1. Empty cabal. Share price $1.
2. Alex funds $100 and gets 100 shares. Pot $100.
3. The cabal buys AAPLx with the $100.
4. AAPLx rises 10%. Pot $110. Alex's 100 shares are worth $110. Share price $1.10.
5. Blair funds $110 and gets `110 ÷ 1.10 = 100` shares. Pot $220, 200 shares. Each owns half.
6. Blair cashes out 50 shares: `50 ÷ 200` of the pot = $55. She keeps 50 shares.

Fine print, all enforced in code:

- Shares and payouts round down, in favour of the pot.
- Minting shares or paying out needs a live price for every holding. Without one, a fund waits and retries, and a cash out fails with the shares returned. Screens may show the purchase price instead so they still render.
- USDC that lands in a treasury without a matching fund is not anyone's gain until it is reconciled.
- If the stock token still trades after the US market closes, the app shows an after-hours label.

## Trading

1. A voter picks an asset from the catalog and an amount. The app checks the trade can route and shows the price. A buy can't be bigger than the whole pot. If Jupiter can't route it, the app refuses the proposal.
2. The voter set votes yes or no before expiry.
3. When the proposal passes, Monaco swaps from the pot. Bought tokens land in the cabal's treasury.
4. If the swap fails, the trade is marked failed and any member can retry it.

Sells work the same way and keep the issuer already held.

### What can be bought

- **Tokenized US stocks** from xStocks (`AAPLx`, `TSLAx`, …). The whole public catalog is searchable, not a fixed list.
- **Pre-IPO tokens** from Tessera and PreStocks (SpaceX, OpenAI, Kalshi, Anthropic, …). They use the same propose, buy, hold and sell path.

| Pre-IPO rule | Detail |
| --- | --- |
| Units | 9 decimals. Quantities are tokens, not shares. PreStocks amounts are stored raw and shown times the token's UI multiplier. |
| Best price | SpaceX, OpenAI and Kalshi exist from both issuers. A buy compares both and keeps the cheaper. Anthropic, Anduril, Neuralink, Figure and Polymarket are PreStocks only. |
| Price | The on-chain price values the pot. A private-market reference is shown beside it when fresh. |
| Hours | They trade around the clock and never show an after-hours label. |
| Names | Display names drop Tessera's `T-` prefix and PreStocks' suffix ("SpaceX"). Holdings say which issuer they came from. |
| Fees | Tessera withholds 0.2% on transfer, PreStocks 1%. |
| Charts | Empty until price samples exist; the screen says so. |

Reference addresses (Solana mainnet):

- USDC: `EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v`
- xStocks: `GET https://api.xstocks.fi/api/v2/public/assets/{symbol}`, then the `deployments` entry where `network == Solana`. `AAPLx` is `XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp`, `TSLAx` is `XsDoVfqeBukxuZHWhdvWHBhgEHjGNst4MLodqsJHzoB`.
- Tessera: `tSpaceX` `TSPXcLV76s6V2zDiZQ18kBfcbnjaE2ZzNT3ga2Pd99v`, `tKalshi` `TKLSidmLVt3cqGaaodG8tyRzoANfQwoh67AccjmubeZ`, `tOpenAI` `oPAiAikWTaFj9RYoRFD35ccfwhnMcB3ThgBZRHSkjTZ`.
- PreStocks: `SPACEX` `PreANxuXjsy2pvisWWMNB6YaJNzr7681wJJr2rHsfTh`, `OPENAI` `PreweJYECqtQwBtpxHL171nL2K6umo692gTm7Q3rpgF`, `KALSHI` `PreLWGkkeqG1s4HEfFZSy9moCrJ7btsHuUtfcCeoRua`, `ANTHROPIC` `Pren1FvFX6J3E4kXhJuCiAD5aDmGEb7qJRncwA8Lkhw`, `ANDURIL` `PresTj4Yc2bAR197Er7wz4UUKSfqt6FryBEdAriBoQB`, `NEURALINK` `PrekqLJvJ3qVdXmBGDiexvwUTF4rLFDa6HWS4HJbw9S`, `FIGUREAI` `PreZad18qfPtbxNpMtMuAuX2zVpvkEU8DnJx56faCWd`, `POLYMARKET` `Pre8AREmFPtoJFT8mQSXQLh56cwJmM7CFDRuoGBZiUP`.

## Agents

A cabal can vote to hand a slice of the pot to an agent, for example 10% for one strategy. Members see how much each agent has and how it has done, and vote to raise, cut, pause or remove it. A cabal can start with every trade as a vote, delegate once a strategy has a record, and pull the money if it doesn't work. The cabal always stays in charge.

How it works:

1. **The cabal votes the agent in** with a name and a USDC budget. When the vote passes, Monaco creates an API key for it.
2. **A member connects the agent.** In the app, **Group → Agent → Copy connect instructions** copies the key, the API address, and a link to the agent's instructions (`/v1/agent/skill.md`).
3. **The agent runs somewhere else**, usually on [ClawPump](how-to/connect-an-agent.md#connect-a-clawpump-agent): paste the instructions in as a custom skill and add an hourly automation. The same text works as the prompt for any LLM agent, and `agents/momentum-bot` is a small reference agent in Go.
4. **The agent sends trades to Monaco.** It reads its budget and prices, then sends buy or sell intents with its key.
5. **Monaco enforces the vote.** A wrong or removed key, a paused agent, or a trade over budget is refused. Valid trades run through the same swap path as a passed vote and land in the cabal's pot and activity feed.

The agent never holds the cabal's money. Turn off ClawPump's own trading skill so the agent can't trade from a ClawPump wallet instead, which would split the pot. Monaco never stores a ClawPump key.

Details: [agent-trading.md](agent-trading.md).

## Performance

Performance is the product. Rank by **percent return**, never by dollars, so a small pot can beat a big one. Dollar gain or loss sits next to the name.

`percent return = current value ÷ net USDC in − 1`, where net USDC in is money funded minus money cashed out. Anyone with nothing in is left off the board.

**Inside a cabal** (the cabal screen):

- **Pot.** USDC plus each holding with units, price, value and purchase price.
- **You.** Your stake in dollars and as a percent of the pot, with your gain or loss.
- **Members.** Every member, ranked by their return in this cabal.

**Across the app** (Home and the Cabals tab):

- **Cabal board.** One row per cabal with money in, ranked by the pot's return. Cabals compete on this. Join-by-request hides entry, not the score.
- **People board.** One row per person across all their cabals. Alex in three cabals is one row.

Pot value is saved after every fund, trade and cash out, so charts and boards can be replayed without recomputing from wallets.

## Money out

Also two steps, mirroring money in:

1. **Cash out** (the **Cash out** button on the cabal screen). Pick an amount from a small minimum up to your whole stake; cashing out everything is the same flow at the maximum. Your shares are debited, your slice of stock is sold if the pot is short of USDC, and USDC equal to your share of the pot lands in your account balance. You get your fraction of the pot now, not the dollars you put in. If the sale raises less than your slice, you are paid what it raised and keep the shares that didn't cover.
2. **Withdraw** (the **Cash out** button on the account balance card, on Home or Profile). Send USDC from your account balance to any Solana address you paste. This never touches cabal holdings.

Payouts are always USDC, never stock.

## Profile

The Profile tab shows the user's photo, display name, join date, account balance, deposit address, and each cabal they are in with its pot, their stake and their return.

- **Display name.** A label, not a unique handle. 1–32 characters, at least one letter or digit, no invisible or control characters. Edits show immediately and roll back with a toast if the server rejects them.
- **Photo.** Picked on Profile or Settings. Storage: [ops-profile-photos.md](ops-profile-photos.md).
- Both are rate limited, and a change updates every board that shows the user.

## Out of scope

- A custom on-chain vault, share token or on-chain voting
- Android and web apps, copy trading
- Buying or redeeming directly with a stock issuer (trades go through Jupiter only)
- App Store listing, KYC and AML, securities licensing

## Before a public launch

Tokenized stocks like `AAPLx` track a stock on-chain; they are not shares held at a broker. Pooling money and trading for a group raises broker-dealer, investment adviser and money-transmitter questions in the US. Get securities and fintech counsel before a consumer launch. Some issuers may require geo-fencing or a licensed partner for US users.

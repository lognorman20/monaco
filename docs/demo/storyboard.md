# Monaco demo film — storyboard

One film, about a hundred seconds, cut from clips recorded on the simulator with three real accounts and fake money (`DEMO_MODE=1`). It tells one story: three friends pool money, vote on a stock, watch it fill, and take profit. Every feature appears because the story needs it, not as a tour.

Two masters: a 9:16 vertical for social, and a 16:9 for the site with the phone centred on paper and the caption beside it. Same clips, same timing.

## Cast

| Account | Phone (fixed OTP) | Name in the film | Face |
| --- | --- | --- | --- |
| Alfred | +1 555 555 7177 / 465354 | Maya | fox (pixel animal, no photo) |
| Bartholomez | +1 555 555 9638 / 648588 | Jordan | owl |
| Cayman | +1 555 555 8215 / 115543 | Priya | penguin |

Names are set once in onboarding. Faces are whatever animal the id hashes to; if two collide, sign out and in on a different account order changes nothing, so accept the draw or rename the cabal, not the member. Everyone starts with $1,000 (demo mode).

## Beats

Timings are targets. Captions are set in Avenir Next on the paper, one line, lower third on vertical, beside the phone on the site cut. The voice-over line is what a narrator would say; the caption is shorter and can stand alone with the sound off.

| # | Clip (screen name) | Account | Seconds | What happens on screen | Caption | Voice-over |
| --- | --- | --- | --- | --- | --- | --- |
| 0 | `intro` (motion) | — | 4 | Cream. The app icon scales up from a coin, the wordmark "Monaco" settles beside it, the tagline types on. | The hedge fund with your friends. | Monaco. The hedge fund with your friends. |
| 1 | `login` | Maya | 6 | Login screen. Number typed, "Send code", the code step reads the number back in mono, six digits, Home appears. | Sign in with a text. No seed phrase. | You sign in with a text. There is no seed phrase to lose. |
| 2 | `start-cabal` | Maya | 10 | Cabals tab → + → Start a cabal. Name "Sunday Investors". The rules: everyone votes, majority passes, votes stay open 24 hours. Create. The cabal screen lands with its ink band. | Start a cabal. Set the rules once. | Start a cabal with your friends and set the rules once: who votes, what passes, how long a vote stays open. |
| 3 | `invite-join` | Maya → Jordan → Priya | 10 | Cabal details: the invite code, Copy. Cut: Jordan's phone, Join with an invite code, paste, Join. The cabal appears. Cut: Priya joins the same way, faster. The member row now shows three faces. | Friends join with a code. | Friends join with a code. That's the whole onboarding. |
| 4 | `fund` | Maya, Priya | 9 | Cabal → Add money → Fund this cabal, $500, "Add $500 to the pot". The pot ticks to $500. Cut: Priya funds $300; the pot reads $800, three slices. | Everyone puts money in the pot. | Everyone puts money in the pot, and everyone owns their slice of it. |
| 5 | `stocks` | Jordan | 10 | Stocks tab: rows with sparklines, in your cabals, popular. Tap GOOGL. The hero price, the curve draws on, scrub it, tap 1M and 1Y. Stats and stock-vs-token below. | Real stocks, live prices. Tokenized on Solana. | These are real stocks, tokenized on Solana, with live prices and history. |
| 6 | `propose` | Jordan | 10 | Propose buy → Sunday Investors → $250 → reason "Super bullish. This stock will only keep growing." → Review: cabal gets about N shares, price, share of the pot → Send to the cabal. | Propose a buy. Make the case. | Anyone can propose a buy and make the case. Nobody can buy alone. |
| 7 | `vote` | Maya, Priya | 12 | Maya's Home: "Needs your vote", GOOGL. Vote → Yes. Cut: Priya's phone, proposal screen, the tally reads 1 of 3, Yes. The tracker moves Voting → Buying → Done. The status chip reads Bought. The pot now holds GOOGL. | The cabal votes. A majority buys. | The cabal votes. When a majority says yes, the pot buys, on chain, at the live price. |
| 8 | `chat` | Jordan, Maya | 6 | Cabal chat: Jordan "in. told you", Maya "we own Google now", a "Voted yes" blip. | Talk it through in the cabal. | The conversation lives where the money is. |
| 9 | `pre-ipo` | Priya | 8 | Stocks → Pre-IPO section → open one (SpaceX via Tessera). Private-market reference, the premium, the disclosure, the other issuers. | Pre-IPO names too. | Pre-IPO names are here too, with the private-market reference beside the price. |
| 10 | `bot` | Maya, then all | 12 | Propose → Add a trading bot "Scout", $200 budget → Send. Cut: the vote passes. The bot's key card, the ClawPump connect instructions, Copy. Cut: activity shows "Scout bought $40 of NVDA". | Or let a bot trade a budget. | Or give a bot a budget. Connect it to ClawPump and it trades inside the rules the cabal set. |
| 11 | `profit` | Maya | 8 | Cabal → Cash out. 25% of the slice. "Your slice is worth $…". Cash out. The account balance ticks up. Receipt. | Take profit whenever you like. | Take profit whenever you like. It's your slice. |
| 12 | `board-outro` | Jordan | 7 | Cabals tab: the leaderboard, Sunday Investors climbing. Home: top investors with the three faces. Cut to cream: wordmark, "trymonaco.xyz". | trymonaco.xyz | Monaco. The hedge fund with your friends. |

Total: about 112 seconds.

## Rules for every clip

- Status bar at 9:41, full battery and bars (`scripts/demo/record-clip.sh` sets it). Light mode. Default text size.
- The app is launched fresh for each clip with `MONACO_API_BASE_URL` pointing at the demo backend, so nothing from a previous take bleeds in. Sign-in persists between launches on the same simulator; the account switch is a sign-out.
- No typing errors on camera: names and reasons are pasted from the clip script, not typed by hand.
- Leave a second of stillness at the start and end of every clip; the cut needs it.
- Never show a real address as the point of a shot. The deposit address is fine in passing.
- If a request fails on camera, stop and re-record; the film never shows an error state.

## Order of recording

Record in story order once the seed is in place, because later clips depend on earlier state (the cabal exists, the pot is funded, the proposal passed). `scripts/demo/README.md` has the setup and the commands. Between clips 3 and 4, and again before 9 and 10, `just faker mixed <group_id>` may add ghost members and history so the cabal looks lived in; it is optional and the film reads without it.

## Composition

`scripts/demo/compose.sh` assembles the film:

- Intro and outro cards are rendered from the app's own icon and the brand type on paper, with a scale-in and a type-on.
- Each clip is cropped to the phone, trimmed to its in and out point, and gets its caption faded in over the first second and out before the cut.
- Cuts are 300ms cross-dissolves on paper, not hard cuts, so the film breathes at the pace the app does.
- The site master frames the phone in a device bezel at the centre-right, caption at the left in the display face, on the cream canvas.
- Music: a single quiet instrumental bed at −24 LUFS under the whole film, ducked under the voice-over if there is one. None is bundled; drop a file at `docs/demo/music.m4a` and the script picks it up.

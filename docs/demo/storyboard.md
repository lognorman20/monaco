# Monaco demo film — storyboard

One film, about 85 seconds, cut from takes recorded on the simulator with three real accounts and fake money (`DEMO_MODE=1`). It tells one story: three friends pool money, vote on a stock, watch it fill, and take profit. Every feature appears because the story needs it, not as a tour.

Two masters: a 9:16 vertical for social, and a 16:9 for the site with the phone centred on paper and the caption beside it. Same clips, same timing.

## Cast

| Account | Phone (fixed OTP) | Name in the film | Face |
| --- | --- | --- | --- |
| Alfred | +1 555 555 7177 / 465354 | Maya | cat (the animal her id hashes to) |
| Bartholomez | +1 555 555 9638 / 648588 | Jordan | rabbit (same) |
| Cayman | +1 555 555 8215 / 115543 | Priya | penguin (picked in the face sheet on Profile, off camera) |

Names are set once in onboarding. A member without a photo wears the animal their id hashes to, and the face sheet on Profile lets them pick another or a photo; the film does not show the sheet, it is something to find. Everyone starts with $1,000 (demo mode).

## Beats

One take per row, shot in this order because each needs the state the one before leaves behind. Captions are one short line on the paper; a blank one lets the screen speak. The cut (segments, speed, punches and cards per beat) lives in `scripts/demo/film.py`.

| # | Take | Account | Caption | On screen |
| --- | --- | --- | --- | --- |
| 0 | intro card | | The hedge fund with your friends. | Icon, wordmark, the app's tagline |
| 1 | `login` | Maya | Sign in | Number, code, Home |
| 2 | `onboarding` | Maya | Create your profile | Name typed, Continue |
| 3 | `start-cabal` | Maya | Start a cabal / Set the rules | Name "Sunday Investors", who joins, who votes, what passes, Create |
| 4 | `invite-join` | Maya | Invite your friends with a code | Cabal details, Copy code |
| 5 | `join-jordan` | Jordan | A paste and they're in! | Join with an invite code, paste, the cabal appears |
| 6 | `fund` | Jordan | Fund the pot | Add money, $500, the pot reads $500 |
| 7 | `join-priya` | Priya | Anyone with the code can join | Same join, three faces in the hero |
| 8 | `fund-priya` | Priya | Everyone owns a slice | $300 in, the pot reads $800, her slice |
| 9 | `fund-maya` | Maya | | $200 in, the pot reads $1,000 |
| 10 | `stocks` | Jordan | Browse real stocks | Stocks tab, Alphabet, scrub, 1M, 1Y, stats |
| 11 | `propose` | Jordan | Propose a buy | $250, "Super bullish. This stock will only keep growing.", Review, Send |
| 12 | `vote-priya` | Priya | Everyone votes | Needs your vote, Yes, 1 of 3 |
| 13 | `vote-maya` | Maya | Majority wins, the cabal buys | Yes, passes, the tracker reaches Done, Bought |
| 14 | `chat-jordan` | Jordan | Talk it over | "In. Told you" |
| 15 | `chat-maya` | Maya | | "We own Google now" |
| 16 | `pre-ipo` | Jordan | Pre-IPO too | Pre-IPO section, SpaceX, the reference price and premium, the issuers |
| 17 | `bot` | Maya | Add a trading bot | Propose, Add a trading bot, $200, "Scout", Send |
| 18 | `vote-bot` | Jordan | | Yes, Passed |
| 19 | `bot-key` | Jordan | Connect it to ClawPump | The bot's screen, the key card, Copy connect instructions |
| 20 | `bot-activity` | Jordan | Watch it trade | Activity: Scout bought $40 of Nvidia (`scripts/demo/agent-intent.sh buy NVDAx 40`) |
| 21 | `profit` | Maya | Cash out any time | Cash out, 25%, the pot and slice shrink, the balance grows |
| 22 | `board-outro` | Maya | See who's up | Cabals tab, Home top investors with the three faces |
| 23 | outro card | | trymonaco.xyz | |

## The launch spot

`scripts/demo/film.py --film=launch` cuts a second film from the same takes: thirty seconds for the waitlist, at `docs/demo/out/monaco-launch-{vertical,site}.mp4`. One story and one pot, so every figure on screen agrees with the one before it. The hook is the product in four words, the ask is the site.

| # | Take | Seconds | Caption | On screen |
| --- | --- | --- | --- | --- |
| 1 | `join-priya` | 3.0 | Invest with your friends. | The cabal with $500 in the pot, as a headline over the phone |
| 2 | `start-cabal` | 2.4 | Start a cabal | The name typed, Create, the cabal lands |
| 3 | `join-jordan` | 1.9 | Friends join with a code | Join cabal, the cabal with 2 members |
| 4 | `fund` | 1.7 | Fund the pot | $500 typed, Add $500 to the pot |
| 5 | `join-priya` | 2.4 | Anyone with the code can join | Join cabal, the $500 pot with 3 members, coins |
| 6 | `stocks` | 2.0 | Pick a real stock | Stocks, Alphabet, the curve draws |
| 7 | `propose` | 4.1 | Propose a buy | Propose buy, the cabal, $250, the thesis, Review, Send, the toast |
| 8 | `vote-priya` and `vote-maya` | 1.8 | Everyone votes | Two phones, two Yes taps |
| 9 | `vote-maya` | 2.9 | Majority wins. The cabal buys. | Bought, the tracker at Done, coins, the cast |
| 10 | `pre-ipo` | 1.6 | Pre-IPO too | The pre-IPO list, SpaceX with its private-market reference |
| 11 | `bot` | 1.1 | Or add a trading bot | $200 budget, Scout, Send to cabal |
| 12 | sign-off | 6.4 | Join the waitlist, trymonaco.xyz | Icon, wordmark, the line, the ask typed on, the cast |

Every cut lands on the frame it names: the takes are filled to a constant rate before cutting, since the simulator only records a frame when the screen changes. Where a take was shot after the story's numbers had moved on, the film covers what would contradict them: the relative stamps the app writes in real time ("34m", a fill date), the $800 holdings figure on Priya's phone (re-set as $500 in the app's own face, moving with its row), the ghost of her slice under the status bar, and an initials disc that predates the animal faces. Each cover copies the frame's own pixels, so nothing is painted in.

## Rules for every clip

- Status bar at 9:41, full battery and bars (`scripts/demo/record-clip.sh` sets it). Light mode. Default text size.
- The app is launched fresh for each clip with `MONACO_API_BASE_URL` pointing at the demo backend, so nothing from a previous take bleeds in. Sign-in persists between launches on the same simulator; the account switch is a sign-out.
- No typing errors on camera: names and reasons are pasted from the clip script, not typed by hand.
- Leave a second of stillness at the start and end of every clip; the cut needs it.
- Never show a real address as the point of a shot. The deposit address is fine in passing.
- If a request fails on camera, stop and re-record; the film never shows an error state.

## Order of recording

Record in story order, because later takes depend on earlier state (the cabal exists, the pot is funded, the proposal passed). `scripts/demo/README.md` has the setup and the commands.

## Composition

`scripts/demo/film.py` cuts the film, about 85 seconds:

- The title card pops the icon and types the wordmark; a cast card bounces the three faces in; the pot counts up on its own card with coins; the sign-off brings the cast back.
- Each beat is a few segments of its take at 1.6 to 2 times life, some punched in on the moment that matters. Where two people do the same thing, their phones run side by side. Every phone settles into place and every caption slides up.
- Coins fly when money moves (the pot, the buy, the bot's trade, the cash-out), and the cast cheers the buy from the foot of the frame.
- Joins are hard cuts within one phone and slides or dissolves when one phone hands to another.
- Music: none is bundled; a file at `docs/demo/music.m4a` is mixed in when present.

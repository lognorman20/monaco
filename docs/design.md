# Monaco design language (iOS)

Direction in one line: **a members-only fund, typeset like a private ledger, that reads like a group chat.** Cream paper, forest ink, gold coins. Words in Avenir Next; the market in monospace.

Signature: the ledger rule. Sections are ruled tables on the paper, not cards on a canvas. Money you own is set in Avenir Next; market data (tickers, quotes, day moves, timestamps) is set in SF Mono. A cabal's tint is its identity wash.

Rejected on purpose: card grids and nested cards, purple or blue gradients, glass and glow, neon-on-dark, stretched display type, emoji in chrome, LIVE pills, uppercase mono eyebrow labels, a forest canvas (tried, too green).

## Tokens

| Role | Light | Dark | Notes |
| --- | --- | --- | --- |
| canvas | `#F8F5EE` | `#0B1512` | flat paper, no gradient |
| surface | `#FFFFFF` | `#13211B` | cards that earn it, sheets, tab bar |
| surfaceSunken | `#F1ECE2` | `#1D2E26` | fields, chips at rest, skeletons, pressed rows |
| ink / primaryText | `#0F291C` | `#F3EEE5` | |
| secondaryText | `#55645B` | `#A3B3A9` | |
| tertiaryText | `#5D6C62` | `#92A398` | timestamps, AA on every surface |
| hairline | `#E5DFD2` | `#2A3E35` | 1pt rules |
| brand (text/glyph) | `#204A37` | `#BFE6C8` | interactive only, never a gain |
| brandFill / onBrand | `#0F291C` / cream | `#E4F2E6` / ink | primary buttons, selected chips |
| heroInk | `#0F291C` | same | the cabal hero band |
| profit / loss | `#007A45` / `#B23A28` | `#35D68C` / `#F08A72` | signed P&L only |
| warning | `#8A5A16` | `#E5B26A` | pending, after hours |
| gold / goldGlyph | `#7A5C12` / `#C9A24A` | `#E2B04A` / `#D9B85A` | rank 1, the coin's family; never a button |
| CabalTint ×5 | pine, ochre, plum, indigo, moss | | identity only: marks, strip card wash, chart lines, the hero's rule |

Type (Avenir Next unless stated; every role scales with Dynamic Type):

| Role | Face | Size | Used for |
| --- | --- | --- | --- |
| display | Bold | 34 | screen titles set in content, login wordmark |
| title | DemiBold | 24 | cabal name in the hero, sheet titles |
| section | DemiBold | 20 | section headers |
| rowTitle / bodyStrong / button | DemiBold | 17 | row titles, emphasis, buttons |
| body | Regular | 17 | prose, chat |
| callout / calloutStrong | Regular / DemiBold | 16 | captions with a verb, chips, "See all" |
| caption / captionStrong | Medium / DemiBold | 13 | labels under figures, subtitles |
| micro | DemiBold | 11 | status chips |
| money hero / large / row / caption | DemiBold (caption: Medium) | 44 / 28 / 17 / 13 | dollars and returns you own; digits are tabular by default |
| ticker | SF Mono Semibold | 17 | the stock's label on every row |
| quote hero / quote | SF Mono Semibold / Medium | 40 / 15 | market prices |
| data / dataCaption / dataMicro | SF Mono Medium / Medium / Semibold | 15 / 13 / 11 | day moves, ranks, stats, timestamps, addresses |

Radius: card 16 · sheet 24 · tile 16 (marks, scales with size) · field 12 · bubble 18 · buttons and pills are capsules.

Spacing: 4 · 8 · 12 · 16 · 24 · 32; gutter 20. Rows are 60pt with a 44pt mark; rules inset to the text.

Motion: 150–250ms state changes; the chart draw-on; the price flash; the numeric roll. Nothing loops except the live dot and skeletons. Reduce Motion turns all of it off.

## Composition rules

- A section is a header, an optional trailing action, a rule, rows, a rule. No surface behind it.
- A card is only a card when it is the thing you act on: a proposal you can vote on, a mover you can tap, a cabal in the strip.
- Forms are ledgers too: a money or settings screen is sections of ruled rows on the paper, never a system `Form` with white grouped cells. The one card allowed is the thing you act on (a deposit address, an invite code, a bot's key).
- Skeletons take the shape of the rows they stand in for (`BoardRowSkeleton`, the Home skeleton), never card-shaped blocks.
- One high-impact surface per screen: the cabal hero band (ink). Home's money sits on the paper.
- Colour says one thing at a time: green up, red down, ink interactive, tint identity, gold rank.
- Labels are sentence case. No eyebrow labels, no ALL CAPS, no "P&L", no "NAV".
- Sample harnesses cover every state; `scripts/qa/screens.sh` is the acceptance gallery.

# Polish v2 — QA screenshots

`before/` is `main` at 00271e2. `after/` is the v2 visual pass on the same
harnesses, same simulator, same status bar, in both colour schemes.

Captured on the iPhone simulator `B6897011-ABFD-4422-A553-32BC3F298C30`,
status bar overridden to 9:41.

## Palette

| Role | Light | Dark |
| --- | --- | --- |
| Brand accent | `#1652F0` | `#3B7BFF` |
| Brand button fill | `#1652F0` | `#2C6BF5` |
| Canvas | `#F5F7FA` | `#0A0D14` |
| Surface | `#FFFFFF` | `#121826` |
| Sunken | `#EDF1F7` | `#1B2334` |
| Primary text | `#0B1220` | `#F3F6FB` |
| Profit (text) | `#00874D` | `#1FD286` |
| Profit (charts, on ink) | `#00B368` | `#1FD286` |
| Loss (text) | `#DC2F33` | `#FF5A5F` |
| Loss (charts, on ink) | `#E5383B` | `#FF5A5F` |
| Money hero card | `#0B1220` | `#151D30` |

Brand blue carries primary buttons, the selected tab, links, selected
segments, the "Yes" vote and its tally dots, and my chat bubbles. It never
means gain — green does.

White on the brand button fill is 6.00:1 in light and 4.64:1 in dark, so the
dark fill is deeper than the accent used for text and strokes. The P&L pair
splits for the same reason: `profit` / `loss` clear AA as text on white
(4.59:1 and 4.67:1), while the vivid pair is reserved for chart strokes,
fills and figures on the deep ink cards, where the dark background carries
the contrast (6.8:1 and better).

Cabal tints are resaturated so their marks carry white initials, and a
brighter `onInk` variant keeps the mark legible on the ink hero cards. A
cabal is the same colour in its mark, its strip card, its hero and its line
on the cabals chart.

## Screens

| Screen | Harness arguments |
| --- | --- |
| `login` | *(none)* |
| `home` | `-MonacoHomeSample populated` |
| `group-detail` | `-MonacoGroupDetailSample populated` |
| `feed` | `-MonacoProposalFeedSample` |
| `cabals` | `-MonacoCabalsTabSample` |
| `profile` | `-MonacoProfileSample photo` |
| `chat` | `-MonacoChatSampleQA` |
| `propose` | `-MonacoProposalFeedSample -MonacoProposeSample -MonacoProposeSampleOpen` |
| `gallery-money` | `-MonacoDesignGallery -MonacoDesignGalleryTab money` |

## Not re-captured

- `propose-amount` / `propose-pick` / `propose-review` in `before/` are the
  later steps of the propose flow. They need the sheet driven by hand, so
  `after/` has the chooser only; those steps inherit the same tokens.
- Asset (Stocks) list and detail need a backend, so they have no harness on
  either side. The detail chart was restyled but is unverified by screenshot.
- The app icon still carries the old green accent circle. The in-app mark
  (`MonacoMark`) is now brand blue; regenerating the icon means re-running
  `scripts/design/render-app-icon.swift`.

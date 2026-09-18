# Mobile design review

Issue #191. Inspired by [Orbix Studio’s Food Delivery Mobile App](https://me.muz.li/orbix-studio/food-delivery-mobile-app).

## Design

Warm paper and ink replace the navy/teal chrome. Avenir Next headings, rounded portfolio/cabal/stock cards, restrained mint and peach surfaces, and pill actions adapt the reference to investing. Initial marks are typographic identities, not fake avatars or company logos. Chart values and portfolio balances remain API-driven.

Shared styles live in `MonacoTheme`, `MonacoAppearance`, and `MonacoIdentityMark`. Existing tabs, view models, authentication, API contracts, and refresh paths are retained. Unavailable stocks cannot enter the Buy flow. Numeric balance transitions and button springs respect Reduce Motion; native tabs provide selection haptics.

## Verification

- 53 host mobile tests pass.
- Native simulator build passes with the working encrypted environment.
- SimSlim disables 110 background services; required keychain, universal-link, and push checks pass.
- Three fixture XCTest scenarios pass: five tabs/stock detail/buy picker, onboarding tour, and dark appearance with accessibility text size on Home/Profile.
- Reviewed and corrected full-row tap areas, fitting of identity initials, distinct chart colors, and large-text chart/leaderboard layouts.
- No backend changes. Screenshots are from a separate, visibly labeled sample-data build. They verify UI rendering/navigation, not real authentication or financial execution.

## Before / after

| Previous Home | Redesigned Home |
|---|---|
| ![Previous Home](../177/home.png) | ![Redesigned Home](home.png) |

## Screens

| Assets | Stock detail |
|---|---|
| ![Assets](assets.png) | ![Stock detail](asset-detail.png) |

| Cabals | Profile |
|---|---|
| ![Cabals](cabals.png) | ![Profile](profile.png) |

### Dark appearance / large text

![Dark appearance at accessibility text size](home-dark-large.png)

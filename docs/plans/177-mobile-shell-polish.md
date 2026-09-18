# Issue 177: mobile shell polish

Scope: finish Logan's oh-yea integration, preserving the existing navy/teal theme and HTTP contracts. Delivery base is oh-yea; current main integration was checked separately and preserved locally. No sell, avatar upload, trading mandates, or unrelated redesign.

## Implementation
- [x] Refresh /me and /home together through one session-owned state loader. Publish only the newest successful response; preserve visible data on transient errors and discard results after logout.
- [x] Refresh after successful membership/profile/proposal mutations, tab changes, foregrounding, and while the shell is visible for background deposit credits. Propagate current membership into pushed asset/cabal pickers.
- [x] Use canonical MonacoCore Home/Groups/Assets DTOs and run decoding tests.
- [x] Refine onboarding/profile and dashboard hierarchy within current visual tokens; remove redundant nested empty cards, dev jargon, generic CTAs, and raw mint rows. Preserve truthful admin-approval join labels.
- [x] Audit actual route registration and test unauthenticated requests through the production mux.

## Verification
- Regression tests: latest refresh wins, failed refresh retains data, logout discards in-flight results; DTO decoding.
- Required gates: just test mobile, just build mobile, just test backend, just build backend with isolated local Docker DB and no live Jupiter calls in tests.
- SimSlim on this machine's existing Monaco simulator. Check fresh OTP/onboarding, all five tabs, profile name, asset detail and buy picker, create/join refresh, and logout. Capture screenshots. Report any blocked acceptance criteria; never claim fixtures prove live auth or money movement.
- Review diff and attach evidence to a draft PR targeting oh-yea. Do not merge Logan's PR.

## Status
Implementation and fixture UI checks pass. Live authentication and standard environment-dependent gates remain blocked; see ../qa/177/README.md.

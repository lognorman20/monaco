# Monaco docs

Monaco is an iOS app where friends pool USDC into a shared pot (a **cabal**), vote on which tokenized US stocks to buy on Solana, and compete on leaderboards. A cabal can also hand part of its pot to a trading agent.

## Start here

Read these in order to understand the repo:

1. **[Product](product.md)**: what the app does, the words it uses, and the rules for cabals, votes, shares and money in and out.
2. **[Architecture](architecture.md)**: the parts of the system, every outside service, the wallets, and how each money flow works, with diagrams.
3. **[README](../README.md)**: clone, configure and run everything locally.

## Reference

| Doc | Read it when |
| --- | --- |
| [api.md](api.md) | You need a route, its auth, rate limits, idempotency or error shape |
| [agent-trading.md](agent-trading.md) | You are building or debugging a trading agent |
| [design.md](design.md) | You are changing the iOS UI: colours, type, layout rules |
| [multi-user-verification.md](multi-user-verification.md) | You changed auth, membership, money or boards and need to re-verify |

## How-to

| Guide | For |
| --- | --- |
| [Connect a trading agent](how-to/connect-an-agent.md) | Hooking up ClawPump, any LLM agent, or `agents/momentum-bot` |
| [Demo checklist](how-to/demo-checklist.md) | A manual end-to-end pass before a demo |
| [Run on the local simulator](how-to/local-simulator.md) | Simulator signing and keychain issues |
| [Debug login](how-to/debug-login.md) | "I can't sign in" |
| [Overnight QA](how-to/overnight-qa.md) | The nightly test and screenshot run, and what CI runs |
| [TestFlight](../apps/mobile/TestFlight.md) | Shipping an iOS build |

## Operations

| Doc | For |
| --- | --- |
| [ops-observability.md](ops-observability.md) | Logs, metrics, alerts, `/health` |
| [ops-sweep-wallets.md](ops-sweep-wallets.md) | Emergency: moving USDC out of Privy wallets |
| [ops-profile-photos.md](ops-profile-photos.md) | Where profile photos are stored |

## Other folders

- [`demo/`](demo/storyboard.md): the demo film's storyboard. Recording scripts are in `scripts/demo`.
- [`archive/`](archive/README.md): the build history (milestone plans, design sketches, agent handoffs, QA screenshots, hackathon notes). Kept for context and not updated, so parts of it no longer match the code. Trust the docs above over anything in the archive.

## Conventions for these docs

- Product rules go in `product.md`. How the system is built goes in `architecture.md`. How to run things goes in the README or `how-to/`.
- Link to code by path (`apps/backend/internal/app/redeem.go`) rather than copying it.
- When code changes a rule or a flow, update the doc in the same pull request.

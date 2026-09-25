# Agent deployments (fund a ClawPump agent from the cabal)

A cabal votes to send treasury USDC to a ClawPump agent's own **Solana** wallet, and later votes
to call it back. The agent trades that USDC itself. Monaco never signs the agent's trades.

## Chain

Everything in this flow is Solana mainnet, SPL USDC mint
`EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v`. Nothing here touches Base, the Dynamic signer,
Kyber, or `wallets.PayUSDC`.

- Each cabal gets a Privy Solana server wallet, provisioned the first time someone proposes a
  deploy, stored in `group_solana_treasuries`. Its SPL USDC counts toward pot NAV.
- Outbound: `internal/solana/treasury` builds a relayer-paid SPL transfer (create-idempotent
  destination token account + `Transfer`), Privy co-signs as treasury owner. Ported from
  `archive/main-before-dynamic` `internal/privy/payout.go`.
- Inbound: the poller scans the treasury's USDC token account (`getSignaturesForAddress` +
  `getTransaction` jsonParsed) for confirmed transfers from the agent wallet.

## Flow

| Step | Trigger | What happens |
| --- | --- | --- |
| Propose deploy | `POST /v1/groups/{id}/proposals` `{"kind":"deploy_agent","agentWalletAddress":"<solana>","usdc":<micros>,"operatorKey":"cpk_…"?}` | Rejected when `usdc` exceeds Solana treasury USDC cash, or the wallet already has an active deployment or open deploy vote. `operatorKey` is sealed (AES-GCM, `WALLET_SHARES_KEY`). |
| Deploy passes | vote | `group_agent_deployments` row, `pending_transfer`. No `group_agents` row, no Monaco API key. |
| Send | `AgentDeploymentPoller` | Sign, store signature, broadcast, confirm → `deployed`, `nav_snapshots` reason `agent_deployment`. A stored signature is never re-sent; a dropped one is unlinked and re-signed. |
| Agent trades | — | Monaco does nothing. |
| Propose recall | `{"kind":"recall_agent","agentWalletAddress":"<solana>"}` | Needs a `deployed` row with USDC outstanding. |
| Recall passes | vote | `recalling`. With an operator key the poller calls ClawPump MCP `set_external_wallet` (treasury) then `agent_send` (outstanding), once. Without one, the vote is a request to the agent owner. |
| Return | `AgentDeploymentPoller` | Each confirmed inbound SPL USDC transfer from the agent wallet is recorded once (`tx_signature` unique) and credited up to outstanding; `closed` when fully returned. |

Pot NAV and `GET /v1/groups/{id}/view` include `deployed − returned` (a `Deployed USDC` pot row)
only while `deployed` or `recalling`. Share units never change.

## Configuration

`PRIVY_APP_ID`, `PRIVY_APP_SECRET`, `PRIVY_AUTHORIZATION_PRIVATE_KEY`, `SOLANA_RELAYER_PRIVATE_KEY`
are required to enable it; `PRIVY_AUTHORIZATION_KEY_ID`, `SOLANA_RPC_URL`, `CLAWPUMP_MCP_URL` are
optional. Without them deploy/recall votes return 503 and the poller does not start.

## Known gaps

- Member deposits and redeems still settle on Base. Getting USDC into a cabal's Solana treasury
  is an ops step until treasuries move to Solana.
- ClawPump does not publish the argument schema for `set_external_wallet` / `agent_send`; the
  client sends `{address}` and `{to, token:"USDC", amount:"<decimal>"}`. Verify against a live
  agent before relying on operated recall.

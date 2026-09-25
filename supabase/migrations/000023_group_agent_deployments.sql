-- Fund a ClawPump agent from the group treasury, on Solana.
--
-- A passed deploy_agent vote sends SPL USDC (mint EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v)
-- from the group's Privy Solana treasury to the agent's own Solana wallet. A passed
-- recall_agent vote asks for it back; the ledger only credits a return once a confirmed
-- inbound SPL USDC transfer from that agent wallet lands in the treasury.
--
-- Tables only. treasuries.address (Base, since 000018) is untouched; the Solana treasury
-- wallet lives in group_solana_treasuries.

CREATE TABLE IF NOT EXISTS group_solana_treasuries (
  group_id uuid PRIMARY KEY REFERENCES groups (id),
  privy_wallet_id text NOT NULL,
  solana_address text NOT NULL UNIQUE,
  created_at timestamptz NOT NULL DEFAULT now()
);

-- Withdrawals-shaped, but never a member payout: no user_id, no share debit.
CREATE TABLE IF NOT EXISTS group_agent_outbound_transfers (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  group_id uuid NOT NULL REFERENCES groups (id),
  to_address text NOT NULL,
  amount bigint NOT NULL CHECK (amount > 0),
  status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'confirmed', 'failed')),
  tx_signature text UNIQUE,
  -- Last block height at which tx_signature can still land; past it an unseen signature
  -- is dropped and the job may sign a fresh transfer.
  last_valid_block_height bigint,
  created_at timestamptz NOT NULL DEFAULT now()
);

-- Deposits-shaped, but never a member deposit: no user_id, no share credit.
CREATE TABLE IF NOT EXISTS group_agent_inbound_transfers (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  group_id uuid NOT NULL REFERENCES groups (id),
  from_address text NOT NULL,
  amount bigint NOT NULL CHECK (amount > 0),
  status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'confirmed')),
  tx_signature text UNIQUE,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS group_agent_deployments (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  group_id uuid NOT NULL REFERENCES groups (id),
  agent_wallet_address text NOT NULL,
  deployed_usdc_micros bigint NOT NULL CHECK (deployed_usdc_micros > 0),
  returned_usdc_micros bigint NOT NULL DEFAULT 0 CHECK (returned_usdc_micros >= 0),
  status text NOT NULL CHECK (status IN ('pending_transfer', 'deployed', 'recalling', 'closed')),
  deploy_proposal_id uuid REFERENCES proposals (id),
  recall_proposal_id uuid REFERENCES proposals (id),
  outbound_transfer_id uuid REFERENCES group_agent_outbound_transfers (id),
  inbound_transfer_id uuid REFERENCES group_agent_inbound_transfers (id),
  operator_key_enc text,
  -- Set once set_external_wallet and agent_send both succeeded for this recall, so the
  -- poller never asks an operated agent to send twice. It is not a return: only a confirmed
  -- inbound transfer moves returned_usdc_micros.
  recall_command_sent_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT group_agent_deployments_returned_le_deployed CHECK (returned_usdc_micros <= deployed_usdc_micros)
);

CREATE UNIQUE INDEX IF NOT EXISTS group_agent_deployments_one_active_per_wallet
  ON group_agent_deployments (group_id, agent_wallet_address)
  WHERE status IN ('pending_transfer', 'deployed', 'recalling');

CREATE INDEX IF NOT EXISTS group_agent_deployments_status_idx
  ON group_agent_deployments (status)
  WHERE status IN ('pending_transfer', 'recalling');

-- An optional cpk_ operator key submitted with a deploy proposal, sealed until the vote
-- passes. The pass moves it onto the deployment and deletes this row.
CREATE TABLE IF NOT EXISTS group_agent_deploy_operator_keys (
  proposal_id uuid PRIMARY KEY REFERENCES proposals (id) ON DELETE CASCADE,
  operator_key_enc text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE proposals
  ADD COLUMN IF NOT EXISTS agent_wallet_address text;

ALTER TABLE proposals DROP CONSTRAINT IF EXISTS proposals_kind_check;
ALTER TABLE proposals
  ADD CONSTRAINT proposals_kind_check CHECK (
    kind IN (
      'buy', 'sell',
      'add_agent', 'pause_agent', 'resume_agent', 'revoke_agent',
      'deploy_agent', 'recall_agent'
    )
  );

ALTER TABLE proposals DROP CONSTRAINT IF EXISTS proposals_amount_by_kind_check;
ALTER TABLE proposals
  ADD CONSTRAINT proposals_amount_by_kind_check CHECK (
    (kind = 'buy' AND usdc_micros IS NOT NULL AND usdc_micros > 0 AND token_amount IS NULL)
    OR
    (kind = 'sell' AND token_amount IS NOT NULL AND token_amount > 0 AND usdc_micros IS NULL)
    OR
    (kind = 'add_agent' AND allocation_usdc_micros IS NOT NULL AND allocation_usdc_micros > 0
      AND agent_display_name IS NOT NULL AND btrim(agent_display_name) <> '')
    OR
    (kind IN ('pause_agent', 'resume_agent', 'revoke_agent'))
    OR
    (kind = 'deploy_agent' AND usdc_micros IS NOT NULL AND usdc_micros > 0 AND token_amount IS NULL
      AND allocation_usdc_micros IS NULL
      AND agent_wallet_address IS NOT NULL AND btrim(agent_wallet_address) <> '')
    OR
    (kind = 'recall_agent' AND usdc_micros IS NULL AND token_amount IS NULL
      AND allocation_usdc_micros IS NULL
      AND agent_wallet_address IS NOT NULL AND btrim(agent_wallet_address) <> '')
  );

ALTER TABLE nav_snapshots DROP CONSTRAINT IF EXISTS nav_snapshots_reason_check;
ALTER TABLE nav_snapshots
  ADD CONSTRAINT nav_snapshots_reason_check CHECK (reason IN (
    'deposit',
    'transaction_confirm',
    'withdrawal_payout',
    'agent_deployment'
  ));

-- M6 agentic trading: group agents, intents, governance proposal kinds.

CREATE TABLE IF NOT EXISTS group_agents (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  group_id uuid NOT NULL REFERENCES groups (id),
  status text NOT NULL CHECK (status IN ('pending', 'active', 'paused', 'revoked')),
  install_proposal_id uuid REFERENCES proposals (id),
  pause_proposal_id uuid REFERENCES proposals (id),
  resume_proposal_id uuid REFERENCES proposals (id),
  revoke_proposal_id uuid REFERENCES proposals (id),
  agent_display_name text NOT NULL,
  allocation_usdc_micros bigint NOT NULL CHECK (allocation_usdc_micros > 0),
  api_key_hash text,
  api_key_prefix text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS group_agents_one_active_or_paused_per_group
  ON group_agents (group_id)
  WHERE status IN ('active', 'paused');

CREATE INDEX IF NOT EXISTS group_agents_api_key_hash_idx
  ON group_agents (api_key_hash)
  WHERE api_key_hash IS NOT NULL AND status <> 'revoked';

CREATE TABLE IF NOT EXISTS group_agent_key_reveals (
  proposal_id uuid PRIMARY KEY REFERENCES proposals (id) ON DELETE CASCADE,
  plaintext_key text NOT NULL
);

CREATE TABLE IF NOT EXISTS agent_intents (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  group_agent_id uuid NOT NULL REFERENCES group_agents (id),
  group_id uuid NOT NULL REFERENCES groups (id),
  side text NOT NULL CHECK (side IN ('buy', 'sell')),
  symbol text NOT NULL,
  usdc_micros bigint,
  token_amount bigint,
  status text NOT NULL CHECK (status IN ('accepted', 'rejected', 'executed', 'failed')),
  reject_reason text,
  transaction_id uuid REFERENCES transactions (id),
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS agent_intents_group_id_created_at_idx
  ON agent_intents (group_id, created_at DESC);

ALTER TABLE proposals
  ADD COLUMN IF NOT EXISTS agent_display_name text,
  ADD COLUMN IF NOT EXISTS allocation_usdc_micros bigint;

ALTER TABLE proposals DROP CONSTRAINT IF EXISTS proposals_kind_check;
ALTER TABLE proposals
  ADD CONSTRAINT proposals_kind_check CHECK (
    kind IN ('buy', 'sell', 'add_agent', 'pause_agent', 'resume_agent', 'revoke_agent')
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
  );

ALTER TABLE transactions
  ADD COLUMN IF NOT EXISTS agent_intent_id uuid REFERENCES agent_intents (id),
  ADD COLUMN IF NOT EXISTS initiated_by text NOT NULL DEFAULT 'member_proposal'
    CHECK (initiated_by IN ('member_proposal', 'agent'));

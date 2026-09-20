-- Agent trading limits: idempotent intents and unique agent key hashes.

-- A bot that times out retries with the same key and gets the first answer back instead of
-- a second trade. Unique per agent, not globally: two agents may pick the same key.
ALTER TABLE agent_intents
  ADD COLUMN IF NOT EXISTS idempotency_key text,
  -- Resolved xStock mint of a sell. An accepted sell reserves its quantity before the swap
  -- writes a transactions row; the mint ties that reservation to the ledger, which is keyed
  -- by mint, whatever spelling of the symbol the bot sent.
  ADD COLUMN IF NOT EXISTS mint text;

CREATE UNIQUE INDEX IF NOT EXISTS agent_intents_agent_idempotency_key_idx
  ON agent_intents (group_agent_id, idempotency_key)
  WHERE idempotency_key IS NOT NULL;

-- Budget and sellable-quantity checks read every intent of one agent.
CREATE INDEX IF NOT EXISTS agent_intents_group_agent_id_idx
  ON agent_intents (group_agent_id);

CREATE INDEX IF NOT EXISTS transactions_agent_intent_id_idx
  ON transactions (agent_intent_id)
  WHERE agent_intent_id IS NOT NULL;

-- One key authenticates one agent. The lookup index was not unique, so two agents minted the
-- same short key would both match and the lookup picked either. Revoked agents carry a NULL
-- hash, so they never collide. This fails loudly if two live agents already share a key:
-- revoke and re-add one of them, then re-run.
DROP INDEX IF EXISTS group_agents_api_key_hash_idx;

CREATE UNIQUE INDEX IF NOT EXISTS group_agents_api_key_hash_key
  ON group_agents (api_key_hash)
  WHERE api_key_hash IS NOT NULL;

-- Persist retrievable agent API keys for cabal members (auth still uses api_key_hash).

ALTER TABLE group_agents
  ADD COLUMN IF NOT EXISTS api_key text;

-- Backfill from one-time reveal rows before they were consumed.
UPDATE group_agents ga
SET api_key = r.plaintext_key
FROM group_agent_key_reveals r
WHERE ga.install_proposal_id = r.proposal_id
  AND ga.api_key IS NULL;

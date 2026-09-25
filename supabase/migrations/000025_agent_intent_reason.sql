-- Why the agent made each trade, as it told us. Feeds the cabal's agent trade history.
ALTER TABLE agent_intents
  ADD COLUMN IF NOT EXISTS reason text;

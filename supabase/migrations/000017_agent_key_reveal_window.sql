-- Agent key reveal window.
--
-- The key used to be deleted by the first GET of the proposal detail. Any refetch (after a
-- vote, a poll, a dropped response) then lost it for good. The proposer can now read it for a
-- fixed window after it is minted; rows past the window are purged on read.
-- Rows that predate this column count as minted at migration time.

ALTER TABLE group_agent_key_reveals
  ADD COLUMN IF NOT EXISTS created_at timestamptz NOT NULL DEFAULT now();

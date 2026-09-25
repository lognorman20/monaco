-- Short invite codes for cabals.
--
-- A member shares a link (https://trymonaco.xyz/join/<code>) or the eight-character code
-- instead of the cabal's 36-character id. The alphabet leaves out 0, O, 1 and I so a code
-- read aloud or typed from a screenshot cannot be misread.
--
-- One live code per cabal: making a new one revokes the old one in the same transaction,
-- and the partial unique index below refuses a second live row even if two requests race.
-- Revoked rows stay so an old link answers "no longer works" rather than silently matching
-- a new cabal; codes are never reused.

CREATE TABLE IF NOT EXISTS invite_codes (
  code text PRIMARY KEY CHECK (code ~ '^[2-9A-HJ-NP-Z]{8}$'),
  group_id uuid NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
  created_by uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  revoked_at timestamptz
);

CREATE UNIQUE INDEX IF NOT EXISTS invite_codes_one_live_per_group
  ON invite_codes (group_id) WHERE revoked_at IS NULL;

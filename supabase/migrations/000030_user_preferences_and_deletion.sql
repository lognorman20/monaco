-- Settings: a member's preferences, and deleting an account.
--
-- users.preferences : the member's settings as one JSON object. Today it holds
--                     {"notifications": {"proposals", "results", "chat", "money"}}, each a
--                     boolean. A key that is absent reads as its default (true), so '{}' is
--                     "everything on". The API validates every write; the column only
--                     insists on an object.
-- users.deleted_at  : when the member deleted their account (UTC). The row stays so the
--                     votes, trades, messages and ledger rows they left in their cabals keep
--                     an author, now named "Deleted member", and so the same Privy login is
--                     refused (410) instead of quietly opening a fresh account on it.

ALTER TABLE users
  ADD COLUMN IF NOT EXISTS preferences jsonb NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS deleted_at timestamptz;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'users_preferences_is_object'
  ) THEN
    ALTER TABLE users
      ADD CONSTRAINT users_preferences_is_object
      CHECK (jsonb_typeof(preferences) = 'object');
  END IF;
END $$;

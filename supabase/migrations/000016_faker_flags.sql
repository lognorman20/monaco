-- #153 demo/test faker flags.
--
-- users.is_faker   : seeded ghost people (privy_user_id like 'faker:user:maya'). Never own member_wallets.
-- groups.is_faker  : wholly fake "scale" clubs. Dummy treasury row, never touched by Privy/RPC/Jupiter.
-- groups.faker_key : idempotency key for seeded fake clubs (upsert target). Only set on faker groups.
--
-- Chain/read filters in the API skip faker rows even when FAKER_ENABLED is off, so leftover
-- seed rows stay inert after the endpoint is disabled.

ALTER TABLE users
  ADD COLUMN IF NOT EXISTS is_faker boolean NOT NULL DEFAULT false;

ALTER TABLE groups
  ADD COLUMN IF NOT EXISTS is_faker boolean NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS faker_key text;

CREATE UNIQUE INDEX IF NOT EXISTS groups_faker_key_unique
  ON groups (faker_key)
  WHERE faker_key IS NOT NULL;

CREATE INDEX IF NOT EXISTS groups_is_faker_idx
  ON groups (id)
  WHERE is_faker;

CREATE INDEX IF NOT EXISTS users_is_faker_idx
  ON users (id)
  WHERE is_faker;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'groups_faker_key_requires_faker'
  ) THEN
    ALTER TABLE groups
      ADD CONSTRAINT groups_faker_key_requires_faker
      CHECK (faker_key IS NULL OR is_faker);
  END IF;
END $$;

-- Faker users must never get a member wallet: the sweep poller scans member_wallets and
-- calls Privy/RPC for every address it finds.
CREATE OR REPLACE FUNCTION member_wallets_reject_faker_user() RETURNS trigger AS $$
BEGIN
  IF EXISTS (SELECT 1 FROM users WHERE id = NEW.user_id AND is_faker) THEN
    RAISE EXCEPTION 'member_wallets: faker user % cannot own a wallet', NEW.user_id
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS member_wallets_reject_faker_user ON member_wallets;
CREATE TRIGGER member_wallets_reject_faker_user
  BEFORE INSERT OR UPDATE ON member_wallets
  FOR EACH ROW EXECUTE FUNCTION member_wallets_reject_faker_user();

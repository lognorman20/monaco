-- Landing page waitlist (trymonaco.xyz).
--
-- Self-contained: no manual SQL or seed data is needed after this migration.
-- The landing page's serverless function holds only the Supabase anon key and
-- can do exactly one thing with it: call join_waitlist(). The API roles have no
-- direct access to the table.
--
-- Abuse limits, enforced here so they hold even for callers that skip the
-- landing page: 5 signups per hashed IP per hour, and 60 signups per minute
-- across everyone.

CREATE TABLE IF NOT EXISTS waitlist (
  id BIGSERIAL PRIMARY KEY,
  email TEXT NOT NULL UNIQUE CHECK (email = lower(email) AND length(email) <= 254),
  source TEXT CHECK (source IS NULL OR length(source) <= 64),
  ip_hash TEXT NOT NULL,
  user_agent TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS waitlist_ip_hash_created_at_idx ON waitlist (ip_hash, created_at);
CREATE INDEX IF NOT EXISTS waitlist_created_at_idx ON waitlist (created_at);

-- RLS on with no policies: the API roles get nothing directly.
ALTER TABLE waitlist ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON waitlist FROM PUBLIC;
REVOKE ALL ON SEQUENCE waitlist_id_seq FROM PUBLIC;

-- Returns: ok | invalid_email | invalid_request | rate_limited | busy.
-- 'ok' is returned for new and existing emails alike.
CREATE OR REPLACE FUNCTION join_waitlist(
  p_email TEXT,
  p_source TEXT,
  p_ip_hash TEXT,
  p_user_agent TEXT
) RETURNS TEXT
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
DECLARE
  v_email TEXT := lower(btrim(coalesce(p_email, '')));
BEGIN
  IF length(v_email) > 254 OR v_email !~ '^[^[:space:]@]+@[^[:space:]@]+\.[^[:space:]@]{2,}$' THEN
    RETURN 'invalid_email';
  END IF;
  IF p_ip_hash IS NULL OR p_ip_hash !~ '^[0-9a-f]{64}$' THEN
    RETURN 'invalid_request';
  END IF;

  IF (SELECT count(*) FROM waitlist WHERE created_at > now() - interval '1 minute') >= 60 THEN
    RETURN 'busy';
  END IF;
  IF (SELECT count(*) FROM waitlist WHERE ip_hash = p_ip_hash AND created_at > now() - interval '1 hour') >= 5 THEN
    RETURN 'rate_limited';
  END IF;

  INSERT INTO waitlist (email, source, ip_hash, user_agent)
  VALUES (v_email, nullif(left(btrim(coalesce(p_source, '')), 64), ''), p_ip_hash, left(p_user_agent, 256))
  ON CONFLICT (email) DO NOTHING;
  RETURN 'ok';
END;
$$;

-- Liveness probe for the landing page's /api/health. Reveals nothing.
CREATE OR REPLACE FUNCTION waitlist_ready() RETURNS BOOLEAN
LANGUAGE sql
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$ SELECT to_regclass('public.waitlist') IS NOT NULL $$;

REVOKE ALL ON FUNCTION join_waitlist(TEXT, TEXT, TEXT, TEXT) FROM PUBLIC;
REVOKE ALL ON FUNCTION waitlist_ready() FROM PUBLIC;

-- Supabase API roles exist only on hosted Supabase, not local Compose Postgres.
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'anon') THEN
    -- Hosted Supabase's default privileges grant API roles access to new objects.
    REVOKE ALL ON waitlist FROM anon, authenticated;
    REVOKE ALL ON SEQUENCE waitlist_id_seq FROM anon, authenticated;
    REVOKE ALL ON FUNCTION join_waitlist(TEXT, TEXT, TEXT, TEXT) FROM anon, authenticated;
    REVOKE ALL ON FUNCTION waitlist_ready() FROM anon, authenticated;
    GRANT EXECUTE ON FUNCTION join_waitlist(TEXT, TEXT, TEXT, TEXT) TO anon;
    GRANT EXECUTE ON FUNCTION waitlist_ready() TO anon;
  END IF;
END
$$;

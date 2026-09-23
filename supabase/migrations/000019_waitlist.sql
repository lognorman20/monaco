-- Landing page waitlist (trymonaco.xyz).
--
-- The landing page's serverless function holds only the Supabase anon key,
-- never the service role key. Its sole capability is calling join_waitlist(),
-- which also requires a writer secret whose hash lives in waitlist_settings.
-- Anyone holding just the public anon key cannot read or write the waitlist.
--
-- One-time setup after this migration, in the Supabase SQL editor:
--   INSERT INTO waitlist_settings (id, writer_secret_sha256)
--   VALUES (true, encode(sha256(convert_to('<WAITLIST_WRITER_SECRET>', 'UTF8')), 'hex'))
--   ON CONFLICT (id) DO UPDATE SET writer_secret_sha256 = EXCLUDED.writer_secret_sha256;

CREATE TABLE IF NOT EXISTS waitlist (
  id BIGSERIAL PRIMARY KEY,
  email TEXT NOT NULL UNIQUE CHECK (email = lower(email) AND length(email) <= 254),
  source TEXT CHECK (source IS NULL OR length(source) <= 64),
  ip_hash TEXT NOT NULL,
  user_agent TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS waitlist_ip_hash_created_at_idx ON waitlist (ip_hash, created_at);

CREATE TABLE IF NOT EXISTS waitlist_settings (
  id BOOLEAN PRIMARY KEY DEFAULT true CHECK (id),
  writer_secret_sha256 TEXT NOT NULL CHECK (length(writer_secret_sha256) = 64)
);

-- RLS on with no policies: the API roles get nothing directly.
ALTER TABLE waitlist ENABLE ROW LEVEL SECURITY;
ALTER TABLE waitlist_settings ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON waitlist, waitlist_settings FROM PUBLIC;
REVOKE ALL ON SEQUENCE waitlist_id_seq FROM PUBLIC;

-- Returns: ok | invalid_email | rate_limited | forbidden | not_configured.
-- 'ok' is returned for new and existing emails alike.
CREATE OR REPLACE FUNCTION join_waitlist(
  p_secret TEXT,
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
  expected TEXT;
  v_email TEXT := lower(btrim(coalesce(p_email, '')));
  recent INTEGER;
BEGIN
  SELECT writer_secret_sha256 INTO expected FROM waitlist_settings WHERE id;
  IF expected IS NULL THEN
    RETURN 'not_configured';
  END IF;
  IF p_secret IS NULL OR encode(sha256(convert_to(p_secret, 'UTF8')), 'hex') <> expected THEN
    RETURN 'forbidden';
  END IF;

  IF length(v_email) > 254 OR v_email !~ '^[^[:space:]@]+@[^[:space:]@]+\.[^[:space:]@]{2,}$' THEN
    RETURN 'invalid_email';
  END IF;
  IF p_ip_hash IS NULL OR p_ip_hash !~ '^[0-9a-f]{64}$' THEN
    RETURN 'forbidden';
  END IF;

  SELECT count(*) INTO recent FROM waitlist
  WHERE ip_hash = p_ip_hash AND created_at > now() - interval '1 hour';
  IF recent >= 5 THEN
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
AS $$ SELECT EXISTS (SELECT 1 FROM waitlist_settings WHERE id) $$;

REVOKE ALL ON FUNCTION join_waitlist(TEXT, TEXT, TEXT, TEXT, TEXT) FROM PUBLIC;
REVOKE ALL ON FUNCTION waitlist_ready() FROM PUBLIC;

-- Supabase API roles exist only on hosted Supabase, not local Compose Postgres.
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'anon') THEN
    -- Hosted Supabase's default privileges grant API roles access to new objects.
    REVOKE ALL ON waitlist, waitlist_settings FROM anon, authenticated;
    REVOKE ALL ON SEQUENCE waitlist_id_seq FROM anon, authenticated;
    REVOKE ALL ON FUNCTION join_waitlist(TEXT, TEXT, TEXT, TEXT, TEXT) FROM anon, authenticated;
    REVOKE ALL ON FUNCTION waitlist_ready() FROM anon, authenticated;
    GRANT EXECUTE ON FUNCTION join_waitlist(TEXT, TEXT, TEXT, TEXT, TEXT) TO anon;
    GRANT EXECUTE ON FUNCTION waitlist_ready() TO anon;
  END IF;
END
$$;

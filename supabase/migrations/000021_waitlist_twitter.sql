-- Swap the waitlist's optional name for an optional Twitter/X handle.
--
-- 000020 shipped an optional `name`; nothing meaningful depends on it yet, so this
-- renames the column in place and updates join_waitlist() to take a handle instead.
-- Capped at 15 characters, X's own handle length limit.

ALTER TABLE waitlist RENAME COLUMN name TO twitter;
ALTER TABLE waitlist DROP CONSTRAINT IF EXISTS waitlist_name_check;
ALTER TABLE waitlist ADD CONSTRAINT waitlist_twitter_check CHECK (twitter IS NULL OR length(twitter) <= 15);

-- CREATE OR REPLACE can't rename a parameter, only DROP and recreate can.
DROP FUNCTION IF EXISTS join_waitlist(TEXT, TEXT, TEXT, TEXT, TEXT);

CREATE FUNCTION join_waitlist(
  p_email TEXT,
  p_twitter TEXT,
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
  -- Trimmed, leading @ stripped, capped at 15, and empty means absent. Control characters
  -- are stripped so a handle cannot smuggle newlines into an email we later send.
  v_twitter TEXT := nullif(left(regexp_replace(ltrim(btrim(coalesce(p_twitter, '')), '@'), '[[:cntrl:]]', '', 'g'), 15), '');
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

  INSERT INTO waitlist (email, twitter, source, ip_hash, user_agent)
  VALUES (v_email, v_twitter, nullif(left(btrim(coalesce(p_source, '')), 64), ''), p_ip_hash, left(p_user_agent, 256))
  ON CONFLICT (email) DO UPDATE
    -- Someone signing up again having typed a handle the first time should not lose it,
    -- and a later handle is the one they meant.
    SET twitter = coalesce(excluded.twitter, waitlist.twitter);
  RETURN 'ok';
END;
$$;

REVOKE ALL ON FUNCTION join_waitlist(TEXT, TEXT, TEXT, TEXT, TEXT) FROM PUBLIC;

-- Supabase API roles exist only on hosted Supabase, not local Compose Postgres.
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'anon') THEN
    REVOKE ALL ON FUNCTION join_waitlist(TEXT, TEXT, TEXT, TEXT, TEXT) FROM anon, authenticated;
    GRANT EXECUTE ON FUNCTION join_waitlist(TEXT, TEXT, TEXT, TEXT, TEXT) TO anon;
  END IF;
END
$$;

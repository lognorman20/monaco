-- An optional name on the waitlist, so an invite email can open with one.
--
-- 000019 shipped join_waitlist(email, source, ip_hash, user_agent). Adding an argument
-- makes a different function rather than replacing that one, so both are defined here:
-- the five-argument version does the work, and the original delegates to it with no name.
-- A deploy where the page and the database are briefly out of step keeps working either way.

ALTER TABLE waitlist ADD COLUMN IF NOT EXISTS name TEXT
  CHECK (name IS NULL OR length(name) <= 80);

CREATE OR REPLACE FUNCTION join_waitlist(
  p_email TEXT,
  p_name TEXT,
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
  -- Trimmed, capped, and empty means absent. Control characters are stripped so a name
  -- cannot smuggle newlines into an email we later send.
  v_name TEXT := nullif(left(regexp_replace(btrim(coalesce(p_name, '')), '[[:cntrl:]]', '', 'g'), 80), '');
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

  INSERT INTO waitlist (email, name, source, ip_hash, user_agent)
  VALUES (v_email, v_name, nullif(left(btrim(coalesce(p_source, '')), 64), ''), p_ip_hash, left(p_user_agent, 256))
  ON CONFLICT (email) DO UPDATE
    -- Someone signing up again having typed a name the first time should not lose it,
    -- and a later name is the one they meant.
    SET name = coalesce(excluded.name, waitlist.name);
  RETURN 'ok';
END;
$$;

CREATE OR REPLACE FUNCTION join_waitlist(
  p_email TEXT,
  p_source TEXT,
  p_ip_hash TEXT,
  p_user_agent TEXT
) RETURNS TEXT
LANGUAGE sql
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$ SELECT join_waitlist(p_email, NULL::TEXT, p_source, p_ip_hash, p_user_agent) $$;

REVOKE ALL ON FUNCTION join_waitlist(TEXT, TEXT, TEXT, TEXT, TEXT) FROM PUBLIC;
REVOKE ALL ON FUNCTION join_waitlist(TEXT, TEXT, TEXT, TEXT) FROM PUBLIC;

-- Supabase API roles exist only on hosted Supabase, not local Compose Postgres.
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'anon') THEN
    -- The new column is on the same table the anon role must not read directly.
    REVOKE ALL ON waitlist FROM anon, authenticated;
    REVOKE ALL ON FUNCTION join_waitlist(TEXT, TEXT, TEXT, TEXT, TEXT) FROM anon, authenticated;
    GRANT EXECUTE ON FUNCTION join_waitlist(TEXT, TEXT, TEXT, TEXT, TEXT) TO anon;
    -- The four-argument version keeps its grant, so a page deployed before this
    -- migration carries on working.
    GRANT EXECUTE ON FUNCTION join_waitlist(TEXT, TEXT, TEXT, TEXT) TO anon;
  END IF;
END
$$;

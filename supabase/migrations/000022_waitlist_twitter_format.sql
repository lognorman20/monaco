-- Validate the waitlist's optional Twitter/X handle instead of trimming whatever arrives.
--
-- 000021 stripped control characters and truncated to 15, so "ada lovelace" or
-- "https://x.com/ada" were stored as junk. A handle is now either absent or X's own
-- format: 1-15 letters, digits or underscores, with one optional leading @ removed.
-- Anything else returns 'invalid_twitter' and stores nothing.

ALTER TABLE waitlist DROP CONSTRAINT IF EXISTS waitlist_twitter_check;
ALTER TABLE waitlist ADD CONSTRAINT waitlist_twitter_check
  CHECK (twitter IS NULL OR twitter ~ '^[A-Za-z0-9_]{1,15}$');

CREATE OR REPLACE FUNCTION join_waitlist(
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
  v_twitter TEXT := nullif(regexp_replace(btrim(coalesce(p_twitter, '')), '^@', ''), '');
BEGIN
  IF length(v_email) > 254 OR v_email !~ '^[^[:space:]@]+@[^[:space:]@]+\.[^[:space:]@]{2,}$' THEN
    RETURN 'invalid_email';
  END IF;
  IF v_twitter IS NOT NULL AND v_twitter !~ '^[A-Za-z0-9_]{1,15}$' THEN
    RETURN 'invalid_twitter';
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

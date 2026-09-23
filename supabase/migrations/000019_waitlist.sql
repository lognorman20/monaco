-- Landing page waitlist (trymonaco.xyz). Written only by the web app's
-- serverless function with the service role key; no public access.

CREATE TABLE IF NOT EXISTS waitlist (
  id BIGSERIAL PRIMARY KEY,
  email TEXT NOT NULL UNIQUE CHECK (email = lower(email) AND length(email) <= 254),
  source TEXT CHECK (source IS NULL OR length(source) <= 64),
  ip_hash TEXT NOT NULL,
  user_agent TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS waitlist_ip_hash_created_at_idx ON waitlist (ip_hash, created_at);

-- RLS on with no policies: anon and authenticated roles get nothing.
ALTER TABLE waitlist ENABLE ROW LEVEL SECURITY;

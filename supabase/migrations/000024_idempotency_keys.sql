-- Idempotency keys for user-initiated money requests.
--
-- A client sends one Idempotency-Key per confirmed action and reuses it when it retries the
-- same submission after a timeout. The first request claims the row (in_progress) and stores
-- its response on completion; a retry replays that response instead of moving money twice.
-- Keys are scoped to the authenticated user and expire 24h after created_at (UTC).

CREATE TABLE IF NOT EXISTS idempotency_keys (
  user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  key text NOT NULL,
  route text NOT NULL,
  request_hash text NOT NULL,
  state text NOT NULL DEFAULT 'in_progress' CHECK (state IN ('in_progress', 'completed')),
  response_status integer,
  response_content_type text,
  response_body bytea,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT idempotency_keys_user_key_unique UNIQUE (user_id, key),
  CONSTRAINT idempotency_keys_completed_has_response
    CHECK (state <> 'completed' OR response_status IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS idempotency_keys_created_at_idx ON idempotency_keys (created_at);

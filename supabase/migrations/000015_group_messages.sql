-- Cabal chat: plain-text messages posted by members inside a group.

CREATE TABLE IF NOT EXISTS group_messages (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  group_id uuid NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
  author_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  body text NOT NULL
    CHECK (char_length(btrim(body)) > 0 AND char_length(body) <= 2000),
  created_at timestamptz NOT NULL DEFAULT now()
);

-- Newest-first keyset pagination: (created_at, id) is the cursor.
CREATE INDEX IF NOT EXISTS group_messages_group_id_created_at_idx
  ON group_messages (group_id, created_at DESC, id DESC);

ALTER TABLE groups DROP CONSTRAINT IF EXISTS groups_join_mode_check;
UPDATE groups SET join_mode = 'request' WHERE join_mode = 'password';
ALTER TABLE groups ADD CONSTRAINT groups_join_mode_check CHECK (join_mode IN ('open', 'request'));

CREATE TABLE IF NOT EXISTS group_join_requests (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  group_id uuid NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
  user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  status text NOT NULL CHECK (status IN ('pending', 'approved', 'denied')),
  created_at timestamptz NOT NULL DEFAULT now(),
  decided_at timestamptz
);

CREATE UNIQUE INDEX IF NOT EXISTS group_join_requests_pending_unique
  ON group_join_requests (group_id, user_id) WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS group_join_requests_group_pending_idx
  ON group_join_requests (group_id) WHERE status = 'pending';

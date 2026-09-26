-- Notifications: the in-app inbox, Apple push device tokens, proposal nudges, and the
-- member preferences that switch categories off.
--
-- Every statement is IF NOT EXISTS so the file is safe to re-apply. The settings lane adds
-- the same users.preferences column the same way; whichever file runs first creates it.

ALTER TABLE users ADD COLUMN IF NOT EXISTS preferences JSONB NOT NULL DEFAULT '{}'::jsonb;

-- One row per member per event. Title and body are written once, in the words the push
-- carried, so the inbox and the lock screen say the same thing. group/proposal/transaction
-- say where a tap goes; symbol picks the stock's mark for buys and sells.
CREATE TABLE IF NOT EXISTS notifications (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  kind text NOT NULL CHECK (char_length(kind) BETWEEN 1 AND 64),
  title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 300),
  body text NOT NULL DEFAULT '' CHECK (char_length(body) <= 600),
  group_id uuid REFERENCES groups (id) ON DELETE CASCADE,
  proposal_id uuid REFERENCES proposals (id) ON DELETE CASCADE,
  transaction_id uuid REFERENCES transactions (id) ON DELETE SET NULL,
  symbol text,
  read_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);

-- Newest-first keyset pagination: (created_at, id) is the cursor.
CREATE INDEX IF NOT EXISTS notifications_user_created_idx
  ON notifications (user_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS notifications_user_unread_idx
  ON notifications (user_id)
  WHERE read_at IS NULL;

-- The chat throttle asks "did this member get a chat notification for this cabal lately".
CREATE INDEX IF NOT EXISTS notifications_chat_throttle_idx
  ON notifications (user_id, group_id, created_at DESC)
  WHERE kind = 'chat_message';

-- APNs device tokens. A token belongs to one install, so it is the key: signing in as someone
-- else on the same phone moves the token to them.
CREATE TABLE IF NOT EXISTS device_tokens (
  user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  token text PRIMARY KEY CHECK (char_length(token) BETWEEN 32 AND 256 AND token ~ '^[0-9a-f]+$'),
  platform text NOT NULL CHECK (platform IN ('ios')),
  app_env text NOT NULL CHECK (app_env IN ('debug', 'production')),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS device_tokens_user_id_idx ON device_tokens (user_id, updated_at DESC);

-- One row per reminder a member sent about a proposal; the newest row rate-limits the next.
CREATE TABLE IF NOT EXISTS nudges (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  proposal_id uuid NOT NULL REFERENCES proposals (id) ON DELETE CASCADE,
  sent_by uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  sent_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS nudges_proposal_sent_at_idx ON nudges (proposal_id, sent_at DESC);

-- The "closes in 1 hour" reminder goes out once per proposal. The row is the claim, so two
-- API processes polling at once cannot both send it.
CREATE TABLE IF NOT EXISTS proposal_reminders (
  proposal_id uuid PRIMARY KEY REFERENCES proposals (id) ON DELETE CASCADE,
  sent_at timestamptz NOT NULL DEFAULT now()
);

-- "Money arrived in your balance": the member wallet is only ever read, never watched, so the
-- API keeps the running total of USDC that reached it from outside Monaco. A rise in that
-- total is money arriving; cabal cash-outs to the balance are subtracted, sweeps and
-- withdrawals added back.
CREATE TABLE IF NOT EXISTS member_balance_marks (
  user_id uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
  inflow_micros bigint NOT NULL,
  observed_at timestamptz NOT NULL DEFAULT now()
);

-- M4 redeem job state machine (debit-first resume).

CREATE TABLE IF NOT EXISTS redeem_jobs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  group_id uuid NOT NULL REFERENCES groups (id),
  user_id uuid NOT NULL REFERENCES users (id),
  share_units bigint NOT NULL CHECK (share_units > 0),
  slice_usdc bigint NOT NULL CHECK (slice_usdc > 0),
  payout_address text NOT NULL,
  status text NOT NULL DEFAULT 'debited'
    CHECK (status IN ('debited', 'selling', 'paying', 'settled')),
  withdrawal_id uuid REFERENCES withdrawals (id),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS redeem_jobs_group_user_status_idx
  ON redeem_jobs (group_id, user_id, status);

CREATE UNIQUE INDEX IF NOT EXISTS redeem_jobs_active_user_group_idx
  ON redeem_jobs (group_id, user_id)
  WHERE status IN ('debited', 'selling', 'paying');

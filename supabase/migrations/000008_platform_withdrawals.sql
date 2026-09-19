-- Platform balance withdrawals: member wallet USDC to arbitrary Solana address.
-- Separate from group-scoped withdrawals (redeem / leave flows on #186).

CREATE TABLE IF NOT EXISTS platform_withdrawals (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users (id),
  amount bigint NOT NULL CHECK (amount > 0),
  to_address text NOT NULL,
  status text NOT NULL DEFAULT 'pending',
  tx_signature text UNIQUE,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS platform_withdrawals_user_pending_idx
  ON platform_withdrawals (user_id)
  WHERE status = 'pending';

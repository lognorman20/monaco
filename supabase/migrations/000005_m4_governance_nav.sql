-- M4 group rules, membership, proposals, votes, NAV snapshots, and payout proofs.

ALTER TABLE groups
  ADD COLUMN IF NOT EXISTS join_mode text NOT NULL DEFAULT 'open'
    CHECK (join_mode IN ('open', 'password')),
  ADD COLUMN IF NOT EXISTS join_password_hash text,
  ADD COLUMN IF NOT EXISTS voter_set_mode text NOT NULL DEFAULT 'all_members'
    CHECK (voter_set_mode IN ('all_members', 'named_subset')),
  ADD COLUMN IF NOT EXISTS threshold text NOT NULL DEFAULT 'majority'
    CHECK (threshold IN ('unanimous', 'majority')),
  ADD COLUMN IF NOT EXISTS vote_expiry_seconds integer NOT NULL DEFAULT 86400
    CHECK (vote_expiry_seconds > 0);

CREATE TABLE IF NOT EXISTS group_members (
  group_id uuid NOT NULL REFERENCES groups (id),
  user_id uuid NOT NULL REFERENCES users (id),
  joined_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (group_id, user_id)
);

CREATE INDEX IF NOT EXISTS group_members_user_id_idx ON group_members (user_id);

CREATE TABLE IF NOT EXISTS group_voters (
  group_id uuid NOT NULL,
  user_id uuid NOT NULL,
  PRIMARY KEY (group_id, user_id),
  FOREIGN KEY (group_id, user_id) REFERENCES group_members (group_id, user_id)
);

CREATE TABLE IF NOT EXISTS proposals (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  group_id uuid NOT NULL REFERENCES groups (id),
  proposer_id uuid NOT NULL REFERENCES users (id),
  symbol text NOT NULL,
  usdc_micros bigint NOT NULL CHECK (usdc_micros > 0),
  status text NOT NULL DEFAULT 'open'
    CHECK (status IN ('open', 'passed', 'failed', 'expired')),
  expires_at timestamptz NOT NULL,
  fill_tx_signature text,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS proposals_group_id_status_idx ON proposals (group_id, status);
CREATE INDEX IF NOT EXISTS proposals_expires_at_idx ON proposals (expires_at)
  WHERE status = 'open';

CREATE TABLE IF NOT EXISTS votes (
  proposal_id uuid NOT NULL REFERENCES proposals (id),
  voter_id uuid NOT NULL REFERENCES users (id),
  choice text NOT NULL CHECK (choice IN ('yes', 'no')),
  cast_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (proposal_id, voter_id)
);

CREATE TABLE IF NOT EXISTS nav_snapshots (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  group_id uuid NOT NULL REFERENCES groups (id),
  pot_nav_micros bigint NOT NULL CHECK (pot_nav_micros >= 0),
  nav_per_share_micros bigint NOT NULL CHECK (nav_per_share_micros >= 0),
  total_shares bigint NOT NULL CHECK (total_shares >= 0),
  reason text NOT NULL CHECK (reason IN (
    'deposit',
    'transaction_confirm',
    'withdrawal_payout'
  )),
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS nav_snapshots_group_id_created_at_idx
  ON nav_snapshots (group_id, created_at DESC);

CREATE TABLE IF NOT EXISTS payout_proofs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users (id),
  group_id uuid NOT NULL REFERENCES groups (id),
  payout_address text NOT NULL,
  message text NOT NULL,
  signature text NOT NULL,
  withdrawal_id uuid REFERENCES withdrawals (id),
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS payout_proofs_withdrawal_id_idx
  ON payout_proofs (withdrawal_id)
  WHERE withdrawal_id IS NOT NULL;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'transactions_proposal_id_fkey'
  ) THEN
    ALTER TABLE transactions
      ADD CONSTRAINT transactions_proposal_id_fkey
      FOREIGN KEY (proposal_id) REFERENCES proposals (id);
  END IF;
END $$;

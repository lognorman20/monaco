-- Redeem payout attempts.
--
-- A cash out used to sign and broadcast its treasury transfer in one Privy call and record
-- nothing until that call returned. An error or a crash in between left the job in `paying`
-- with no trace of whether money had moved, and the next tap paid again.
--
-- The signed transfer and its signature are now written here BEFORE the transfer is broadcast,
-- in the same transaction that moves the job to `paying`. The job settles only once that
-- signature is confirmed on chain, rolls back when it failed or its blockhash expired, and is
-- never paid a second time: a job gets exactly one attempt.
--
-- redeem_job_id carries no foreign key on purpose: a rolled back job is deleted, and its
-- attempt stays behind as the record of what was signed.

CREATE TABLE IF NOT EXISTS redeem_payouts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  redeem_job_id uuid NOT NULL,
  user_id uuid NOT NULL REFERENCES users (id),
  group_id uuid NOT NULL REFERENCES groups (id),
  amount bigint NOT NULL CHECK (amount > 0),
  to_address text NOT NULL,
  tx_signature text NOT NULL UNIQUE,
  signed_tx text NOT NULL,
  last_valid_block_height bigint NOT NULL CHECK (last_valid_block_height > 0),
  proof_message text,
  proof_signature text,
  status text NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending', 'confirmed', 'dropped', 'failed')),
  failure_reason text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS redeem_payouts_redeem_job_id_idx
  ON redeem_payouts (redeem_job_id);

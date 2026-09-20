-- Deposit sweep poller: multi-instance claims, per-deposit retry state, and hot-path indexes.
--
-- claimed_at / claimed_by            : lease taken with FOR UPDATE SKIP LOCKED so two API processes never
--                                      work the same deposit. An expired lease (crashed owner) is reclaimable.
-- attempt_count / next_attempt_at    : consecutive transient failures and the backoff they earned.
--                                      next_attempt_at NULL means "eligible now".
-- last_error                         : most recent failure, for operators.
-- sweep_submit_count                 : how many sweep transactions were signed for this deposit.
-- sweep_last_valid_block_height      : last block height at which the signed sweep (tx_signature) can land.
--                                      Once the finalized height passes it and the signature is unknown,
--                                      the transaction was dropped and a re-submit cannot double-sweep.
--                                      NULL on rows broadcast before this migration; the poller bounds
--                                      those from the current height the first time it sees them.
-- treasuries.surplus_checked_at      : staggers treasury surplus reconciliation across ticks and instances.

ALTER TABLE deposits
  ADD COLUMN IF NOT EXISTS claimed_at timestamptz,
  ADD COLUMN IF NOT EXISTS claimed_by text,
  ADD COLUMN IF NOT EXISTS attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
  ADD COLUMN IF NOT EXISTS next_attempt_at timestamptz,
  ADD COLUMN IF NOT EXISTS last_error text,
  ADD COLUMN IF NOT EXISTS sweep_submit_count integer NOT NULL DEFAULT 0 CHECK (sweep_submit_count >= 0),
  ADD COLUMN IF NOT EXISTS sweep_last_valid_block_height bigint CHECK (sweep_last_valid_block_height >= 0);

ALTER TABLE treasuries
  ADD COLUMN IF NOT EXISTS surplus_checked_at timestamptz;

-- Status values written by the API: 'pending', 'confirmed', 'failed', and 'failed: <stage>'.
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'deposits_status_check'
  ) THEN
    ALTER TABLE deposits
      ADD CONSTRAINT deposits_status_check
      CHECK (status IN ('pending', 'confirmed', 'failed') OR status LIKE 'failed: %');
  END IF;
END $$;

-- The poller claims pending rows every few seconds, oldest due first.
CREATE INDEX IF NOT EXISTS deposits_pending_due_idx
  ON deposits (next_attempt_at NULLS FIRST, created_at)
  WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS deposits_group_id_created_at_idx
  ON deposits (group_id, created_at DESC);

CREATE INDEX IF NOT EXISTS deposits_user_id_idx
  ON deposits (user_id);

-- Reservation sums and "has pending" probes filter on pending per member / per group.
CREATE INDEX IF NOT EXISTS deposits_user_pending_idx
  ON deposits (user_id)
  WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS deposits_group_pending_idx
  ON deposits (group_id)
  WHERE status = 'pending';

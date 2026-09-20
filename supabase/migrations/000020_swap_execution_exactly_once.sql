-- Exactly-once, crash-safe treasury swap execution.
--
-- A swap row is now written BEFORE anything is submitted to the venue, carrying what a
-- reconciler needs to decide the outcome from the chain or the venue if the process dies
-- or the poll times out after submit.

ALTER TABLE transactions
  ADD COLUMN IF NOT EXISTS provider text,
  ADD COLUMN IF NOT EXISTS signed_tx_signature text,
  ADD COLUMN IF NOT EXISTS signed_tx_blockhash text,
  ADD COLUMN IF NOT EXISTS submit_expires_at timestamptz,
  ADD COLUMN IF NOT EXISTS submitted_at timestamptz,
  ADD COLUMN IF NOT EXISTS failure_reason text;

-- The reconciler scans unresolved swaps oldest first.
CREATE INDEX IF NOT EXISTS transactions_pending_swaps_idx
  ON transactions (created_at)
  WHERE status = 'pending';

-- At most one active (pending or confirmed) swap per proposal and side. Only a definitively
-- failed swap frees the slot for a retry. Rows written before this migration are exempt so
-- historical duplicates (the bug this fixes) cannot block the migration; the executor still
-- checks them under the proposal claim.
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_indexes WHERE indexname = 'transactions_one_active_swap_per_proposal'
  ) THEN
    EXECUTE format(
      'CREATE UNIQUE INDEX transactions_one_active_swap_per_proposal
         ON transactions (proposal_id, action)
         WHERE proposal_id IS NOT NULL
           AND status IN (''pending'', ''confirmed'')
           AND created_at >= %L::timestamptz',
      (now() AT TIME ZONE 'UTC')::text || '+00'
    );
  END IF;
END $$;

-- Persisted execute claim and backoff, shared by every API instance. execute_next_attempt_at
-- is the claim lease while an executor holds the proposal and the backoff deadline after a
-- failed attempt.
ALTER TABLE proposals
  ADD COLUMN IF NOT EXISTS execute_attempts integer NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS execute_next_attempt_at timestamptz,
  ADD COLUMN IF NOT EXISTS execute_last_error text;

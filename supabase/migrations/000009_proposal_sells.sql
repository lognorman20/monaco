-- Sell quantities are exact 8-decimal xStock SPL atomics:
-- 100000000 token_amount = one whole share.
-- Existing 000005 CHECK (usdc_micros > 0) is NULL-safe: a NULL usdc_micros
-- row is allowed after DROP NOT NULL. Do not drop that column check.
ALTER TABLE proposals
  ADD COLUMN IF NOT EXISTS kind text NOT NULL DEFAULT 'buy',
  ADD COLUMN IF NOT EXISTS token_amount bigint,
  ALTER COLUMN usdc_micros DROP NOT NULL;

ALTER TABLE proposals DROP CONSTRAINT IF EXISTS proposals_kind_check;
ALTER TABLE proposals
  ADD CONSTRAINT proposals_kind_check CHECK (kind IN ('buy', 'sell'));

ALTER TABLE proposals DROP CONSTRAINT IF EXISTS proposals_amount_by_kind_check;
ALTER TABLE proposals
  ADD CONSTRAINT proposals_amount_by_kind_check CHECK (
    (kind = 'buy' AND usdc_micros IS NOT NULL AND usdc_micros > 0 AND token_amount IS NULL)
    OR
    (kind = 'sell' AND token_amount IS NOT NULL AND token_amount > 0 AND usdc_micros IS NULL)
  );

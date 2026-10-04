ALTER TABLE swaps
  ADD COLUMN out_decimals smallint NOT NULL DEFAULT -1,
  ADD COLUMN source_batch_size integer NOT NULL DEFAULT 1;

UPDATE swaps
SET out_decimals = 6
WHERE out_mint = 'EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v';

UPDATE swaps AS s
SET out_decimals = a.decimals
FROM assets AS a
WHERE s.out_mint = a.mint;

UPDATE swaps AS s
SET source_batch_size = jsonb_array_length(p.legs)
FROM cashout_sell_plans AS p
WHERE s.source_kind = 'cashout' AND s.source_id = p.job_id;

ALTER TABLE swaps
  ALTER COLUMN out_decimals DROP DEFAULT,
  ADD CONSTRAINT swaps_out_decimals_check CHECK (out_decimals BETWEEN 0 AND 255),
  ADD CONSTRAINT swaps_source_batch_size_check CHECK (source_batch_size > 0);

CREATE INDEX swaps_stale_submitted_idx ON swaps (updated_at, id) WHERE status = 'submitted';

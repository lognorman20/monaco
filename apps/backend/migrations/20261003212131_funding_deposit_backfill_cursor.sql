ALTER TABLE deposit_cursors
  ADD COLUMN backfill_before_signature text,
  ADD COLUMN backfill_head_signature text,
  ADD COLUMN backfill_head_slot bigint;

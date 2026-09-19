-- Record the group's net USDC contributed (sum of positions.amount_deposited -
-- amount_withdrawn) alongside each pot NAV snapshot, in the same transaction
-- that moved the money. Group P&L at a snapshot is then exactly
-- pot_nav_micros - net_contributed_micros.
--
-- Nullable: rows written before this migration have no recorded value. Readers
-- estimate those from the deposits/withdrawals ledger (see
-- apps/backend/internal/app/group_pnl_history.go) rather than backfilling a
-- guess into the table.

ALTER TABLE nav_snapshots
  ADD COLUMN IF NOT EXISTS net_contributed_micros bigint;

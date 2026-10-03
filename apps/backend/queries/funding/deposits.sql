-- name: InsertDeposit :execrows
INSERT INTO deposits (
  id, user_id, wallet_address, tx_signature, amount_micros, slot, block_time, credited_at
) VALUES ($1, $2, $3, $4, sqlc.arg(amount_micros)::text::numeric, $5,
  NULLIF(sqlc.arg(block_time)::timestamptz, '0001-01-01 00:00:00+00'::timestamptz), $6)
ON CONFLICT (tx_signature, wallet_address) DO NOTHING;

-- name: AdvanceDepositCursor :exec
INSERT INTO deposit_cursors (wallet_address, last_signature, cursor_slot, scanned_at)
VALUES ($1, sqlc.arg(last_signature)::text, $2, $3)
ON CONFLICT (wallet_address) DO UPDATE
SET last_signature = EXCLUDED.last_signature,
    cursor_slot = EXCLUDED.cursor_slot,
    scanned_at = EXCLUDED.scanned_at
WHERE EXCLUDED.cursor_slot >= deposit_cursors.cursor_slot;

-- name: DepositCursorsForWallets :many
WITH wallets AS (SELECT unnest(sqlc.arg(wallet_addresses)::text[])::text AS wallet_address)
SELECT wallets.wallet_address,
  COALESCE(deposit_cursors.last_signature, '') AS last_signature,
  COALESCE(deposit_cursors.cursor_slot, 0) AS cursor_slot,
  COALESCE(deposit_cursors.backfill_before_signature, '') AS backfill_before_signature,
  COALESCE(deposit_cursors.backfill_head_signature, '') AS backfill_head_signature,
  COALESCE(deposit_cursors.backfill_head_slot, 0) AS backfill_head_slot,
  COALESCE(deposit_cursors.scanned_at, to_timestamp(0)) AS scanned_at,
  (deposit_cursors.wallet_address IS NOT NULL)::bool AS exists
FROM wallets
LEFT JOIN deposit_cursors ON deposit_cursors.wallet_address = wallets.wallet_address
ORDER BY COALESCE(deposit_cursors.scanned_at, to_timestamp(0));

-- name: SetDepositBackfill :exec
UPDATE deposit_cursors
SET backfill_before_signature = sqlc.arg(before_signature)::text,
    backfill_head_signature = sqlc.arg(head_signature)::text,
    backfill_head_slot = sqlc.arg(head_slot)::bigint
WHERE wallet_address = $1;

-- name: TouchDepositCursor :exec
INSERT INTO deposit_cursors (wallet_address, last_signature, cursor_slot, scanned_at)
VALUES ($1, '', 0, $2)
ON CONFLICT (wallet_address) DO UPDATE SET scanned_at = EXCLUDED.scanned_at;

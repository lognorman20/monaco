-- Member history reads (GET /v1/me/transactions and its CSV export).
--
-- History is one member's money across every table that moves it, newest first. Deposits are
-- already indexed by user; the tables below were only ever read by group or by pending state,
-- so a per-member page scanned them whole. Each index serves one branch of that read: the
-- member's rows, newest first, so a page is an index range rather than a sort of the table.

CREATE INDEX IF NOT EXISTS withdrawals_user_id_created_at_idx
  ON withdrawals (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS platform_withdrawals_user_id_created_at_idx
  ON platform_withdrawals (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS redeem_jobs_user_id_created_at_idx
  ON redeem_jobs (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS redeem_payouts_user_id_created_at_idx
  ON redeem_payouts (user_id, created_at DESC);

-- A cabal's buys and sells, newest first: the member's history reads them per cabal, and so
-- does the cabal's own activity list.
CREATE INDEX IF NOT EXISTS transactions_group_id_created_at_idx
  ON transactions (group_id, created_at DESC);

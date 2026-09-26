-- Price alerts: "tell me when Apple is above $240".
--
-- An alert is one-shot. The alert poller reads the active alerts' symbols once a minute,
-- marks them the way the Stocks tab prices a row, and fires an alert the first time its mark
-- reaches the line: at or above it for 'above', at or below it for 'below'. Firing stamps
-- triggered_at and the mark that tripped it and clears active in the same statement, so an
-- alert fires exactly once however many pollers run. A member keeps at most 20 active alerts
-- (enforced by the API under a row lock on the member). Rows go with the member.

CREATE TABLE IF NOT EXISTS price_alerts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  symbol text NOT NULL CHECK (char_length(btrim(symbol)) BETWEEN 1 AND 32),
  direction text NOT NULL CHECK (direction IN ('above', 'below')),
  price_usdc_micros bigint NOT NULL CHECK (price_usdc_micros > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  triggered_at timestamptz,
  triggered_price_usdc_micros bigint
    CHECK (triggered_price_usdc_micros IS NULL OR triggered_price_usdc_micros > 0),
  active boolean NOT NULL DEFAULT true,
  -- A fired alert is never active again; a new line is a new alert.
  CONSTRAINT price_alerts_fired_is_inactive CHECK (triggered_at IS NULL OR NOT active)
);

CREATE INDEX IF NOT EXISTS price_alerts_user_created_idx
  ON price_alerts (user_id, created_at DESC);

-- The poller's scan: active alerts by symbol.
CREATE INDEX IF NOT EXISTS price_alerts_active_symbol_idx
  ON price_alerts (symbol) WHERE active;

-- A member's watchlist: the stocks they follow before (or without) a cabal buying them.
--
-- One row per stock, ordered by position (0 first). Symbols are the catalogue's own
-- spelling ("AAPLx", "tSpaceX"); the lower() index keeps one row per stock however a
-- client cases it. Rows go with the member.

CREATE TABLE IF NOT EXISTS watchlist (
  user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  symbol text NOT NULL CHECK (char_length(btrim(symbol)) BETWEEN 1 AND 32),
  position integer NOT NULL CHECK (position >= 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, symbol)
);

CREATE UNIQUE INDEX IF NOT EXISTS watchlist_user_symbol_lower_key
  ON watchlist (user_id, lower(symbol));

CREATE INDEX IF NOT EXISTS watchlist_user_position_idx
  ON watchlist (user_id, position);

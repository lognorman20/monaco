-- Weekly matchups: cabal vs cabal.
--
-- Every Monday 00:00 UTC the API draws the week: each cabal with at least two members and more
-- than $1 in the pot is paired with a neighbour of similar pot size (an odd one out gets a bye),
-- and the two race on percent return until the next Monday, when the week is frozen.
--
-- Records (wins, losses, ties, points, streak) are not stored. They are read back from the
-- frozen rows below, so there is one source of truth and "freeze the week" is a single
-- conditional write that can happen exactly once.

CREATE TABLE IF NOT EXISTS matchup_weeks (
  -- Monday of the week, UTC.
  week_start date PRIMARY KEY CHECK (extract(isodow FROM week_start) = 1),
  status text NOT NULL DEFAULT 'live' CHECK (status IN ('live', 'final')),
  drawn_at timestamptz NOT NULL DEFAULT now(),
  finalized_at timestamptz,
  CHECK ((status = 'final') = (finalized_at IS NOT NULL))
);

-- A friendly challenge locks a pair for the next week's draw ahead of the pot-size pairing.
CREATE TABLE IF NOT EXISTS matchup_challenges (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  -- The week the pair would play: the Monday after the challenge was sent.
  week_start date NOT NULL CHECK (extract(isodow FROM week_start) = 1),
  challenger_group_id uuid NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
  challenged_group_id uuid NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
  created_by_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
  accepted_by_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
  -- pending: sent. accepted: locked for its week. scheduled: the draw paired it.
  -- void: the draw ran and one side was not eligible. expired: the draw ran while it was pending.
  status text NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending', 'accepted', 'scheduled', 'void', 'expired')),
  created_at timestamptz NOT NULL DEFAULT now(),
  accepted_at timestamptz,
  CHECK (challenger_group_id <> challenged_group_id)
);

-- One open challenge per direction per week; resending returns the one already open.
CREATE UNIQUE INDEX IF NOT EXISTS matchup_challenges_open_pair_idx
  ON matchup_challenges (week_start, challenger_group_id, challenged_group_id)
  WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS matchup_challenges_challenger_idx
  ON matchup_challenges (challenger_group_id, week_start);
CREATE INDEX IF NOT EXISTS matchup_challenges_challenged_idx
  ON matchup_challenges (challenged_group_id, week_start);

CREATE TABLE IF NOT EXISTS matchups (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  week_start date NOT NULL REFERENCES matchup_weeks (week_start) ON DELETE CASCADE,
  group_a uuid NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
  -- Null is a bye: the cabal sits the week out and its record does not move.
  group_b uuid REFERENCES groups (id) ON DELETE CASCADE,
  -- Each side's pot and net money in when the pair was drawn: the baseline its return for the
  -- week is measured from. History marks stock at cost, so the week's start cannot be read
  -- back later at market; it is recorded at the draw instead.
  start_pot_a_micros bigint NOT NULL,
  start_net_in_a_micros bigint NOT NULL,
  start_pot_b_micros bigint,
  start_net_in_b_micros bigint,
  started_at timestamptz NOT NULL,
  -- Percent return for the week as a ratio (0.0123 is +1.23%), frozen at the week's end.
  score_a numeric(14, 6),
  score_b numeric(14, 6),
  -- Null on a frozen row is a tie, or a bye.
  winner uuid,
  frozen_at timestamptz,
  challenge_id uuid REFERENCES matchup_challenges (id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (group_b IS NULL OR group_b <> group_a),
  CHECK (winner IS NULL OR winner = group_a OR winner = group_b),
  CHECK ((group_b IS NULL) = (start_pot_b_micros IS NULL)),
  CHECK ((group_b IS NULL) = (start_net_in_b_micros IS NULL))
);

-- A cabal plays at most once a week on either side.
CREATE UNIQUE INDEX IF NOT EXISTS matchups_week_group_a_idx ON matchups (week_start, group_a);
CREATE UNIQUE INDEX IF NOT EXISTS matchups_week_group_b_idx ON matchups (week_start, group_b);

-- A cabal's recent results and record read by group on either side, newest first.
CREATE INDEX IF NOT EXISTS matchups_group_a_week_idx ON matchups (group_a, week_start DESC);
CREATE INDEX IF NOT EXISTS matchups_group_b_week_idx ON matchups (group_b, week_start DESC)
  WHERE group_b IS NOT NULL;

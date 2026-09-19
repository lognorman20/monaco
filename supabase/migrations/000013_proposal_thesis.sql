-- Optional trade rationale on buy/sell proposals.

ALTER TABLE proposals
  ADD COLUMN IF NOT EXISTS thesis text;

ALTER TABLE proposals DROP CONSTRAINT IF EXISTS proposals_thesis_length_check;
ALTER TABLE proposals
  ADD CONSTRAINT proposals_thesis_length_check CHECK (
    thesis IS NULL OR char_length(thesis) <= 500
  );

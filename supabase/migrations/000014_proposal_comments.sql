-- Proposal discussion threads: member comments, optionally replying to another comment on the same proposal.

CREATE TABLE IF NOT EXISTS proposal_comments (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  proposal_id uuid NOT NULL REFERENCES proposals (id),
  author_id uuid NOT NULL REFERENCES users (id),
  parent_comment_id uuid,
  body text NOT NULL
    CHECK (char_length(btrim(body)) BETWEEN 1 AND 1000),
  created_at timestamptz NOT NULL DEFAULT now(),
  -- Lets replies reference (parent id, proposal id) so a reply can never
  -- point at a comment on a different proposal.
  CONSTRAINT proposal_comments_id_proposal_key UNIQUE (id, proposal_id),
  CONSTRAINT proposal_comments_parent_same_proposal_fkey
    FOREIGN KEY (parent_comment_id, proposal_id)
    REFERENCES proposal_comments (id, proposal_id),
  CONSTRAINT proposal_comments_not_own_parent
    CHECK (parent_comment_id IS NULL OR parent_comment_id <> id)
);

CREATE INDEX IF NOT EXISTS proposal_comments_proposal_created_idx
  ON proposal_comments (proposal_id, created_at, id);

CREATE INDEX IF NOT EXISTS proposal_comments_author_created_idx
  ON proposal_comments (author_id, created_at);

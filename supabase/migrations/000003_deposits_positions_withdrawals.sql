-- M2 deposits, positions, and withdrawals schema.

CREATE TABLE IF NOT EXISTS deposits (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users (id),
  group_id uuid NOT NULL REFERENCES groups (id),
  amount bigint NOT NULL CHECK (amount > 0),
  from_address text NOT NULL,
  status text NOT NULL DEFAULT 'pending',
  tx_signature text UNIQUE,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS positions (
  user_id uuid NOT NULL REFERENCES users (id),
  group_id uuid NOT NULL REFERENCES groups (id),
  share_units bigint NOT NULL DEFAULT 0 CHECK (share_units >= 0),
  amount_deposited bigint NOT NULL DEFAULT 0 CHECK (amount_deposited >= 0),
  amount_withdrawn bigint NOT NULL DEFAULT 0 CHECK (amount_withdrawn >= 0),
  PRIMARY KEY (user_id, group_id)
);

CREATE TABLE IF NOT EXISTS withdrawals (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users (id),
  group_id uuid NOT NULL REFERENCES groups (id),
  amount bigint NOT NULL CHECK (amount > 0),
  to_address text NOT NULL,
  status text NOT NULL DEFAULT 'pending',
  tx_signature text UNIQUE,
  created_at timestamptz NOT NULL DEFAULT now()
);

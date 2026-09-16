-- M3 group-level Jupiter swap transactions.

CREATE TABLE IF NOT EXISTS transactions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  group_id uuid NOT NULL REFERENCES groups (id),
  proposal_id uuid,
  amount bigint NOT NULL CHECK (amount > 0),
  action text NOT NULL CHECK (action IN ('buy', 'sell')),
  input_mint text NOT NULL,
  output_mint text NOT NULL,
  status text NOT NULL DEFAULT 'pending',
  tx_signature text UNIQUE,
  execute_request_id text UNIQUE,
  cost_basis_price bigint,
  cost_basis_amount bigint,
  created_at timestamptz NOT NULL DEFAULT now(),
  confirmed_at timestamptz
);

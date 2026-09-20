-- M6: chain-neutral schema for Dynamic auth and Base (EVM) wallets.

ALTER TABLE users RENAME COLUMN privy_user_id TO dynamic_user_id;

ALTER TABLE member_wallets RENAME COLUMN privy_wallet_id TO wallet_id;
ALTER TABLE member_wallets RENAME COLUMN solana_address TO address;
ALTER TABLE member_wallets
  ADD COLUMN IF NOT EXISTS wallet_metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS key_shares_enc text;

ALTER TABLE treasuries RENAME COLUMN privy_wallet_id TO wallet_id;
ALTER TABLE treasuries RENAME COLUMN solana_address TO address;
ALTER TABLE treasuries
  ADD COLUMN IF NOT EXISTS wallet_metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS key_shares_enc text,
  ADD COLUMN IF NOT EXISTS gas_topped_up_at timestamptz;

ALTER TABLE deposits RENAME COLUMN tx_signature TO tx_hash;
ALTER TABLE withdrawals RENAME COLUMN tx_signature TO tx_hash;
ALTER TABLE platform_withdrawals RENAME COLUMN tx_signature TO tx_hash;
ALTER TABLE transactions RENAME COLUMN tx_signature TO tx_hash;
ALTER TABLE proposals RENAME COLUMN fill_tx_signature TO fill_tx_hash;

ALTER TABLE transactions RENAME COLUMN input_mint TO input_token;
ALTER TABLE transactions RENAME COLUMN output_mint TO output_token;

DROP TABLE IF EXISTS payout_proofs;

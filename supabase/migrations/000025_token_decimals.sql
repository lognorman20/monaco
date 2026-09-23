ALTER TABLE transactions ADD COLUMN token_decimals smallint NOT NULL DEFAULT 8 CHECK (token_decimals BETWEEN 0 AND 12);
ALTER TABLE proposals ADD COLUMN token_decimals smallint NOT NULL DEFAULT 8 CHECK (token_decimals BETWEEN 0 AND 12);
ALTER TABLE proposals ADD COLUMN premium_bps integer NULL;
COMMENT ON COLUMN proposals.token_amount IS 'token atomics; scale is 10^token_decimals';

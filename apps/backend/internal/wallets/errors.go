package wallets

import "errors"

var (
	ErrInvalidToken       = errors.New("wallets: invalid access token")
	ErrInvalidPayoutProof = errors.New("wallets: invalid payout proof")
	ErrAPI                = errors.New("wallets: api error")
	ErrNotConfigured      = errors.New("signer wallet client not configured")
)

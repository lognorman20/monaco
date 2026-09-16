package privy

import "errors"

var (
	ErrInvalidToken      = errors.New("privy: invalid access token")
	ErrInvalidPayoutProof = errors.New("privy: invalid payout proof")
	ErrAPI               = errors.New("privy: api error")
)

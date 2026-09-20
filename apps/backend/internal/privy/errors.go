package privy

import "errors"

var (
	ErrInvalidToken      = errors.New("privy: invalid access token")
	ErrInvalidPayoutProof = errors.New("privy: invalid payout proof")
	ErrAPI               = errors.New("privy: api error")
	// ErrBroadcastRejected means the transaction was refused before it reached the chain,
	// so it can never land and the caller may safely give up on it.
	ErrBroadcastRejected = errors.New("privy: broadcast rejected")
)

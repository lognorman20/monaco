package privy

import "errors"

var (
	ErrInvalidToken = errors.New("privy: invalid access token")
	ErrAPI          = errors.New("privy: api error")
)

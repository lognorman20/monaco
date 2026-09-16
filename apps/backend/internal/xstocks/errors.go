package xstocks

import "errors"

var (
	ErrNotFound        = errors.New("xstocks: asset not found")
	ErrNoSolanaMint    = errors.New("xstocks: no Solana deployment")
	ErrInvalidResponse = errors.New("xstocks: invalid response")
)

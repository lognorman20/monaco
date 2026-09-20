package b20

import "errors"

// ErrNotFound means the symbol is not in the B20 catalog.
var ErrNotFound = errors.New("b20: symbol not found")

package marks

import "errors"

// ErrMarkUnavailable means a Chainlink feed returned a zero or missing answer.
var ErrMarkUnavailable = errors.New("mark unavailable")

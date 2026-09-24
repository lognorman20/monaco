package mintinfo

import (
	"errors"
	"math/big"
	"time"
)

// ErrUnknownMint is returned when no RPC, cache, last-good, or static entry exists.
var ErrUnknownMint = errors.New("unknown mint")

// Token2022ProgramID is the SPL Token-2022 program owner.
const Token2022ProgramID = "TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb"

// Info holds parsed mint account fields used for pricing and routing.
type Info struct {
	Mint              string
	Decimals          int
	TransferFeeBps    int
	UiMultiplier      *big.Rat
	Paused            bool
	PermanentDelegate bool
	FetchedAt         time.Time
}

func ratOne() *big.Rat {
	return big.NewRat(1, 1)
}

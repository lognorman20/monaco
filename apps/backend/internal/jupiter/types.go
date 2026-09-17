package jupiter

// ExecuteStatus values returned by Jupiter /execute.
const (
	ExecuteStatusPending = "Pending"
	ExecuteStatusSuccess = "Success"
	ExecuteStatusFailed  = "Failed"
)

// AAPLxMint is the Solana mainnet AAPLx token mint used in tests and docs.
const AAPLxMint = "XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp"

// TSLAxMint is the Solana mainnet TSLAx token mint used in tests and docs.
const TSLAxMint = "XsDoVfqeBukxuZHWhdvWHBhgEHjGNst4MLodqsJHzoB"

// XStockDecimals is the on-chain decimal count for xStock SPL tokens on Solana mainnet.
const XStockDecimals = 8

// XStockAtomicScale is 10^XStockDecimals for Jupiter outAmount atomics → whole shares.
const XStockAtomicScale int64 = 100_000_000

// ExecuteResult is a parsed Jupiter /execute response.
type ExecuteResult struct {
	Status             string
	Code               int
	Signature          string
	RequestID          string
	InputAmountResult  string
	OutputAmountResult string
	TotalOutputAmount  string
	Error              string
}

// IsConfirmedSuccess reports whether Jupiter confirmed the swap.
func (r ExecuteResult) IsConfirmedSuccess() bool {
	return r.Status == ExecuteStatusSuccess && r.Code == 0
}

// IsTerminal reports whether polling should stop.
func (r ExecuteResult) IsTerminal() bool {
	switch r.Status {
	case ExecuteStatusFailed:
		return true
	case ExecuteStatusSuccess:
		return true
	default:
		return false
	}
}

// BuyOrder is an unsigned Jupiter buy order ready for treasury signing.
type BuyOrder struct {
	RequestID   string
	Transaction string
	InAmount    string
	OutAmount   string
	InputMint   string
	OutputMint  string
}

// SellQuote is a parsed Jupiter sell quote (xStock → USDC).
type SellQuote struct {
	Routable   bool
	InputMint  string
	OutputMint string
	InAmount   string
	OutAmount  string
	RequestID  string
	Transaction string
}

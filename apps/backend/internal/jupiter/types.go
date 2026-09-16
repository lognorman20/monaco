package jupiter

// ExecuteStatus values returned by Jupiter /execute.
const (
	ExecuteStatusPending = "Pending"
	ExecuteStatusSuccess = "Success"
	ExecuteStatusFailed  = "Failed"
)

// AAPLxMint is the Solana mainnet AAPLx token mint used in tests and docs.
const AAPLxMint = "XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp"

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

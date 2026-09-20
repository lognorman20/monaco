package wallets

// UserID is a Monaco user UUID.
type UserID string

// GroupID is a Monaco group UUID.
type GroupID string

// WalletRef is a member EVM wallet.
type WalletRef struct {
	UserID   UserID
	WalletID string
	Address  string
}

// TreasuryRef is a group treasury EVM wallet.
type TreasuryRef struct {
	GroupID  GroupID
	WalletID string
	Address  string
}

// SweepRequest is a relayer-paid USDC sweep from member wallet to treasury.
type SweepRequest struct {
	MemberAddress   string
	TreasuryAddress string
	Amount          int64
	IntentID        string
}

// SweepResult is the submitted sweep transaction hash.
type SweepResult struct {
	TxHash string
}

// TransferRequest is a relayer-paid USDC transfer from a member wallet.
type TransferRequest struct {
	MemberAddress string
	ToAddress     string
	Amount        int64
	IntentID      string
}

// TransferResult is the submitted transfer transaction hash.
type TransferResult struct {
	TxHash string
}

// PayUSDCRequest pays USDC from a treasury to a recipient (treasury pays gas).
type PayUSDCRequest struct {
	TreasuryRef     TreasuryRef
	ToAddress       string
	Amount          int64
	TreasuryAddress string // legacy callers; prefer TreasuryRef.Address
}

// PayUSDCResult is the treasury payout transaction hash.
type PayUSDCResult struct {
	TxHash string
}

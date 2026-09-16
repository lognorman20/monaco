package privy

// AccessToken is a Privy user access token from mobile login.
type AccessToken string

// UserID is a Monaco user UUID.
type UserID string

// GroupID is a Monaco group UUID.
type GroupID string

// Identity is the verified Privy session subject.
type Identity struct {
	PrivyUserID string
	SessionID   string
	DisplayName string
}

// WalletRef is a member Solana wallet provisioned in Privy.
type WalletRef struct {
	UserID         UserID
	PrivyWalletID  string
	SolanaAddress  string
}

// TreasuryRef is a group treasury Solana wallet provisioned in Privy.
type TreasuryRef struct {
	GroupID        GroupID
	PrivyWalletID  string
	SolanaAddress  string
}

// SweepRequest is a server-signed USDC transfer from member wallet to treasury.
type SweepRequest struct {
	MemberAddress   string
	TreasuryAddress string
	Amount          int64
	RelayerKey      string
}

// SweepResult is the submitted sweep transaction signature.
type SweepResult struct {
	TxSignature string
}

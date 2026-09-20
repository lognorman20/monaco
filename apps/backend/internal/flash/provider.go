package flash

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	solanakey "github.com/monaco/monaco/apps/backend/internal/solana/key"
	"github.com/monaco/monaco/apps/backend/internal/swapprovider"
)

const (
	// DefaultMaxSlippage bounds executed vs quoted output. Set explicitly: when omitted,
	// Flash applies its own recommendedSlippage, which is far wider for thin xStock pools.
	DefaultMaxSlippage = "0.01"

	associatedTokenProgramID = "ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL"
	ed25519SignatureSize     = 64
)

// TreasurySigner signs the order message and, when Flash sponsors the delegation,
// the sponsored delegate transaction with the treasury wallet.
type TreasurySigner interface {
	swapprovider.MessageSigner
	swapprovider.TransactionSigner
}

// SetupSubmitter lands the one-time onchain setup (token accounts + SPL delegation)
// with the treasury as authority, and returns the transaction signature.
type SetupSubmitter interface {
	SubmitTreasurySetup(ctx context.Context, wallet swapprovider.Wallet, instructions []Instruction) (string, error)
}

// ProviderConfig tunes the Flash swap provider.
type ProviderConfig struct {
	// MaxSlippage is a decimal string (0.01 = 1%). Blank means DefaultMaxSlippage.
	MaxSlippage string
	// SponsorAddress pays token-account rent instead of the treasury (the relayer).
	// Blank leaves the treasury in the payer slot, as Flash returns it.
	SponsorAddress string
	// SetupPoll bounds the re-quote loop that waits for onchain setup to land.
	SetupPoll PollConfig
	// Now is the clock used for quote deadlines. Nil means time.Now.
	Now func() time.Time
}

// SwapProvider adapts Flash quote → setup → sign → order → poll to swapprovider.Provider.
type SwapProvider struct {
	client Client
	signer TreasurySigner
	setup  SetupSubmitter
	cfg    ProviderConfig
}

// NewSwapProvider wires a Flash provider for treasury swaps.
func NewSwapProvider(client Client, signer TreasurySigner, setup SetupSubmitter, cfg ProviderConfig) *SwapProvider {
	if strings.TrimSpace(cfg.MaxSlippage) == "" {
		cfg.MaxSlippage = DefaultMaxSlippage
	}
	if cfg.SetupPoll.MaxAttempts <= 0 {
		cfg.SetupPoll = PollConfig{MaxAttempts: 15, Interval: 2 * time.Second}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &SwapProvider{client: client, signer: signer, setup: setup, cfg: cfg}
}

// Name identifies the provider in logs.
func (p *SwapProvider) Name() string {
	return swapprovider.NameFlash
}

// SubmitBuy spends treasury USDC for req.OutputMint.
func (p *SwapProvider) SubmitBuy(ctx context.Context, req swapprovider.Request) (swapprovider.Submission, error) {
	req.Side = swapprovider.SideBuy
	return p.submit(ctx, req, req.OutputMint, req.InputMint)
}

// SubmitSell spends treasury req.InputMint for USDC.
func (p *SwapProvider) SubmitSell(ctx context.Context, req swapprovider.Request) (swapprovider.Submission, error) {
	req.Side = swapprovider.SideSell
	return p.submit(ctx, req, req.InputMint, req.OutputMint)
}

func (p *SwapProvider) submit(ctx context.Context, req swapprovider.Request, targetAsset, contraAsset string) (swapprovider.Submission, error) {
	if req.Wallet.PrivyWalletID == "" || req.Wallet.SolanaAddress == "" {
		return swapprovider.Submission{}, swapprovider.AtStage("validate", "", fmt.Errorf("flash: treasury wallet is required"))
	}
	qty, err := AtomicsToDecimal(req.Amount, req.InputDecimals)
	if err != nil {
		return swapprovider.Submission{}, swapprovider.AtStage("validate", "", err)
	}
	params := QuoteParams{
		GroupID:       req.GroupID,
		UserID:        req.UserID,
		Symbol:        req.Symbol,
		Side:          string(req.Side),
		TargetAsset:   targetAsset,
		ContraAsset:   contraAsset,
		Qty:           qty,
		MaxSlippage:   p.cfg.MaxSlippage,
		FunderAddress: req.Wallet.SolanaAddress,
	}

	quote, err := p.client.Quote(ctx, params)
	if err != nil {
		if errors.Is(err, ErrNoRoute) {
			return swapprovider.Submission{}, fmt.Errorf("%w: %w", swapprovider.ErrNotRoutable, err)
		}
		return swapprovider.Submission{}, swapprovider.AtStage("quote", "", err)
	}

	if quote.NeedsOnchainSetup() {
		quote, err = p.runOnchainSetup(ctx, req.Wallet, params, quote)
		if err != nil {
			return swapprovider.Submission{}, swapprovider.AtStage("onchain_setup", quote.QuoteID, err)
		}
	}

	if err := p.checkSignable(quote, req); err != nil {
		return swapprovider.Submission{}, swapprovider.AtStage("check_quote", quote.QuoteID, err)
	}

	signedDelegateTx := ""
	if quote.SponsoredDelegateTx != "" {
		signedDelegateTx, err = p.signer.SignTreasuryTransaction(ctx, req.Wallet.PrivyWalletID, quote.SponsoredDelegateTx)
		if err != nil {
			return swapprovider.Submission{}, swapprovider.AtStage("sign_delegate", quote.QuoteID, err)
		}
	}

	signature, err := p.signer.SignTreasuryMessage(ctx, req.Wallet.PrivyWalletID, []byte(quote.OrderMessage))
	if err != nil {
		return swapprovider.Submission{}, swapprovider.AtStage("sign_treasury", quote.QuoteID, err)
	}
	if len(signature) != ed25519SignatureSize {
		err := fmt.Errorf("flash: treasury signature is %d bytes, want %d", len(signature), ed25519SignatureSize)
		return swapprovider.Submission{}, swapprovider.AtStage("sign_treasury", quote.QuoteID, err)
	}

	orderID, err := p.client.SubmitOrder(ctx, SubmitOrderParams{
		Quote:                     params,
		QuoteID:                   quote.QuoteID,
		UserSignature:             solanakey.EncodeBase58(signature),
		Nonce:                     quote.Nonce,
		Deadline:                  quote.Deadline,
		SignedSponsoredDelegateTx: signedDelegateTx,
	})
	if err != nil {
		return swapprovider.Submission{}, swapprovider.AtStage("order_submit", quote.QuoteID, err)
	}
	return swapprovider.Submission{RequestID: orderID, Receipt: req.Wallet.SolanaAddress, Request: req}, nil
}

// runOnchainSetup lands token-account creation and delegation, then re-quotes until
// Flash sees them onchain. POST /order rejects an order whose setup has not landed.
func (p *SwapProvider) runOnchainSetup(ctx context.Context, wallet swapprovider.Wallet, params QuoteParams, quote Quote) (Quote, error) {
	if p.setup == nil {
		return quote, fmt.Errorf("flash: onchain setup required but no setup submitter is configured")
	}

	instructions := SponsorATASetup(quote.ATASetupIxs, wallet.SolanaAddress, p.cfg.SponsorAddress)
	if quote.DelegateIx != nil {
		instructions = append(instructions, *quote.DelegateIx)
	}
	txSignature, err := p.setup.SubmitTreasurySetup(ctx, wallet, instructions)
	logSetupSubmit(params, len(instructions), txSignature, err)
	if err != nil {
		return quote, err
	}

	for attempt := 0; attempt < p.cfg.SetupPoll.MaxAttempts; attempt++ {
		if err := sleep(ctx, p.cfg.SetupPoll.Interval); err != nil {
			return quote, err
		}
		fresh, err := p.client.Quote(ctx, params)
		if err != nil {
			return quote, err
		}
		quote = fresh
		if !quote.NeedsOnchainSetup() {
			return quote, nil
		}
	}
	return quote, fmt.Errorf("%w: tx %s", ErrSetupNotConfirmed, txSignature)
}

// checkSignable refuses to sign unless the quote is live and its order message commits
// to the mint and atomic amount this swap spends.
func (p *SwapProvider) checkSignable(quote Quote, req swapprovider.Request) error {
	if quote.OrderMessage == "" || quote.Nonce == "" || quote.Deadline == "" {
		return fmt.Errorf("flash: quote missing signing payload")
	}
	deadline, err := strconv.ParseInt(quote.Deadline, 10, 64)
	if err != nil {
		return fmt.Errorf("flash: invalid quote deadline %q: %w", quote.Deadline, err)
	}
	if p.cfg.Now().Unix() >= deadline {
		return fmt.Errorf("%w: deadline %d", ErrQuoteExpired, deadline)
	}

	mint, total := orderMessageCommitment(quote.OrderMessage)
	if mint != req.InputMint || total != strconv.FormatInt(req.Amount, 10) {
		return fmt.Errorf("%w: message commits mint=%q total=%q, want mint=%q total=%d",
			ErrOrderMessageMismatch, mint, total, req.InputMint, req.Amount)
	}
	return nil
}

// orderMessageCommitment reads the spent mint (m=) and atomic total (t=) from a Flash
// Solana order message, e.g. "DFS|m=<mint>|t=5000000|h=<hash>".
func orderMessageCommitment(message string) (mint, total string) {
	for _, part := range strings.Split(message, "|") {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		switch key {
		case "m":
			mint = value
		case "t":
			total = value
		}
	}
	return mint, total
}

// SponsorATASetup moves the sponsor into the payer slot of create-associated-token-account
// instructions so the relayer, not the treasury, pays rent. The treasury stays the owner.
func SponsorATASetup(instructions []Instruction, funder, sponsor string) []Instruction {
	out := make([]Instruction, 0, len(instructions)+1)
	for _, ix := range instructions {
		if sponsor != "" && ix.ProgramID == associatedTokenProgramID && len(ix.Accounts) > 0 && ix.Accounts[0].Pubkey == funder {
			accounts := append([]AccountMeta(nil), ix.Accounts...)
			accounts[0].Pubkey = sponsor
			ix.Accounts = accounts
		}
		out = append(out, ix)
	}
	return out
}

// AwaitFill polls the order until Flash reports it filled, then converts fill totals to atomics.
func (p *SwapProvider) AwaitFill(ctx context.Context, sub swapprovider.Submission, cfg swapprovider.PollConfig) (swapprovider.Fill, error) {
	if cfg.MaxAttempts <= 0 {
		cfg = DefaultPollConfig()
	}
	req := sub.Request

	order, err := PollUntilFilled(ctx, p.client, GetOrderParams{
		GroupID:       req.GroupID,
		UserID:        req.UserID,
		Symbol:        req.Symbol,
		OrderID:       sub.RequestID,
		FunderAddress: sub.Receipt,
	}, cfg)
	fill := swapprovider.Fill{Confirmed: order.IsFilled(), Signature: order.TransactionID, Status: order.Status}
	if err != nil {
		return fill, err
	}

	// Target is the xStock on both sides; contra is USDC.
	spent, received := order.FilledContraAmount, order.FilledTargetAmount
	if req.Side == swapprovider.SideSell {
		spent, received = order.FilledTargetAmount, order.FilledContraAmount
	}
	fill.InputAmount, err = DecimalToAtomics(spent, req.InputDecimals)
	if err != nil {
		return fill, fmt.Errorf("flash: filled input: %w", err)
	}
	fill.OutputAmount, err = DecimalToAtomics(received, req.OutputDecimals)
	if err != nil {
		return fill, fmt.Errorf("flash: filled output: %w", err)
	}
	if fill.InputAmount <= 0 || fill.OutputAmount <= 0 {
		return fill, fmt.Errorf("flash: fill amounts must be positive (input=%d output=%d)", fill.InputAmount, fill.OutputAmount)
	}
	return fill, nil
}

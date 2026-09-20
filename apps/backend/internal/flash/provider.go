package flash

import (
	"context"
	"encoding/json"
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

// PrepareBuy quotes and signs an order spending treasury USDC for req.OutputMint. No order is sent.
func (p *SwapProvider) PrepareBuy(ctx context.Context, req swapprovider.Request) (swapprovider.Prepared, error) {
	req.Side = swapprovider.SideBuy
	return p.prepare(ctx, req, req.OutputMint, req.InputMint)
}

// PrepareSell quotes and signs an order spending treasury req.InputMint for USDC. No order is sent.
func (p *SwapProvider) PrepareSell(ctx context.Context, req swapprovider.Request) (swapprovider.Prepared, error) {
	req.Side = swapprovider.SideSell
	return p.prepare(ctx, req, req.InputMint, req.OutputMint)
}

func (p *SwapProvider) prepare(ctx context.Context, req swapprovider.Request, targetAsset, contraAsset string) (swapprovider.Prepared, error) {
	if req.Wallet.PrivyWalletID == "" || req.Wallet.SolanaAddress == "" {
		return swapprovider.Prepared{}, swapprovider.AtStage("validate", "", fmt.Errorf("flash: treasury wallet is required"))
	}
	qty, err := AtomicsToDecimal(req.Amount, req.InputDecimals)
	if err != nil {
		return swapprovider.Prepared{}, swapprovider.AtStage("validate", "", err)
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
			return swapprovider.Prepared{}, fmt.Errorf("%w: %w", swapprovider.ErrNotRoutable, err)
		}
		return swapprovider.Prepared{}, swapprovider.AtStage("quote", "", err)
	}

	if quote.NeedsOnchainSetup() {
		quote, err = p.runOnchainSetup(ctx, req.Wallet, params, quote)
		if err != nil {
			return swapprovider.Prepared{}, swapprovider.AtStage("onchain_setup", quote.QuoteID, err)
		}
	}

	if err := p.checkSignable(quote, req); err != nil {
		return swapprovider.Prepared{}, swapprovider.AtStage("check_quote", quote.QuoteID, err)
	}

	signedDelegateTx := ""
	if quote.SponsoredDelegateTx != "" {
		signedDelegateTx, err = p.signer.SignTreasuryTransaction(ctx, req.Wallet.PrivyWalletID, quote.SponsoredDelegateTx)
		if err != nil {
			return swapprovider.Prepared{}, swapprovider.AtStage("sign_delegate", quote.QuoteID, err)
		}
	}

	signature, err := p.signer.SignTreasuryMessage(ctx, req.Wallet.PrivyWalletID, []byte(quote.OrderMessage))
	if err != nil {
		return swapprovider.Prepared{}, swapprovider.AtStage("sign_treasury", quote.QuoteID, err)
	}
	if len(signature) != ed25519SignatureSize {
		err := fmt.Errorf("flash: treasury signature is %d bytes, want %d", len(signature), ed25519SignatureSize)
		return swapprovider.Prepared{}, swapprovider.AtStage("sign_treasury", quote.QuoteID, err)
	}

	deadline, err := strconv.ParseInt(quote.Deadline, 10, 64)
	if err != nil {
		return swapprovider.Prepared{}, swapprovider.AtStage("check_quote", quote.QuoteID, fmt.Errorf("flash: invalid quote deadline %q: %w", quote.Deadline, err))
	}
	payload, err := json.Marshal(SubmitOrderParams{
		Quote:                     params,
		QuoteID:                   quote.QuoteID,
		UserSignature:             solanakey.EncodeBase58(signature),
		Nonce:                     quote.Nonce,
		Deadline:                  quote.Deadline,
		SignedSponsoredDelegateTx: signedDelegateTx,
	})
	if err != nil {
		return swapprovider.Prepared{}, swapprovider.AtStage("encode_order", quote.QuoteID, err)
	}
	return swapprovider.Prepared{
		RequestID: quote.QuoteID,
		ExpiresAt: time.Unix(deadline, 0).UTC(),
		Payload:   string(payload),
		Request:   req,
	}, nil
}

// Submit posts the signed order. Flash settles with its own transaction and only hands back
// an order id here, so a Submit whose response is lost cannot be looked up again: only an
// explicit refusal (auth or 4xx) is reported as ErrNotSubmitted.
func (p *SwapProvider) Submit(ctx context.Context, prepared swapprovider.Prepared) (swapprovider.Submission, error) {
	var params SubmitOrderParams
	if err := json.Unmarshal([]byte(prepared.Payload), &params); err != nil {
		err = fmt.Errorf("%w: flash: invalid prepared order: %w", swapprovider.ErrNotSubmitted, err)
		return swapprovider.Submission{}, swapprovider.AtStage("order_submit", prepared.RequestID, err)
	}
	orderID, err := p.client.SubmitOrder(ctx, params)
	if err != nil {
		var apiErr *APIError
		refused := errors.Is(err, ErrUnauthorized) ||
			(errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500)
		if refused {
			err = fmt.Errorf("%w: %w", swapprovider.ErrNotSubmitted, err)
		}
		return swapprovider.Submission{}, swapprovider.AtStage("order_submit", prepared.RequestID, err)
	}
	return swapprovider.Submission{RequestID: orderID, Receipt: prepared.Request.Wallet.SolanaAddress, Request: prepared.Request}, nil
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
	if quote.QuoteID == "" || quote.OrderMessage == "" || quote.Nonce == "" || quote.Deadline == "" {
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
	fill := swapprovider.Fill{
		Confirmed: order.IsFilled(),
		Rejected:  closedWithoutFill(order),
		Signature: order.TransactionID,
		Status:    order.Status,
	}
	if err != nil {
		return fill, err
	}
	return fillAmounts(fill, order, req)
}

// Resolve asks Flash for the order's state. An order id that was never recorded (the process
// died or the response was lost during Submit) cannot be looked up, so it stays unknown.
func (p *SwapProvider) Resolve(ctx context.Context, pending swapprovider.PendingSwap) (swapprovider.Resolution, error) {
	if !pending.Submitted {
		reason := "flash order id was never recorded; needs manual review"
		if !pending.ExpiresAt.IsZero() && p.cfg.Now().Before(pending.ExpiresAt) {
			reason = "submit outcome unknown; quote still live"
		}
		return swapprovider.Resolution{Outcome: swapprovider.OutcomeUnknown, Reason: reason}, nil
	}

	req := pending.Request
	order, err := p.client.GetOrder(ctx, GetOrderParams{
		GroupID:       req.GroupID,
		UserID:        req.UserID,
		Symbol:        req.Symbol,
		OrderID:       pending.RequestID,
		FunderAddress: req.Wallet.SolanaAddress,
	})
	if err != nil {
		return swapprovider.Resolution{}, err
	}
	if closedWithoutFill(order) {
		return swapprovider.Resolution{Outcome: swapprovider.OutcomeFailed, Reason: "flash order " + order.Status + " " + order.CloseReason}, nil
	}
	if !order.IsFilled() || order.TransactionID == "" {
		return swapprovider.Resolution{Outcome: swapprovider.OutcomeUnknown, Reason: "flash order " + order.Status}, nil
	}
	fill, err := fillAmounts(swapprovider.Fill{Confirmed: true, Signature: order.TransactionID, Status: order.Status}, order, req)
	if err != nil {
		return swapprovider.Resolution{}, err
	}
	return swapprovider.Resolution{Outcome: swapprovider.OutcomeFilled, Fill: fill}, nil
}

// closedWithoutFill reports whether Flash closed the order with nothing filled. A terminal
// order that carries fill amounts moved funds and is never reported as rejected.
func closedWithoutFill(order Order) bool {
	if !order.IsTerminal() || order.IsFilled() {
		return false
	}
	return strings.Trim(order.FilledTargetAmount, "0.") == "" && strings.Trim(order.FilledContraAmount, "0.") == ""
}

// fillAmounts converts the order's decimal fill totals to atomics on fill.
func fillAmounts(fill swapprovider.Fill, order Order, req swapprovider.Request) (swapprovider.Fill, error) {
	var err error
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

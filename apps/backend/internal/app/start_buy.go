package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

// ErrQuoteNotRoutable means Jupiter returned no route for the requested buy.
var ErrQuoteNotRoutable = errors.New("quote not routable")

// CatalogRoutabilityProbeMicros is the USDC amount used for Jupiter catalog routability probes.
const CatalogRoutabilityProbeMicros = 1_000_000

// ErrProposalNotPassed means Jupiter execute requires a passed proposal tally.
var ErrProposalNotPassed = errors.New("proposal not passed")

// StartBuyRequest is input for quote gating before proposal create or execute.
type StartBuyRequest struct {
	GroupID    string
	UserID     string
	Symbol     string
	USDCAmount int64
	// Taker is the group treasury wallet. When set, Jupiter must return a buildable
	// unsigned transaction (same /order constraints as execute OrderBuy).
	Taker string
}

// StartBuyResult holds a routable Jupiter quote ready for execute.
type StartBuyResult struct {
	OutputMint string
	Quote      jupiter.BuyQuote
}

// BuyService gates treasury buys behind quote availability.
type BuyService struct {
	jupiter jupiter.Client
	xstocks xstocks.Resolver
}

// NewBuyService wires Jupiter quote and xStocks mint resolution.
func NewBuyService(jupiterClient jupiter.Client, resolver xstocks.Resolver) *BuyService {
	return &BuyService{
		jupiter: jupiterClient,
		xstocks: resolver,
	}
}

// JupiterCatalogRoutabilityProber probes Jupiter for USDC→xStock routes during catalog ranking.
type JupiterCatalogRoutabilityProber struct {
	jupiter jupiter.Client
}

// NewJupiterCatalogRoutabilityProber returns a catalog prober backed by Jupiter quotes.
func NewJupiterCatalogRoutabilityProber(jupiterClient jupiter.Client) *JupiterCatalogRoutabilityProber {
	return &JupiterCatalogRoutabilityProber{jupiter: jupiterClient}
}

// IsRoutable reports whether Jupiter can quote a small USDC buy into the asset mint.
func (p *JupiterCatalogRoutabilityProber) IsRoutable(ctx context.Context, asset xstocks.CatalogAsset) bool {
	if p == nil || p.jupiter == nil || strings.TrimSpace(asset.SolanaMint) == "" {
		return false
	}
	quote, err := p.jupiter.QuoteBuy(ctx, jupiter.QuoteBuyParams{
		Symbol:     asset.Symbol,
		OutputMint: asset.SolanaMint,
		USDCAmount: CatalogRoutabilityProbeMicros,
	})
	if err != nil {
		// Catalog probes are advisory. A Jupiter rate limit, timeout, or
		// below-minimum probe must not make a buyable stock look disabled.
		// Keep only an explicit no-route response as a definitive negative;
		// the real buy amount is checked again by StartBuy.
		return !errors.Is(err, jupiter.ErrNoRoute)
	}
	return quote.Routable
}

// ResolveOutputMint returns the Solana mint for a catalog symbol.
func (s *BuyService) ResolveOutputMint(ctx context.Context, symbol string) (string, error) {
	return s.xstocks.ResolveSolanaMint(ctx, symbol)
}

// StartBuy resolves the xStock mint and refuses when Jupiter has no route.
func (s *BuyService) StartBuy(ctx context.Context, req StartBuyRequest) (StartBuyResult, error) {
	logSwapQuoteAttempt(req.GroupID, req.UserID, req.Symbol, req.USDCAmount)

	outputMint, err := s.ResolveOutputMint(ctx, req.Symbol)
	if err != nil {
		logSwapRefusal(req.GroupID, req.UserID, req.Symbol, err.Error())
		return StartBuyResult{}, err
	}

	quote, err := s.jupiter.QuoteBuy(ctx, jupiter.QuoteBuyParams{
		GroupID:    req.GroupID,
		UserID:     req.UserID,
		Symbol:     req.Symbol,
		OutputMint: outputMint,
		USDCAmount: req.USDCAmount,
		Taker:      req.Taker,
	})
	if err != nil {
		reason := err.Error()
		if errors.Is(err, jupiter.ErrNoRoute) {
			reason = "no route"
		}
		logSwapRefusal(req.GroupID, req.UserID, req.Symbol, reason)
		if errors.Is(err, jupiter.ErrNoRoute) {
			return StartBuyResult{}, fmt.Errorf("%w: %s", ErrQuoteNotRoutable, reason)
		}
		return StartBuyResult{}, err
	}
	if !quote.Routable {
		logSwapRefusal(req.GroupID, req.UserID, req.Symbol, "no route")
		return StartBuyResult{}, fmt.Errorf("%w: no route", ErrQuoteNotRoutable)
	}

	return StartBuyResult{
		OutputMint: outputMint,
		Quote:      quote,
	}, nil
}

// ExecuteOnPassService orchestrates Jupiter v2 buy execute after proposal pass (M4-T19).
type ExecuteOnPassService struct {
	swap  *SwapService
	store *postgres.Store
}

// NewExecuteOnPassService wires vote-pass buy execute dependencies.
func NewExecuteOnPassService(swap *SwapService, store *postgres.Store) *ExecuteOnPassService {
	return &ExecuteOnPassService{
		swap:  swap,
		store: store,
	}
}

// ExecuteOnPassResult is a confirmed treasury buy linked to a passed proposal.
type ExecuteOnPassResult struct {
	Transaction postgres.TransactionRow
	Created     bool
}

// ExecuteOnPass builds, signs, POSTs Jupiter execute, polls Success code 0, and persists the buy.
// Only proposals with status passed may execute. Idempotency on proposal id is wired for M4-T21.
func (s *ExecuteOnPassService) ExecuteOnPass(ctx context.Context, proposal Proposal) (ExecuteOnPassResult, error) {
	kind := proposal.Kind
	if kind == "" {
		kind = ProposalKindBuy
	}
	logExecuteOnPassStart(proposal.ID, proposal.GroupID, proposal.Symbol, proposal.UsdcMicros)

	if proposal.Status != ProposalPassed {
		logExecuteOnPassBranchWarn("execute on pass rejected", "proposal not passed", "proposal_id", proposal.ID, "status", proposal.Status)
		return ExecuteOnPassResult{}, ErrProposalNotPassed
	}
	if proposal.ID == "" || proposal.GroupID == "" {
		logExecuteOnPassBranchWarn("execute on pass rejected", "missing ids")
		return ExecuteOnPassResult{}, fmt.Errorf("proposal id and group id are required")
	}
	if proposal.Symbol == "" {
		logExecuteOnPassBranchWarn("execute on pass rejected", "symbol required", "proposal_id", proposal.ID)
		return ExecuteOnPassResult{}, fmt.Errorf("symbol is required")
	}

	// Faker groups and faker proposers (#153) never execute (buy or sell): seeded passed proposals must
	// not trigger live Jupiter swaps from a real (mixed club) treasury.
	if err := s.rejectFakerProposal(ctx, proposal); err != nil {
		logExecuteOnPassBranchWarn("execute on pass rejected", "faker proposal", "proposal_id", proposal.ID, "group_id", proposal.GroupID)
		return ExecuteOnPassResult{}, err
	}

	switch kind {
	case ProposalKindBuy:
		return s.executeBuyOnPass(ctx, proposal)
	case ProposalKindSell:
		return s.executeSellOnPass(ctx, proposal)
	default:
		return ExecuteOnPassResult{}, fmt.Errorf("invalid proposal kind")
	}
}

func (s *ExecuteOnPassService) executeBuyOnPass(ctx context.Context, proposal Proposal) (ExecuteOnPassResult, error) {
	if proposal.UsdcMicros <= 0 {
		logExecuteOnPassBranchWarn("execute on pass rejected", "usdc not positive", "proposal_id", proposal.ID)
		return ExecuteOnPassResult{}, fmt.Errorf("usdc must be positive")
	}

	if existing, ok, err := s.existingBuyForProposal(ctx, proposal.ID); err != nil {
		logExecuteOnPassBranchError("execute on pass lookup existing failed", err, "proposal_id", proposal.ID)
		return ExecuteOnPassResult{}, err
	} else if ok {
		logExecuteOnPassIdempotent(proposal.ID, existing.ID)
		return ExecuteOnPassResult{Transaction: existing, Created: false}, nil
	}

	executeResult, err := s.swap.DevExecuteBuy(ctx, DevExecuteBuyRequest{
		GroupID:    proposal.GroupID,
		UserID:     proposal.ProposerID,
		Symbol:     proposal.Symbol,
		USDCAmount: proposal.UsdcMicros,
		ProposalID: proposal.ID,
	})
	if err != nil {
		logExecuteOnPassBranchError("execute on pass buy failed", err,
			"proposal_id", proposal.ID, "group_id", proposal.GroupID, "symbol", proposal.Symbol,
			"usdc_amount", proposal.UsdcMicros, "stage", "dev_execute_buy")
		return ExecuteOnPassResult{}, err
	}

	linked, _, err := s.linkBuyToProposal(ctx, proposal.ID, executeResult.Transaction)
	if err != nil {
		logExecuteOnPassBranchError("execute on pass link failed", err, "proposal_id", proposal.ID)
		return ExecuteOnPassResult{}, err
	}

	if err := s.recordTreasuryHoldingsAndNavSnapshot(ctx, proposal, linked, executeResult.Created); err != nil {
		logExecuteOnPassBranchError("execute on pass record holdings failed", err, "proposal_id", proposal.ID)
		return ExecuteOnPassResult{}, err
	}

	logExecuteOnPassSuccess(proposal.ID, linked.ID, executeResult.Created)
	return ExecuteOnPassResult{
		Transaction: linked,
		Created:     executeResult.Created,
	}, nil
}

func (s *ExecuteOnPassService) executeSellOnPass(ctx context.Context, proposal Proposal) (ExecuteOnPassResult, error) {
	if proposal.TokenAmount <= 0 {
		return ExecuteOnPassResult{}, fmt.Errorf("token amount must be positive")
	}
	if existing, found, err := s.store.GetConfirmedTransactionByProposalAndAction(ctx, proposal.ID, postgres.TransactionActionSell); err != nil {
		return ExecuteOnPassResult{}, err
	} else if found {
		logExecuteOnPassIdempotent(proposal.ID, existing.ID)
		return ExecuteOnPassResult{Transaction: existing, Created: false}, nil
	}
	if latest, found, err := s.store.GetLatestTransactionByProposalAndAction(ctx, proposal.ID, postgres.TransactionActionSell); err != nil {
		return ExecuteOnPassResult{}, err
	} else if found && (latest.Status == postgres.TransactionStatusFailed || latest.Status == postgres.TransactionStatusPending) {
		return ExecuteOnPassResult{}, fmt.Errorf("sell execute already attempted")
	}

	inputMint, err := s.swap.buy.ResolveOutputMint(ctx, proposal.Symbol)
	if err != nil {
		return ExecuteOnPassResult{}, err
	}
	result, err := s.swap.SellToUSDC(ctx, SellToUSDCRequest{
		GroupID:    proposal.GroupID,
		UserID:     proposal.ProposerID,
		Symbol:     proposal.Symbol,
		InputMint:  inputMint,
		Amount:     proposal.TokenAmount,
		ProposalID: proposal.ID,
	})
	if err != nil {
		logExecuteOnPassBranchError("execute on pass sell failed", err,
			"proposal_id", proposal.ID, "group_id", proposal.GroupID, "symbol", proposal.Symbol)
		return ExecuteOnPassResult{}, err
	}
	logExecuteOnPassSuccess(proposal.ID, result.Transaction.ID, result.Created)
	return ExecuteOnPassResult{Transaction: result.Transaction, Created: result.Created}, nil
}

func (s *ExecuteOnPassService) rejectFakerProposal(ctx context.Context, proposal Proposal) error {
	if err := rejectFakerGroup(ctx, s.store, proposal.GroupID); err != nil {
		return err
	}
	if proposal.ProposerID == "" {
		return nil
	}
	isFaker, err := s.store.IsFakerUser(ctx, proposal.ProposerID)
	if err != nil {
		return err
	}
	if isFaker {
		return ErrFakerGroupReadOnly
	}
	return nil
}

func (s *ExecuteOnPassService) existingBuyForProposal(ctx context.Context, proposalID string) (postgres.TransactionRow, bool, error) {
	existing, found, err := s.store.GetConfirmedTransactionByProposal(ctx, proposalID)
	if err != nil {
		return postgres.TransactionRow{}, false, err
	}
	if found {
		return existing, true, nil
	}
	return postgres.TransactionRow{}, false, nil
}

func (s *ExecuteOnPassService) linkBuyToProposal(ctx context.Context, proposalID string, tx postgres.TransactionRow) (postgres.TransactionRow, bool, error) {
	if tx.ProposalID.Valid && tx.ProposalID.String == proposalID {
		return tx, false, nil
	}
	if tx.ProposalID.Valid && tx.ProposalID.String != proposalID {
		return postgres.TransactionRow{}, false, fmt.Errorf("transaction already linked to another proposal")
	}
	if !tx.TxSignature.Valid {
		return postgres.TransactionRow{}, false, fmt.Errorf("transaction signature is required")
	}
	if bySig, found, err := s.store.GetConfirmedTransactionBySignature(ctx, tx.TxSignature.String); err != nil {
		return postgres.TransactionRow{}, false, err
	} else if found && bySig.ProposalID.Valid && bySig.ProposalID.String != proposalID {
		return postgres.TransactionRow{}, false, fmt.Errorf("transaction signature already linked to another proposal")
	}
	return s.store.SetTransactionProposalID(ctx, tx.ID, proposalID)
}

func (s *ExecuteOnPassService) recordTreasuryHoldingsAndNavSnapshot(
	ctx context.Context,
	proposal Proposal,
	tx postgres.TransactionRow,
	buyCreated bool,
) error {
	if !buyCreated {
		return nil
	}
	treasury, err := s.swap.privy.EnsureTreasury(ctx, privy.GroupID(proposal.GroupID))
	if err != nil {
		return err
	}
	treasuryUSDC, err := s.swap.treasuryUSDCForSnapshot(ctx, treasury.SolanaAddress)
	if err != nil {
		return err
	}
	return RecordConfirmedBuyHoldings(ctx, s.store, proposal.GroupID, tx, treasuryUSDC)
}

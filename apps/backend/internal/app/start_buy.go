package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

// ErrQuoteNotRoutable means Jupiter returned no route for the requested buy.
var ErrQuoteNotRoutable = errors.New("quote not routable")

// ErrProposalNotPassed means Jupiter execute requires a passed proposal tally.
var ErrProposalNotPassed = errors.New("proposal not passed")

// StartBuyRequest is input for quote gating before proposal create or execute.
type StartBuyRequest struct {
	GroupID    string
	UserID     string
	Symbol     string
	USDCAmount int64
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

// StartBuy resolves the xStock mint and refuses when Jupiter has no route.
func (s *BuyService) StartBuy(ctx context.Context, req StartBuyRequest) (StartBuyResult, error) {
	logSwapQuoteAttempt(req.GroupID, req.UserID, req.Symbol, req.USDCAmount)

	outputMint, err := s.xstocks.ResolveSolanaMint(ctx, req.Symbol)
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
	if proposal.Status != ProposalPassed {
		return ExecuteOnPassResult{}, ErrProposalNotPassed
	}
	if proposal.ID == "" || proposal.GroupID == "" {
		return ExecuteOnPassResult{}, fmt.Errorf("proposal id and group id are required")
	}
	if proposal.Symbol == "" {
		return ExecuteOnPassResult{}, fmt.Errorf("symbol is required")
	}
	if proposal.UsdcMicros <= 0 {
		return ExecuteOnPassResult{}, fmt.Errorf("usdc must be positive")
	}

	if existing, ok, err := s.existingBuyForProposal(ctx, proposal.ID); err != nil {
		return ExecuteOnPassResult{}, err
	} else if ok {
		return ExecuteOnPassResult{Transaction: existing, Created: false}, nil
	}

	executeResult, err := s.swap.DevExecuteBuy(ctx, DevExecuteBuyRequest{
		GroupID:    proposal.GroupID,
		UserID:     proposal.ProposerID,
		Symbol:     proposal.Symbol,
		USDCAmount: proposal.UsdcMicros,
	})
	if err != nil {
		return ExecuteOnPassResult{}, err
	}

	linked, _, err := s.linkBuyToProposal(ctx, proposal.ID, executeResult.Transaction)
	if err != nil {
		return ExecuteOnPassResult{}, err
	}

	if err := s.recordTreasuryHoldingsAndNavSnapshot(ctx, proposal, linked, executeResult.Created); err != nil {
		return ExecuteOnPassResult{}, err
	}

	return ExecuteOnPassResult{
		Transaction: linked,
		Created:     executeResult.Created,
	}, nil
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

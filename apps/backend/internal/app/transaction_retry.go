package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
)

// ErrTransactionNotRetryable means the transaction is not a failed buy/sell swap.
var ErrTransactionNotRetryable = errors.New("transaction not retryable")

// ErrTransactionNotFound means no transaction exists for the id.
var ErrTransactionNotFound = errors.New("transaction not found")

// RetryFailedSwapRequest retries a failed treasury buy or sell.
type RetryFailedSwapRequest struct {
	TransactionID string
	UserID        string
}

// RetryFailedSwapResult is the new or existing swap attempt after retry.
type RetryFailedSwapResult struct {
	Transaction postgres.TransactionRow
	Created     bool
}

// RetryFailedSwap re-executes a failed buy or sell with a fresh Jupiter order.
func (s *SwapService) RetryFailedSwap(ctx context.Context, req RetryFailedSwapRequest) (RetryFailedSwapResult, error) {
	if strings.TrimSpace(req.TransactionID) == "" {
		return RetryFailedSwapResult{}, fmt.Errorf("transaction id is required")
	}
	if strings.TrimSpace(req.UserID) == "" {
		return RetryFailedSwapResult{}, fmt.Errorf("user id is required")
	}

	tx, found, err := s.store.GetTransactionByID(ctx, req.TransactionID)
	if err != nil {
		return RetryFailedSwapResult{}, err
	}
	if !found {
		return RetryFailedSwapResult{}, ErrTransactionNotFound
	}
	if err := rejectFakerGroup(ctx, s.store, tx.GroupID); err != nil {
		return RetryFailedSwapResult{}, err
	}
	if tx.Status != postgres.TransactionStatusFailed {
		return RetryFailedSwapResult{}, ErrTransactionNotRetryable
	}
	if tx.Action != postgres.TransactionActionBuy && tx.Action != postgres.TransactionActionSell {
		return RetryFailedSwapResult{}, ErrTransactionNotRetryable
	}

	if tx.ProposalID.Valid {
		if confirmed, ok, err := s.store.GetConfirmedTransactionByProposal(ctx, tx.ProposalID.String); err != nil {
			return RetryFailedSwapResult{}, err
		} else if ok {
			return RetryFailedSwapResult{Transaction: confirmed, Created: false}, nil
		}
	}

	proposalID := ""
	if tx.ProposalID.Valid {
		proposalID = tx.ProposalID.String
	}

	switch tx.Action {
	case postgres.TransactionActionBuy:
		symbol := s.symbolForMint(ctx, tx.OutputMint)
		if symbol == "" || symbol == unknownStockSymbol {
			return RetryFailedSwapResult{}, fmt.Errorf("unsupported output mint for retry")
		}
		result, err := s.DevExecuteBuy(ctx, DevExecuteBuyRequest{
			GroupID:    tx.GroupID,
			UserID:     req.UserID,
			Symbol:     symbol,
			USDCAmount: tx.Amount,
			ProposalID: proposalID,
		})
		if err != nil {
			return RetryFailedSwapResult{}, err
		}
		return RetryFailedSwapResult{Transaction: result.Transaction, Created: result.Created}, nil
	case postgres.TransactionActionSell:
		symbol := s.symbolForMint(ctx, tx.InputMint)
		if symbol == "" || symbol == unknownStockSymbol {
			return RetryFailedSwapResult{}, fmt.Errorf("unsupported input mint for retry")
		}
		// The proposal id keeps a manual retry and the execute poller on the same execution slot.
		result, err := s.SellToUSDC(ctx, SellToUSDCRequest{
			GroupID:    tx.GroupID,
			UserID:     req.UserID,
			Symbol:     symbol,
			InputMint:  tx.InputMint,
			Amount:     tx.Amount,
			ProposalID: proposalID,
		})
		if err != nil {
			return RetryFailedSwapResult{}, err
		}
		return RetryFailedSwapResult{Transaction: result.Transaction, Created: result.Created}, nil
	default:
		return RetryFailedSwapResult{}, ErrTransactionNotRetryable
	}
}

package app

import (
	"context"
	"errors"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/solana/mintinfo"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

// ErrIssuerPaused means the token issuer has paused transfers on this mint.
var ErrIssuerPaused = errors.New("issuer paused")

const executeFailureIssuerPaused = "issuer_paused:"

func transferFeeBpsAtExecute(ctx context.Context, reader mintinfo.Reader, asset xstocks.CatalogAsset) int {
	feeBps := asset.TransferFeeBps
	if reader == nil {
		return feeBps
	}
	info, err := reader.Info(ctx, asset.SolanaMint)
	if err != nil {
		return feeBps
	}
	if info.TransferFeeBps > 0 {
		return info.TransferFeeBps
	}
	return feeBps
}

func preIPOSwapGuard(ctx context.Context, reader mintinfo.Reader, asset xstocks.CatalogAsset) error {
	if asset.Kind != xstocks.AssetKindPreIPO {
		return nil
	}
	if reader != nil {
		info, err := reader.Info(ctx, asset.SolanaMint)
		if err == nil && info.Paused {
			return ErrIssuerPaused
		}
		return nil
	}
	if asset.Paused {
		return ErrIssuerPaused
	}
	return nil
}

func issuerPausedExecuteRequestID(proposalID string) string {
	if proposalID == "" {
		return executeFailureIssuerPaused + "none"
	}
	return executeFailureIssuerPaused + proposalID
}

func swapFailureReason(executeRequestID string) string {
	if executeRequestID == "" {
		return ""
	}
	if len(executeRequestID) >= len(executeFailureIssuerPaused) &&
		executeRequestID[:len(executeFailureIssuerPaused)] == executeFailureIssuerPaused {
		return "issuer_paused"
	}
	return ""
}

func (s *SwapService) recordIssuerPausedBuyFailure(ctx context.Context, req DevExecuteBuyRequest, outputMint string) error {
	if s.store == nil {
		return nil
	}
	_, err := s.store.InsertFailedTransaction(
		ctx,
		req.GroupID,
		postgres.TransactionActionBuy,
		jupiter.USDCMint,
		outputMint,
		req.USDCAmount,
		issuerPausedExecuteRequestID(req.ProposalID),
	)
	return err
}

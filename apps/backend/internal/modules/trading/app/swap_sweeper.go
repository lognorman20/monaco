package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/monaco/monaco/apps/backend/internal/errs"
	"github.com/monaco/monaco/apps/backend/internal/modules/trading/domain"
	"github.com/monaco/monaco/apps/backend/internal/modules/trading/sqlc"
	"github.com/monaco/monaco/apps/backend/internal/platform/chain"
	"github.com/monaco/monaco/apps/backend/internal/platform/clock"
	"github.com/monaco/monaco/apps/backend/internal/platform/db"
	"github.com/monaco/monaco/apps/backend/internal/platform/ids"
	"github.com/monaco/monaco/apps/backend/internal/platform/poller"
)

const (
	SwapSweepInterval = 30 * time.Second
	SwapSweepAge      = 2 * time.Minute
	SwapSweepBatch    = 100
)

type SwapSweeper struct {
	uow   *db.UnitOfWork
	reads sqlc.DBTX
	clock clock.Clock
	chain ChainReader
	hints Hints
}

func NewSwapSweeper(
	uow *db.UnitOfWork,
	reads sqlc.DBTX,
	c clock.Clock,
	reader ChainReader,
	hints Hints,
) *SwapSweeper {
	return &SwapSweeper{uow: uow, reads: reads, clock: c, chain: reader, hints: hints}
}

func (*SwapSweeper) Name() string { return "trading.swap_sweeper" }

func (*SwapSweeper) Interval() time.Duration { return SwapSweepInterval }

func (p *SwapSweeper) Tick(ctx context.Context) (poller.Report, error) {
	now := p.clock.Now()
	created, err := sqlc.New(p.reads).ListStaleCreated(ctx, sqlc.ListStaleCreatedParams{
		OlderThan: now.Add(-SwapSweepAge), MaxRows: SwapSweepBatch,
	})
	if err != nil {
		return poller.Report{}, errs.Wrap(err, errs.CodeInternal, "trading.SwapSweeper.created")
	}
	report, failures := poller.Report{Scanned: len(created)}, []error{}
	for _, row := range created {
		moved, moveErr := p.failCreated(ctx, now, row)
		if moved {
			report.Changed++
		}
		failures = append(failures, moveErr)
	}
	submitted, err := p.staleSubmitted(ctx, now, failures)
	if err != nil {
		return report, err
	}
	report.Scanned += len(submitted)
	if len(submitted) == 0 {
		return report, errors.Join(failures...)
	}
	return p.resolve(ctx, now, report, failures, submitted)
}

func (p *SwapSweeper) staleSubmitted(
	ctx context.Context, now time.Time, failures []error,
) ([]sqlc.ListStaleSubmittedRow, error) {
	submitted, err := sqlc.New(p.reads).ListStaleSubmitted(ctx, sqlc.ListStaleSubmittedParams{
		OlderThan: now.Add(-SwapSweepAge), MaxRows: SwapSweepBatch,
	})
	if err != nil {
		return nil, errors.Join(
			append(failures, errs.Wrap(err, errs.CodeInternal, "trading.SwapSweeper.submitted"))...)
	}
	return submitted, nil
}

func (p *SwapSweeper) resolve(
	ctx context.Context,
	now time.Time,
	report poller.Report,
	failures []error,
	submitted []sqlc.ListStaleSubmittedRow,
) (poller.Report, error) {
	rows := make([]sqlc.ListStaleSubmittedRow, 0, len(submitted))
	signatures := make([]chain.Signature, 0, len(submitted))
	for _, row := range submitted {
		if !row.TxSignature.Valid || row.TxSignature.String == "" {
			decodeErr := errs.New(
				errs.CodeDecodeFailed,
				"trading.SwapSweeper.signature",
				slog.String("swap_id", row.ID.String()),
			)
			failures = append(failures, decodeErr)
			failures = append(failures, p.rotate(ctx, row.ID, now))
			continue
		}
		rows = append(rows, row)
		signatures = append(signatures, chain.Signature(row.TxSignature.String))
	}
	statuses, err := p.chain.SignatureStatuses(ctx, signatures)
	if err != nil {
		return report, errors.Join(append(failures, err)...)
	}
	if len(statuses) != len(rows) {
		decodeErr := errs.New(
			errs.CodeDecodeFailed,
			"trading.SwapSweeper.statuses",
			slog.Int("statuses", len(statuses)),
			slog.Int("swaps", len(rows)),
		)
		return report, errors.Join(append(failures, decodeErr)...)
	}
	for i, row := range rows {
		moved, moveErr := p.resolveSubmitted(ctx, now, row, statuses[i])
		if moved {
			report.Changed++
		} else {
			moveErr = errors.Join(moveErr, p.rotate(ctx, row.ID, now))
		}
		failures = append(failures, moveErr)
	}
	return report, errors.Join(failures...)
}

func (p *SwapSweeper) rotate(ctx context.Context, id uuid.UUID, now time.Time) error {
	_, err := sqlc.New(p.reads).TouchSubmitted(ctx, sqlc.TouchSubmittedParams{ID: id, UpdatedAt: now})
	if err == nil {
		return nil
	}
	return errs.Wrap(err, errs.CodeInternal, "trading.SwapSweeper.rotate")
}

func (p *SwapSweeper) failCreated(
	ctx context.Context,
	now time.Time,
	row sqlc.ListStaleCreatedRow,
) (bool, error) {
	req, err := sweepRequest(
		row.CabalID,
		row.SourceKind,
		row.SourceID,
		row.Action,
		row.Symbol,
		row.InMint,
		row.InAmount,
		row.SourceBatchSize,
	)
	if err != nil {
		return false, err
	}
	return p.move(ctx, now, req, failed(req, row.ID, domain.FailureNeverSubmitted, ""))
}

func (p *SwapSweeper) resolveSubmitted(
	ctx context.Context, now time.Time, row sqlc.ListStaleSubmittedRow, status SigStatus,
) (bool, error) {
	req, err := sweepRequest(
		row.CabalID,
		row.SourceKind,
		row.SourceID,
		row.Action,
		row.Symbol,
		row.InMint,
		row.InAmount,
		row.SourceBatchSize,
	)
	if err != nil {
		return false, err
	}
	signature := chain.Signature(row.TxSignature.String)
	switch status.State {
	case SigProcessing:
		return false, nil
	case SigNotFound:
		return p.expire(ctx, now, req, row, signature)
	case SigFinalized:
		return p.finalized(ctx, now, req, row, status, signature)
	default:
		return false, errs.New(errs.CodeDecodeFailed, "trading.SwapSweeper.status",
			slog.String("swap_id", row.ID.String()), slog.Int("state", int(status.State)))
	}
}

func (p *SwapSweeper) expire(
	ctx context.Context, now time.Time, req SwapRequest, row sqlc.ListStaleSubmittedRow, signature chain.Signature,
) (bool, error) {
	valid, err := p.chain.BlockhashValid(ctx, row.SignedTx)
	if err != nil {
		return false, err
	}
	if valid {
		return false, nil
	}
	statuses, err := p.chain.SignatureStatuses(ctx, []chain.Signature{signature})
	if err != nil {
		return false, err
	}
	if len(statuses) != 1 {
		return false, errs.New(errs.CodeDecodeFailed, "trading.SwapSweeper.expiryStatus",
			slog.String("swap_id", row.ID.String()), slog.Int("statuses", len(statuses)))
	}
	if statuses[0].State != SigNotFound {
		return p.resolveSubmitted(ctx, now, row, statuses[0])
	}
	return p.move(ctx, now, req, failed(req, row.ID, domain.FailureBlockhashExpired, ""))
}

func (p *SwapSweeper) finalized(
	ctx context.Context,
	now time.Time,
	req SwapRequest,
	row sqlc.ListStaleSubmittedRow,
	status SigStatus,
	signature chain.Signature,
) (bool, error) {
	if status.Failed {
		return p.move(ctx, now, req, failed(req, row.ID, domain.FailureJupiterFailed, ""))
	}
	if row.OutDecimals < 0 || row.OutDecimals > 255 {
		return false, errs.New(errs.CodeDecodeFailed, "trading.SwapSweeper.outMint",
			slog.String("swap_id", row.ID.String()), slog.Int("decimals", int(row.OutDecimals)))
	}
	req.OutMint = chain.Mint{Address: chain.SolanaAddress(row.OutMint), Decimals: uint8(row.OutDecimals)}
	out, err := p.chain.InboundAmount(
		ctx,
		signature,
		chain.SolanaAddress(row.TreasuryAddress),
		req.OutMint,
	)
	if err != nil {
		return false, err
	}
	column, ok := domain.Column(out.Uint64())
	if !ok {
		return false, errs.New(errs.CodeDecodeFailed, "trading.SwapSweeper.outAmount",
			slog.String("swap_id", row.ID.String()), slog.Uint64("out_amount", out.Uint64()))
	}
	return p.move(ctx, now, req, confirmed(req, row.ID, signature, out.Uint64(), column))
}

func (p *SwapSweeper) move(
	ctx context.Context,
	now time.Time,
	req SwapRequest,
	s step,
) (bool, error) {
	moved := false
	err := p.uow.Do(ctx, func(ctx context.Context, tx db.Tx) error {
		n, err := s.apply(ctx, sqlc.New(tx.Queries()), now)
		if err != nil || n == 0 {
			return err
		}
		if err := tx.Events.Append(ctx, s.event(now)); err != nil {
			return err
		}
		moved = true
		tx.AfterCommit(func(ctx context.Context) {
			p.hints.PublishHint(ctx, "cabal."+req.CabalID.UUID().String()+".swap_updated", nil)
		})
		return nil
	})
	return moved && err == nil, err
}

func sweepRequest(
	cabalID uuid.UUID,
	sourceKind string,
	sourceID uuid.UUID,
	action, symbol, inMint string,
	inAmount int64,
	batchSize int32,
) (SwapRequest, error) {
	kind, err := domain.ParseSourceKind(sourceKind)
	if err != nil {
		return SwapRequest{}, err
	}
	parsedAction, err := domain.ParseAction(action)
	if err != nil {
		return SwapRequest{}, err
	}
	amount, err := domain.ParseAmount(inAmount)
	if err != nil {
		return SwapRequest{}, err
	}
	if batchSize < 1 {
		return SwapRequest{}, errs.New(errs.CodeDecodeFailed, "trading.SwapSweeper.sourceBatchSize",
			slog.Int("source_batch_size", int(batchSize)))
	}
	return SwapRequest{
		Source: domain.Source{
			Kind: kind,
			ID:   sourceID,
		}, CabalID: ids.CabalIDFrom(cabalID), Action: parsedAction,
		Symbol: symbol, InMint: chain.Mint{Address: chain.SolanaAddress(inMint)}, InAmount: amount,
		SourceBatchSize: int(batchSize),
	}, nil
}

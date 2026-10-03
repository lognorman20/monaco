package app

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/monaco/monaco/apps/backend/internal/errs"
	"github.com/monaco/monaco/apps/backend/internal/events"
	"github.com/monaco/monaco/apps/backend/internal/modules/trading/domain"
	"github.com/monaco/monaco/apps/backend/internal/modules/trading/sqlc"
	"github.com/monaco/monaco/apps/backend/internal/platform/chain"
	"github.com/monaco/monaco/apps/backend/internal/platform/clock"
	"github.com/monaco/monaco/apps/backend/internal/platform/db"
	"github.com/monaco/monaco/apps/backend/internal/platform/faultpoint"
	"github.com/monaco/monaco/apps/backend/internal/platform/ids"
	"github.com/monaco/monaco/apps/backend/internal/platform/money"
	"github.com/monaco/monaco/apps/backend/internal/platform/observability"
)

const heartbeatEvery = 10 * time.Second

type TreasuryWallet struct {
	PrivyWalletID string
	Address       chain.SolanaAddress
}

type SwapRequest struct {
	Source          domain.Source
	CabalID         ids.CabalID
	TreasuryWallet  TreasuryWallet
	Action          domain.Action
	Symbol          string
	InMint          chain.Mint
	OutMint         chain.Mint
	InAmount        uint64
	QuoteOutAmount  uint64
	SlippageBps     int64
	SourceBatchSize int
}

type Hints interface {
	PublishHint(ctx context.Context, key string, payload []byte)
}

type SwapLayerDeps struct {
	UoW    *db.UnitOfWork
	Reads  sqlc.DBTX
	Clock  clock.Clock
	IDs    ids.Generator
	Venue  Venue
	Signer Signer
	Hints  Hints
}

type SwapLayer struct {
	d SwapLayerDeps
}

func NewSwapLayer(d SwapLayerDeps) *SwapLayer { return &SwapLayer{d: d} }

func (l *SwapLayer) Run(ctx context.Context, req SwapRequest, heartbeat func()) (SwapView, error) {
	insert, err := req.insertParams(l.d.IDs.NewV7(), l.d.Clock.Now())
	if err != nil {
		return SwapView{}, err
	}
	id, owned, err := l.claim(ctx, req, insert)
	if err == nil && owned {
		faultpoint.Hit(ctx, faultpoint.AfterCreate)
		err = l.beating(ctx, heartbeat, func(ctx context.Context) error { return l.drive(ctx, req, id) })
	}
	if err != nil {
		return SwapView{}, err
	}
	return NewQueries(l.d.Reads).Swap(ctx, ids.SwapIDFrom(id))
}

func (l *SwapLayer) claim(
	ctx context.Context, req SwapRequest, insert sqlc.InsertCreatedParams,
) (uuid.UUID, bool, error) {
	insertErr := l.d.UoW.Do(ctx, func(ctx context.Context, tx db.Tx) error {
		if err := sqlc.New(tx.Queries()).InsertCreated(ctx, insert); err != nil {
			return err
		}
		tx.AfterCommit(func(ctx context.Context) { l.hint(ctx, req.CabalID, insert.ID) })
		return nil
	})
	if insertErr == nil {
		return insert.ID, true, nil
	}
	live, err := sqlc.New(l.d.Reads).ClaimLive(ctx, sqlc.ClaimLiveParams{
		SourceKind: insert.SourceKind, SourceID: insert.SourceID, InMint: insert.InMint,
	})
	if err != nil {
		return uuid.Nil, false, insertErr
	}
	return live.ID, false, nil
}

func (l *SwapLayer) drive(ctx context.Context, req SwapRequest, id uuid.UUID) error {
	order, err := l.d.Venue.Order(ctx, OrderSpec{
		Taker: req.TreasuryWallet.Address, InMint: req.InMint, OutMint: req.OutMint,
		InAmount: req.InAmount, SlippageBps: req.SlippageBps,
	})
	if err != nil {
		return l.orderFailed(ctx, req, id, err)
	}
	signed, signature, err := l.d.Signer.Sign(ctx, req.TreasuryWallet.PrivyWalletID, order.Transaction)
	if err != nil {
		return err
	}
	moved, err := l.move(ctx, req, id, submitted(req, id, order.RequestID, signed, signature))
	if err != nil || !moved {
		return err
	}
	faultpoint.Hit(ctx, faultpoint.AfterSign)
	result, err := l.d.Venue.ExecuteUntilTerminal(ctx, order.RequestID, signed)
	faultpoint.Hit(ctx, faultpoint.AfterExecute)
	return l.settle(ctx, req, id, signature, result, err)
}

func (l *SwapLayer) settle(
	ctx context.Context, req SwapRequest, id uuid.UUID, signature chain.Signature, result ExecuteResult, cause error,
) error {
	if cause != nil {
		return unfinished(cause)
	}
	switch result.Status {
	case ExecuteSuccess:
		return l.confirm(ctx, req, id, signature, result)
	case ExecuteFailed:
		_, err := l.move(ctx, req, id, failed(req, id, domain.FailureJupiterFailed, strconv.Itoa(result.ErrorCode)))
		return err
	case ExecutePending:
	}
	return nil
}

func unfinished(cause error) error {
	if code := errs.CodeOf(cause); code == errs.CodeUpstreamTimeout || code == errs.CodeJupiterUnavailable {
		return nil
	}
	return cause
}

func (l *SwapLayer) confirm(
	ctx context.Context, req SwapRequest, id uuid.UUID, signature chain.Signature, result ExecuteResult,
) error {
	out, ok := domain.Column(result.OutAmount)
	if !ok {
		return errs.New(errs.CodeDecodeFailed, "trading.SwapLayer.confirm", slog.Uint64("out_amount", result.OutAmount))
	}
	_, err := l.move(ctx, req, id, confirmed(req, id, signature, result.OutAmount, out))
	return err
}

func (l *SwapLayer) orderFailed(ctx context.Context, req SwapRequest, id uuid.UUID, cause error) error {
	if errs.CodeOf(cause) != errs.CodeJupiterRejected {
		return cause
	}
	_, err := l.move(ctx, req, id, failed(req, id, domain.FailureNeverSubmitted, ""))
	return err
}

type step struct {
	status  domain.Status
	failure domain.FailureCode
	apply   func(ctx context.Context, q *sqlc.Queries, at time.Time) (int64, error)
	event   func(at time.Time) events.Event
}

func (l *SwapLayer) move(ctx context.Context, req SwapRequest, id uuid.UUID, s step) (bool, error) {
	moved := false
	err := l.d.UoW.Do(ctx, func(ctx context.Context, tx db.Tx) error {
		moved = false
		at := l.d.Clock.Now()
		n, err := s.apply(ctx, sqlc.New(tx.Queries()), at)
		if err != nil || n == 0 {
			return err
		}
		if err := tx.Events.Append(ctx, s.event(at)); err != nil {
			return err
		}
		moved = true
		tx.AfterCommit(func(ctx context.Context) { l.committed(ctx, req, id, s) })
		return nil
	})
	return moved && err == nil, err
}

func (l *SwapLayer) committed(ctx context.Context, req SwapRequest, id uuid.UUID, s step) {
	l.hint(ctx, req.CabalID, id)
	if s.status != domain.StatusConfirmed && s.status != domain.StatusFailed {
		return
	}
	observability.Info(ctx, observability.TradingSwapFinished,
		slog.String("swap_id", id.String()),
		slog.String("source", string(req.Source.Kind)+":"+req.Source.ID.String()),
		slog.String("status", string(s.status)),
		slog.String("failure_code", string(s.failure)))
}

func (l *SwapLayer) hint(ctx context.Context, cabal ids.CabalID, id uuid.UUID) {
	l.d.Hints.PublishHint(ctx, "cabal."+cabal.UUID().String()+".swap_updated", fmt.Appendf(nil, `{"swap_id":%q}`, id))
}

func (l *SwapLayer) beating(ctx context.Context, beat func(), work func(ctx context.Context) error) error {
	if beat == nil {
		return work(ctx)
	}
	ctx, stop := context.WithCancel(ctx)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		ticker := l.d.Clock.NewTicker(heartbeatEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C():
				beat()
			case <-gctx.Done():
				return nil
			}
		}
	})
	defer func() {
		stop()
		_ = g.Wait()
	}()
	return work(ctx)
}

func (r SwapRequest) insertParams(id uuid.UUID, now time.Time) (sqlc.InsertCreatedParams, error) {
	in, inOK := domain.Column(r.InAmount)
	quote, quoteOK := domain.Column(r.QuoteOutAmount)
	if !inOK || !quoteOK || r.SourceBatchSize < 1 || r.SourceBatchSize > math.MaxInt32 ||
		r.SlippageBps < 0 || r.SlippageBps > math.MaxInt32 {
		return sqlc.InsertCreatedParams{}, errs.New(errs.CodeInvalidInput, "trading.SwapLayer.Run",
			slog.Uint64("in_amount", r.InAmount), slog.Uint64("quote_out_amount", r.QuoteOutAmount),
			slog.Int64("slippage_bps", r.SlippageBps), slog.Int("source_batch_size", r.SourceBatchSize))
	}
	p := sqlc.InsertCreatedParams{
		ID: id, SourceKind: string(r.Source.Kind), SourceID: r.Source.ID, CabalID: r.CabalID.UUID(),
		TreasuryAddress: string(r.TreasuryWallet.Address), Action: string(r.Action), Symbol: r.Symbol,
		InMint: string(r.InMint.Address), OutMint: string(r.OutMint.Address), OutDecimals: int16(r.OutMint.Decimals),
		InAmount: in, SlippageBps: int32(r.SlippageBps), SourceBatchSize: int32(r.SourceBatchSize), CreatedAt: now,
	}
	p.QuoteOutAmount.Int64, p.QuoteOutAmount.Valid = quote, true
	return p, nil
}

func (r SwapRequest) tradeSource() events.TradeSource {
	return events.TradeSource{Kind: string(r.Source.Kind), ID: r.Source.ID}
}

func submitted(req SwapRequest, id uuid.UUID, requestID string, signed []byte, signature chain.Signature) step {
	return step{
		status: domain.StatusSubmitted,
		apply: func(ctx context.Context, q *sqlc.Queries, at time.Time) (int64, error) {
			return q.MarkSubmitted(ctx, sqlc.MarkSubmittedParams{
				ID: id, ExecuteRequestID: requestID, SignedTx: signed, TxSignature: string(signature), SubmittedAt: at,
			})
		},
		event: func(time.Time) events.Event {
			return events.TradeSubmitted{
				V: 1, SwapID: id, CabalID: req.CabalID.UUID(), Source: req.tradeSource(),
				SourceBatchSize: req.SourceBatchSize, Action: string(req.Action), Symbol: req.Symbol,
				InMint: req.InMint.Address, OutMint: req.OutMint.Address, InAmount: req.InAmount,
				TxSignature: signature,
			}
		},
	}
}

func confirmed(req SwapRequest, id uuid.UUID, signature chain.Signature, outAmount uint64, column int64) step {
	usdc := req.InAmount
	if req.Action == domain.ActionSell {
		usdc = outAmount
	}
	return step{
		status: domain.StatusConfirmed,
		apply: func(ctx context.Context, q *sqlc.Queries, at time.Time) (int64, error) {
			return q.FinishConfirmed(ctx, sqlc.FinishConfirmedParams{ID: id, OutAmount: column, ConfirmedAt: at})
		},
		event: func(at time.Time) events.Event {
			return events.TradeConfirmed{
				V: 1, SwapID: id, CabalID: req.CabalID.UUID(), Source: req.tradeSource(),
				SourceBatchSize: req.SourceBatchSize, Action: string(req.Action), Symbol: req.Symbol,
				InMint: req.InMint.Address, InAmount: req.InAmount, OutMint: req.OutMint.Address, OutAmount: outAmount,
				USDCMicros: money.MicrosFromUint64(usdc), FeeMicros: money.MicrosFromUint64(0),
				TxSignature: signature, ConfirmedAt: at,
			}
		},
	}
}

func failed(req SwapRequest, id uuid.UUID, code domain.FailureCode, jupiterCode string) step {
	return step{
		status: domain.StatusFailed, failure: code,
		apply: func(ctx context.Context, q *sqlc.Queries, at time.Time) (int64, error) {
			return q.FinishFailed(ctx, sqlc.FinishFailedParams{ID: id, FailureCode: string(code), FailedAt: at})
		},
		event: func(time.Time) events.Event {
			return events.TradeFailed{
				V: 1, SwapID: id, CabalID: req.CabalID.UUID(), Source: req.tradeSource(),
				SourceBatchSize: req.SourceBatchSize, Action: string(req.Action), Symbol: req.Symbol,
				InMint: req.InMint.Address, InAmount: req.InAmount, FailureCode: string(code), JupiterCode: jupiterCode,
			}
		},
	}
}

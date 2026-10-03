package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/monaco/monaco/apps/backend/internal/errs"
	"github.com/monaco/monaco/apps/backend/internal/events"
	"github.com/monaco/monaco/apps/backend/internal/modules/funding/sqlc"
	"github.com/monaco/monaco/apps/backend/internal/platform/chain"
	"github.com/monaco/monaco/apps/backend/internal/platform/db"
	"github.com/monaco/monaco/apps/backend/internal/platform/ids"
	"github.com/monaco/monaco/apps/backend/internal/platform/money"
	"github.com/monaco/monaco/apps/backend/internal/platform/observability"
)

type CreditDeposit struct {
	ID              uuid.UUID
	UserID          ids.UserID
	WalletAddress   chain.SolanaAddress
	TxSignature     chain.Signature
	Amount          money.Micros
	Slot            int64
	BlockTime       time.Time
	CreditedAt      time.Time
	CursorSignature chain.Signature
	DeferCursor     bool
}

type CreditDepositHandler struct {
	uow   *db.UnitOfWork
	hints HintPublisher
}

type HintPublisher interface {
	PublishHint(context.Context, string, []byte)
}

func NewCreditDepositHandler(uow *db.UnitOfWork, hints HintPublisher) *CreditDepositHandler {
	return &CreditDepositHandler{uow: uow, hints: hints}
}

func (h *CreditDepositHandler) Handle(ctx context.Context, cmd CreditDeposit) (bool, error) {
	if cmd.Amount.IsZero() {
		return false, errs.New(errs.CodeInvalidInput, "funding.CreditDeposit.Handle")
	}
	credited := false
	err := h.uow.Do(ctx, func(ctx context.Context, tx db.Tx) error {
		q := sqlc.New(tx.Queries())
		inserted, err := q.InsertDeposit(ctx, sqlc.InsertDepositParams{
			ID: cmd.ID, UserID: cmd.UserID.UUID(), WalletAddress: string(cmd.WalletAddress),
			TxSignature: string(cmd.TxSignature), AmountMicros: cmd.Amount.String(), Slot: cmd.Slot,
			BlockTime: cmd.BlockTime, CreditedAt: cmd.CreditedAt,
		})
		if err != nil {
			return err
		}
		if inserted == 0 {
			observability.Debug(ctx, observability.FundingDepositDuplicate,
				slog.String("wallet_address", string(cmd.WalletAddress)))
			return nil
		}
		credited = true
		event := events.DepositCredited{
			V:             1,
			DepositID:     cmd.ID,
			UserID:        cmd.UserID.UUID(),
			WalletAddress: cmd.WalletAddress,
			AmountMicros:  cmd.Amount,
			TxSignature:   cmd.TxSignature,
			Slot:          cmd.Slot,
			BlockTime:     optionalTime(cmd.BlockTime),
		}
		if err := tx.Events.Append(ctx, event); err != nil {
			return err
		}
		if err := advanceCursor(ctx, q, cmd); err != nil {
			return err
		}
		tx.AfterCommit(func(ctx context.Context) {
			h.hints.PublishHint(ctx, events.UserBalanceChangedHint(cmd.UserID), nil)
			observability.Info(
				ctx,
				observability.FundingDepositCredited,
				slog.String("wallet_address", string(cmd.WalletAddress)),
				slog.String("amount_micros", cmd.Amount.String()),
			)
		})
		return nil
	})
	return credited, err
}

func advanceCursor(ctx context.Context, q *sqlc.Queries, cmd CreditDeposit) error {
	if cmd.DeferCursor {
		return nil
	}
	return q.AdvanceDepositCursor(ctx, sqlc.AdvanceDepositCursorParams{
		WalletAddress: string(cmd.WalletAddress), LastSignature: string(cmd.CursorSignature), CursorSlot: cmd.Slot,
		ScannedAt: cmd.CreditedAt,
	})
}

func optionalTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}

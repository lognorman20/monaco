package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"golang.org/x/time/rate"

	"github.com/monaco/monaco/apps/backend/internal/errs"
	"github.com/monaco/monaco/apps/backend/internal/modules/funding/sqlc"
	"github.com/monaco/monaco/apps/backend/internal/modules/identity/port"
	"github.com/monaco/monaco/apps/backend/internal/platform/chain"
	"github.com/monaco/monaco/apps/backend/internal/platform/chain/solana"
	"github.com/monaco/monaco/apps/backend/internal/platform/clock"
	"github.com/monaco/monaco/apps/backend/internal/platform/concurrency"
	"github.com/monaco/monaco/apps/backend/internal/platform/db"
	"github.com/monaco/monaco/apps/backend/internal/platform/ids"
	"github.com/monaco/monaco/apps/backend/internal/platform/money"
	"github.com/monaco/monaco/apps/backend/internal/platform/poller"
)

const (
	DepositPollInterval        = 30 * time.Second
	depositSignaturePageSize   = 1000
	bootstrapSignaturePageSize = 1
)

var errRateBudgetSpent = errs.New(errs.CodeInternal, "funding.DepositPoller.rateBudgetSpent")

type DepositRPC interface {
	SignaturesFor(
		context.Context, chain.SolanaAddress, chain.Signature, chain.Signature, int,
	) ([]solana.SignatureInfo, error)
	InboundTransfersForMint(
		context.Context, chain.Signature, chain.SolanaAddress, chain.SolanaAddress,
	) ([]solana.Transfer, error)
}

type RPCLimiter interface {
	Wait(context.Context) error
}

func NewRPCLimiter(perSecond int32) *rate.Limiter {
	if perSecond <= 0 {
		perSecond = 20
	}
	return rate.NewLimiter(rate.Limit(perSecond), int(perSecond))
}

type DepositPoller struct {
	reads   sqlc.DBTX
	uow     *db.UnitOfWork
	ids     ids.Generator
	clock   clock.Clock
	wallets port.WalletReader
	rpc     DepositRPC
	usdc    chain.SolanaAddress
	period  time.Duration
	hints   HintPublisher
	limit   RPCLimiter
}

type memberWallet struct {
	wallet port.MemberWallet
	cursor sqlc.DepositCursorsForWalletsRow
}

type backfillCursor struct {
	before    chain.Signature
	until     chain.Signature
	head      chain.Signature
	slot      uint64
	completed bool
}

func NewDepositPoller(
	reads sqlc.DBTX, uow *db.UnitOfWork, g ids.Generator, c clock.Clock, wallets port.WalletReader,
	rpc DepositRPC, usdc chain.SolanaAddress, period time.Duration, limit RPCLimiter, hints HintPublisher,
) *DepositPoller {
	return &DepositPoller{
		reads: reads, uow: uow, ids: g, clock: c, wallets: wallets, rpc: rpc, usdc: usdc,
		period: period, hints: hints, limit: limit,
	}
}

func (*DepositPoller) Name() string { return "funding.deposits" }

func (p *DepositPoller) Interval() time.Duration { return p.period }

func (p *DepositPoller) Tick(ctx context.Context) (poller.Report, error) {
	var report poller.Report
	wallets, err := p.memberWallets(ctx)
	if err != nil {
		return report, err
	}
	changed, deferred, tickErr := p.scanWallets(ctx, wallets)
	report.Scanned = len(wallets) - deferred
	report.Changed = changed
	report.Attrs = []slog.Attr{slog.Int("deferred", deferred)}
	return report, tickErr
}

func (p *DepositPoller) scanWallets(ctx context.Context, wallets []memberWallet) (int, int, error) {
	out, errc := concurrency.Pool(ctx, 16, concurrency.Feed(ctx, wallets), p.scanMember)
	var changed, deferred int
	var scanErr error
	for out != nil || errc != nil {
		select {
		case delta, ok := <-out:
			if !ok {
				out = nil
				continue
			}
			changed += delta
		case err, ok := <-errc:
			if !ok {
				errc = nil
				continue
			}
			if errors.Is(err, errRateBudgetSpent) {
				deferred++
				continue
			}
			scanErr = errors.Join(scanErr, err)
		}
	}
	if err := context.Cause(ctx); err != nil {
		return changed, deferred, errs.Wrap(err, errs.CodeInternal, "funding.DepositPoller.scanWallets")
	}
	return changed, deferred, scanErr
}

func (p *DepositPoller) memberWallets(ctx context.Context) ([]memberWallet, error) {
	var wallets []port.MemberWallet
	var after ids.UserID
	for {
		page, err := p.wallets.MemberWallets(ctx, after, port.MaxWalletPage)
		if err != nil {
			return nil, errs.Wrap(err, errs.CodeInternal, "funding.DepositPoller.memberWallets")
		}
		wallets = append(wallets, page...)
		if len(page) < port.MaxWalletPage {
			addresses := make([]string, len(wallets))
			byAddress := make(map[string]port.MemberWallet, len(wallets))
			for i, wallet := range wallets {
				addresses[i] = string(wallet.Address)
				byAddress[addresses[i]] = wallet
			}
			cursors, err := sqlc.New(p.reads).DepositCursorsForWallets(ctx, addresses)
			if err != nil {
				return nil, errs.Wrap(err, errs.CodeInternal, "funding.DepositPoller.memberWallets")
			}
			ordered := make([]memberWallet, 0, len(cursors))
			for _, cursor := range cursors {
				ordered = append(ordered, memberWallet{wallet: byAddress[cursor.WalletAddress], cursor: cursor})
			}
			return ordered, nil
		}
		after = page[len(page)-1].UserID
	}
}

func (p *DepositPoller) scanMember(ctx context.Context, member memberWallet) (int, error) {
	ata, err := chain.AssociatedTokenAccount(member.wallet.Address, p.usdc, chain.SPLProgram)
	if err != nil {
		return 0, errs.Wrap(err, errs.CodeInvalidAddress, "funding.DepositPoller.scan")
	}
	if !member.cursor.Exists {
		return 0, p.bootstrap(ctx, ata, member.wallet.Address)
	}
	backfill, err := newBackfillCursor(member.cursor)
	if err != nil {
		return 0, err
	}
	if backfill.completed {
		return 0, p.finishBackfill(ctx, member.wallet.Address, backfill)
	}
	return p.walkBackfill(ctx, ata, member.wallet, backfill)
}

func newBackfillCursor(cursor sqlc.DepositCursorsForWalletsRow) (backfillCursor, error) {
	backfill := backfillCursor{
		before: chain.Signature(cursor.BackfillBeforeSignature),
		until:  chain.Signature(cursor.LastSignature),
		head:   chain.Signature(cursor.BackfillHeadSignature),
	}
	if backfill.before == "" {
		return backfill, nil
	}
	if backfill.head == "" || cursor.BackfillHeadSlot < 0 {
		return backfillCursor{}, errs.New(errs.CodeInternal, "funding.DepositPoller.backfill")
	}
	backfill.slot = uint64(cursor.BackfillHeadSlot)
	backfill.completed = backfill.until == backfill.head
	return backfill, nil
}

func (p *DepositPoller) walkBackfill(
	ctx context.Context, ata chain.SolanaAddress, wallet port.MemberWallet, backfill backfillCursor,
) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, errs.Wrap(err, errs.CodeInternal, "funding.DepositPoller.backfill")
	}
	page, err := p.signaturesPage(ctx, ata, backfill.before, backfill.until, depositSignaturePageSize)
	if err != nil {
		return 0, err
	}
	if len(page) == 0 {
		return 0, p.finishBackfill(ctx, wallet.Address, backfill)
	}
	return p.processBackfillPage(ctx, wallet, page, &backfill)
}

func (p *DepositPoller) processBackfillPage(
	ctx context.Context, wallet port.MemberWallet, page []solana.SignatureInfo, backfill *backfillCursor,
) (int, error) {
	changed := 0
	if backfill.head == "" {
		backfill.head, backfill.slot = page[0].Signature, page[0].Slot
	}
	for _, sig := range page {
		added, err := p.scanSignature(ctx, wallet, sig)
		if err != nil {
			return changed, err
		}
		changed += added
		if err := p.checkpointBackfill(ctx, wallet.Address, sig.Signature, backfill); err != nil {
			return changed, err
		}
	}
	if len(page) == depositSignaturePageSize {
		return changed, nil
	}
	return changed, p.finishBackfill(ctx, wallet.Address, *backfill)
}

func (p *DepositPoller) checkpointBackfill(
	ctx context.Context, address chain.SolanaAddress, before chain.Signature, backfill *backfillCursor,
) error {
	backfill.before = before
	return p.setBackfill(ctx, address, backfill.before, backfill.head, backfill.slot)
}

func (p *DepositPoller) finishBackfill(
	ctx context.Context, address chain.SolanaAddress, backfill backfillCursor,
) error {
	if backfill.head == "" {
		return p.touch(ctx, address)
	}
	if backfill.slot > math.MaxInt64 {
		return errs.New(errs.CodeInternal, "funding.DepositPoller.finishBackfill")
	}
	slot := int64(backfill.slot)
	err := p.uow.Do(ctx, func(ctx context.Context, tx db.Tx) error {
		q := sqlc.New(tx.Queries())
		if err := q.AdvanceDepositCursor(ctx, sqlc.AdvanceDepositCursorParams{
			WalletAddress: string(address), LastSignature: string(backfill.head), CursorSlot: slot,
			ScannedAt: p.clock.Now(),
		}); err != nil {
			return err
		}
		return q.SetDepositBackfill(ctx, sqlc.SetDepositBackfillParams{
			WalletAddress: string(address), BeforeSignature: "", HeadSignature: "", HeadSlot: 0,
		})
	})
	if err != nil {
		return errs.Wrap(err, errs.CodeOf(err), "funding.DepositPoller.finishBackfill")
	}
	return nil
}

func (p *DepositPoller) setBackfill(
	ctx context.Context, address chain.SolanaAddress, before, head chain.Signature, slot uint64,
) error {
	if slot > math.MaxInt64 {
		return errs.New(errs.CodeInternal, "funding.DepositPoller.setBackfill")
	}
	slotInt := int64(slot)
	return p.uow.Do(ctx, func(ctx context.Context, tx db.Tx) error {
		q := sqlc.New(tx.Queries())
		if err := q.SetDepositBackfill(ctx, sqlc.SetDepositBackfillParams{
			WalletAddress:   string(address),
			BeforeSignature: string(before),
			HeadSignature:   string(head),
			HeadSlot:        slotInt,
		}); err != nil {
			return err
		}
		return q.TouchDepositCursor(ctx, sqlc.TouchDepositCursorParams{
			WalletAddress: string(address), ScannedAt: p.clock.Now(),
		})
	})
}

func (p *DepositPoller) bootstrap(ctx context.Context, ata, address chain.SolanaAddress) error {
	page, err := p.signaturesPage(ctx, ata, "", "", bootstrapSignaturePageSize)
	if err != nil {
		return err
	}
	if len(page) == 0 {
		return p.touch(ctx, address)
	}
	return p.advance(ctx, address, page[0].Signature, page[0].Slot)
}

func (p *DepositPoller) touch(ctx context.Context, address chain.SolanaAddress) error {
	return p.uow.Do(ctx, func(ctx context.Context, tx db.Tx) error {
		return sqlc.New(tx.Queries()).TouchDepositCursor(ctx, sqlc.TouchDepositCursorParams{
			WalletAddress: string(address), ScannedAt: p.clock.Now(),
		})
	})
}

func (p *DepositPoller) signaturesPage(
	ctx context.Context, address chain.SolanaAddress, before, until chain.Signature, limit int,
) ([]solana.SignatureInfo, error) {
	if err := p.waitRPC(ctx); err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "funding.DepositPoller.signatures")
	}
	page, err := p.rpc.SignaturesFor(ctx, address, before, until, limit)
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeRPCUnavailable, "funding.DepositPoller.signatures")
	}
	return page, nil
}

func (p *DepositPoller) waitRPC(ctx context.Context) error {
	err := p.limit.Wait(ctx)
	if err == nil {
		return nil
	}
	if ctx.Err() == nil {
		return errRateBudgetSpent
	}
	return errs.Wrap(err, errs.CodeInternal, "funding.DepositPoller.waitRPC")
}

func (p *DepositPoller) scanSignature(
	ctx context.Context, wallet port.MemberWallet, sig solana.SignatureInfo,
) (int, error) {
	if sig.Failed {
		return 0, nil
	}
	if err := p.waitRPC(ctx); err != nil {
		return 0, fmt.Errorf("funding.DepositPoller.scanSignature: %w", err)
	}
	transfers, err := p.rpc.InboundTransfersForMint(ctx, sig.Signature, wallet.Address, p.usdc)
	if err != nil {
		return 0, err
	}
	amount, err := p.amountFor(transfers)
	if err != nil {
		return 0, err
	}
	if amount.IsZero() {
		return 0, nil
	}
	credited, err := p.credit(ctx, wallet, sig, amount)
	if err != nil {
		return 0, err
	}
	if credited {
		return 1, nil
	}
	return 0, nil
}

func (p *DepositPoller) amountFor(transfers []solana.Transfer) (money.Micros, error) {
	amount := money.Micros{}
	for _, transfer := range transfers {
		if transfer.Mint.Address != p.usdc || transfer.Net.Decimals() != 6 || transfer.Net.IsZero() {
			continue
		}
		micros := money.MicrosFromUint64(transfer.Net.Uint64())
		next, err := amount.Add(micros)
		if err != nil {
			return money.Micros{}, err
		}
		amount = next
	}
	return amount, nil
}

func (p *DepositPoller) credit(
	ctx context.Context,
	wallet port.MemberWallet,
	sig solana.SignatureInfo,
	amount money.Micros,
) (bool, error) {
	if sig.Slot > math.MaxInt64 {
		return false, errs.New(errs.CodeInternal, "funding.DepositPoller.credit")
	}
	credited, err := NewCreditDepositHandler(p.uow, p.hints).Handle(ctx, CreditDeposit{
		ID:              p.ids.NewV7(),
		UserID:          wallet.UserID,
		WalletAddress:   wallet.Address,
		TxSignature:     sig.Signature,
		Amount:          amount,
		Slot:            int64(sig.Slot),
		BlockTime:       sig.BlockTime,
		CreditedAt:      p.clock.Now(),
		CursorSignature: sig.Signature,
		DeferCursor:     true,
	})
	if err != nil {
		return false, errs.Wrap(err, errs.CodeOf(err), "funding.DepositPoller.credit")
	}
	return credited, nil
}

func (p *DepositPoller) advance(
	ctx context.Context,
	address chain.SolanaAddress,
	sig chain.Signature,
	slot uint64,
) error {
	if slot > math.MaxInt64 {
		return errs.New(errs.CodeInternal, "funding.DepositPoller.advance")
	}
	slotInt := int64(slot)
	err := p.uow.Do(ctx, func(ctx context.Context, tx db.Tx) error {
		return sqlc.New(tx.Queries()).AdvanceDepositCursor(ctx, sqlc.AdvanceDepositCursorParams{
			WalletAddress: string(address), LastSignature: string(sig), CursorSlot: slotInt,
			ScannedAt: p.clock.Now(),
		})
	})
	if err != nil {
		return errs.Wrap(err, errs.CodeOf(err), "funding.DepositPoller.advance")
	}
	return nil
}

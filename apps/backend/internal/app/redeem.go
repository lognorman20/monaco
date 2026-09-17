package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/packages/domain"
)

// RedeemRequest starts or resumes a debit-first redeem job.
type RedeemRequest struct {
	AccessToken        string
	GroupID            string
	ShareAmountMicros  *int64
	DollarTargetMicros *int64
	PayoutProof        privy.PayoutProof
	ResumeJobID        string
}

// RedeemJobView is the persisted redeem job returned to callers.
type RedeemJobView struct {
	ID            string
	GroupID       string
	UserID        string
	ShareUnits    int64
	SliceUsdc     int64
	PayoutAddress string
	Status        domain.RedeemJobStatus
	WithdrawalID  string
	Position      Position
}

// ErrInvalidRedeemRequest means share/dollar inputs are invalid or below dust.
var ErrInvalidRedeemRequest = errors.New("invalid redeem request")

// ErrInvalidPayoutProof means payout ownership proof failed verification.
var ErrInvalidPayoutProof = errors.New("invalid payout proof")

// ErrRedeemAlreadyInProgress means another redeem job is active for this member.
var ErrRedeemAlreadyInProgress = errors.New("redeem already in progress")

// RedeemService orchestrates debit-first redeem with resume support.
type RedeemService struct {
	store   *postgres.Store
	privy   privy.Client
	pyth    pyth.Client
	jupiter jupiter.Client
	swap    *SwapService
	signer  TreasurySigner
}

// NewRedeemService wires redeem dependencies.
func NewRedeemService(
	store *postgres.Store,
	privyClient privy.Client,
	pythClient pyth.Client,
	jupiterClient jupiter.Client,
	swap *SwapService,
	signer TreasurySigner,
) *RedeemService {
	return &RedeemService{
		store:   store,
		privy:   privyClient,
		pyth:    pythClient,
		jupiter: jupiterClient,
		swap:    swap,
		signer:  signer,
	}
}

// Redeem verifies payout proof, debits share units first, sells slice if needed, and pays USDC.
func (r *RedeemService) Redeem(ctx context.Context, req RedeemRequest) (RedeemJobView, error) {
	if req.ResumeJobID != "" {
		return r.resumeRedeemJob(ctx, req.ResumeJobID, req.PayoutProof)
	}

	if req.GroupID == "" {
		return RedeemJobView{}, fmt.Errorf("group id is required")
	}

	identity, err := r.privy.VerifySession(ctx, privy.AccessToken(req.AccessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			return RedeemJobView{}, privy.ErrInvalidToken
		}
		return RedeemJobView{}, fmt.Errorf("verify session: %w", err)
	}

	user, found, err := r.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		return RedeemJobView{}, err
	}
	if !found {
		return RedeemJobView{}, ErrUserNotFound
	}

	if err := r.privy.VerifyPayoutProof(ctx, user.ID, req.PayoutProof); err != nil {
		if errors.Is(err, privy.ErrInvalidPayoutProof) {
			return RedeemJobView{}, ErrInvalidPayoutProof
		}
		return RedeemJobView{}, err
	}

	release, acquired, err := r.store.TryAcquireMemberRedeemLock(ctx, user.ID, req.GroupID)
	if err != nil {
		return RedeemJobView{}, err
	}
	if !acquired {
		return RedeemJobView{}, ErrRedeemAlreadyInProgress
	}
	defer release()

	treasury, found, err := r.store.GetTreasuryByGroupID(ctx, req.GroupID)
	if err != nil {
		return RedeemJobView{}, err
	}
	if !found {
		return RedeemJobView{}, ErrGroupNotFound
	}

	shareUnitsToDebit, potNav, totalSharesMicro, err := r.resolveRedeemShares(ctx, req, treasury.SolanaAddress)
	if err != nil {
		return RedeemJobView{}, err
	}

	slice, err := domain.ComputeRedeemSlice(domain.RedeemSliceInput{
		SharesRedeemedMicros: shareUnitsToDebit,
		TotalSharesMicros:    totalSharesMicro,
		PotNav:               domain.USDCMicros(potNav),
	})
	if err != nil {
		return RedeemJobView{}, err
	}
	if slice.UsdcOwed < domain.RedeemDustMinimumMicros {
		return RedeemJobView{}, fmt.Errorf("%w: below dust minimum", ErrInvalidRedeemRequest)
	}

	tx, err := r.store.BeginTx(ctx)
	if err != nil {
		return RedeemJobView{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	position, err := r.store.DebitPositionShareUnitsTx(ctx, tx, user.ID, req.GroupID, shareUnitsToDebit)
	if err != nil {
		return RedeemJobView{}, err
	}

	job, err := r.store.InsertRedeemJobTx(ctx, tx, user.ID, req.GroupID, shareUnitsToDebit, int64(slice.UsdcOwed), req.PayoutProof.PayoutAddress)
	if err != nil {
		if errors.Is(err, postgres.ErrActiveRedeemJobExists) {
			return RedeemJobView{}, ErrRedeemAlreadyInProgress
		}
		return RedeemJobView{}, err
	}

	if err := tx.Commit(); err != nil {
		return RedeemJobView{}, fmt.Errorf("commit redeem debit: %w", err)
	}
	committed = true

	view := redeemJobFromRow(job, positionFromRowPostgres(position))
	return r.continueRedeemJob(ctx, view, req.PayoutProof)
}

func (r *RedeemService) resumeRedeemJob(ctx context.Context, jobID string, proof privy.PayoutProof) (RedeemJobView, error) {
	job, found, err := r.store.GetRedeemJobByID(ctx, jobID)
	if err != nil {
		return RedeemJobView{}, err
	}
	if !found {
		return RedeemJobView{}, fmt.Errorf("redeem job not found")
	}

	release, acquired, err := r.store.TryAcquireMemberRedeemLock(ctx, job.UserID, job.GroupID)
	if err != nil {
		return RedeemJobView{}, err
	}
	if !acquired {
		return RedeemJobView{}, ErrRedeemAlreadyInProgress
	}
	defer release()

	position, hasPosition, err := r.store.GetPosition(ctx, job.UserID, job.GroupID)
	if err != nil {
		return RedeemJobView{}, err
	}
	if !hasPosition {
		position = postgres.PositionRow{UserID: job.UserID, GroupID: job.GroupID}
	}

	if proof.PayoutAddress == "" {
		proof.PayoutAddress = job.PayoutAddress
	}

	view := redeemJobFromRow(job, positionFromRowPostgres(position))
	return r.continueRedeemJob(ctx, view, proof)
}

func (r *RedeemService) continueRedeemJob(ctx context.Context, view RedeemJobView, proof privy.PayoutProof) (RedeemJobView, error) {
	if view.Status == domain.RedeemJobSettled {
		return view, nil
	}

	if view.Status == domain.RedeemJobDebited {
		if err := r.sellRedeemSliceIfNeeded(ctx, &view); err != nil {
			return view, err
		}
	}

	if view.Status == domain.RedeemJobDebited || view.Status == domain.RedeemJobSelling {
		if err := r.store.UpdateRedeemJobStatus(ctx, view.ID, string(domain.RedeemJobPaying)); err != nil {
			return view, err
		}
		view.Status = domain.RedeemJobPaying
	}

	if view.Status == domain.RedeemJobPaying {
		return r.payRedeemSlice(ctx, view, proof)
	}

	return view, fmt.Errorf("unsupported redeem job status %q", view.Status)
}

func (r *RedeemService) sellRedeemSliceIfNeeded(ctx context.Context, view *RedeemJobView) error {
	holdings, err := r.store.ListNetTokenHoldingsByGroup(ctx, view.GroupID)
	if err != nil {
		return err
	}
	if len(holdings) == 0 {
		if err := r.store.UpdateRedeemJobStatus(ctx, view.ID, string(domain.RedeemJobPaying)); err != nil {
			return err
		}
		view.Status = domain.RedeemJobPaying
		return nil
	}

	totalSharesMicro, err := r.store.SumShareUnitsByGroup(ctx, view.GroupID)
	if err != nil {
		return err
	}
	totalSharesMicro += view.ShareUnits

	treasury, err := r.privy.EnsureTreasury(ctx, privy.GroupID(view.GroupID))
	if err != nil {
		return err
	}

	for _, holding := range holdings {
		sellAmount := jupiter.RedeemSliceSellAmount(holding.Amount, view.ShareUnits, totalSharesMicro)
		if sellAmount <= 0 {
			continue
		}
		symbol := symbolForOutputMint(holding.Mint)
		if _, err := r.swap.SellToUSDC(ctx, SellToUSDCRequest{
			GroupID:   view.GroupID,
			UserID:    view.UserID,
			Symbol:    symbol,
			InputMint: holding.Mint,
			Amount:    sellAmount,
		}); err != nil {
			return fmt.Errorf("sell redeem slice: %w", err)
		}
		_ = treasury
	}

	if err := r.store.UpdateRedeemJobStatus(ctx, view.ID, string(domain.RedeemJobSelling)); err != nil {
		return err
	}
	view.Status = domain.RedeemJobSelling
	return nil
}

func (r *RedeemService) payRedeemSlice(ctx context.Context, view RedeemJobView, proof privy.PayoutProof) (RedeemJobView, error) {
	treasury, err := r.privy.EnsureTreasury(ctx, privy.GroupID(view.GroupID))
	if err != nil {
		return view, err
	}

	payout, err := r.privy.PayUSDC(ctx, privy.PayUSDCRequest{
		TreasuryPrivyWalletID: treasury.PrivyWalletID,
		TreasuryAddress:       treasury.SolanaAddress,
		ToAddress:             proof.PayoutAddress,
		Amount:                view.SliceUsdc,
		GroupID:               view.GroupID,
		UserID:                view.UserID,
	})
	if err != nil {
		return view, err
	}

	treasuryUsdc, err := r.privy.TreasuryUSDCBalance(ctx, treasury.SolanaAddress)
	if err != nil {
		return view, fmt.Errorf("treasury usdc balance: %w", err)
	}

	tx, err := r.store.BeginTx(ctx)
	if err != nil {
		return view, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	withdrawal, err := r.store.InsertWithdrawalTx(ctx, tx, view.UserID, view.GroupID, view.SliceUsdc, proof.PayoutAddress)
	if err != nil {
		return view, err
	}

	if err := r.store.InsertPayoutProofTx(ctx, tx, view.UserID, view.GroupID, proof.PayoutAddress, proof.Message, proof.Signature, withdrawal.ID); err != nil {
		return view, err
	}

	confirmed, newlyPaid, err := r.store.ConfirmWithdrawalPayoutTx(ctx, tx, withdrawal.ID, payout.TxSignature, treasuryUsdc)
	if err != nil {
		return view, err
	}
	if !newlyPaid {
		return view, fmt.Errorf("withdrawal payout not applied")
	}
	_ = confirmed

	if err := r.store.AttachWithdrawalToRedeemJobTx(ctx, tx, view.ID, withdrawal.ID); err != nil {
		return view, err
	}

	position, _, err := r.store.GetPositionTx(ctx, tx, view.UserID, view.GroupID)
	if err != nil {
		return view, err
	}

	if err := tx.Commit(); err != nil {
		return view, fmt.Errorf("commit redeem payout: %w", err)
	}
	committed = true

	view.Status = domain.RedeemJobSettled
	view.WithdrawalID = withdrawal.ID
	view.Position = positionFromRowPostgres(position)
	return view, nil
}

func (r *RedeemService) resolveRedeemShares(ctx context.Context, req RedeemRequest, treasuryAddress string) (shareUnits int64, potNav int64, totalShares int64, err error) {
	totalShares, err = r.store.SumShareUnitsByGroup(ctx, req.GroupID)
	if err != nil {
		return 0, 0, 0, err
	}
	if totalShares <= 0 {
		return 0, 0, 0, fmt.Errorf("%w: no shares outstanding", ErrInvalidRedeemRequest)
	}

	treasuryUsdc, err := r.privy.TreasuryUSDCBalance(ctx, treasuryAddress)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("treasury usdc balance: %w", err)
	}

	navVals, err := r.store.ComputeNavSnapshotValues(ctx, req.GroupID, treasuryUsdc)
	if err != nil {
		return 0, 0, 0, err
	}
	potNav = navVals.PotNavMicros

	switch {
	case req.ShareAmountMicros != nil:
		shareUnits = *req.ShareAmountMicros
	case req.DollarTargetMicros != nil:
		shareUnits, err = domain.ShareUnitsMicrosForDollarTarget(domain.USDCMicros(*req.DollarTargetMicros), domain.USDCMicros(navVals.NavPerShareMicros))
		if err != nil {
			return 0, 0, 0, fmt.Errorf("%w: %v", ErrInvalidRedeemRequest, err)
		}
	default:
		return 0, 0, 0, fmt.Errorf("%w: share amount or dollar target required", ErrInvalidRedeemRequest)
	}

	if shareUnits <= 0 || shareUnits > totalShares {
		return 0, 0, 0, fmt.Errorf("%w: share amount out of range", ErrInvalidRedeemRequest)
	}
	return shareUnits, potNav, totalShares, nil
}

func redeemJobFromRow(row postgres.RedeemJobRow, position Position) RedeemJobView {
	status, _ := domain.ParseRedeemJobStatus(row.Status)
	view := RedeemJobView{
		ID:            row.ID,
		GroupID:       row.GroupID,
		UserID:        row.UserID,
		ShareUnits:    row.ShareUnits,
		SliceUsdc:     row.SliceUsdc,
		PayoutAddress: row.PayoutAddress,
		Status:        status,
		Position:      position,
	}
	if row.WithdrawalID.Valid {
		view.WithdrawalID = row.WithdrawalID.String
	}
	return view
}

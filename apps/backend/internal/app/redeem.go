package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/packages/domain"
)

// WithdrawToBalanceRequest withdraws deployed stake to the member Privy wallet (platform balance).
type WithdrawToBalanceRequest struct {
	AccessToken       string
	GroupID           string
	ShareAmountMicros *int64
	ResumeJobID       string
}

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

// ErrRedeemPotIlliquid means the pot could not raise enough USDC to cover the payout.
var ErrRedeemPotIlliquid = errors.New("redeem pot illiquid")

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

// WithdrawToBalance debits share units, sells slice if needed, and pays USDC to the member wallet.
func (r *RedeemService) WithdrawToBalance(ctx context.Context, req WithdrawToBalanceRequest) (RedeemJobView, error) {
	if req.ResumeJobID != "" {
		return r.resumeRedeemJob(ctx, req.ResumeJobID, privy.PayoutProof{})
	}

	if req.GroupID == "" {
		logRedeemBranchWarn("withdraw to balance rejected", "group id required")
		return RedeemJobView{}, fmt.Errorf("group id is required")
	}
	// Faker scale clubs (#153) have a dummy treasury: no sells, payouts, or Privy reads.
	if err := rejectFakerGroup(ctx, r.store, req.GroupID); err != nil {
		return RedeemJobView{}, err
	}


	identity, err := r.privy.VerifySession(ctx, privy.AccessToken(req.AccessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			logRedeemBranchWarn("withdraw to balance rejected", "invalid token", "group_id", req.GroupID)
			return RedeemJobView{}, privy.ErrInvalidToken
		}
		logRedeemBranchError("withdraw to balance verify session failed", err, "group_id", req.GroupID)
		return RedeemJobView{}, fmt.Errorf("verify session: %w", err)
	}

	user, found, err := r.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		logRedeemBranchError("withdraw to balance lookup user failed", err, "group_id", req.GroupID)
		return RedeemJobView{}, err
	}
	if !found {
		logRedeemBranchWarn("withdraw to balance rejected", "user not found", "group_id", req.GroupID)
		return RedeemJobView{}, ErrUserNotFound
	}

	member, err := r.store.IsGroupMember(ctx, req.GroupID, user.ID)
	if err != nil {
		return RedeemJobView{}, err
	}
	if !member {
		return RedeemJobView{}, ErrNotGroupMember
	}

	memberWallet, err := r.ensureMemberWalletAddress(ctx, identity.PrivyUserID, user.ID)
	if err != nil {
		return RedeemJobView{}, err
	}

	logRedeemStart(req.GroupID, user.ID, "")

	release, acquired, err := r.store.TryAcquireMemberRedeemLock(ctx, user.ID, req.GroupID)
	if err != nil {
		logRedeemBranchError("withdraw to balance acquire lock failed", err, "group_id", req.GroupID, "user_id", user.ID)
		return RedeemJobView{}, err
	}
	if !acquired {
		logRedeemLockContended(user.ID, req.GroupID)
		return RedeemJobView{}, ErrRedeemAlreadyInProgress
	}
	defer release()

	if resumed, handled, err := r.reconcileActiveRedeemBeforeWithdraw(ctx, user.ID, req.GroupID, memberWallet); handled {
		return resumed, err
	}

	treasury, found, err := r.store.GetTreasuryByGroupID(ctx, req.GroupID)
	if err != nil {
		return RedeemJobView{}, err
	}
	if !found {
		return RedeemJobView{}, ErrGroupNotFound
	}

	shareUnitsToDebit, potNav, totalSharesMicro, err := r.resolveWithdrawShares(ctx, req, user.ID, treasury.SolanaAddress)
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
		logRedeemBranchWarn("withdraw to balance rejected", "below dust minimum", "group_id", req.GroupID, "user_id", user.ID, "slice_usdc", int64(slice.UsdcOwed))
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

	job, err := r.store.InsertRedeemJobTx(ctx, tx, user.ID, req.GroupID, shareUnitsToDebit, int64(slice.UsdcOwed), memberWallet)
	if err != nil {
		if errors.Is(err, postgres.ErrActiveRedeemJobExists) {
			return RedeemJobView{}, ErrRedeemAlreadyInProgress
		}
		return RedeemJobView{}, err
	}

	if err := tx.Commit(); err != nil {
		logRedeemBranchError("withdraw to balance commit debit failed", err, "group_id", req.GroupID, "user_id", user.ID)
		return RedeemJobView{}, fmt.Errorf("commit withdraw debit: %w", err)
	}
	committed = true

	logRedeemDebited(job.ID, user.ID, req.GroupID, shareUnitsToDebit, int64(slice.UsdcOwed))
	view := redeemJobFromRow(job, positionFromRowPostgres(position))
	return r.continueRedeemJob(ctx, view, privy.PayoutProof{PayoutAddress: memberWallet})
}

// Redeem verifies payout proof, debits share units first, sells slice if needed, and pays USDC.
func (r *RedeemService) Redeem(ctx context.Context, req RedeemRequest) (RedeemJobView, error) {
	if req.ResumeJobID != "" {
		return r.resumeRedeemJob(ctx, req.ResumeJobID, req.PayoutProof)
	}

	if req.GroupID == "" {
		logRedeemBranchWarn("redeem rejected", "group id required")
		return RedeemJobView{}, fmt.Errorf("group id is required")
	}
	// Faker scale clubs (#153) have a dummy treasury: no sells, payouts, or Privy reads.
	if err := rejectFakerGroup(ctx, r.store, req.GroupID); err != nil {
		return RedeemJobView{}, err
	}


	identity, err := r.privy.VerifySession(ctx, privy.AccessToken(req.AccessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			logRedeemBranchWarn("redeem rejected", "invalid token", "group_id", req.GroupID)
			return RedeemJobView{}, privy.ErrInvalidToken
		}
		logRedeemBranchError("redeem verify session failed", err, "group_id", req.GroupID)
		return RedeemJobView{}, fmt.Errorf("verify session: %w", err)
	}

	user, found, err := r.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		logRedeemBranchError("redeem lookup user failed", err, "group_id", req.GroupID)
		return RedeemJobView{}, err
	}
	if !found {
		logRedeemBranchWarn("redeem rejected", "user not found", "group_id", req.GroupID)
		return RedeemJobView{}, ErrUserNotFound
	}

	logRedeemStart(req.GroupID, user.ID, "")

	if err := r.privy.VerifyPayoutProof(ctx, user.ID, req.PayoutProof); err != nil {
		if errors.Is(err, privy.ErrInvalidPayoutProof) {
			logRedeemBranchWarn("redeem rejected", "invalid payout proof", "group_id", req.GroupID, "user_id", user.ID)
			return RedeemJobView{}, ErrInvalidPayoutProof
		}
		logRedeemBranchError("redeem verify payout proof failed", err, "group_id", req.GroupID, "user_id", user.ID)
		return RedeemJobView{}, err
	}

	release, acquired, err := r.store.TryAcquireMemberRedeemLock(ctx, user.ID, req.GroupID)
	if err != nil {
		logRedeemBranchError("redeem acquire lock failed", err, "group_id", req.GroupID, "user_id", user.ID)
		return RedeemJobView{}, err
	}
	if !acquired {
		logRedeemLockContended(user.ID, req.GroupID)
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
		logRedeemBranchWarn("redeem rejected", "below dust minimum", "group_id", req.GroupID, "user_id", user.ID, "slice_usdc", int64(slice.UsdcOwed))
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
		logRedeemBranchError("redeem commit debit failed", err, "group_id", req.GroupID, "user_id", user.ID)
		return RedeemJobView{}, fmt.Errorf("commit redeem debit: %w", err)
	}
	committed = true

	logRedeemDebited(job.ID, user.ID, req.GroupID, shareUnitsToDebit, int64(slice.UsdcOwed))
	view := redeemJobFromRow(job, positionFromRowPostgres(position))
	return r.continueRedeemJob(ctx, view, req.PayoutProof)
}

func (r *RedeemService) resumeRedeemJob(ctx context.Context, jobID string, proof privy.PayoutProof) (RedeemJobView, error) {
	slog.Info("redeem resume start", "job_id", jobID)

	job, found, err := r.store.GetRedeemJobByID(ctx, jobID)
	if err != nil {
		logRedeemBranchError("redeem resume lookup job failed", err, "job_id", jobID)
		return RedeemJobView{}, err
	}
	if !found {
		logRedeemBranchWarn("redeem resume rejected", "job not found", "job_id", jobID)
		return RedeemJobView{}, fmt.Errorf("redeem job not found")
	}

	release, acquired, err := r.store.TryAcquireMemberRedeemLock(ctx, job.UserID, job.GroupID)
	if err != nil {
		logRedeemBranchError("redeem resume acquire lock failed", err, "job_id", jobID, "user_id", job.UserID, "group_id", job.GroupID)
		return RedeemJobView{}, err
	}
	if !acquired {
		logRedeemLockContended(job.UserID, job.GroupID)
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
	slog.Info("redeem continue", "job_id", view.ID, "status", string(view.Status))

	if view.Status == domain.RedeemJobSettled {
		slog.Info("redeem already settled", "job_id", view.ID)
		return view, nil
	}
	switch view.Status {
	case domain.RedeemJobDebited, domain.RedeemJobSelling, domain.RedeemJobPaying:
	default:
		logRedeemBranchWarn("redeem rejected", "unsupported status", "job_id", view.ID, "status", string(view.Status))
		return view, fmt.Errorf("unsupported redeem job status %q", view.Status)
	}

	treasury, err := r.privy.EnsureTreasury(ctx, privy.GroupID(view.GroupID))
	if err != nil {
		return view, err
	}

	// Re-price against the pot as it really is now. A job debited against a stale treasury
	// balance (or one wedged from an earlier failure) can carry a slice larger than the
	// member's actual claim, and paying it would overdraw the treasury.
	cash, owed, err := r.repriceRedeemJob(ctx, view, treasury.SolanaAddress)
	if err != nil {
		return r.failRedeemJob(ctx, view, err)
	}
	if owed != view.SliceUsdc {
		slog.Warn("redeem slice re-priced", "job_id", view.ID,
			"stored_slice_usdc", view.SliceUsdc, "repriced_slice_usdc", owed, "treasury_usdc", cash)
		if err := r.store.UpdateRedeemJobSliceUsdc(ctx, view.ID, owed); err != nil {
			return r.failRedeemJob(ctx, view, err)
		}
		view.SliceUsdc = owed
	}

	// A job already in `selling` has a confirmed sell behind it; anything else still needs the
	// cash raised. Sizing the sell by the shortfall keeps that decision safe either way.
	if cash < owed && view.Status != domain.RedeemJobSelling {
		if err := r.sellRedeemShortfall(ctx, &view, cash, owed); err != nil {
			logRedeemBranchError("redeem sell slice failed", err, "job_id", view.ID)
			return r.failRedeemJob(ctx, view, err)
		}
		cash, err = r.privy.TreasuryUSDCBalance(ctx, treasury.SolanaAddress)
		if err != nil {
			return r.failRedeemJob(ctx, view, fmt.Errorf("treasury usdc balance: %w", err))
		}
	} else if cash >= owed {
		slog.Info("redeem sell slice skipped", "job_id", view.ID, "reason", "treasury usdc covers slice",
			"treasury_usdc", cash, "slice_usdc", owed)
	}

	// The transfer may never exceed the treasury's real USDC: an SPL transfer for more than the
	// token account holds fails simulation with Custom:1 and 500s the whole cash out.
	payAmount := owed
	if cash < payAmount {
		slog.Warn("redeem payout clamped to treasury usdc", "job_id", view.ID,
			"owed_usdc", owed, "treasury_usdc", cash)
		payAmount = cash
	}
	if payAmount < int64(domain.RedeemDustMinimumMicros) {
		return r.failRedeemJob(ctx, view, fmt.Errorf(
			"%w: the pot only has %d USDC micros available against a %d payout", ErrRedeemPotIlliquid, cash, owed))
	}
	if payAmount != view.SliceUsdc {
		if err := r.store.UpdateRedeemJobSliceUsdc(ctx, view.ID, payAmount); err != nil {
			return r.failRedeemJob(ctx, view, err)
		}
		view.SliceUsdc = payAmount
	}

	if view.Status != domain.RedeemJobPaying {
		prev := view.Status
		if err := r.store.UpdateRedeemJobStatus(ctx, view.ID, string(domain.RedeemJobPaying)); err != nil {
			logRedeemBranchError("redeem update status failed", err, "job_id", view.ID)
			return r.failRedeemJob(ctx, view, err)
		}
		view.Status = domain.RedeemJobPaying
		logRedeemStatusTransition(view.ID, string(prev), string(view.Status))
	}

	platformPayout, err := r.isPlatformPayoutAddress(ctx, view.UserID, view.PayoutAddress)
	if err != nil {
		return r.failRedeemJob(ctx, view, err)
	}
	return r.payRedeemSlice(ctx, view, proof, platformPayout, treasury)
}

// repriceRedeemJob values the pot from the authoritative on-chain treasury balance and returns
// that balance plus what the member is owed, never more than the slice quoted at debit time.
func (r *RedeemService) repriceRedeemJob(ctx context.Context, view RedeemJobView, treasuryAddress string) (cash int64, owed int64, err error) {
	cash, err = r.privy.TreasuryUSDCBalance(ctx, treasuryAddress)
	if err != nil {
		return 0, 0, fmt.Errorf("treasury usdc balance: %w", err)
	}

	// The job's shares are already debited, so add them back: the pot still owes them.
	navVals, err := r.store.ComputeNavSnapshotValuesWithShareBase(ctx, view.GroupID, cash, view.ShareUnits)
	if err != nil {
		return 0, 0, err
	}
	totalSharesMicro, err := r.store.SumShareUnitsByGroup(ctx, view.GroupID)
	if err != nil {
		return 0, 0, err
	}
	totalSharesMicro += view.ShareUnits

	slice, err := domain.ComputeRedeemSlice(domain.RedeemSliceInput{
		SharesRedeemedMicros: view.ShareUnits,
		TotalSharesMicros:    totalSharesMicro,
		PotNav:               domain.USDCMicros(navVals.PotNavMicros),
	})
	if err != nil {
		return 0, 0, err
	}

	owed = int64(slice.UsdcOwed)
	if owed > view.SliceUsdc {
		owed = view.SliceUsdc
	}
	return cash, owed, nil
}

// sellRedeemShortfall sells pot holdings to USDC until the payout can be funded, waiting for
// each sell to confirm before returning.
func (r *RedeemService) sellRedeemShortfall(ctx context.Context, view *RedeemJobView, cash, owed int64) error {
	holdings, err := r.store.ListNetTokenHoldingsByGroup(ctx, view.GroupID)
	if err != nil {
		return err
	}
	if len(holdings) == 0 {
		slog.Info("redeem sell slice skipped", "job_id", view.ID, "reason", "no token holdings")
		return nil
	}

	// Marked value of everything the pot holds that is not already cash.
	navVals, err := r.store.ComputeNavSnapshotValuesWithShareBase(ctx, view.GroupID, cash, view.ShareUnits)
	if err != nil {
		return err
	}
	stockValue := navVals.PotNavMicros - cash
	if stockValue <= 0 {
		slog.Info("redeem sell slice skipped", "job_id", view.ID, "reason", "no marked stock value")
		return nil
	}
	shortfall := owed - cash

	for _, holding := range holdings {
		sellAmount := jupiter.RedeemShortfallSellAmount(holding.Amount, shortfall, stockValue)
		if sellAmount <= 0 {
			continue
		}
		symbol := symbolForOutputMint(ctx, r.swap.symbols, holding.Mint)
		if _, err := r.swap.SellToUSDC(ctx, SellToUSDCRequest{
			GroupID:   view.GroupID,
			UserID:    view.UserID,
			Symbol:    symbol,
			InputMint: holding.Mint,
			Amount:    sellAmount,
		}); err != nil {
			if errors.Is(err, ErrQuoteNotRoutable) {
				return fmt.Errorf("%w: stock sell below swap minimum; try a larger amount or wait for more USDC in the pot", ErrQuoteNotRoutable)
			}
			return fmt.Errorf("sell redeem slice: %w", err)
		}
	}

	prev := view.Status
	if err := r.store.UpdateRedeemJobStatus(ctx, view.ID, string(domain.RedeemJobSelling)); err != nil {
		return err
	}
	view.Status = domain.RedeemJobSelling
	logRedeemStatusTransition(view.ID, string(prev), string(view.Status))
	return nil
}

// failRedeemJob rolls an unpaid job back so the member keeps their shares and can retry, then
// returns the original cause. A job that already has a withdrawal attached is never rolled back.
func (r *RedeemService) failRedeemJob(ctx context.Context, view RedeemJobView, cause error) (RedeemJobView, error) {
	if view.WithdrawalID != "" {
		logRedeemBranchError("redeem failed after payout; leaving job for manual review", cause, "job_id", view.ID)
		return view, cause
	}
	if abortErr := r.abortRedeemJob(ctx, view); abortErr != nil {
		logRedeemBranchError("redeem abort after failure failed", abortErr, "job_id", view.ID)
	}
	return view, cause
}

func (r *RedeemService) payRedeemSlice(ctx context.Context, view RedeemJobView, proof privy.PayoutProof, platformPayout bool, treasury privy.TreasuryRef) (RedeemJobView, error) {
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

	if !platformPayout {
		if err := r.store.InsertPayoutProofTx(ctx, tx, view.UserID, view.GroupID, proof.PayoutAddress, proof.Message, proof.Signature, withdrawal.ID); err != nil {
			return view, err
		}
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
	logRedeemSettled(view.ID, view.UserID, view.GroupID, withdrawal.ID, view.SliceUsdc)
	return view, nil
}

func (r *RedeemService) ensureMemberWalletAddress(ctx context.Context, privyUserID, userID string) (string, error) {
	existing, found, err := r.store.GetMemberWalletByUserID(ctx, userID)
	if err != nil {
		return "", err
	}
	if found {
		return existing.SolanaAddress, nil
	}
	ref, err := r.privy.EnsureMemberWallet(ctx, privyUserID, privy.UserID(userID))
	if err != nil {
		return "", fmt.Errorf("privy ensure member wallet: %w", err)
	}
	if _, err := r.store.InsertMemberWallet(ctx, userID, ref.PrivyWalletID, ref.SolanaAddress); err != nil {
		return "", err
	}
	return ref.SolanaAddress, nil
}

func (r *RedeemService) isPlatformPayoutAddress(ctx context.Context, userID, payoutAddress string) (bool, error) {
	wallet, found, err := r.store.GetMemberWalletByUserID(ctx, userID)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	return wallet.SolanaAddress == payoutAddress, nil
}

func (r *RedeemService) resolveWithdrawShares(ctx context.Context, req WithdrawToBalanceRequest, userID, treasuryAddress string) (shareUnits int64, potNav int64, totalShares int64, err error) {
	position, hasPosition, err := r.store.GetPosition(ctx, userID, req.GroupID)
	if err != nil {
		return 0, 0, 0, err
	}
	if !hasPosition || position.ShareUnits <= 0 {
		return 0, 0, 0, fmt.Errorf("%w: no share units to withdraw", ErrInvalidRedeemRequest)
	}

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
	default:
		shareUnits = position.ShareUnits
	}

	if shareUnits <= 0 || shareUnits > position.ShareUnits {
		return 0, 0, 0, fmt.Errorf("%w: share amount out of range", ErrInvalidRedeemRequest)
	}
	return shareUnits, potNav, totalShares, nil
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

// reconcileActiveRedeemBeforeWithdraw clears stuck debited jobs or resumes in-flight payout work.
func (r *RedeemService) reconcileActiveRedeemBeforeWithdraw(ctx context.Context, userID, groupID, payoutAddress string) (RedeemJobView, bool, error) {
	job, found, err := r.store.GetActiveRedeemJobForUser(ctx, userID, groupID)
	if err != nil {
		return RedeemJobView{}, false, err
	}
	if !found {
		return RedeemJobView{}, false, nil
	}

	status, err := domain.ParseRedeemJobStatus(job.Status)
	if err != nil {
		return RedeemJobView{}, false, err
	}

	position, hasPosition, err := r.store.GetPosition(ctx, userID, groupID)
	if err != nil {
		return RedeemJobView{}, false, err
	}
	if !hasPosition {
		position = postgres.PositionRow{UserID: userID, GroupID: groupID}
	}
	view := redeemJobFromRow(job, positionFromRowPostgres(position))

	switch status {
	case domain.RedeemJobDebited:
		slog.Warn("redeem abort stuck debited job before new withdraw", "job_id", job.ID, "user_id", userID, "group_id", groupID)
		if err := r.abortRedeemJob(ctx, view); err != nil {
			return RedeemJobView{}, false, err
		}
		return RedeemJobView{}, false, nil
	case domain.RedeemJobSelling, domain.RedeemJobPaying:
		proof := privy.PayoutProof{PayoutAddress: payoutAddress}
		if proof.PayoutAddress == "" {
			proof.PayoutAddress = job.PayoutAddress
		}
		result, err := r.continueRedeemJob(ctx, view, proof)
		return result, true, err
	default:
		return RedeemJobView{}, false, fmt.Errorf("unsupported active redeem job status %q", job.Status)
	}
}

// abortRedeemJob returns the debited share units and clears the job. It is safe for any status
// that has not produced a payout: nothing has left the treasury, and a sell that already
// happened only left the pot holding more USDC than before.
func (r *RedeemService) abortRedeemJob(ctx context.Context, view RedeemJobView) error {
	switch view.Status {
	case domain.RedeemJobDebited, domain.RedeemJobSelling, domain.RedeemJobPaying:
	default:
		return fmt.Errorf("cannot abort redeem job in status %q", view.Status)
	}
	if view.WithdrawalID != "" {
		return fmt.Errorf("cannot abort redeem job %s: payout already recorded", view.ID)
	}

	tx, err := r.store.BeginTx(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := r.store.CreditPositionShareUnitsTx(ctx, tx, view.UserID, view.GroupID, view.ShareUnits); err != nil {
		return err
	}
	if err := r.store.DeleteRedeemJobTx(ctx, tx, view.ID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit abort redeem job: %w", err)
	}
	committed = true

	slog.Info("redeem job aborted", "job_id", view.ID, "user_id", view.UserID, "group_id", view.GroupID, "share_units", view.ShareUnits)
	return nil
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

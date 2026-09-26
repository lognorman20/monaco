package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/apps/backend/internal/solana/mintinfo"
	"github.com/monaco/monaco/apps/backend/internal/telemetry"
	"github.com/monaco/monaco/packages/domain"
)

// WithdrawToBalanceRequest withdraws deployed stake to the member Privy wallet (platform balance).
type WithdrawToBalanceRequest struct {
	AccessToken       string
	GroupID           string
	ShareAmountMicros *int64
}

// RedeemRequest starts or resumes a debit-first redeem job.
type RedeemRequest struct {
	AccessToken        string
	GroupID            string
	ShareAmountMicros  *int64
	DollarTargetMicros *int64
	PayoutProof        privy.PayoutProof
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

// ErrRedeemPayoutPending means the payout was broadcast but Solana has not confirmed it yet.
// The job stays in `paying`; the next request or the recovery poller settles it from the
// signature on record. Nothing is paid again.
var ErrRedeemPayoutPending = errors.New("redeem payout pending confirmation")

// ErrRedeemPayoutFailed means the payout landed on chain with an error. No USDC moved and
// the debited share units went back to the member.
var ErrRedeemPayoutFailed = errors.New("redeem payout failed on chain")

// ErrRedeemPayoutDropped means the payout never landed before its blockhash expired. No USDC
// moved and the debited share units went back to the member, who can cash out again.
var ErrRedeemPayoutDropped = errors.New("redeem payout dropped")

const (
	// defaultRedeemPayoutConfirmTimeout is how long a request waits for its payout to confirm.
	// A transfer normally confirms within seconds; past this the request answers "still
	// confirming" (inside the app's 60s request timeout) and the job is finished by the
	// member's next request or the recovery poller.
	defaultRedeemPayoutConfirmTimeout = 45 * time.Second
	defaultRedeemPayoutPollInterval   = 2 * time.Second
)

// RedeemService orchestrates debit-first redeem with resume support.
type RedeemService struct {
	store   *postgres.Store
	privy   privy.Client
	pyth    pyth.Client
	jupiter jupiter.Client
	swap    *SwapService
	signer  TreasurySigner

	payoutConfirmTimeout time.Duration
	payoutPollInterval   time.Duration

	// lane: notifications
	notifier *Notifier
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

		payoutConfirmTimeout: defaultRedeemPayoutConfirmTimeout,
		payoutPollInterval:   defaultRedeemPayoutPollInterval,
	}
}

// WithdrawToBalance debits share units, sells slice if needed, and pays USDC to the member wallet.
func (r *RedeemService) WithdrawToBalance(ctx context.Context, req WithdrawToBalanceRequest) (RedeemJobView, error) {
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

	if resumed, handled, err := r.reconcileActiveRedeemBeforeWithdraw(ctx, user.ID, req.GroupID); handled {
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
	view, err := r.redeem(ctx, req)
	if err != nil {
		// Success is counted where the payout settles, which resumed jobs also reach.
		telemetry.MoneyEvent(telemetry.EventRedeem, moneyOutcome(err))
	}
	return view, err
}

func (r *RedeemService) redeem(ctx context.Context, req RedeemRequest) (RedeemJobView, error) {
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

func (r *RedeemService) continueRedeemJob(ctx context.Context, view RedeemJobView, proof privy.PayoutProof) (RedeemJobView, error) {
	slog.Info("redeem continue", "job_id", view.ID, "status", string(view.Status))

	if view.Status == domain.RedeemJobSettled {
		slog.Info("redeem already settled", "job_id", view.ID)
		return view, nil
	}
	// A `paying` job already has a signed payout on record. It is decided from that signature
	// (resolveRedeemPayout) and never comes back here, where a second transfer would be signed.
	switch view.Status {
	case domain.RedeemJobDebited, domain.RedeemJobSelling:
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
		// Share units are claim tickets: a payout short of the slice only retires the share
		// units it covers. The rest go back to the member before anything is signed.
		if err := r.shrinkRedeemJobToPayout(ctx, &view, owed, payAmount); err != nil {
			return r.failRedeemJob(ctx, view, err)
		}
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

	// The job's shares are already debited; the valuation's share base still counts them,
	// because the pot owes them until the payout lands.
	valuation, err := r.valuePotForRedeem(ctx, view.GroupID, treasuryAddress, cash)
	if err != nil {
		return 0, 0, err
	}
	if valuation.PotNavMicros <= 0 {
		return 0, 0, fmt.Errorf("%w: the pot holds nothing to pay a %d share-unit claim from", ErrRedeemPotIlliquid, view.ShareUnits)
	}

	slice, err := domain.ComputeRedeemSlice(domain.RedeemSliceInput{
		SharesRedeemedMicros: view.ShareUnits,
		TotalSharesMicros:    valuation.ShareBaseMicros,
		PotNav:               domain.USDCMicros(valuation.PotNavMicros),
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
	treasury, found, err := r.store.GetTreasuryByGroupID(ctx, view.GroupID)
	if err != nil {
		return err
	}
	if !found {
		return ErrGroupNotFound
	}
	valuation, err := r.valuePotForRedeem(ctx, view.GroupID, treasury.SolanaAddress, cash)
	if err != nil {
		return err
	}
	stockValue := valuation.HoldingsMicros
	if stockValue <= 0 {
		slog.Info("redeem sell slice skipped", "job_id", view.ID, "reason", "no marked stock value")
		return nil
	}
	shortfall := owed - cash

	for _, holding := range holdings {
		bufferBps := redeemSellSlippageBufferBps(ctx, r.redeemSymbols(), r.swapMintInfo(), holding.Mint)
		sellAmount := jupiter.RedeemShortfallSellAmount(holding.Amount, shortfall, stockValue, bufferBps)
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

// shrinkRedeemJobToPayout cuts an unpaid job down to what the treasury can really pay: it
// keeps the share units payAmount covers out of owed and credits the rest back to the member,
// in one transaction, so the job, the position and the transfer always agree.
func (r *RedeemService) shrinkRedeemJobToPayout(ctx context.Context, view *RedeemJobView, owed, payAmount int64) error {
	burned, err := domain.ShareUnitsForPartialPayout(view.ShareUnits, domain.USDCMicros(owed), domain.USDCMicros(payAmount))
	if err != nil {
		return err
	}
	returned := view.ShareUnits - burned

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

	position := view.Position
	if returned > 0 {
		row, err := r.store.CreditPositionShareUnitsTx(ctx, tx, view.UserID, view.GroupID, returned)
		if err != nil {
			return err
		}
		position = positionFromRowPostgres(row)
	}
	if err := r.store.ReduceRedeemJobTx(ctx, tx, view.ID, burned, payAmount); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit redeem job shrink: %w", err)
	}
	committed = true

	slog.Warn("redeem job shrunk to payable amount", "job_id", view.ID,
		"owed_usdc", owed, "pay_usdc", payAmount,
		"share_units_burned", burned, "share_units_returned", returned)
	view.ShareUnits = burned
	view.SliceUsdc = payAmount
	view.Position = position
	return nil
}

// payRedeemSlice signs the payout, records it, and only then broadcasts it. The job settles
// on the chain's word alone: see resolveRedeemPayout.
func (r *RedeemService) payRedeemSlice(ctx context.Context, view RedeemJobView, proof privy.PayoutProof, platformPayout bool, treasury privy.TreasuryRef) (RedeemJobView, error) {
	if !platformPayout && (proof.Message == "" || proof.Signature == "") {
		return r.failRedeemJob(ctx, view, fmt.Errorf("%w: payout to %s needs an ownership proof", ErrInvalidPayoutProof, view.PayoutAddress))
	}

	// Signing moves no money, so a failure up to the recorded intent still rolls back cleanly.
	prepared, err := r.privy.PrepareUSDCPayout(ctx, privy.PayUSDCRequest{
		TreasuryPrivyWalletID: treasury.PrivyWalletID,
		TreasuryAddress:       treasury.SolanaAddress,
		ToAddress:             view.PayoutAddress,
		Amount:                view.SliceUsdc,
		GroupID:               view.GroupID,
		UserID:                view.UserID,
	})
	if err != nil {
		return r.failRedeemJob(ctx, view, fmt.Errorf("prepare redeem payout: %w", err))
	}

	intent := postgres.RedeemPayoutIntent{
		RedeemJobID:          view.ID,
		UserID:               view.UserID,
		GroupID:              view.GroupID,
		Amount:               view.SliceUsdc,
		ToAddress:            view.PayoutAddress,
		TxSignature:          prepared.TxSignature,
		SignedTx:             prepared.SignedTransaction,
		LastValidBlockHeight: prepared.LastValidBlockHeight,
	}
	if !platformPayout {
		intent.ProofMessage = proof.Message
		intent.ProofSignature = proof.Signature
	}
	if _, err := r.store.RecordRedeemPayoutIntent(ctx, intent); err != nil {
		// If the commit did land, the abort refuses and the unsent transfer expires as dropped.
		return r.failRedeemJob(ctx, view, fmt.Errorf("record redeem payout intent: %w", err))
	}
	prev := view.Status
	view.Status = domain.RedeemJobPaying
	logRedeemStatusTransition(view.ID, string(prev), string(view.Status))

	return r.resolveRedeemPayout(ctx, view, true)
}

// resolveRedeemPayout decides a `paying` job from the payout signature on record and nothing
// else: confirmed on chain settles it, failed on chain or dropped (blockhash expired without
// landing) returns the share units, and anything still in flight stays as it is. It never
// signs a second transfer, so however often it runs the member is paid at most once.
//
// With broadcast set (a member's request) the recorded transaction is sent and the call
// waits for a verdict. Sending it again after an error, a crash or a second tap is safe: it
// is the same signature. Without it (the recovery poller) the chain is only read, once.
func (r *RedeemService) resolveRedeemPayout(ctx context.Context, view RedeemJobView, broadcast bool) (RedeemJobView, error) {
	payout, found, err := r.store.GetRedeemPayoutByJobID(ctx, view.ID)
	if err != nil {
		return view, err
	}
	if !found {
		// Only a job left by the old sign-and-send path is `paying` with no payout recorded.
		logRedeemBranchWarn("redeem payout unverified", "paying job has no payout on record", "job_id", view.ID)
		return view, ErrRedeemPayoutUnverified
	}
	if payout.Status != postgres.RedeemPayoutStatusPending {
		return view, fmt.Errorf("%w: job %s is paying but payout %s is %s", ErrRedeemPayoutUnverified, view.ID, payout.TxSignature, payout.Status)
	}

	prepared := privy.PreparedPayout{
		TxSignature:          payout.TxSignature,
		SignedTransaction:    payout.SignedTx,
		LastValidBlockHeight: payout.LastValidBlockHeight,
	}
	if broadcast {
		if err := r.privy.BroadcastUSDCPayout(ctx, prepared); err != nil {
			// Not proof the transfer missed the chain; the signature status below decides.
			slog.Warn("redeem payout broadcast returned an error", "job_id", view.ID, "tx_signature", payout.TxSignature, "err", err)
		}
	}

	deadline := time.Now().Add(r.payoutConfirmTimeout)
	for {
		status, statusErr := r.privy.USDCPayoutStatus(ctx, prepared)
		switch {
		case statusErr != nil:
			slog.Warn("redeem payout status check failed", "job_id", view.ID, "tx_signature", payout.TxSignature, "err", statusErr)
		case status.State == privy.PayoutStateConfirmed:
			return r.settleRedeemPayout(ctx, view, payout)
		case status.State == privy.PayoutStateFailed:
			return r.rollBackRedeemPayout(ctx, view, payout, postgres.RedeemPayoutStatusFailed, status.Reason, ErrRedeemPayoutFailed)
		case status.State == privy.PayoutStateDropped:
			return r.rollBackRedeemPayout(ctx, view, payout, postgres.RedeemPayoutStatusDropped, "blockhash expired before the transfer landed", ErrRedeemPayoutDropped)
		}

		if !broadcast {
			if statusErr != nil {
				return view, fmt.Errorf("redeem payout status: %w", statusErr)
			}
			return view, ErrRedeemPayoutPending
		}
		if !time.Now().Before(deadline) {
			if statusErr != nil {
				return view, fmt.Errorf("%w: %v", ErrRedeemPayoutPending, statusErr)
			}
			return view, ErrRedeemPayoutPending
		}
		select {
		case <-ctx.Done():
			return view, fmt.Errorf("%w: %v", ErrRedeemPayoutPending, ctx.Err())
		case <-time.After(r.payoutPollInterval):
		}
	}
}

// rollBackRedeemPayout closes a payout that moved no money and returns the share units, in
// one transaction, then reports why through cause.
func (r *RedeemService) rollBackRedeemPayout(ctx context.Context, view RedeemJobView, payout postgres.RedeemPayoutRow, payoutStatus, reason string, cause error) (RedeemJobView, error) {
	slog.Warn("redeem payout moved no money; returning share units", "job_id", view.ID,
		"user_id", view.UserID, "group_id", view.GroupID, "tx_signature", payout.TxSignature,
		"payout_status", payoutStatus, "reason", reason, "share_units", view.ShareUnits)
	if err := r.abortRedeemJobWithVerdict(ctx, view, payoutStatus, reason); err != nil {
		logRedeemBranchError("redeem payout roll back failed", err, "job_id", view.ID, "tx_signature", payout.TxSignature)
		return view, err
	}
	return view, fmt.Errorf("%w: %s", cause, reason)
}

// settleRedeemPayout records a payout the chain has confirmed: the withdrawal, the NAV
// snapshot and the settled job commit together, keyed on the confirmed signature.
func (r *RedeemService) settleRedeemPayout(ctx context.Context, view RedeemJobView, payout postgres.RedeemPayoutRow) (RedeemJobView, error) {
	treasury, err := r.privy.EnsureTreasury(ctx, privy.GroupID(view.GroupID))
	if err != nil {
		return view, err
	}
	// Read after confirmation, so the snapshot values the pot without the USDC that just left.
	treasuryUsdc, err := r.privy.TreasuryUSDCBalance(ctx, treasury.SolanaAddress)
	if err != nil {
		return view, fmt.Errorf("treasury usdc balance: %w", err)
	}
	navAfterPayout, err := r.navSnapshotAfterPayout(ctx, view, treasury.SolanaAddress, treasuryUsdc)
	if err != nil {
		return view, err
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

	if err := r.store.ResolveRedeemPayoutTx(ctx, tx, payout.ID, postgres.RedeemPayoutStatusConfirmed, ""); err != nil {
		return view, err
	}

	withdrawal, err := r.store.InsertWithdrawalTx(ctx, tx, view.UserID, view.GroupID, payout.Amount, payout.ToAddress)
	if err != nil {
		return view, err
	}

	if payout.ProofMessage.Valid && payout.ProofSignature.Valid {
		if err := r.store.InsertPayoutProofTx(ctx, tx, view.UserID, view.GroupID, payout.ToAddress, payout.ProofMessage.String, payout.ProofSignature.String, withdrawal.ID); err != nil {
			return view, err
		}
	}

	confirmed, newlyPaid, err := r.store.ConfirmWithdrawalPayoutTx(ctx, tx, withdrawal.ID, payout.TxSignature, navAfterPayout)
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
	view.SliceUsdc = payout.Amount
	view.Position = positionFromRowPostgres(position)
	logRedeemSettled(view.ID, view.UserID, view.GroupID, withdrawal.ID, view.SliceUsdc)
	telemetry.MoneyMoved(telemetry.EventRedeem, view.SliceUsdc)
	// lane: notifications
	r.notifier.CashOutSettled(ctx, view.UserID, view.GroupID, payout.Amount, payout.ToAddress)
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

	valuation, err := r.quotePotForRedeem(ctx, req.GroupID, treasuryAddress)
	if err != nil {
		return 0, 0, 0, err
	}
	potNav, totalShares = valuation.PotNavMicros, valuation.ShareBaseMicros

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
	valuation, err := r.quotePotForRedeem(ctx, req.GroupID, treasuryAddress)
	if err != nil {
		return 0, 0, 0, err
	}
	potNav, totalShares = valuation.PotNavMicros, valuation.ShareBaseMicros
	navVals, err := valuation.navSnapshotValues()
	if err != nil {
		return 0, 0, 0, err
	}

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

// quotePotForRedeem reads the treasury balance and values the pot for a new redeem quote.
func (r *RedeemService) quotePotForRedeem(ctx context.Context, groupID, treasuryAddress string) (potValuation, error) {
	treasuryUsdc, err := r.privy.TreasuryUSDCBalance(ctx, treasuryAddress)
	if err != nil {
		return potValuation{}, fmt.Errorf("treasury usdc balance: %w", err)
	}
	valuation, err := r.valuePotForRedeem(ctx, groupID, treasuryAddress, treasuryUsdc)
	if err != nil {
		return potValuation{}, err
	}
	if valuation.ShareBaseMicros <= 0 {
		return potValuation{}, fmt.Errorf("%w: no shares outstanding", ErrInvalidRedeemRequest)
	}
	return valuation, nil
}

// valuePotForRedeem is the shared pot valuation with live marks required: a payout is never
// sized from cost basis. ErrPotMarkUnavailable leaves the member's shares where they were.
func (r *RedeemService) valuePotForRedeem(ctx context.Context, groupID, treasuryAddress string, treasuryUsdc int64) (potValuation, error) {
	valuation, err := valuePot(ctx, r.store, r.pyth, r.redeemSymbols(), nil, groupID, treasuryAddress, treasuryUsdc, potMarksLiveOnly)
	if err != nil {
		if errors.Is(err, ErrPotMarkUnavailable) {
			logPotMarkUnavailable(groupID, "redeem", err)
		}
		return potValuation{}, err
	}
	return valuation, nil
}

// navSnapshotAfterPayout values the pot for the NAV history row written with a confirmed
// payout. The USDC has already left, so history takes the best available mark rather than
// failing; the paid job's units stop counting as a claim in the same transaction.
func (r *RedeemService) navSnapshotAfterPayout(ctx context.Context, view RedeemJobView, treasuryAddress string, treasuryUsdc int64) (postgres.NavSnapshotValues, error) {
	valuation, err := valuePot(ctx, r.store, r.pyth, r.redeemSymbols(), nil, view.GroupID, treasuryAddress, treasuryUsdc, potMarksBestAvailable)
	if err != nil {
		return postgres.NavSnapshotValues{}, err
	}
	shareBase := valuation.ShareBaseMicros - view.ShareUnits
	if shareBase < 0 {
		shareBase = 0
	}
	return navSnapshotValuesFor(valuation.PotNavMicros, shareBase)
}

func (r *RedeemService) redeemSymbols() *SymbolResolver {
	if r.swap == nil {
		return nil
	}
	return r.swap.symbols
}

func (r *RedeemService) swapMintInfo() mintinfo.Reader {
	if r.swap == nil {
		return nil
	}
	return r.swap.mintinfo
}

// reconcileActiveRedeemBeforeWithdraw clears stuck debited jobs or resumes in-flight payout work.
func (r *RedeemService) reconcileActiveRedeemBeforeWithdraw(ctx context.Context, userID, groupID string) (RedeemJobView, bool, error) {
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
	case domain.RedeemJobSelling:
		// Nothing has been signed yet. The payout goes to the address the job was opened
		// for, never to one supplied by the resuming request.
		result, err := r.continueRedeemJob(ctx, view, privy.PayoutProof{PayoutAddress: job.PayoutAddress})
		return result, true, err
	case domain.RedeemJobPaying:
		result, err := r.resolveRedeemPayout(ctx, view, true)
		if errors.Is(err, ErrRedeemPayoutDropped) {
			// The old transfer can never land and its shares are back; serve this request fresh.
			return RedeemJobView{}, false, nil
		}
		return result, true, err
	default:
		return RedeemJobView{}, false, fmt.Errorf("unsupported active redeem job status %q", job.Status)
	}
}

// abortRedeemJob returns the debited share units and clears the job. It is safe for any job
// with no payout on record: nothing has left the treasury, and a sell that already happened
// only left the pot holding more USDC than before. A job whose payout is on record is only
// aborted together with the chain's verdict on that payout (rollBackRedeemPayout).
func (r *RedeemService) abortRedeemJob(ctx context.Context, view RedeemJobView) error {
	return r.abortRedeemJobWithVerdict(ctx, view, "", "")
}

// abortRedeemJobWithVerdict is abortRedeemJob for a job with a payout on record: the payout
// is closed as dropped or failed in the same transaction that returns the share units.
func (r *RedeemService) abortRedeemJobWithVerdict(ctx context.Context, view RedeemJobView, payoutStatus, reason string) error {
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

	payout, hasPayout, err := r.store.LockRedeemPayoutByJobIDTx(ctx, tx, view.ID)
	if err != nil {
		return err
	}
	switch {
	case hasPayout && payoutStatus != "":
		if err := r.store.ResolveRedeemPayoutTx(ctx, tx, payout.ID, payoutStatus, reason); err != nil {
			return err
		}
	case hasPayout:
		return fmt.Errorf("cannot abort redeem job %s: payout %s is %s: %w", view.ID, payout.TxSignature, payout.Status, ErrRedeemPayoutUnverified)
	case payoutStatus != "":
		return fmt.Errorf("cannot abort redeem job %s: no payout on record to close", view.ID)
	}

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

package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/apps/backend/internal/telemetry"
)

// DepositStatus is the lifecycle state of a deposit intent.
type DepositStatus string

const (
	DepositStatusPending   DepositStatus = "pending"
	DepositStatusConfirmed DepositStatus = "confirmed"
	DepositStatusFailed    DepositStatus = "failed"
)

// Deposit is a user funding intent for a group treasury sweep.
type Deposit struct {
	ID          string
	UserID      string
	GroupID     string
	Amount      int64
	FromAddress string
	Status      DepositStatus
	TxSignature string
	CreatedAt   time.Time
}

// Position tracks a member's share units and net USDC in a group pot.
type Position struct {
	UserID          string
	GroupID         string
	ShareUnits      int64
	AmountDeposited int64
	AmountWithdrawn int64
}

// ObservedSweep is a confirmed on-chain USDC transfer from member wallet to group treasury.
type ObservedSweep struct {
	TxSignature string
	FromAddress string
	ToAddress   string
	Amount      int64
	DepositID   string
	UserID      string
	GroupID     string
}

// CreateDepositResult is the persisted pending deposit row.
type CreateDepositResult struct {
	Deposit Deposit
}

// PlatformBalanceResult is the authenticated user's spendable USDC in their member wallet.
type PlatformBalanceResult struct {
	AvailableUsdcMicros     int64
	MemberWalletAddress     string
	PendingAllocationMicros int64
}

// ObserveSweepResult is the outcome after applying a confirmed treasury credit.
type ObserveSweepResult struct {
	Deposit  Deposit
	Position Position
	Credited bool
}

// ErrDepositNotFound means the deposit row does not exist.
var ErrDepositNotFound = errors.New("deposit not found")

// ErrNotGroupMember means the user cannot deposit into the group.
var ErrNotGroupMember = errors.New("not a group member")

// ErrInsufficientPlatformBalance means the fund amount exceeds available member-wallet USDC.
var ErrInsufficientPlatformBalance = errors.New("insufficient platform balance")

// ErrInvalidSweepTarget means the sweep did not arrive at group treasury.
var ErrInvalidSweepTarget = errors.New("invalid sweep target")

// DepositService orchestrates deposit create and sweep credit flows.
type DepositService struct {
	store   *postgres.Store
	privy   privy.Client
	pyth    pyth.Client
	symbols *SymbolResolver
}

// NewDepositService wires deposit dependencies.
func NewDepositService(store *postgres.Store, privyClient privy.Client, pythClient pyth.Client, symbols *SymbolResolver) *DepositService {
	return &DepositService{
		store:   store,
		privy:   privyClient,
		pyth:    pythClient,
		symbols: symbols,
	}
}

// GetPlatformBalance returns chain member-wallet USDC minus in-flight fund reservations.
func (d *DepositService) GetPlatformBalance(ctx context.Context, accessToken string) (PlatformBalanceResult, error) {
	identity, err := d.privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			return PlatformBalanceResult{}, privy.ErrInvalidToken
		}
		return PlatformBalanceResult{}, fmt.Errorf("verify session: %w", err)
	}

	user, found, err := d.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		return PlatformBalanceResult{}, err
	}
	if !found {
		return PlatformBalanceResult{}, ErrUserNotFound
	}

	wallet, found, err := d.store.GetMemberWalletByUserID(ctx, user.ID)
	if err != nil {
		return PlatformBalanceResult{}, err
	}
	if !found {
		return PlatformBalanceResult{}, ErrUserNotFound
	}

	balance, pending, err := d.platformBalanceForWallet(ctx, user.ID, wallet.SolanaAddress)
	if err != nil {
		return PlatformBalanceResult{}, err
	}

	return PlatformBalanceResult{
		AvailableUsdcMicros:     balance,
		MemberWalletAddress:     wallet.SolanaAddress,
		PendingAllocationMicros: pending,
	}, nil
}

// FundGroup records a user-initiated cabal fund intent and enqueues an exact-amount sweep.
func (d *DepositService) FundGroup(ctx context.Context, accessToken string, groupID string, amount int64) (CreateDepositResult, error) {
	logDepositCreateStart(groupID, amount)

	if groupID == "" {
		logDepositBranchWarn("fund group rejected", "group id required")
		return CreateDepositResult{}, fmt.Errorf("group id is required")
	}
	if amount <= 0 {
		logDepositBranchWarn("fund group rejected", "amount not positive", "group_id", groupID)
		return CreateDepositResult{}, fmt.Errorf("amount must be positive")
	}

	identity, err := d.privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			logDepositBranchWarn("fund group rejected", "invalid token", "group_id", groupID)
			return CreateDepositResult{}, privy.ErrInvalidToken
		}
		logDepositBranchError("fund group verify session failed", err, "group_id", groupID)
		return CreateDepositResult{}, fmt.Errorf("verify session: %w", err)
	}

	user, found, err := d.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		logDepositBranchError("fund group lookup user failed", err, "group_id", groupID)
		return CreateDepositResult{}, err
	}
	if !found {
		logDepositBranchWarn("fund group rejected", "user not found", "group_id", groupID)
		return CreateDepositResult{}, ErrUserNotFound
	}

	// Faker scale clubs (#153) are read-only: never create a fund intent against a dummy treasury.
	if err := rejectFakerGroup(ctx, d.store, groupID); err != nil {
		logDepositBranchWarn("fund group rejected", "faker group", "group_id", groupID, "user_id", user.ID)
		return CreateDepositResult{}, err
	}

	member, err := d.store.IsGroupMember(ctx, groupID, user.ID)
	if err != nil {
		logDepositBranchError("fund group membership check failed", err, "group_id", groupID, "user_id", user.ID)
		return CreateDepositResult{}, err
	}
	if !member {
		logDepositBranchWarn("fund group rejected", "not group member", "group_id", groupID, "user_id", user.ID)
		return CreateDepositResult{}, ErrNotGroupMember
	}

	_, found, err = d.store.GetTreasuryByGroupID(ctx, groupID)
	if err != nil {
		logDepositBranchError("fund group lookup treasury failed", err, "group_id", groupID, "user_id", user.ID)
		return CreateDepositResult{}, err
	}
	if !found {
		logDepositBranchWarn("fund group rejected", "group not found", "group_id", groupID, "user_id", user.ID)
		return CreateDepositResult{}, ErrGroupNotFound
	}

	wallet, found, err := d.store.GetMemberWalletByUserID(ctx, user.ID)
	if err != nil {
		logDepositBranchError("fund group lookup wallet failed", err, "group_id", groupID, "user_id", user.ID)
		return CreateDepositResult{}, err
	}
	if !found {
		logDepositBranchWarn("fund group rejected", "wallet not found", "group_id", groupID, "user_id", user.ID)
		return CreateDepositResult{}, ErrUserNotFound
	}

	row, available, err := d.reserveFundDeposit(ctx, user.ID, groupID, amount, wallet.SolanaAddress)
	if errors.Is(err, ErrInsufficientPlatformBalance) {
		logDepositBranchWarn("fund group rejected", "insufficient platform balance",
			"group_id", groupID, "user_id", user.ID, "amount", amount, "available", available)
		return CreateDepositResult{}, ErrInsufficientPlatformBalance
	}
	if err != nil {
		logDepositBranchError("fund group reserve failed", err, "group_id", groupID, "user_id", user.ID)
		return CreateDepositResult{}, err
	}

	logDepositCreateSuccess(user.ID, groupID, row.ID, amount)
	return CreateDepositResult{Deposit: depositFromRow(row)}, nil
}

// reserveFundDeposit checks the member's available balance and inserts the pending deposit
// under the member funds lock, so concurrent fund requests cannot both reserve the same USDC.
// Reservations are summed before the chain read: a sweep landing in between then shrinks the
// chain balance without shrinking the reservations, which errs toward rejecting.
func (d *DepositService) reserveFundDeposit(ctx context.Context, userID, groupID string, amount int64, memberAddress string) (postgres.DepositRow, int64, error) {
	tx, err := d.store.BeginTx(ctx)
	if err != nil {
		return postgres.DepositRow{}, 0, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := d.store.LockMemberFundsTx(ctx, tx, userID); err != nil {
		return postgres.DepositRow{}, 0, err
	}
	reserved, err := d.store.SumPendingReservationsByUserIDTx(ctx, tx, userID)
	if err != nil {
		return postgres.DepositRow{}, 0, err
	}
	chainBalance, err := d.privy.MemberUSDCBalance(ctx, memberAddress)
	if err != nil {
		return postgres.DepositRow{}, 0, fmt.Errorf("member usdc balance: %w", err)
	}
	available := chainBalance - reserved
	if available < 0 {
		available = 0
	}
	if amount > available {
		return postgres.DepositRow{}, available, ErrInsufficientPlatformBalance
	}

	row, err := d.store.InsertDepositTx(ctx, tx, userID, groupID, amount, memberAddress)
	if err != nil {
		return postgres.DepositRow{}, 0, err
	}
	if err := tx.Commit(); err != nil {
		return postgres.DepositRow{}, 0, fmt.Errorf("commit fund deposit: %w", err)
	}
	committed = true
	return row, available, nil
}

func (d *DepositService) platformBalanceForWallet(ctx context.Context, userID, memberAddress string) (available int64, pending int64, err error) {
	chainBalance, err := d.privy.MemberUSDCBalance(ctx, memberAddress)
	if err != nil {
		return 0, 0, fmt.Errorf("member usdc balance: %w", err)
	}
	pendingDeposits, err := d.store.SumPendingDepositAmountByUserID(ctx, userID)
	if err != nil {
		return 0, 0, err
	}
	pendingWithdrawals, err := d.store.SumPendingPlatformWithdrawalAmountByUserID(ctx, userID)
	if err != nil {
		return 0, 0, err
	}
	pending = pendingDeposits + pendingWithdrawals
	available = chainBalance - pending
	if available < 0 {
		available = 0
	}
	return available, pending, nil
}

// CreateDeposit records a pending deposit intent for an authenticated group member.
func (d *DepositService) CreateDeposit(ctx context.Context, accessToken string, groupID string, amount int64) (CreateDepositResult, error) {
	logDepositCreateStart(groupID, amount)

	if groupID == "" {
		logDepositBranchWarn("deposit create rejected", "group id required")
		return CreateDepositResult{}, fmt.Errorf("group id is required")
	}
	if amount <= 0 {
		logDepositBranchWarn("deposit create rejected", "amount not positive", "group_id", groupID)
		return CreateDepositResult{}, fmt.Errorf("amount must be positive")
	}

	identity, err := d.privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			logDepositBranchWarn("deposit create rejected", "invalid token", "group_id", groupID)
			return CreateDepositResult{}, privy.ErrInvalidToken
		}
		logDepositBranchError("deposit create verify session failed", err, "group_id", groupID)
		return CreateDepositResult{}, fmt.Errorf("verify session: %w", err)
	}

	user, found, err := d.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		logDepositBranchError("deposit create lookup user failed", err, "group_id", groupID)
		return CreateDepositResult{}, err
	}
	if !found {
		logDepositBranchWarn("deposit create rejected", "user not found", "group_id", groupID)
		return CreateDepositResult{}, ErrUserNotFound
	}

	group, found, err := d.store.GetGroupByID(ctx, groupID)
	if err != nil {
		logDepositBranchError("deposit create lookup group failed", err, "group_id", groupID, "user_id", user.ID)
		return CreateDepositResult{}, err
	}
	if !found {
		logDepositBranchWarn("deposit create rejected", "group not found", "group_id", groupID, "user_id", user.ID)
		return CreateDepositResult{}, ErrGroupNotFound
	}
	if group.IsFaker {
		logDepositBranchWarn("deposit create rejected", "faker group", "group_id", groupID, "user_id", user.ID)
		return CreateDepositResult{}, ErrFakerGroupReadOnly
	}
	member, err := d.store.IsGroupMember(ctx, group.ID, user.ID)
	if err != nil {
		logDepositBranchError("deposit create membership check failed", err, "group_id", groupID, "user_id", user.ID)
		return CreateDepositResult{}, err
	}
	if !member {
		logDepositBranchWarn("deposit create rejected", "not group member", "group_id", groupID, "user_id", user.ID)
		return CreateDepositResult{}, ErrNotGroupMember
	}

	wallet, found, err := d.store.GetMemberWalletByUserID(ctx, user.ID)
	if err != nil {
		logDepositBranchError("deposit create lookup wallet failed", err, "group_id", groupID, "user_id", user.ID)
		return CreateDepositResult{}, err
	}
	if !found {
		logDepositBranchWarn("deposit create rejected", "wallet not found", "group_id", groupID, "user_id", user.ID)
		return CreateDepositResult{}, ErrUserNotFound
	}

	row, err := d.store.InsertDeposit(ctx, user.ID, groupID, amount, wallet.SolanaAddress)
	if err != nil {
		logDepositBranchError("deposit create insert failed", err, "group_id", groupID, "user_id", user.ID)
		return CreateDepositResult{}, err
	}

	logDepositCreateSuccess(user.ID, groupID, row.ID, amount)
	return CreateDepositResult{Deposit: depositFromRow(row)}, nil
}

// ObserveSweep credits position share units on confirmed treasury arrival, idempotent on signature.
func (d *DepositService) ObserveSweep(ctx context.Context, sweep ObservedSweep) (ObserveSweepResult, error) {
	logDepositObserveSweepStart(sweep.DepositID, sweep.GroupID, sweep.UserID, sweep.Amount, sweep.TxSignature)

	if sweep.TxSignature == "" {
		logDepositBranchWarn("deposit observe sweep rejected", "tx signature required", "deposit_id", sweep.DepositID)
		return ObserveSweepResult{}, fmt.Errorf("tx signature is required")
	}
	if sweep.DepositID == "" {
		logDepositBranchWarn("deposit observe sweep rejected", "deposit id required")
		return ObserveSweepResult{}, fmt.Errorf("deposit id is required")
	}
	if sweep.Amount <= 0 {
		logDepositBranchWarn("deposit observe sweep rejected", "amount not positive", "deposit_id", sweep.DepositID)
		return ObserveSweepResult{}, fmt.Errorf("amount must be positive")
	}

	if err := rejectFakerGroup(ctx, d.store, sweep.GroupID); err != nil {
		logDepositBranchWarn("deposit observe sweep rejected", "faker group", "deposit_id", sweep.DepositID, "group_id", sweep.GroupID)
		return ObserveSweepResult{}, err
	}

	treasury, found, err := d.store.GetTreasuryByGroupID(ctx, sweep.GroupID)
	if err != nil {
		logDepositBranchError("deposit observe sweep lookup treasury failed", err, "deposit_id", sweep.DepositID, "group_id", sweep.GroupID)
		return ObserveSweepResult{}, err
	}
	if !found {
		logDepositBranchWarn("deposit observe sweep rejected", "group not found", "deposit_id", sweep.DepositID, "group_id", sweep.GroupID)
		return ObserveSweepResult{}, ErrGroupNotFound
	}
	if sweep.ToAddress != treasury.SolanaAddress {
		logDepositBranchWarn("deposit observe sweep rejected", "invalid sweep target", "deposit_id", sweep.DepositID, "group_id", sweep.GroupID)
		return ObserveSweepResult{}, ErrInvalidSweepTarget
	}

	if existing, found, err := d.store.GetDepositByTxSignature(ctx, sweep.TxSignature); err != nil {
		logDepositBranchError("deposit observe sweep lookup by signature failed", err, "deposit_id", sweep.DepositID, "tx_signature", sweep.TxSignature)
		return ObserveSweepResult{}, err
	} else if found {
		logDepositObserveSweepIdempotent(sweep.DepositID, sweep.TxSignature)
		position, hasPosition, err := d.store.GetPosition(ctx, existing.UserID, existing.GroupID)
		if err != nil {
			logDepositBranchError("deposit observe sweep load position failed", err, "deposit_id", sweep.DepositID)
			return ObserveSweepResult{}, err
		}
		if !hasPosition {
			position = postgres.PositionRow{UserID: existing.UserID, GroupID: existing.GroupID}
		}
		return ObserveSweepResult{
			Deposit:  depositFromRow(existing),
			Position: positionFromRowPostgres(position),
			Credited: false,
		}, nil
	}

	depositRow, found, err := d.store.GetDepositByID(ctx, sweep.DepositID)
	if err != nil {
		logDepositBranchError("deposit observe sweep lookup deposit failed", err,
			"deposit_id", sweep.DepositID, "group_id", sweep.GroupID)
		return ObserveSweepResult{}, err
	}
	if !found {
		logDepositBranchWarn("deposit observe sweep rejected", "deposit not found",
			"deposit_id", sweep.DepositID, "group_id", sweep.GroupID)
		return ObserveSweepResult{}, ErrDepositNotFound
	}
	if depositRow.FromAddress != sweep.FromAddress {
		logDepositBranchWarn("deposit observe sweep rejected", "from address mismatch",
			"deposit_id", sweep.DepositID, "group_id", sweep.GroupID,
			"expected_from", depositRow.FromAddress, "actual_from", sweep.FromAddress)
		return ObserveSweepResult{}, fmt.Errorf("from address mismatch")
	}
	if depositRow.Amount != sweep.Amount {
		logDepositBranchWarn("deposit observe sweep rejected", "amount mismatch",
			"deposit_id", sweep.DepositID, "group_id", sweep.GroupID,
			"expected_amount", depositRow.Amount, "actual_amount", sweep.Amount)
		return ObserveSweepResult{}, fmt.Errorf("amount mismatch")
	}

	tx, err := d.store.BeginTx(ctx)
	if err != nil {
		logDepositBranchError("deposit observe sweep begin tx failed", err,
			"deposit_id", sweep.DepositID, "group_id", sweep.GroupID)
		return ObserveSweepResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	confirmed, newlyConfirmed, err := d.store.ConfirmDepositTx(ctx, tx, sweep.DepositID, sweep.TxSignature)
	if err != nil {
		logDepositBranchError("deposit observe sweep confirm deposit failed", err,
			"deposit_id", sweep.DepositID, "group_id", sweep.GroupID, "tx_signature", sweep.TxSignature)
		return ObserveSweepResult{}, err
	}

	var positionRow postgres.PositionRow
	if newlyConfirmed {
		shareUnits, navAfterCredit, err := d.shareCreditForSweep(ctx, tx, sweep.GroupID, treasury.SolanaAddress, sweep.Amount)
		if err != nil {
			if errors.Is(err, ErrPotMarkUnavailable) {
				// Rolling back leaves the deposit pending, so the poller prices it again next tick.
				logPotMarkUnavailable(sweep.GroupID, "deposit_credit", err)
			}
			logDepositBranchError("deposit observe sweep share credit failed", err,
				"deposit_id", sweep.DepositID, "group_id", sweep.GroupID, "amount", sweep.Amount)
			return ObserveSweepResult{}, err
		}

		positionRow, err = d.store.IncrementPositionTx(ctx, tx, sweep.UserID, sweep.GroupID, shareUnits, sweep.Amount)
		if err != nil {
			logDepositBranchError("deposit observe sweep increment position failed", err,
				"deposit_id", sweep.DepositID, "group_id", sweep.GroupID, "user_id", sweep.UserID)
			return ObserveSweepResult{}, err
		}

		if err := d.store.WriteNavSnapshotOnDepositConfirmTx(ctx, tx, sweep.GroupID, navAfterCredit); err != nil {
			logDepositBranchError("deposit observe sweep nav snapshot failed", err,
				"deposit_id", sweep.DepositID, "group_id", sweep.GroupID)
			return ObserveSweepResult{}, err
		}

		slog.Info("share credit applied",
			"group_id", sweep.GroupID,
			"user_id", sweep.UserID,
			"deposit_id", sweep.DepositID,
			"tx_signature", sweep.TxSignature,
			"share_units", positionRow.ShareUnits,
			"amount_deposited", positionRow.AmountDeposited,
		)
	} else {
		var hasPosition bool
		positionRow, hasPosition, err = d.store.GetPositionTx(ctx, tx, sweep.UserID, sweep.GroupID)
		if err != nil {
			return ObserveSweepResult{}, err
		}
		if !hasPosition {
			positionRow = postgres.PositionRow{UserID: sweep.UserID, GroupID: sweep.GroupID}
		}
	}

	if err := tx.Commit(); err != nil {
		logDepositBranchError("deposit observe sweep commit failed", err, "deposit_id", sweep.DepositID)
		return ObserveSweepResult{}, fmt.Errorf("commit observe sweep: %w", err)
	}
	committed = true

	logDepositObserveSweepConfirmed(sweep.DepositID, sweep.GroupID, sweep.UserID, sweep.TxSignature, newlyConfirmed)
	if newlyConfirmed {
		telemetry.MoneyMoved(telemetry.EventDepositSweep, confirmed.Amount)
	}
	return ObserveSweepResult{
		Deposit:  depositFromRow(confirmed),
		Position: positionFromRowPostgres(positionRow),
		Credited: newlyConfirmed,
	}, nil
}

func depositFromRow(row postgres.DepositRow) Deposit {
	deposit := Deposit{
		ID:          row.ID,
		UserID:      row.UserID,
		GroupID:     row.GroupID,
		Amount:      row.Amount,
		FromAddress: row.FromAddress,
		Status:      DepositStatus(row.Status),
		CreatedAt:   row.CreatedAt,
	}
	if row.TxSignature.Valid {
		deposit.TxSignature = row.TxSignature.String
	}
	return deposit
}

func positionFromRowPostgres(row postgres.PositionRow) Position {
	return Position{
		UserID:          row.UserID,
		GroupID:         row.GroupID,
		ShareUnits:      row.ShareUnits,
		AmountDeposited: row.AmountDeposited,
		AmountWithdrawn: row.AmountWithdrawn,
	}
}

// GetDeposit returns a deposit by id for authenticated owner.
func (d *DepositService) GetDeposit(ctx context.Context, accessToken, depositID string) (Deposit, Position, error) {
	identity, err := d.privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			return Deposit{}, Position{}, privy.ErrInvalidToken
		}
		return Deposit{}, Position{}, fmt.Errorf("verify session: %w", err)
	}

	user, found, err := d.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		return Deposit{}, Position{}, err
	}
	if !found {
		return Deposit{}, Position{}, ErrUserNotFound
	}

	row, found, err := d.store.GetDepositByID(ctx, depositID)
	if err != nil {
		return Deposit{}, Position{}, err
	}
	if !found {
		return Deposit{}, Position{}, ErrDepositNotFound
	}
	if row.UserID != user.ID {
		// Members read group deposits; any authed user may read faker scale club deposits (#153).
		readable, err := d.store.CanReadGroup(ctx, row.GroupID, user.ID)
		if err != nil {
			return Deposit{}, Position{}, err
		}
		if !readable {
			return Deposit{}, Position{}, ErrDepositNotFound
		}
	}

	position := Position{UserID: user.ID, GroupID: row.GroupID}
	if row.UserID == user.ID {
		positionRow, hasPosition, err := d.store.GetPosition(ctx, user.ID, row.GroupID)
		if err != nil {
			return Deposit{}, Position{}, err
		}
		if hasPosition {
			position = positionFromRowPostgres(positionRow)
		}
	}

	return depositFromRow(row), position, nil
}

// GetMemberPosition returns share units for the authenticated user in a group.
func (d *DepositService) GetMemberPosition(ctx context.Context, accessToken, groupID string) (Position, error) {
	identity, err := d.privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			return Position{}, privy.ErrInvalidToken
		}
		return Position{}, fmt.Errorf("verify session: %w", err)
	}

	user, found, err := d.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		return Position{}, err
	}
	if !found {
		return Position{}, ErrUserNotFound
	}

	positionRow, hasPosition, err := d.store.GetPosition(ctx, user.ID, groupID)
	if err != nil {
		return Position{}, err
	}
	if !hasPosition {
		return Position{UserID: user.ID, GroupID: groupID}, nil
	}
	return positionFromRowPostgres(positionRow), nil
}

// GetTreasuryUSDCBalance returns treasury USDC balance via Privy for read API.
func (d *DepositService) GetTreasuryUSDCBalance(ctx context.Context, accessToken, groupID string) (int64, string, error) {
	identity, err := d.privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			return 0, "", privy.ErrInvalidToken
		}
		return 0, "", fmt.Errorf("verify session: %w", err)
	}

	user, found, err := d.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		return 0, "", err
	}
	if !found {
		return 0, "", ErrUserNotFound
	}

	group, found, err := d.store.GetGroupByID(ctx, groupID)
	if err != nil {
		return 0, "", err
	}
	// Faker treasuries are dummy rows (#153): never read them through Privy.
	if !found || group.IsFaker {
		return 0, "", ErrGroupNotFound
	}
	member, err := d.store.IsGroupMember(ctx, group.ID, user.ID)
	if err != nil {
		return 0, "", err
	}
	if !member {
		return 0, "", ErrGroupNotFound
	}

	treasury, found, err := d.store.GetTreasuryByGroupID(ctx, groupID)
	if err != nil {
		return 0, "", err
	}
	if !found {
		return 0, "", ErrGroupNotFound
	}

	balance, err := d.privy.TreasuryUSDCBalance(ctx, treasury.SolanaAddress)
	if err != nil {
		return 0, "", err
	}
	return balance, treasury.SolanaAddress, nil
}

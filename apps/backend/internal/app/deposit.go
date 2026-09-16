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
)

// DepositStatus is the lifecycle state of a deposit intent.
type DepositStatus string

const (
	DepositStatusPending   DepositStatus = "pending"
	DepositStatusConfirmed DepositStatus = "confirmed"
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

// ErrInvalidSweepTarget means the sweep did not arrive at group treasury.
var ErrInvalidSweepTarget = errors.New("invalid sweep target")

// DepositService orchestrates deposit create and sweep credit flows.
type DepositService struct {
	store *postgres.Store
	privy privy.Client
	pyth  pyth.Client
}

// NewDepositService wires deposit dependencies.
func NewDepositService(store *postgres.Store, privyClient privy.Client, pythClient pyth.Client) *DepositService {
	return &DepositService{
		store: store,
		privy: privyClient,
		pyth:  pythClient,
	}
}

// CreateDeposit records a pending deposit intent for an authenticated group member.
func (d *DepositService) CreateDeposit(ctx context.Context, accessToken string, groupID string, amount int64) (CreateDepositResult, error) {
	if groupID == "" {
		return CreateDepositResult{}, fmt.Errorf("group id is required")
	}
	if amount <= 0 {
		return CreateDepositResult{}, fmt.Errorf("amount must be positive")
	}

	identity, err := d.privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			return CreateDepositResult{}, privy.ErrInvalidToken
		}
		return CreateDepositResult{}, fmt.Errorf("verify session: %w", err)
	}

	user, found, err := d.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		return CreateDepositResult{}, err
	}
	if !found {
		return CreateDepositResult{}, ErrUserNotFound
	}

	group, found, err := d.store.GetGroupByID(ctx, groupID)
	if err != nil {
		return CreateDepositResult{}, err
	}
	if !found {
		return CreateDepositResult{}, ErrGroupNotFound
	}
	if group.CreatorUserID != user.ID {
		return CreateDepositResult{}, ErrNotGroupMember
	}

	wallet, found, err := d.store.GetMemberWalletByUserID(ctx, user.ID)
	if err != nil {
		return CreateDepositResult{}, err
	}
	if !found {
		return CreateDepositResult{}, ErrUserNotFound
	}

	row, err := d.store.InsertDeposit(ctx, user.ID, groupID, amount, wallet.SolanaAddress)
	if err != nil {
		return CreateDepositResult{}, err
	}

	return CreateDepositResult{Deposit: depositFromRow(row)}, nil
}

// ObserveSweep credits position share units on confirmed treasury arrival, idempotent on signature.
func (d *DepositService) ObserveSweep(ctx context.Context, sweep ObservedSweep) (ObserveSweepResult, error) {
	if sweep.TxSignature == "" {
		return ObserveSweepResult{}, fmt.Errorf("tx signature is required")
	}
	if sweep.DepositID == "" {
		return ObserveSweepResult{}, fmt.Errorf("deposit id is required")
	}
	if sweep.Amount <= 0 {
		return ObserveSweepResult{}, fmt.Errorf("amount must be positive")
	}

	treasury, found, err := d.store.GetTreasuryByGroupID(ctx, sweep.GroupID)
	if err != nil {
		return ObserveSweepResult{}, err
	}
	if !found {
		return ObserveSweepResult{}, ErrGroupNotFound
	}
	if sweep.ToAddress != treasury.SolanaAddress {
		return ObserveSweepResult{}, ErrInvalidSweepTarget
	}

	if existing, found, err := d.store.GetDepositByTxSignature(ctx, sweep.TxSignature); err != nil {
		return ObserveSweepResult{}, err
	} else if found {
		position, hasPosition, err := d.store.GetPosition(ctx, existing.UserID, existing.GroupID)
		if err != nil {
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
		return ObserveSweepResult{}, err
	}
	if !found {
		return ObserveSweepResult{}, ErrDepositNotFound
	}
	if depositRow.FromAddress != sweep.FromAddress {
		return ObserveSweepResult{}, fmt.Errorf("from address mismatch")
	}
	if depositRow.Amount != sweep.Amount {
		return ObserveSweepResult{}, fmt.Errorf("amount mismatch")
	}

	tx, err := d.store.BeginTx(ctx)
	if err != nil {
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
		return ObserveSweepResult{}, err
	}

	var positionRow postgres.PositionRow
	if newlyConfirmed {
		shareUnits, err := d.shareCreditForSweep(ctx, sweep.GroupID, treasury.SolanaAddress, sweep.Amount)
		if err != nil {
			return ObserveSweepResult{}, err
		}

		positionRow, err = d.store.IncrementPositionTx(ctx, tx, sweep.UserID, sweep.GroupID, shareUnits, sweep.Amount)
		if err != nil {
			return ObserveSweepResult{}, err
		}

		treasuryUsdc, err := d.privy.TreasuryUSDCBalance(ctx, treasury.SolanaAddress)
		if err != nil {
			return ObserveSweepResult{}, fmt.Errorf("treasury usdc balance: %w", err)
		}
		if err := d.store.WriteNavSnapshotOnDepositConfirmTx(ctx, tx, sweep.GroupID, treasuryUsdc); err != nil {
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
		return ObserveSweepResult{}, fmt.Errorf("commit observe sweep: %w", err)
	}
	committed = true

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
	if !found || row.UserID != user.ID {
		return Deposit{}, Position{}, ErrDepositNotFound
	}

	positionRow, hasPosition, err := d.store.GetPosition(ctx, user.ID, row.GroupID)
	if err != nil {
		return Deposit{}, Position{}, err
	}
	position := Position{UserID: user.ID, GroupID: row.GroupID}
	if hasPosition {
		position = positionFromRowPostgres(positionRow)
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
	if !found || group.CreatorUserID != user.ID {
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

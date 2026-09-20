package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/auth"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
)

// Confirmer checks whether an on-chain transaction reached confirmed status.
type Confirmer interface {
	IsConfirmed(ctx context.Context, txHash string) (bool, error)
}

// PlatformWithdrawalStatus is the lifecycle state of a platform withdrawal intent.
type PlatformWithdrawalStatus string

const (
	PlatformWithdrawalStatusPending   PlatformWithdrawalStatus = "pending"
	PlatformWithdrawalStatusConfirmed PlatformWithdrawalStatus = "confirmed"
	PlatformWithdrawalStatusFailed    PlatformWithdrawalStatus = "failed"
)

// PlatformWithdrawal is a user-initiated USDC send from member wallet to an external address.
type PlatformWithdrawal struct {
	ID        string
	UserID    string
	Amount    int64
	ToAddress string
	Status    PlatformWithdrawalStatus
	TxHash    string
	CreatedAt time.Time
}

// ErrPlatformWithdrawalNotFound means the platform withdrawal row does not exist.
var ErrPlatformWithdrawalNotFound = errors.New("platform withdrawal not found")

// ErrPendingPlatformWithdrawal means the user already has an in-flight platform withdrawal.
var ErrPendingPlatformWithdrawal = errors.New("pending platform withdrawal exists")

// ErrInvalidWithdrawAddress means the destination address failed validation.
var ErrInvalidWithdrawAddress = errors.New("invalid withdraw address")

// ErrWithdrawToMemberWallet means the user tried to send USDC to their own member wallet.
var ErrWithdrawToMemberWallet = errors.New("cannot withdraw to member wallet")

// PlatformWithdrawService orchestrates member-wallet USDC withdrawals.
type PlatformWithdrawService struct {
	store     *postgres.Store
	auth      auth.Verifier
	wallets   wallets.Client
	deposits  *DepositService
	confirmer Confirmer
}

// NewPlatformWithdrawService wires platform withdrawal dependencies.
func NewPlatformWithdrawService(
	store *postgres.Store,
	verifier auth.Verifier,
	walletClient wallets.Client,
	deposits *DepositService,
	confirmer Confirmer,
) *PlatformWithdrawService {
	return &PlatformWithdrawService{
		store:     store,
		auth:      verifier,
		wallets:   walletClient,
		deposits:  deposits,
		confirmer: confirmer,
	}
}

// CreatePlatformWithdrawal validates balance, broadcasts a member-wallet USDC transfer, and records the intent.
func (s *PlatformWithdrawService) CreatePlatformWithdrawal(ctx context.Context, accessToken string, amount int64, toAddress string) (PlatformWithdrawal, error) {
	toAddress = strings.TrimSpace(toAddress)
	if amount <= 0 {
		return PlatformWithdrawal{}, fmt.Errorf("amount must be positive")
	}
	if err := wallets.ValidateAddress(toAddress); err != nil {
		return PlatformWithdrawal{}, ErrInvalidWithdrawAddress
	}

	identity, err := s.auth.VerifySession(ctx, auth.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			return PlatformWithdrawal{}, auth.ErrUnauthorized
		}
		return PlatformWithdrawal{}, fmt.Errorf("verify session: %w", err)
	}

	user, found, err := s.store.GetUserByDynamicUserID(ctx, identity.DynamicUserID)
	if err != nil {
		return PlatformWithdrawal{}, err
	}
	if !found {
		return PlatformWithdrawal{}, ErrUserNotFound
	}

	wallet, found, err := s.store.GetMemberWalletByUserID(ctx, user.ID)
	if err != nil {
		return PlatformWithdrawal{}, err
	}
	if !found {
		return PlatformWithdrawal{}, ErrUserNotFound
	}
	if strings.EqualFold(toAddress, wallet.Address) {
		return PlatformWithdrawal{}, ErrWithdrawToMemberWallet
	}

	hasPending, err := s.store.HasPendingPlatformWithdrawalForUser(ctx, user.ID)
	if err != nil {
		return PlatformWithdrawal{}, err
	}
	if hasPending {
		return PlatformWithdrawal{}, ErrPendingPlatformWithdrawal
	}

	balance, err := s.deposits.GetPlatformBalance(ctx, accessToken)
	if err != nil {
		return PlatformWithdrawal{}, err
	}
	if amount > balance.AvailableUsdcMicros {
		return PlatformWithdrawal{}, ErrInsufficientPlatformBalance
	}

	row, err := s.store.InsertPlatformWithdrawal(ctx, user.ID, amount, toAddress)
	if err != nil {
		return PlatformWithdrawal{}, err
	}

	transferReq := wallets.TransferRequest{
		MemberAddress: wallet.Address,
		ToAddress:     toAddress,
		Amount:        amount,
		IntentID:      row.ID,
	}

	result, err := s.wallets.SubmitMemberUSDCTransfer(ctx, transferReq)
	if err != nil {
		_, _, _ = s.store.FailPlatformWithdrawal(ctx, row.ID, "broadcast failed")
		return PlatformWithdrawal{}, fmt.Errorf("submit transfer: %w", err)
	}

	if existing, found, err := s.store.GetPlatformWithdrawalByTxHash(ctx, result.TxHash); err != nil {
		_, _, _ = s.store.FailPlatformWithdrawal(ctx, row.ID, "signature lookup failed")
		return PlatformWithdrawal{}, err
	} else if found && existing.ID != row.ID {
		_, _, _ = s.store.FailPlatformWithdrawal(ctx, row.ID, "duplicate tx signature")
		return platformWithdrawalFromRow(existing), nil
	}

	if err := s.store.SetPlatformWithdrawalBroadcastSignature(ctx, row.ID, result.TxHash); err != nil {
		_, _, _ = s.store.FailPlatformWithdrawal(ctx, row.ID, "persist signature failed")
		return PlatformWithdrawal{}, fmt.Errorf("persist broadcast signature: %w", err)
	}

	slog.Info("platform withdrawal broadcast",
		"user_id", user.ID,
		"amount", amount,
		"to_address", toAddress,
		"tx_hash", result.TxHash,
		"withdrawal_id", row.ID,
	)

	withdrawal, err := s.refreshConfirmation(ctx, row.ID, result.TxHash)
	if err != nil {
		return PlatformWithdrawal{}, err
	}
	return withdrawal, nil
}

// GetPlatformWithdrawal returns a platform withdrawal for the authenticated owner, refreshing confirmation when pending.
func (s *PlatformWithdrawService) GetPlatformWithdrawal(ctx context.Context, accessToken, withdrawalID string) (PlatformWithdrawal, error) {
	identity, err := s.auth.VerifySession(ctx, auth.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			return PlatformWithdrawal{}, auth.ErrUnauthorized
		}
		return PlatformWithdrawal{}, fmt.Errorf("verify session: %w", err)
	}

	user, found, err := s.store.GetUserByDynamicUserID(ctx, identity.DynamicUserID)
	if err != nil {
		return PlatformWithdrawal{}, err
	}
	if !found {
		return PlatformWithdrawal{}, ErrUserNotFound
	}

	row, found, err := s.store.GetPlatformWithdrawalByID(ctx, withdrawalID)
	if err != nil {
		return PlatformWithdrawal{}, err
	}
	if !found || row.UserID != user.ID {
		return PlatformWithdrawal{}, ErrPlatformWithdrawalNotFound
	}

	if row.Status == string(PlatformWithdrawalStatusPending) && row.TxHash.Valid && row.TxHash.String != "" {
		return s.refreshConfirmation(ctx, row.ID, row.TxHash.String)
	}
	return platformWithdrawalFromRow(row), nil
}

func (s *PlatformWithdrawService) refreshConfirmation(ctx context.Context, withdrawalID, txHash string) (PlatformWithdrawal, error) {
	if s.confirmer == nil {
		row, found, err := s.store.GetPlatformWithdrawalByID(ctx, withdrawalID)
		if err != nil {
			return PlatformWithdrawal{}, err
		}
		if !found {
			return PlatformWithdrawal{}, ErrPlatformWithdrawalNotFound
		}
		return platformWithdrawalFromRow(row), nil
	}

	confirmed, err := s.confirmer.IsConfirmed(ctx, txHash)
	if err != nil {
		return PlatformWithdrawal{}, fmt.Errorf("confirm withdrawal tx: %w", err)
	}
	if !confirmed {
		row, found, err := s.store.GetPlatformWithdrawalByID(ctx, withdrawalID)
		if err != nil {
			return PlatformWithdrawal{}, err
		}
		if !found {
			return PlatformWithdrawal{}, ErrPlatformWithdrawalNotFound
		}
		return platformWithdrawalFromRow(row), nil
	}

	row, newlyConfirmed, err := s.store.ConfirmPlatformWithdrawal(ctx, withdrawalID, txHash)
	if err != nil {
		return PlatformWithdrawal{}, err
	}
	if newlyConfirmed {
		slog.Info("platform withdrawal confirmed",
			"user_id", row.UserID,
			"amount", row.Amount,
			"to_address", row.ToAddress,
			"tx_hash", txHash,
			"withdrawal_id", row.ID,
		)
	}
	return platformWithdrawalFromRow(row), nil
}

func platformWithdrawalFromRow(row postgres.PlatformWithdrawalRow) PlatformWithdrawal {
	withdrawal := PlatformWithdrawal{
		ID:        row.ID,
		UserID:    row.UserID,
		Amount:    row.Amount,
		ToAddress: row.ToAddress,
		Status:    PlatformWithdrawalStatus(row.Status),
		CreatedAt: row.CreatedAt,
	}
	if row.TxHash.Valid {
		withdrawal.TxHash = row.TxHash.String
	}
	return withdrawal
}

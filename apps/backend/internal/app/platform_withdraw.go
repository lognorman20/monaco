package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

// SolanaConfirmer checks whether an on-chain transaction reached confirmed/finalized status.
type SolanaConfirmer interface {
	IsConfirmed(ctx context.Context, txSignature string) (bool, error)
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
	ID          string
	UserID      string
	Amount      int64
	ToAddress   string
	Status      PlatformWithdrawalStatus
	TxSignature string
	CreatedAt   time.Time
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
	store      *postgres.Store
	privy      privy.Client
	deposits   *DepositService
	solana     SolanaConfirmer
	relayerKey string
}

// NewPlatformWithdrawService wires platform withdrawal dependencies.
func NewPlatformWithdrawService(
	store *postgres.Store,
	privyClient privy.Client,
	deposits *DepositService,
	solana SolanaConfirmer,
	relayerKey string,
) *PlatformWithdrawService {
	return &PlatformWithdrawService{
		store:      store,
		privy:      privyClient,
		deposits:   deposits,
		solana:     solana,
		relayerKey: relayerKey,
	}
}

// CreatePlatformWithdrawal validates balance, broadcasts a member-wallet USDC transfer, and records the intent.
func (s *PlatformWithdrawService) CreatePlatformWithdrawal(ctx context.Context, accessToken string, amount int64, toAddress string) (PlatformWithdrawal, error) {
	toAddress = strings.TrimSpace(toAddress)
	if amount <= 0 {
		return PlatformWithdrawal{}, fmt.Errorf("amount must be positive")
	}
	if err := privy.ValidateSolanaAddress(toAddress); err != nil {
		return PlatformWithdrawal{}, ErrInvalidWithdrawAddress
	}

	identity, err := s.privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			return PlatformWithdrawal{}, privy.ErrInvalidToken
		}
		return PlatformWithdrawal{}, fmt.Errorf("verify session: %w", err)
	}

	user, found, err := s.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
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
	if strings.EqualFold(toAddress, wallet.SolanaAddress) {
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

	transferReq, err := privy.BuildTransferRequest(wallet.SolanaAddress, toAddress, amount, s.relayerKey)
	if err != nil {
		_, _, _ = s.store.FailPlatformWithdrawal(ctx, row.ID, "build transfer")
		return PlatformWithdrawal{}, fmt.Errorf("build transfer: %w", err)
	}

	result, err := s.privy.SubmitMemberUSDCTransfer(ctx, transferReq)
	if err != nil {
		_, _, _ = s.store.FailPlatformWithdrawal(ctx, row.ID, "broadcast failed")
		return PlatformWithdrawal{}, fmt.Errorf("submit transfer: %w", err)
	}

	if existing, found, err := s.store.GetPlatformWithdrawalByTxSignature(ctx, result.TxSignature); err != nil {
		_, _, _ = s.store.FailPlatformWithdrawal(ctx, row.ID, "signature lookup failed")
		return PlatformWithdrawal{}, err
	} else if found && existing.ID != row.ID {
		return platformWithdrawalFromRow(existing), nil
	}

	if err := s.store.SetPlatformWithdrawalBroadcastSignature(ctx, row.ID, result.TxSignature); err != nil {
		return PlatformWithdrawal{}, err
	}

	slog.Info("platform withdrawal broadcast",
		"user_id", user.ID,
		"amount", amount,
		"to_address", toAddress,
		"tx_signature", result.TxSignature,
		"withdrawal_id", row.ID,
	)

	withdrawal, err := s.refreshConfirmation(ctx, row.ID, result.TxSignature)
	if err != nil {
		return PlatformWithdrawal{}, err
	}
	return withdrawal, nil
}

// GetPlatformWithdrawal returns a platform withdrawal for the authenticated owner, refreshing confirmation when pending.
func (s *PlatformWithdrawService) GetPlatformWithdrawal(ctx context.Context, accessToken, withdrawalID string) (PlatformWithdrawal, error) {
	identity, err := s.privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			return PlatformWithdrawal{}, privy.ErrInvalidToken
		}
		return PlatformWithdrawal{}, fmt.Errorf("verify session: %w", err)
	}

	user, found, err := s.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
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

	if row.Status == string(PlatformWithdrawalStatusPending) && row.TxSignature.Valid && row.TxSignature.String != "" {
		return s.refreshConfirmation(ctx, row.ID, row.TxSignature.String)
	}
	return platformWithdrawalFromRow(row), nil
}

func (s *PlatformWithdrawService) refreshConfirmation(ctx context.Context, withdrawalID, txSignature string) (PlatformWithdrawal, error) {
	if s.solana == nil {
		row, found, err := s.store.GetPlatformWithdrawalByID(ctx, withdrawalID)
		if err != nil {
			return PlatformWithdrawal{}, err
		}
		if !found {
			return PlatformWithdrawal{}, ErrPlatformWithdrawalNotFound
		}
		return platformWithdrawalFromRow(row), nil
	}

	confirmed, err := s.solana.IsConfirmed(ctx, txSignature)
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

	row, newlyConfirmed, err := s.store.ConfirmPlatformWithdrawal(ctx, withdrawalID, txSignature)
	if err != nil {
		return PlatformWithdrawal{}, err
	}
	if newlyConfirmed {
		slog.Info("platform withdrawal confirmed",
			"user_id", row.UserID,
			"amount", row.Amount,
			"to_address", row.ToAddress,
			"tx_signature", txSignature,
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
	if row.TxSignature.Valid {
		withdrawal.TxSignature = row.TxSignature.String
	}
	return withdrawal
}

package app

import (
	"context"
	"fmt"
	"strconv"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

// DevExecuteBuyRequest is input for the M3 dev-only buy execute path.
type DevExecuteBuyRequest struct {
	GroupID    string
	UserID     string
	Symbol     string
	USDCAmount int64
}

// DevExecuteBuyResult is the persisted confirmed buy transaction.
type DevExecuteBuyResult struct {
	Transaction postgres.TransactionRow
	Created       bool
}

// SellToUSDCRequest sells treasury xStock back to USDC.
type SellToUSDCRequest struct {
	GroupID   string
	UserID    string
	Symbol    string
	InputMint string
	Amount    int64
}

// SellToUSDCResult is the persisted confirmed sell transaction.
type SellToUSDCResult struct {
	Transaction postgres.TransactionRow
	Created       bool
}

// TreasuryBalances tracks fake treasury token balances for integration tests.
type TreasuryBalances struct {
	USDC   int64
	XStock int64
}

// SwapService orchestrates Jupiter buy and sell flows for group treasuries.
type SwapService struct {
	store      *postgres.Store
	buy        *BuyService
	jupiter    jupiter.Client
	privy      privy.Client
	signer     TreasurySigner
	balances   map[string]TreasuryBalances
	pollConfig jupiter.PollConfig
}

// NewSwapService wires swap dependencies.
func NewSwapService(
	store *postgres.Store,
	buy *BuyService,
	jupiterClient jupiter.Client,
	privyClient privy.Client,
	signer TreasurySigner,
) *SwapService {
	return &SwapService{
		store:    store,
		buy:      buy,
		jupiter:  jupiterClient,
		privy:    privyClient,
		signer:   signer,
		balances: make(map[string]TreasuryBalances),
	}
}

// SetPollConfigForTests configures execute polling for integration tests.
func (s *SwapService) SetPollConfigForTests(cfg jupiter.PollConfig) {
	s.pollConfig = cfg
}

func (s *SwapService) pollCfg() jupiter.PollConfig {
	if s.pollConfig.MaxAttempts > 0 {
		return s.pollConfig
	}
	return jupiter.DefaultPollConfig()
}

// SetTreasuryBalances seeds treasury token balances for integration tests.
func (s *SwapService) SetTreasuryBalances(treasuryAddress string, balances TreasuryBalances) {
	s.balances[treasuryAddress] = balances
}

// TreasuryBalancesFor returns tracked treasury balances for tests.
func (s *SwapService) TreasuryBalancesFor(treasuryAddress string) TreasuryBalances {
	if balances, ok := s.balances[treasuryAddress]; ok {
		return balances
	}
	return TreasuryBalances{}
}

// DevExecuteBuy quotes, signs, executes, polls, and persists a treasury buy.
func (s *SwapService) DevExecuteBuy(ctx context.Context, req DevExecuteBuyRequest) (DevExecuteBuyResult, error) {
	start, err := s.buy.StartBuy(ctx, StartBuyRequest{
		GroupID:    req.GroupID,
		UserID:     req.UserID,
		Symbol:     req.Symbol,
		USDCAmount: req.USDCAmount,
	})
	if err != nil {
		return DevExecuteBuyResult{}, err
	}

	treasury, err := s.privy.EnsureTreasury(ctx, privy.GroupID(req.GroupID))
	if err != nil {
		return DevExecuteBuyResult{}, err
	}

	order, err := s.jupiter.OrderBuy(ctx, jupiter.OrderBuyParams{
		GroupID:    req.GroupID,
		UserID:     req.UserID,
		Symbol:     req.Symbol,
		OutputMint: start.OutputMint,
		Amount:     req.USDCAmount,
		Taker:      treasury.SolanaAddress,
	})
	if err != nil {
		return DevExecuteBuyResult{}, err
	}

	signedTx, err := s.signer.SignTreasuryTransaction(ctx, treasury.PrivyWalletID, order.Transaction)
	if err != nil {
		return DevExecuteBuyResult{}, err
	}

	_, err = s.jupiter.ExecuteBuy(ctx, jupiter.ExecuteBuyParams{
		GroupID:           req.GroupID,
		UserID:            req.UserID,
		Symbol:            req.Symbol,
		RequestID:         order.RequestID,
		SignedTransaction: signedTx,
	})
	if err != nil {
		return DevExecuteBuyResult{}, err
	}
	logSwapExecuteSubmit(req.GroupID, req.UserID, req.Symbol, "", order.RequestID)

	fill, err := jupiter.PollUntilConfirmed(ctx, s.jupiter, jupiter.PollExecuteParams{
		GroupID:           req.GroupID,
		UserID:            req.UserID,
		Symbol:            req.Symbol,
		RequestID:         order.RequestID,
		SignedTransaction: signedTx,
	}, s.pollCfg())
	if err != nil {
		return DevExecuteBuyResult{}, err
	}
	if !fill.IsConfirmedSuccess() {
		return DevExecuteBuyResult{}, fmt.Errorf("jupiter buy not confirmed: status=%s code=%d", fill.Status, fill.Code)
	}
	logSwapPollTransition(req.GroupID, req.UserID, req.Symbol, fill.Signature, jupiter.ExecuteStatusPending, fill.Status, fill.Code)

	costBasisAmount, err := parseAmount(fill.OutputAmountResult)
	if err != nil {
		return DevExecuteBuyResult{}, err
	}
	costBasisPrice, err := parseAmount(fill.InputAmountResult)
	if err != nil {
		return DevExecuteBuyResult{}, err
	}

	row, created, err := s.store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID:          req.GroupID,
		Amount:           req.USDCAmount,
		InputMint:        jupiter.USDCMint,
		OutputMint:       start.OutputMint,
		TxSignature:      fill.Signature,
		ExecuteRequestID: order.RequestID,
		CostBasisPrice:   costBasisPrice,
		CostBasisAmount:  costBasisAmount,
	})
	if err != nil {
		return DevExecuteBuyResult{}, err
	}

	if created {
		s.applyBuyBalances(treasury.SolanaAddress, start.OutputMint, req.USDCAmount, costBasisAmount)
		treasuryUsdc, err := s.treasuryUSDCForSnapshot(ctx, treasury.SolanaAddress)
		if err != nil {
			return DevExecuteBuyResult{}, err
		}
		if err := s.store.WriteNavSnapshotOnTransactionConfirm(ctx, req.GroupID, treasuryUsdc); err != nil {
			return DevExecuteBuyResult{}, err
		}
	}

	return DevExecuteBuyResult{Transaction: row, Created: created}, nil
}

// SellToUSDC quotes, signs, executes, polls, and persists a treasury sell.
// Confirmed sells are idempotent on tx_signature via postgres.ConfirmSellTransaction.
func (s *SwapService) SellToUSDC(ctx context.Context, req SellToUSDCRequest) (SellToUSDCResult, error) {
	treasury, err := s.privy.EnsureTreasury(ctx, privy.GroupID(req.GroupID))
	if err != nil {
		return SellToUSDCResult{}, err
	}

	quote, err := s.jupiter.QuoteSell(ctx, jupiter.QuoteSellParams{
		GroupID:   req.GroupID,
		UserID:    req.UserID,
		Symbol:    req.Symbol,
		InputMint: req.InputMint,
		Amount:    req.Amount,
		Taker:     treasury.SolanaAddress,
	})
	if err != nil {
		return SellToUSDCResult{}, err
	}
	if !quote.Routable {
		return SellToUSDCResult{}, ErrQuoteNotRoutable
	}

	signedTx, err := s.signer.SignTreasuryTransaction(ctx, treasury.PrivyWalletID, quote.Transaction)
	if err != nil {
		return SellToUSDCResult{}, err
	}

	_, err = s.jupiter.SellToUSDC(ctx, jupiter.SellToUSDCParams{
		GroupID:           req.GroupID,
		UserID:            req.UserID,
		Symbol:            req.Symbol,
		RequestID:         quote.RequestID,
		SignedTransaction: signedTx,
		InputMint:         req.InputMint,
		OutputMint:        jupiter.USDCMint,
		Amount:            req.Amount,
	})
	if err != nil {
		return SellToUSDCResult{}, err
	}
	logSwapExecuteSubmit(req.GroupID, req.UserID, req.Symbol, "", quote.RequestID)

	fill, err := jupiter.PollUntilConfirmed(ctx, s.jupiter, jupiter.PollExecuteParams{
		GroupID:           req.GroupID,
		UserID:            req.UserID,
		Symbol:            req.Symbol,
		RequestID:         quote.RequestID,
		SignedTransaction: signedTx,
	}, s.pollCfg())
	if err != nil {
		return SellToUSDCResult{}, err
	}
	if !fill.IsConfirmedSuccess() {
		return SellToUSDCResult{}, fmt.Errorf("jupiter sell not confirmed: status=%s code=%d", fill.Status, fill.Code)
	}
	logSwapPollTransition(req.GroupID, req.UserID, req.Symbol, fill.Signature, jupiter.ExecuteStatusPending, fill.Status, fill.Code)

	proceeds, err := parseAmount(fill.OutputAmountResult)
	if err != nil {
		return SellToUSDCResult{}, err
	}

	row, created, err := s.store.ConfirmSellTransaction(ctx, postgres.ConfirmSellTransactionParams{
		GroupID:          req.GroupID,
		Amount:           req.Amount,
		InputMint:        req.InputMint,
		OutputMint:       jupiter.USDCMint,
		TxSignature:      fill.Signature,
		ExecuteRequestID: quote.RequestID,
		ProceedsUSDC:     proceeds,
	})
	if err != nil {
		return SellToUSDCResult{}, err
	}

	if created {
		s.applySellBalances(treasury.SolanaAddress, req.InputMint, req.Amount, proceeds)
		treasuryUsdc, err := s.treasuryUSDCForSnapshot(ctx, treasury.SolanaAddress)
		if err != nil {
			return SellToUSDCResult{}, err
		}
		if err := s.store.WriteNavSnapshotOnTransactionConfirm(ctx, req.GroupID, treasuryUsdc); err != nil {
			return SellToUSDCResult{}, err
		}
	}

	return SellToUSDCResult{Transaction: row, Created: created}, nil
}

func (s *SwapService) treasuryUSDCForSnapshot(ctx context.Context, treasuryAddress string) (int64, error) {
	if balances, ok := s.balances[treasuryAddress]; ok {
		return balances.USDC, nil
	}
	balance, err := s.privy.TreasuryUSDCBalance(ctx, treasuryAddress)
	if err != nil {
		return 0, fmt.Errorf("treasury usdc balance: %w", err)
	}
	return balance, nil
}

func (s *SwapService) applyBuyBalances(treasuryAddress, outputMint string, usdcSpent, xStockReceived int64) {
	balances := s.TreasuryBalancesFor(treasuryAddress)
	if outputMint == jupiter.AAPLxMint || outputMint != jupiter.USDCMint {
		balances.XStock += xStockReceived
	}
	balances.USDC -= usdcSpent
	if balances.USDC < 0 {
		balances.USDC = 0
	}
	s.balances[treasuryAddress] = balances
}

func (s *SwapService) applySellBalances(treasuryAddress, inputMint string, xStockSold, usdcReceived int64) {
	balances := s.TreasuryBalancesFor(treasuryAddress)
	if inputMint == jupiter.AAPLxMint || inputMint != jupiter.USDCMint {
		balances.XStock -= xStockSold
		if balances.XStock < 0 {
			balances.XStock = 0
		}
	}
	balances.USDC += usdcReceived
	s.balances[treasuryAddress] = balances
}

func parseAmount(raw string) (int64, error) {
	if raw == "" {
		return 0, fmt.Errorf("missing fill amount")
	}
	amount, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid fill amount %q: %w", raw, err)
	}
	if amount <= 0 {
		return 0, fmt.Errorf("fill amount must be positive")
	}
	return amount, nil
}

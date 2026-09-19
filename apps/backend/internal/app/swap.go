package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/solana/txsign"
)

// DevExecuteBuyRequest is input for the M3 dev-only buy execute path.
type DevExecuteBuyRequest struct {
	GroupID    string
	UserID     string
	Symbol     string
	USDCAmount int64
	ProposalID string
}

// DevExecuteBuyResult is the persisted confirmed buy transaction.
type DevExecuteBuyResult struct {
	Transaction postgres.TransactionRow
	Created       bool
}

// SellToUSDCRequest sells treasury xStock back to USDC.
type SellToUSDCRequest struct {
	GroupID    string
	UserID     string
	Symbol     string
	InputMint  string
	Amount     int64
	ProposalID string
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
	relayerKey string
	balances   map[string]TreasuryBalances
	pollConfig jupiter.PollConfig
	symbols    *SymbolResolver
}

// NewSwapService wires swap dependencies.
func NewSwapService(
	store *postgres.Store,
	buy *BuyService,
	jupiterClient jupiter.Client,
	privyClient privy.Client,
	signer TreasurySigner,
	relayerKey string,
	symbols *SymbolResolver,
) *SwapService {
	return &SwapService{
		store:      store,
		buy:        buy,
		jupiter:    jupiterClient,
		privy:      privyClient,
		signer:     signer,
		relayerKey: relayerKey,
		balances:   make(map[string]TreasuryBalances),
		symbols:    symbols,
	}
}

// SetPollConfigForTests configures execute polling for integration tests.
func (s *SwapService) SetPollConfigForTests(cfg jupiter.PollConfig) {
	s.pollConfig = cfg
}

func (s *SwapService) symbolForMint(ctx context.Context, mint string) string {
	if s.symbols != nil {
		return s.symbols.SymbolForMint(ctx, mint)
	}
	return symbolForOutputMint(ctx, nil, mint)
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
	logSwapBuyStart(req.GroupID, req.UserID, req.Symbol, req.USDCAmount)

	treasury, err := s.privy.EnsureTreasury(ctx, privy.GroupID(req.GroupID))
	if err != nil {
		logSwapBranchError("swap buy ensure treasury failed", err,
			"group_id", req.GroupID, "user_id", req.UserID, "symbol", req.Symbol, "stage", "ensure_treasury")
		return DevExecuteBuyResult{}, err
	}

	outputMint, err := s.buy.ResolveOutputMint(ctx, req.Symbol)
	if err != nil {
		logSwapBranchError("swap buy resolve mint failed", err, "group_id", req.GroupID, "user_id", req.UserID, "symbol", req.Symbol, "stage", "resolve_mint")
		return DevExecuteBuyResult{}, err
	}

	order, err := s.jupiter.OrderBuy(ctx, jupiter.OrderBuyParams{
		GroupID:    req.GroupID,
		UserID:     req.UserID,
		Symbol:     req.Symbol,
		OutputMint: outputMint,
		Amount:     req.USDCAmount,
		Taker:      treasury.SolanaAddress,
	})
	if err != nil {
		logSwapBranchError("swap buy order failed", err,
			"group_id", req.GroupID, "user_id", req.UserID, "symbol", req.Symbol,
			"stage", "order_buy", "taker", treasury.SolanaAddress, "output_mint", outputMint, "usdc_amount", req.USDCAmount)
		return DevExecuteBuyResult{}, err
	}

	signedTx, err := s.signSwapTransaction(ctx, treasury.PrivyWalletID, order.Transaction)
	if err != nil {
		logSwapBranchError("swap buy sign failed", err,
			"group_id", req.GroupID, "user_id", req.UserID, "symbol", req.Symbol,
			"stage", "sign_treasury", "request_id", order.RequestID)
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
		logSwapBranchError("swap buy execute submit failed", err,
			"group_id", req.GroupID, "user_id", req.UserID, "symbol", req.Symbol,
			"stage", "execute_submit", "request_id", order.RequestID)
		return DevExecuteBuyResult{}, err
	}
	logSwapExecuteSubmit(req.GroupID, req.UserID, req.Symbol, "", order.RequestID)
	if _, _, err := s.store.InsertPendingTransaction(ctx, postgres.InsertPendingTransactionParams{
		GroupID:          req.GroupID,
		ProposalID:       req.ProposalID,
		Action:           postgres.TransactionActionBuy,
		InputMint:        jupiter.USDCMint,
		OutputMint:       outputMint,
		Amount:           req.USDCAmount,
		ExecuteRequestID: order.RequestID,
	}); err != nil {
		return DevExecuteBuyResult{}, err
	}

	fill, err := jupiter.PollUntilConfirmed(ctx, s.jupiter, jupiter.PollExecuteParams{
		GroupID:           req.GroupID,
		UserID:            req.UserID,
		Symbol:            req.Symbol,
		RequestID:         order.RequestID,
		SignedTransaction: signedTx,
	}, s.pollCfg())
	if err != nil {
		logSwapBranchError("swap buy poll failed", err,
			"group_id", req.GroupID, "user_id", req.UserID, "symbol", req.Symbol,
			"stage", "poll_confirm", "request_id", order.RequestID)
		_ = s.markSwapFailed(ctx, req.GroupID, postgres.TransactionActionBuy, jupiter.USDCMint, outputMint, req.USDCAmount, order.RequestID)
		return DevExecuteBuyResult{}, err
	}
	if !fill.IsConfirmedSuccess() {
		err := fmt.Errorf("jupiter buy not confirmed: status=%s code=%d", fill.Status, fill.Code)
		logSwapBranchError("swap buy poll not confirmed", err,
			"group_id", req.GroupID, "user_id", req.UserID, "symbol", req.Symbol,
			"stage", "poll_confirm", "request_id", order.RequestID, "status", fill.Status, "code", fill.Code)
		_ = s.markSwapFailed(ctx, req.GroupID, postgres.TransactionActionBuy, jupiter.USDCMint, outputMint, req.USDCAmount, order.RequestID)
		return DevExecuteBuyResult{}, err
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
		OutputMint:       outputMint,
		TxSignature:      fill.Signature,
		ExecuteRequestID: order.RequestID,
		CostBasisPrice:   costBasisPrice,
		CostBasisAmount:  costBasisAmount,
	})
	if err != nil {
		return DevExecuteBuyResult{}, err
	}

	if created {
		s.applyBuyBalances(treasury.SolanaAddress, outputMint, req.USDCAmount, costBasisAmount)
		treasuryUsdc, err := s.treasuryUSDCForSnapshot(ctx, treasury.SolanaAddress)
		if err != nil {
			return DevExecuteBuyResult{}, err
		}
		if err := s.store.WriteNavSnapshotOnTransactionConfirm(ctx, req.GroupID, treasuryUsdc); err != nil {
			return DevExecuteBuyResult{}, err
		}
	}

	logSwapBuySuccess(req.GroupID, req.UserID, req.Symbol, row.ID, created)
	return DevExecuteBuyResult{Transaction: row, Created: created}, nil
}

// SellToUSDC quotes, signs, executes, polls, and persists a treasury sell.
// Confirmed sells are idempotent on tx_signature via postgres.ConfirmSellTransaction.
func (s *SwapService) SellToUSDC(ctx context.Context, req SellToUSDCRequest) (SellToUSDCResult, error) {
	logSwapSellStart(req.GroupID, req.UserID, req.Symbol, req.Amount)

	treasury, err := s.privy.EnsureTreasury(ctx, privy.GroupID(req.GroupID))
	if err != nil {
		logSwapBranchError("swap sell ensure treasury failed", err,
			"group_id", req.GroupID, "user_id", req.UserID, "symbol", req.Symbol, "stage", "ensure_treasury")
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
		if errors.Is(err, jupiter.ErrNoRoute) || errors.Is(err, jupiter.ErrBelowMinimumSize) {
			logSwapRefusal(req.GroupID, req.UserID, req.Symbol, err.Error())
			return SellToUSDCResult{}, ErrQuoteNotRoutable
		}
		logSwapBranchError("swap sell quote failed", err,
			"group_id", req.GroupID, "user_id", req.UserID, "symbol", req.Symbol,
			"stage", "quote_sell", "taker", treasury.SolanaAddress, "input_mint", req.InputMint, "amount", req.Amount)
		return SellToUSDCResult{}, err
	}
	if !quote.Routable {
		logSwapRefusal(req.GroupID, req.UserID, req.Symbol, "no route")
		return SellToUSDCResult{}, ErrQuoteNotRoutable
	}

	signedTx, err := s.signSwapTransaction(ctx, treasury.PrivyWalletID, quote.Transaction)
	if err != nil {
		logSwapBranchError("swap sell sign failed", err,
			"group_id", req.GroupID, "user_id", req.UserID, "symbol", req.Symbol,
			"stage", "sign_treasury", "request_id", quote.RequestID)
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
		logSwapBranchError("swap sell execute submit failed", err,
			"group_id", req.GroupID, "user_id", req.UserID, "symbol", req.Symbol,
			"stage", "execute_submit", "request_id", quote.RequestID)
		return SellToUSDCResult{}, err
	}
	logSwapExecuteSubmit(req.GroupID, req.UserID, req.Symbol, "", quote.RequestID)
	if _, _, err := s.store.InsertPendingTransaction(ctx, postgres.InsertPendingTransactionParams{
		GroupID:          req.GroupID,
		ProposalID:       req.ProposalID,
		Action:           postgres.TransactionActionSell,
		InputMint:        req.InputMint,
		OutputMint:       jupiter.USDCMint,
		Amount:           req.Amount,
		ExecuteRequestID: quote.RequestID,
	}); err != nil {
		return SellToUSDCResult{}, err
	}

	fill, err := jupiter.PollUntilConfirmed(ctx, s.jupiter, jupiter.PollExecuteParams{
		GroupID:           req.GroupID,
		UserID:            req.UserID,
		Symbol:            req.Symbol,
		RequestID:         quote.RequestID,
		SignedTransaction: signedTx,
	}, s.pollCfg())
	if err != nil {
		logSwapBranchError("swap sell poll failed", err,
			"group_id", req.GroupID, "user_id", req.UserID, "symbol", req.Symbol,
			"stage", "poll_confirm", "request_id", quote.RequestID)
		_ = s.markSwapFailed(ctx, req.GroupID, postgres.TransactionActionSell, req.InputMint, jupiter.USDCMint, req.Amount, quote.RequestID)
		return SellToUSDCResult{}, err
	}
	if !fill.IsConfirmedSuccess() {
		err := fmt.Errorf("jupiter sell not confirmed: status=%s code=%d", fill.Status, fill.Code)
		logSwapBranchError("swap sell poll not confirmed", err,
			"group_id", req.GroupID, "user_id", req.UserID, "symbol", req.Symbol,
			"stage", "poll_confirm", "request_id", quote.RequestID, "status", fill.Status, "code", fill.Code)
		_ = s.markSwapFailed(ctx, req.GroupID, postgres.TransactionActionSell, req.InputMint, jupiter.USDCMint, req.Amount, quote.RequestID)
		return SellToUSDCResult{}, err
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

	logSwapSellSuccess(req.GroupID, req.UserID, req.Symbol, row.ID, created)
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

func (s *SwapService) signSwapTransaction(ctx context.Context, walletID, unsignedTx string) (string, error) {
	tx := unsignedTx
	if s.relayerKey != "" {
		var err error
		tx, err = txsign.SignLocalSignerIfRequired(tx, s.relayerKey)
		if err != nil {
			return "", fmt.Errorf("sign fee payer: %w", err)
		}
	}
	return s.signer.SignTreasuryTransaction(ctx, walletID, tx)
}

func (s *SwapService) markSwapFailed(ctx context.Context, groupID, action, inputMint, outputMint string, amount int64, executeRequestID string) error {
	if executeRequestID == "" {
		return nil
	}
	if _, ok, err := s.store.FailTransactionByExecuteRequestID(ctx, executeRequestID); err != nil {
		return err
	} else if ok {
		return nil
	}
	_, err := s.store.InsertFailedTransaction(ctx, groupID, action, inputMint, outputMint, amount, executeRequestID)
	return err
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

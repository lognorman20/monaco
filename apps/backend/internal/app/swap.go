package app

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/dex"
	"github.com/monaco/monaco/apps/backend/internal/evm"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
)

// DevExecuteBuyRequest is input for treasury buy execution.
type DevExecuteBuyRequest struct {
	GroupID       string
	UserID        string
	Symbol        string
	USDCAmount    int64
	ProposalID    string
	AgentIntentID string
	InitiatedBy   string
}

// DevExecuteBuyResult is the persisted confirmed buy transaction.
type DevExecuteBuyResult struct {
	Transaction postgres.TransactionRow
	Created       bool
}

// SellToUSDCRequest sells treasury B20 back to USDC.
type SellToUSDCRequest struct {
	GroupID       string
	UserID        string
	Symbol        string
	InputToken    string
	Amount        int64
	ProposalID    string
	AgentIntentID string
	InitiatedBy   string
}

// SellToUSDCResult is the persisted confirmed sell transaction.
type SellToUSDCResult struct {
	Transaction postgres.TransactionRow
	Created       bool
}

// TreasuryBalances tracks fake treasury token balances for integration tests.
type TreasuryBalances struct {
	USDC  int64
	Token int64
}

// SwapService orchestrates DEX buy and sell flows for group treasuries.
type SwapService struct {
	store    *postgres.Store
	buy      *BuyService
	dex      dex.Client
	wallets  wallets.Client
	chain    evm.Client
	balances map[string]TreasuryBalances
	symbols  *SymbolResolver
}

// NewSwapService wires swap dependencies.
func NewSwapService(
	store *postgres.Store,
	buy *BuyService,
	dexClient dex.Client,
	walletClient wallets.Client,
	chain evm.Client,
	symbols *SymbolResolver,
) *SwapService {
	return &SwapService{
		store:    store,
		buy:      buy,
		dex:      dexClient,
		wallets:  walletClient,
		chain:    chain,
		balances: make(map[string]TreasuryBalances),
		symbols:  symbols,
	}
}

func (s *SwapService) symbolForToken(ctx context.Context, token string) string {
	if s.symbols != nil {
		return s.symbols.SymbolForMint(ctx, token)
	}
	return symbolForOutputToken(ctx, nil, token)
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

func executeRequestID(proposalID, agentIntentID string) string {
	if agentIntentID != "" {
		return agentIntentID
	}
	return proposalID
}

// DevExecuteBuy quotes, swaps, confirms, and persists a treasury buy.
func (s *SwapService) DevExecuteBuy(ctx context.Context, req DevExecuteBuyRequest) (DevExecuteBuyResult, error) {
	logSwapBuyStart(req.GroupID, req.UserID, req.Symbol, req.USDCAmount)
	if err := rejectFakerGroup(ctx, s.store, req.GroupID); err != nil {
		return DevExecuteBuyResult{}, err
	}

	treasury, err := s.wallets.EnsureTreasury(ctx, wallets.GroupID(req.GroupID))
	if err != nil {
		return DevExecuteBuyResult{}, err
	}

	tokenOut, err := s.buy.ResolveOutputToken(ctx, req.Symbol)
	if err != nil {
		return DevExecuteBuyResult{}, err
	}

	usdcIn := big.NewInt(req.USDCAmount)
	quote, err := s.dex.QuoteBuy(ctx, tokenOut, usdcIn)
	if err != nil {
		return DevExecuteBuyResult{}, err
	}
	if !quote.Routable {
		logSwapRefusal(req.GroupID, req.UserID, req.Symbol, "no route")
		return DevExecuteBuyResult{}, ErrQuoteNotRoutable
	}

	execID := executeRequestID(req.ProposalID, req.AgentIntentID)
	if err := s.ensureTreasuryGas(ctx, treasury); err != nil {
		return DevExecuteBuyResult{}, err
	}
	if err := s.ensureAllowance(ctx, treasury, quote, usdcIn); err != nil {
		return DevExecuteBuyResult{}, err
	}

	swapCall, err := s.dex.BuildSwap(ctx, quote, treasury.Address, treasury.Address)
	if err != nil {
		return DevExecuteBuyResult{}, err
	}

	txHash, err := s.wallets.SendTreasuryTransaction(ctx, treasury, swapCall.Router, swapCall.Data, big.NewInt(0))
	if err != nil {
		return DevExecuteBuyResult{}, err
	}

	if _, _, err := s.store.InsertPendingTransaction(ctx, postgres.InsertPendingTransactionParams{
		GroupID:          req.GroupID,
		ProposalID:       req.ProposalID,
		AgentIntentID:    req.AgentIntentID,
		InitiatedBy:      req.InitiatedBy,
		Action:           postgres.TransactionActionBuy,
		InputToken:       evm.USDCAddress,
		OutputToken:      tokenOut,
		Amount:           req.USDCAmount,
		ExecuteRequestID: execID,
	}); err != nil {
		return DevExecuteBuyResult{}, err
	}

	fillAmount, err := s.waitFill(ctx, txHash, tokenOut, treasury.Address)
	if err != nil {
		_ = s.markSwapFailed(ctx, req.GroupID, postgres.TransactionActionBuy, evm.USDCAddress, tokenOut, req.USDCAmount, execID)
		return DevExecuteBuyResult{}, err
	}

	row, created, err := s.store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID:          req.GroupID,
		Amount:           req.USDCAmount,
		InputToken:       evm.USDCAddress,
		OutputToken:      tokenOut,
		TxHash:           txHash,
		ExecuteRequestID: execID,
		CostBasisPrice:   req.USDCAmount,
		CostBasisAmount:  fillAmount,
	})
	if err != nil {
		return DevExecuteBuyResult{}, err
	}

	if created {
		s.applyBuyBalances(treasury.Address, tokenOut, req.USDCAmount, fillAmount)
		treasuryUsdc, err := s.treasuryUSDCForSnapshot(ctx, treasury.Address)
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

// SellToUSDC quotes, swaps, confirms, and persists a treasury sell.
func (s *SwapService) SellToUSDC(ctx context.Context, req SellToUSDCRequest) (SellToUSDCResult, error) {
	logSwapSellStart(req.GroupID, req.UserID, req.Symbol, req.Amount)
	if err := rejectFakerGroup(ctx, s.store, req.GroupID); err != nil {
		return SellToUSDCResult{}, err
	}

	treasury, err := s.wallets.EnsureTreasury(ctx, wallets.GroupID(req.GroupID))
	if err != nil {
		return SellToUSDCResult{}, err
	}

	amountIn := big.NewInt(req.Amount)
	quote, err := s.dex.QuoteSell(ctx, req.InputToken, amountIn)
	if err != nil {
		return SellToUSDCResult{}, err
	}
	if !quote.Routable {
		logSwapRefusal(req.GroupID, req.UserID, req.Symbol, "no route")
		return SellToUSDCResult{}, ErrQuoteNotRoutable
	}

	execID := executeRequestID(req.ProposalID, req.AgentIntentID)
	if err := s.ensureTreasuryGas(ctx, treasury); err != nil {
		return SellToUSDCResult{}, err
	}
	if err := s.ensureAllowance(ctx, treasury, quote, amountIn); err != nil {
		return SellToUSDCResult{}, err
	}

	swapCall, err := s.dex.BuildSwap(ctx, quote, treasury.Address, treasury.Address)
	if err != nil {
		return SellToUSDCResult{}, err
	}

	txHash, err := s.wallets.SendTreasuryTransaction(ctx, treasury, swapCall.Router, swapCall.Data, big.NewInt(0))
	if err != nil {
		return SellToUSDCResult{}, err
	}

	if _, _, err := s.store.InsertPendingTransaction(ctx, postgres.InsertPendingTransactionParams{
		GroupID:          req.GroupID,
		ProposalID:       req.ProposalID,
		AgentIntentID:    req.AgentIntentID,
		InitiatedBy:      req.InitiatedBy,
		Action:           postgres.TransactionActionSell,
		InputToken:       req.InputToken,
		OutputToken:      evm.USDCAddress,
		Amount:           req.Amount,
		ExecuteRequestID: execID,
	}); err != nil {
		return SellToUSDCResult{}, err
	}

	proceeds, err := s.waitFill(ctx, txHash, evm.USDCAddress, treasury.Address)
	if err != nil {
		_ = s.markSwapFailed(ctx, req.GroupID, postgres.TransactionActionSell, req.InputToken, evm.USDCAddress, req.Amount, execID)
		return SellToUSDCResult{}, err
	}

	row, created, err := s.store.ConfirmSellTransaction(ctx, postgres.ConfirmSellTransactionParams{
		GroupID:          req.GroupID,
		Amount:           req.Amount,
		InputToken:       req.InputToken,
		OutputToken:      evm.USDCAddress,
		TxHash:           txHash,
		ExecuteRequestID: execID,
		ProceedsUSDC:     proceeds,
	})
	if err != nil {
		return SellToUSDCResult{}, err
	}

	if created {
		s.applySellBalances(treasury.Address, req.InputToken, req.Amount, proceeds)
		treasuryUsdc, err := s.treasuryUSDCForSnapshot(ctx, treasury.Address)
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

func (s *SwapService) ensureTreasuryGas(ctx context.Context, treasury wallets.TreasuryRef) error {
	_ = ctx
	_ = treasury
	return nil
}

func (s *SwapService) ensureAllowance(ctx context.Context, treasury wallets.TreasuryRef, quote dex.Quote, amount *big.Int) error {
	if s.chain == nil {
		return nil
	}
	spender := quote.TokenOut
	if quote.TokenIn != evm.USDCAddress {
		spender = quote.TokenOut
	}
	// Router address comes from BuildSwap; allowance is checked against quote route in T5.
	allowance, err := s.chain.Allowance(ctx, quote.TokenIn, treasury.Address, spender)
	if err != nil {
		return err
	}
	if allowance.Cmp(amount) >= 0 {
		return nil
	}
	data, err := evm.EncodeApprove(spender, amount)
	if err != nil {
		return err
	}
	txHash, err := s.wallets.SendTreasuryTransaction(ctx, treasury, quote.TokenIn, data, big.NewInt(0))
	if err != nil {
		return err
	}
	_, err = s.waitReceipt(ctx, txHash)
	return err
}

func (s *SwapService) waitFill(ctx context.Context, txHash, tokenOut, treasury string) (int64, error) {
	receipt, err := s.waitReceipt(ctx, txHash)
	if err != nil {
		return 0, err
	}
	if receipt.Status != 1 {
		return 0, fmt.Errorf("transaction failed")
	}
	fill := evm.DecodeERC20TransferLogs(receipt.Logs, tokenOut, treasury)
	if fill.Sign() <= 0 {
		return 0, fmt.Errorf("missing fill amount")
	}
	if !fill.IsInt64() {
		return 0, fmt.Errorf("fill overflow")
	}
	return fill.Int64(), nil
}

func (s *SwapService) waitReceipt(ctx context.Context, txHash string) (evm.Receipt, error) {
	if s.chain == nil {
		return evm.Receipt{Found: true, Status: 1}, nil
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		receipt, err := s.chain.Receipt(ctx, txHash)
		if err != nil {
			return evm.Receipt{}, err
		}
		if receipt.Found {
			return receipt, nil
		}
		select {
		case <-ctx.Done():
			return evm.Receipt{}, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return evm.Receipt{}, fmt.Errorf("receipt timeout")
}

func (s *SwapService) treasuryUSDCForSnapshot(ctx context.Context, treasuryAddress string) (int64, error) {
	if balances, ok := s.balances[treasuryAddress]; ok {
		return balances.USDC, nil
	}
	balance, err := s.wallets.TreasuryUSDCBalance(ctx, treasuryAddress)
	if err != nil {
		return 0, fmt.Errorf("treasury usdc balance: %w", err)
	}
	return balance, nil
}

func (s *SwapService) applyBuyBalances(treasuryAddress, outputToken string, usdcSpent, tokenReceived int64) {
	balances := s.TreasuryBalancesFor(treasuryAddress)
	if outputToken != evm.USDCAddress {
		balances.Token += tokenReceived
	}
	balances.USDC -= usdcSpent
	if balances.USDC < 0 {
		balances.USDC = 0
	}
	s.balances[treasuryAddress] = balances
}

func (s *SwapService) applySellBalances(treasuryAddress, inputToken string, tokenSold, usdcReceived int64) {
	balances := s.TreasuryBalancesFor(treasuryAddress)
	if inputToken != evm.USDCAddress {
		balances.Token -= tokenSold
		if balances.Token < 0 {
			balances.Token = 0
		}
	}
	balances.USDC += usdcReceived
	s.balances[treasuryAddress] = balances
}

func (s *SwapService) markSwapFailed(ctx context.Context, groupID, action, inputToken, outputToken string, amount int64, executeRequestID string) error {
	if executeRequestID == "" {
		return nil
	}
	if _, ok, err := s.store.FailTransactionByExecuteRequestID(ctx, executeRequestID); err != nil {
		return err
	} else if ok {
		return nil
	}
	_, err := s.store.InsertFailedTransaction(ctx, groupID, action, inputToken, outputToken, amount, executeRequestID)
	return err
}

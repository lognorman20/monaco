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
	"github.com/monaco/monaco/apps/backend/internal/solana/mintinfo"
	"github.com/monaco/monaco/apps/backend/internal/solana/txsign"
	"github.com/monaco/monaco/apps/backend/internal/swapprovider"
	"github.com/monaco/monaco/apps/backend/internal/telemetry"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

// DevExecuteBuyRequest is input for the M3 dev-only buy execute path.
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
	Created     bool
}

// SellToUSDCRequest sells treasury xStock back to USDC.
type SellToUSDCRequest struct {
	GroupID       string
	UserID        string
	Symbol        string
	InputMint     string
	Amount        int64
	ProposalID    string
	AgentIntentID string
	InitiatedBy   string
}

// SellToUSDCResult is the persisted confirmed sell transaction.
type SellToUSDCResult struct {
	Transaction postgres.TransactionRow
	Created     bool
	// ProceedsUSDC is the USDC the fill raised, in micros.
	ProceedsUSDC int64
}

// TreasuryBalances tracks fake treasury token balances for integration tests.
type TreasuryBalances struct {
	USDC   int64
	XStock int64
}

// usdcDecimals is the on-chain decimal count for mainnet USDC.
const usdcDecimals = 6

// SwapService orchestrates buy and sell flows for group treasuries.
// The venue (Jupiter by default) sits behind swapprovider.Provider.
type SwapService struct {
	store      *postgres.Store
	buy        *BuyService
	jupiter    jupiter.Client
	provider   swapprovider.Provider
	privy      privy.Client
	signer     TreasurySigner
	relayerKey string
	balances   map[string]TreasuryBalances
	pollConfig jupiter.PollConfig
	symbols    *SymbolResolver
	pyth       pyth.Client
	mintinfo   mintinfo.Reader
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
	s := &SwapService{
		store:      store,
		buy:        buy,
		jupiter:    jupiterClient,
		privy:      privyClient,
		signer:     signer,
		relayerKey: relayerKey,
		balances:   make(map[string]TreasuryBalances),
		symbols:    symbols,
	}
	s.provider = jupiter.NewSwapProvider(jupiterClient, swapTransactionSigner{swap: s})
	return s
}

// SetSwapProvider replaces the default Jupiter provider (SWAP_PROVIDER=flash).
func (s *SwapService) SetSwapProvider(provider swapprovider.Provider) {
	if provider != nil {
		s.provider = provider
	}
}

// SetPriceClient wires the price chain used to mark holdings in post-swap NAV snapshots.
func (s *SwapService) SetPriceClient(client pyth.Client) {
	s.pyth = client
}

// SetMintInfo wires live mint extension reads for pre-IPO fee and pause checks at execute time.
func (s *SwapService) SetMintInfo(reader mintinfo.Reader) {
	s.mintinfo = reader
}

// SwapProviderName reports which venue executes treasury swaps.
func (s *SwapService) SwapProviderName() string {
	return s.provider.Name()
}

// swapTransactionSigner co-signs with the relayer fee payer, then the treasury wallet.
type swapTransactionSigner struct {
	swap *SwapService
}

func (t swapTransactionSigner) SignTreasuryTransaction(ctx context.Context, walletID, unsignedTxBase64 string) (string, error) {
	return t.swap.signSwapTransaction(ctx, walletID, unsignedTxBase64)
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
	result, err := s.executeBuy(ctx, req)
	recordSwap(telemetry.EventSwapBuy, req.USDCAmount, result.Created, err)
	return result, err
}

func (s *SwapService) executeBuy(ctx context.Context, req DevExecuteBuyRequest) (DevExecuteBuyResult, error) {
	logSwapBuyStart(req.GroupID, req.UserID, req.Symbol, req.USDCAmount)

	// Faker scale clubs (#153) have a dummy treasury: never reach Privy or Jupiter for them.
	if err := rejectFakerGroup(ctx, s.store, req.GroupID); err != nil {
		logSwapBranchError("swap buy rejected", err, "group_id", req.GroupID, "user_id", req.UserID, "symbol", req.Symbol, "stage", "faker_guard")
		return DevExecuteBuyResult{}, err
	}

	treasury, err := s.privy.EnsureTreasury(ctx, privy.GroupID(req.GroupID))
	if err != nil {
		logSwapBranchError("swap buy ensure treasury failed", err,
			"group_id", req.GroupID, "user_id", req.UserID, "symbol", req.Symbol, "stage", "ensure_treasury")
		return DevExecuteBuyResult{}, err
	}

	asset, err := s.buy.ResolveAsset(ctx, req.Symbol)
	if err != nil {
		logSwapBranchError("swap buy resolve mint failed", err, "group_id", req.GroupID, "user_id", req.UserID, "symbol", req.Symbol, "stage", "resolve_mint")
		return DevExecuteBuyResult{}, err
	}
	outputMint := asset.SolanaMint
	tokenDecimals := asset.Normalize().Decimals

	if err := preIPOSwapGuard(ctx, s.mintinfo, asset); err != nil {
		if errors.Is(err, ErrIssuerPaused) {
			_ = s.recordIssuerPausedBuyFailure(ctx, req, outputMint)
		}
		logSwapBranchError("swap buy rejected", err, "group_id", req.GroupID, "user_id", req.UserID, "symbol", req.Symbol, "stage", "issuer_paused")
		return DevExecuteBuyResult{}, err
	}
	transferFeeBps := transferFeeBpsAtExecute(ctx, s.mintinfo, asset)

	swapReq := swapprovider.Request{
		GroupID:        req.GroupID,
		UserID:         req.UserID,
		Symbol:         req.Symbol,
		Side:           swapprovider.SideBuy,
		InputMint:      jupiter.USDCMint,
		OutputMint:     outputMint,
		InputDecimals:  usdcDecimals,
		OutputDecimals: tokenDecimals,
		Kind:           swapAssetKind(asset.Kind),
		TransferFeeBps: transferFeeBps,
		Amount:         req.USDCAmount,
		Wallet:         swapprovider.Wallet{PrivyWalletID: treasury.PrivyWalletID, SolanaAddress: treasury.SolanaAddress},
	}
	sub, err := s.provider.SubmitBuy(ctx, swapReq)
	if err != nil {
		if errors.Is(err, swapprovider.ErrNotRoutable) {
			logSwapRefusal(req.GroupID, req.UserID, req.Symbol, err.Error())
			return DevExecuteBuyResult{}, ErrQuoteNotRoutable
		}
		stage, requestID := swapprovider.StageOf(err, "submit_buy")
		logSwapBranchError("swap buy submit failed", err,
			"group_id", req.GroupID, "user_id", req.UserID, "symbol", req.Symbol,
			"provider", s.provider.Name(), "stage", stage, "request_id", requestID,
			"taker", treasury.SolanaAddress, "output_mint", outputMint, "usdc_amount", req.USDCAmount)
		return DevExecuteBuyResult{}, err
	}
	logSwapExecuteSubmit(req.GroupID, req.UserID, req.Symbol, "", sub.RequestID)
	if _, _, err := s.store.InsertPendingTransaction(ctx, postgres.InsertPendingTransactionParams{
		GroupID:          req.GroupID,
		ProposalID:       req.ProposalID,
		AgentIntentID:    req.AgentIntentID,
		InitiatedBy:      req.InitiatedBy,
		Action:           postgres.TransactionActionBuy,
		InputMint:        jupiter.USDCMint,
		OutputMint:       outputMint,
		Amount:           req.USDCAmount,
		ExecuteRequestID: sub.RequestID,
		TokenDecimals:    tokenDecimals,
	}); err != nil {
		return DevExecuteBuyResult{}, err
	}

	fill, err := s.provider.AwaitFill(ctx, sub, s.pollConfig)
	if err != nil {
		logSwapBranchError("swap buy poll failed", err,
			"group_id", req.GroupID, "user_id", req.UserID, "symbol", req.Symbol,
			"provider", s.provider.Name(), "stage", "poll_confirm", "request_id", sub.RequestID,
			"status", fill.Status, "code", fill.Code)
		// A confirmed swap whose fill amounts are unreadable moved funds: leave it pending for reconcile.
		if !fill.Confirmed {
			_ = s.markSwapFailed(ctx, req.GroupID, postgres.TransactionActionBuy, jupiter.USDCMint, outputMint, req.USDCAmount, sub.RequestID)
		}
		return DevExecuteBuyResult{}, err
	}
	logSwapPollTransition(req.GroupID, req.UserID, req.Symbol, fill.Signature, jupiter.ExecuteStatusPending, fill.Status, fill.Code)

	costBasisAmount, quotedOut, receivedOut := s.reconcileBuyFill(ctx, req.GroupID, treasury.SolanaAddress, outputMint, fill)
	costBasisPrice := fill.InputAmount
	s.logBuyFillReconciliation(ctx, req, asset, transferFeeBps, quotedOut, receivedOut, fill, sub.RequestID)

	row, created, err := s.store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID:          req.GroupID,
		Amount:           req.USDCAmount,
		InputMint:        jupiter.USDCMint,
		OutputMint:       outputMint,
		TxSignature:      fill.Signature,
		ExecuteRequestID: sub.RequestID,
		CostBasisPrice:   costBasisPrice,
		CostBasisAmount:  costBasisAmount,
		TokenDecimals:    tokenDecimals,
	})
	if err != nil {
		return DevExecuteBuyResult{}, err
	}

	if created {
		s.applyBuyBalances(treasury.SolanaAddress, outputMint, req.USDCAmount, costBasisAmount)
		navVals, err := s.navSnapshotAfterSwap(ctx, req.GroupID, treasury.SolanaAddress)
		if err != nil {
			return DevExecuteBuyResult{}, err
		}
		if err := s.store.WriteNavSnapshotOnTransactionConfirm(ctx, req.GroupID, navVals); err != nil {
			return DevExecuteBuyResult{}, err
		}
	}

	logSwapBuySuccess(req.GroupID, req.UserID, req.Symbol, row.ID, created)
	return DevExecuteBuyResult{Transaction: row, Created: created}, nil
}

// SellToUSDC quotes, signs, executes, polls, and persists a treasury sell.
// Confirmed sells are idempotent on tx_signature via postgres.ConfirmSellTransaction.
func (s *SwapService) SellToUSDC(ctx context.Context, req SellToUSDCRequest) (SellToUSDCResult, error) {
	result, err := s.executeSell(ctx, req)
	// A sell is sized in token units; the USDC it raised is on the confirmed transaction.
	recordSwap(telemetry.EventSwapSell, result.ProceedsUSDC, result.Created, err)
	return result, err
}

func (s *SwapService) executeSell(ctx context.Context, req SellToUSDCRequest) (SellToUSDCResult, error) {
	logSwapSellStart(req.GroupID, req.UserID, req.Symbol, req.Amount)

	if err := rejectFakerGroup(ctx, s.store, req.GroupID); err != nil {
		logSwapBranchError("swap sell rejected", err, "group_id", req.GroupID, "user_id", req.UserID, "symbol", req.Symbol, "stage", "faker_guard")
		return SellToUSDCResult{}, err
	}

	treasury, err := s.privy.EnsureTreasury(ctx, privy.GroupID(req.GroupID))
	if err != nil {
		logSwapBranchError("swap sell ensure treasury failed", err,
			"group_id", req.GroupID, "user_id", req.UserID, "symbol", req.Symbol, "stage", "ensure_treasury")
		return SellToUSDCResult{}, err
	}

	sellAsset := s.assetForInputMint(ctx, req.GroupID, req.InputMint)
	tokenDecimals := sellAsset.Decimals

	if err := preIPOSwapGuard(ctx, s.mintinfo, sellAsset); err != nil {
		logSwapBranchError("swap sell rejected", err, "group_id", req.GroupID, "user_id", req.UserID, "symbol", req.Symbol, "stage", "issuer_paused")
		return SellToUSDCResult{}, err
	}
	sellTransferFeeBps := transferFeeBpsAtExecute(ctx, s.mintinfo, sellAsset)

	swapReq := swapprovider.Request{
		GroupID:        req.GroupID,
		UserID:         req.UserID,
		Symbol:         req.Symbol,
		Side:           swapprovider.SideSell,
		InputMint:      req.InputMint,
		OutputMint:     jupiter.USDCMint,
		InputDecimals:  tokenDecimals,
		OutputDecimals: usdcDecimals,
		Kind:           swapAssetKind(sellAsset.Kind),
		TransferFeeBps: sellTransferFeeBps,
		Amount:         req.Amount,
		Wallet:         swapprovider.Wallet{PrivyWalletID: treasury.PrivyWalletID, SolanaAddress: treasury.SolanaAddress},
	}
	sub, err := s.provider.SubmitSell(ctx, swapReq)
	if err != nil {
		if errors.Is(err, swapprovider.ErrNotRoutable) {
			logSwapRefusal(req.GroupID, req.UserID, req.Symbol, err.Error())
			return SellToUSDCResult{}, ErrQuoteNotRoutable
		}
		stage, requestID := swapprovider.StageOf(err, "submit_sell")
		logSwapBranchError("swap sell submit failed", err,
			"group_id", req.GroupID, "user_id", req.UserID, "symbol", req.Symbol,
			"provider", s.provider.Name(), "stage", stage, "request_id", requestID,
			"taker", treasury.SolanaAddress, "input_mint", req.InputMint, "amount", req.Amount)
		return SellToUSDCResult{}, err
	}
	logSwapExecuteSubmit(req.GroupID, req.UserID, req.Symbol, "", sub.RequestID)
	if _, _, err := s.store.InsertPendingTransaction(ctx, postgres.InsertPendingTransactionParams{
		GroupID:          req.GroupID,
		ProposalID:       req.ProposalID,
		AgentIntentID:    req.AgentIntentID,
		InitiatedBy:      req.InitiatedBy,
		Action:           postgres.TransactionActionSell,
		InputMint:        req.InputMint,
		OutputMint:       jupiter.USDCMint,
		Amount:           req.Amount,
		ExecuteRequestID: sub.RequestID,
		TokenDecimals:    tokenDecimals,
	}); err != nil {
		return SellToUSDCResult{}, err
	}

	fill, err := s.provider.AwaitFill(ctx, sub, s.pollConfig)
	if err != nil {
		logSwapBranchError("swap sell poll failed", err,
			"group_id", req.GroupID, "user_id", req.UserID, "symbol", req.Symbol,
			"provider", s.provider.Name(), "stage", "poll_confirm", "request_id", sub.RequestID,
			"status", fill.Status, "code", fill.Code)
		if !fill.Confirmed {
			_ = s.markSwapFailed(ctx, req.GroupID, postgres.TransactionActionSell, req.InputMint, jupiter.USDCMint, req.Amount, sub.RequestID)
		}
		return SellToUSDCResult{}, err
	}
	logSwapPollTransition(req.GroupID, req.UserID, req.Symbol, fill.Signature, jupiter.ExecuteStatusPending, fill.Status, fill.Code)

	proceeds := fill.OutputAmount
	logSwapSellFill(req.GroupID, req.UserID, req.Symbol, req.Amount, fill.QuotedInputAmount, fill.InputAmount)

	row, created, err := s.store.ConfirmSellTransaction(ctx, postgres.ConfirmSellTransactionParams{
		GroupID:          req.GroupID,
		Amount:           req.Amount,
		InputMint:        req.InputMint,
		OutputMint:       jupiter.USDCMint,
		TxSignature:      fill.Signature,
		ExecuteRequestID: sub.RequestID,
		ProceedsUSDC:     proceeds,
		TokenDecimals:    tokenDecimals,
	})
	if err != nil {
		return SellToUSDCResult{}, err
	}

	if created {
		s.applySellBalances(treasury.SolanaAddress, req.InputMint, req.Amount, proceeds)
		navVals, err := s.navSnapshotAfterSwap(ctx, req.GroupID, treasury.SolanaAddress)
		if err != nil {
			return SellToUSDCResult{}, err
		}
		if err := s.store.WriteNavSnapshotOnTransactionConfirm(ctx, req.GroupID, navVals); err != nil {
			return SellToUSDCResult{}, err
		}
	}

	logSwapSellSuccess(req.GroupID, req.UserID, req.Symbol, row.ID, created)
	return SellToUSDCResult{Transaction: row, Created: created, ProceedsUSDC: proceeds}, nil
}

// navSnapshotAfterSwap values the pot for the NAV history row written after a confirmed swap.
// The swap already moved funds, so a missing live mark must not fail it: history takes the
// best available mark.
func (s *SwapService) navSnapshotAfterSwap(ctx context.Context, groupID, treasuryAddress string) (postgres.NavSnapshotValues, error) {
	treasuryUsdc, err := s.treasuryUSDCForSnapshot(ctx, treasuryAddress)
	if err != nil {
		return postgres.NavSnapshotValues{}, err
	}
	valuation, err := valuePot(ctx, s.store, s.pyth, s.symbols, nil, groupID, treasuryAddress, treasuryUsdc, potMarksBestAvailable)
	if err != nil {
		return postgres.NavSnapshotValues{}, err
	}
	return valuation.navSnapshotValues()
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

func swapAssetKind(kind xstocks.AssetKind) swapprovider.AssetKind {
	if kind == xstocks.AssetKindPreIPO {
		return swapprovider.AssetKindPreIPO
	}
	return swapprovider.AssetKindStock
}

func (s *SwapService) assetForInputMint(ctx context.Context, groupID, mint string) xstocks.CatalogAsset {
	if s.buy != nil {
		if asset, ok := s.buy.LookupAssetByMint(ctx, mint); ok {
			return asset
		}
	}
	if s.store != nil {
		if decimals, ok, err := s.store.MaxTokenDecimalsForGroupMint(ctx, groupID, mint); err == nil && ok {
			return xstocks.CatalogAsset{SolanaMint: mint, Decimals: decimals}.Normalize()
		}
	}
	return xstocks.CatalogAsset{SolanaMint: mint}.Normalize()
}

func (s *SwapService) reconcileBuyFill(ctx context.Context, groupID, treasuryAddress, outputMint string, fill swapprovider.Fill) (costBasisAmount, quotedOut, receivedOut int64) {
	quotedOut = fill.QuotedOutputAmount
	if quotedOut <= 0 {
		quotedOut = fill.OutputAmount
	}
	receivedOut = fill.OutputAmount

	if s.privy == nil || fill.Signature == "" {
		return receivedOut, quotedOut, receivedOut
	}
	delta, err := s.privy.TokenBalanceDelta(ctx, fill.Signature, treasuryAddress, outputMint)
	if err == nil && delta > 0 {
		receivedOut = delta
		return delta, quotedOut, receivedOut
	}
	if err != nil {
		slog.Warn("fill reconciliation unavailable",
			"group_id", groupID,
			"tx_signature", fill.Signature,
			"output_mint", outputMint,
			"err", err,
		)
	}
	return receivedOut, quotedOut, receivedOut
}

func (s *SwapService) logBuyFillReconciliation(ctx context.Context, req DevExecuteBuyRequest, asset xstocks.CatalogAsset, transferFeeBps int, quotedOut, receivedOut int64, fill swapprovider.Fill, requestID string) {
	if quotedOut <= 0 {
		return
	}
	feeBpsObserved := (quotedOut - receivedOut) * 10_000 / quotedOut
	slog.Info("swap buy fill reconciliation",
		"group_id", req.GroupID,
		"user_id", req.UserID,
		"symbol", req.Symbol,
		"request_id", requestID,
		"quoted_out", quotedOut,
		"received_out", receivedOut,
		"fee_bps_observed", feeBpsObserved,
	)

	slippageBps := 50
	if asset.Kind == xstocks.AssetKindPreIPO {
		slippageBps = jupiter.PreIPOSlippageBps
	}
	if feeBpsObserved > int64(transferFeeBps+slippageBps) {
		telemetry.Alert(ctx, telemetry.AlertEvent{
			Kind:     "swap_fill_fee_high",
			Key:      "swap_fill_fee_high:" + requestID,
			Severity: telemetry.SeverityWarning,
			Title:    "Treasury buy fill wider than expected",
			Detail:   "Observed output fee exceeded transfer fee plus slippage tolerance.",
			Fields: map[string]string{
				"group_id":         req.GroupID,
				"symbol":           req.Symbol,
				"fee_bps_observed": fmt.Sprintf("%d", feeBpsObserved),
			},
		})
	}
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

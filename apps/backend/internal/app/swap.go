package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/solana/txsign"
	"github.com/monaco/monaco/apps/backend/internal/swapprovider"
	"github.com/monaco/monaco/apps/backend/internal/telemetry"
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
	Created       bool
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
	Created       bool
	// ProceedsUSDC is the USDC the fill raised, in micros.
	ProceedsUSDC int64
}

// ErrSwapOutcomeUnknown means the swap was handed to the venue but its result was not observed.
// The transaction row stays pending until the swap reconciler resolves it; callers must not retry.
var ErrSwapOutcomeUnknown = errors.New("swap outcome unknown; left pending for reconcile")

// ErrSwapInFlight means the proposal already has a pending swap whose outcome is not known yet.
var ErrSwapInFlight = errors.New("swap already in flight for proposal")

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
	pollConfig jupiter.PollConfig
	symbols    *SymbolResolver

	jupiterProvider *jupiter.SwapProvider
	// providers holds every configured venue by name so a pending swap is always resolved by
	// the venue that took it, whichever one executes new swaps.
	providers map[string]swapprovider.Provider
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
		symbols:    symbols,
	}
	s.jupiterProvider = jupiter.NewSwapProvider(jupiterClient, swapTransactionSigner{swap: s})
	s.provider = s.jupiterProvider
	s.providers = map[string]swapprovider.Provider{s.jupiterProvider.Name(): s.jupiterProvider}
	return s
}

// SetSwapProvider replaces the default Jupiter provider (SWAP_PROVIDER=flash).
func (s *SwapService) SetSwapProvider(provider swapprovider.Provider) {
	if provider != nil {
		s.provider = provider
		s.providers[provider.Name()] = provider
	}
}

// SetChainReader wires the Solana reader that decides Jupiter swaps whose outcome was not
// observed. Until it is set those swaps stay pending.
func (s *SwapService) SetChainReader(chain swapprovider.ChainReader) {
	s.jupiterProvider.SetChainReader(chain)
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

	outputMint, err := s.buy.ResolveOutputMint(ctx, req.Symbol)
	if err != nil {
		logSwapBranchError("swap buy resolve mint failed", err, "group_id", req.GroupID, "user_id", req.UserID, "symbol", req.Symbol, "stage", "resolve_mint")
		return DevExecuteBuyResult{}, err
	}

	swapReq := swapprovider.Request{
		GroupID:        req.GroupID,
		UserID:         req.UserID,
		Symbol:         req.Symbol,
		Side:           swapprovider.SideBuy,
		InputMint:      jupiter.USDCMint,
		OutputMint:     outputMint,
		InputDecimals:  usdcDecimals,
		OutputDecimals: jupiter.XStockDecimals,
		Amount:         req.USDCAmount,
		Wallet:         swapprovider.Wallet{PrivyWalletID: treasury.PrivyWalletID, SolanaAddress: treasury.SolanaAddress},
	}
	row, created, err := s.executeSwap(ctx, swapExecution{
		req:           swapReq,
		action:        postgres.TransactionActionBuy,
		proposalID:    req.ProposalID,
		agentIntentID: req.AgentIntentID,
		initiatedBy:   req.InitiatedBy,
	})
	if err != nil {
		return DevExecuteBuyResult{}, err
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

	swapReq := swapprovider.Request{
		GroupID:        req.GroupID,
		UserID:         req.UserID,
		Symbol:         req.Symbol,
		Side:           swapprovider.SideSell,
		InputMint:      req.InputMint,
		OutputMint:     jupiter.USDCMint,
		InputDecimals:  jupiter.XStockDecimals,
		OutputDecimals: usdcDecimals,
		Amount:         req.Amount,
		Wallet:         swapprovider.Wallet{PrivyWalletID: treasury.PrivyWalletID, SolanaAddress: treasury.SolanaAddress},
	}
	row, created, err := s.executeSwap(ctx, swapExecution{
		req:           swapReq,
		action:        postgres.TransactionActionSell,
		proposalID:    req.ProposalID,
		agentIntentID: req.AgentIntentID,
		initiatedBy:   req.InitiatedBy,
	})
	if err != nil {
		return SellToUSDCResult{}, err
	}

	logSwapSellSuccess(req.GroupID, req.UserID, req.Symbol, row.ID, created)
	return SellToUSDCResult{Transaction: row, Created: created, ProceedsUSDC: proceeds}, nil
}

// swapExecution is one treasury swap plus the ledger links its transaction row carries.
type swapExecution struct {
	req           swapprovider.Request
	action        string
	proposalID    string
	agentIntentID string
	initiatedBy   string
}

// executeSwap runs one swap exactly once. The pending row is written before anything is
// submitted, so a crash or timeout after submit leaves a row the reconciler can resolve, and
// the row is marked failed only when the venue guarantees no funds moved. For a proposal the
// row also takes the proposal's single execution slot, so a concurrent executor stops here.
func (s *SwapService) executeSwap(ctx context.Context, exec swapExecution) (postgres.TransactionRow, bool, error) {
	req := exec.req
	logAttrs := []any{
		"group_id", req.GroupID, "user_id", req.UserID, "symbol", req.Symbol,
		"provider", s.provider.Name(), "side", string(req.Side), "amount", req.Amount,
	}

	if exec.proposalID != "" {
		if row, done, err := s.activeProposalSwap(ctx, exec.proposalID, exec.action); err != nil || done {
			return row, false, err
		}
	}

	var prepared swapprovider.Prepared
	var err error
	if req.Side == swapprovider.SideSell {
		prepared, err = s.provider.PrepareSell(ctx, req)
	} else {
		prepared, err = s.provider.PrepareBuy(ctx, req)
	}
	if err != nil {
		if errors.Is(err, swapprovider.ErrNotRoutable) {
			logSwapRefusal(req.GroupID, req.UserID, req.Symbol, err.Error())
			return postgres.TransactionRow{}, false, ErrQuoteNotRoutable
		}
		stage, requestID := swapprovider.StageOf(err, "prepare_"+string(req.Side))
		logSwapBranchError("swap prepare failed", err, append(logAttrs,
			"stage", stage, "request_id", requestID, "taker", req.Wallet.SolanaAddress,
			"input_mint", req.InputMint, "output_mint", req.OutputMint)...)
		return postgres.TransactionRow{}, false, err
	}

	intent, err := s.store.InsertSwapIntent(ctx, postgres.InsertSwapIntentParams{
		GroupID:           req.GroupID,
		ProposalID:        exec.proposalID,
		AgentIntentID:     exec.agentIntentID,
		InitiatedBy:       exec.initiatedBy,
		Action:            exec.action,
		InputMint:         req.InputMint,
		OutputMint:        req.OutputMint,
		Amount:            req.Amount,
		Provider:          s.provider.Name(),
		ExecuteRequestID:  prepared.RequestID,
		SignedTxSignature: prepared.TxSignature,
		SignedTxBlockhash: prepared.Blockhash,
		SubmitExpiresAt:   prepared.ExpiresAt,
	})
	if errors.Is(err, postgres.ErrActiveSwapExists) {
		// Another executor took the proposal's slot between the check above and this insert.
		// Nothing was submitted here; the signed payload is simply dropped.
		row, _, err := s.activeProposalSwap(ctx, exec.proposalID, exec.action)
		if err == nil && row.ID == "" {
			err = ErrSwapInFlight
		}
		return row, false, err
	}
	if err != nil {
		logSwapBranchError("swap intent insert failed", err, append(logAttrs, "stage", "record_intent", "request_id", prepared.RequestID)...)
		return postgres.TransactionRow{}, false, err
	}
	logAttrs = append(logAttrs, "transaction_id", intent.ID)

	sub, err := s.provider.Submit(ctx, prepared)
	if err != nil {
		stage, requestID := swapprovider.StageOf(err, "submit_"+string(req.Side))
		logSwapBranchError("swap submit failed", err, append(logAttrs, "stage", stage, "request_id", requestID)...)
		if errors.Is(err, swapprovider.ErrNotSubmitted) {
			s.failPendingSwap(ctx, intent.ID, err.Error(), logAttrs)
			return postgres.TransactionRow{}, false, err
		}
		return postgres.TransactionRow{}, false, fmt.Errorf("%w: %w", ErrSwapOutcomeUnknown, err)
	}
	logSwapExecuteSubmit(req.GroupID, req.UserID, req.Symbol, prepared.TxSignature, sub.RequestID)
	if err := s.store.MarkSwapSubmitted(ctx, intent.ID, sub.RequestID); err != nil {
		// The swap is live; keep going so an inline fill still lands in the ledger.
		logSwapBranchError("swap mark submitted failed", err, append(logAttrs, "stage", "mark_submitted", "request_id", sub.RequestID)...)
	}

	fill, err := s.provider.AwaitFill(ctx, sub, s.pollConfig)
	if err != nil {
		logSwapBranchError("swap poll failed", err, append(logAttrs,
			"stage", "poll_confirm", "request_id", sub.RequestID, "status", fill.Status, "code", fill.Code,
			"confirmed", fill.Confirmed, "rejected", fill.Rejected)...)
		if fill.Rejected && !fill.Confirmed {
			s.failPendingSwap(ctx, intent.ID, err.Error(), logAttrs)
			return postgres.TransactionRow{}, false, err
		}
		// Timeout, poll error, or a confirmed fill with unreadable amounts: funds may have moved.
		return postgres.TransactionRow{}, false, fmt.Errorf("%w: %w", ErrSwapOutcomeUnknown, err)
	}
	logSwapPollTransition(req.GroupID, req.UserID, req.Symbol, fill.Signature, jupiter.ExecuteStatusPending, fill.Status, fill.Code)

	return s.confirmSwap(ctx, intent.ID, req, fill)
}

// activeProposalSwap reports whether the proposal's execution slot is taken. done is true with
// the confirmed row when the swap already landed; a pending row returns ErrSwapInFlight.
func (s *SwapService) activeProposalSwap(ctx context.Context, proposalID, action string) (postgres.TransactionRow, bool, error) {
	active, found, err := s.store.GetActiveSwapByProposalAndAction(ctx, proposalID, action)
	if err != nil || !found {
		return postgres.TransactionRow{}, false, err
	}
	if active.Status == postgres.TransactionStatusConfirmed {
		return active, true, nil
	}
	return postgres.TransactionRow{}, false, fmt.Errorf("%w: transaction %s", ErrSwapInFlight, active.ID)
}

// confirmSwap records the fill on the pending row and snapshots NAV from the on-chain balance.
func (s *SwapService) confirmSwap(ctx context.Context, transactionID string, req swapprovider.Request, fill swapprovider.Fill) (postgres.TransactionRow, bool, error) {
	row, created, err := s.store.ConfirmPendingSwap(ctx, transactionID, postgres.SwapFill{
		TxSignature:  fill.Signature,
		InputAmount:  fill.InputAmount,
		OutputAmount: fill.OutputAmount,
	})
	if err != nil {
		logSwapBranchError("swap confirm failed", err,
			"group_id", req.GroupID, "transaction_id", transactionID, "tx_signature", fill.Signature, "stage", "record_fill")
		return postgres.TransactionRow{}, false, err
	}
	if created {
		treasuryUsdc, err := s.treasuryUSDCForSnapshot(ctx, req.Wallet.SolanaAddress)
		if err != nil {
			return postgres.TransactionRow{}, false, err
		}
		if err := s.store.WriteNavSnapshotOnTransactionConfirm(ctx, req.GroupID, treasuryUsdc); err != nil {
			return postgres.TransactionRow{}, false, err
		}
	}
	return row, created, nil
}

func (s *SwapService) failPendingSwap(ctx context.Context, transactionID, reason string, logAttrs []any) {
	if _, _, err := s.store.FailPendingSwap(ctx, transactionID, reason); err != nil {
		// The row stays pending, which blocks a retry: safe, and the reconciler will look again.
		logSwapBranchError("swap mark failed failed", err, append(logAttrs, "stage", "mark_failed")...)
	}
}

// treasuryUSDCForSnapshot reads the treasury's USDC from the chain. Swaps move it, so nothing
// remembered in process is ever a substitute.
func (s *SwapService) treasuryUSDCForSnapshot(ctx context.Context, treasuryAddress string) (int64, error) {
	balance, err := s.privy.TreasuryUSDCBalance(ctx, treasuryAddress)
	if err != nil {
		return 0, fmt.Errorf("treasury usdc balance: %w", err)
	}
	return balance, nil
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

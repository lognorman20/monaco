package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/telemetry"
)

// SignatureState is what the chain knows about a transaction signature.
type SignatureState string

const (
	// SignatureNotFound: no status, even in transaction history. The transaction has not
	// landed; whether it still can depends on its blockhash expiry.
	SignatureNotFound SignatureState = "not_found"
	// SignaturePending: seen but below confirmed commitment.
	SignaturePending SignatureState = "pending"
	// SignatureConfirmed: landed successfully at confirmed or finalized commitment.
	SignatureConfirmed SignatureState = "confirmed"
	// SignatureFailed: landed and failed. It is final and moved no funds.
	SignatureFailed SignatureState = "failed"
)

// SignatureStatus is the chain outcome of one transaction. Err is set for SignatureFailed.
type SignatureStatus struct {
	State SignatureState
	Err   string
}

// SolanaRPC resolves on-chain sweep outcomes for the sweep poller.
type SolanaRPC interface {
	SignatureStatus(ctx context.Context, txSignature string) (SignatureStatus, error)
	FinalizedBlockHeight(ctx context.Context) (uint64, error)
}

// HTTPSolanaRPC confirms transactions via Solana JSON-RPC.
type HTTPSolanaRPC struct {
	endpoint   string
	httpClient *http.Client
}

// NewHTTPSolanaRPC returns an RPC client for endpoint; pass config.Config.SolanaRPCEndpoint()
// so SOLANA_RPC_URL is honoured. This client confirms sweeps and payouts, so it must not
// silently fall back to the rate-limited public endpoint when a paid one is configured.
func NewHTTPSolanaRPC(endpoint string) *HTTPSolanaRPC {
	return &HTTPSolanaRPC{
		endpoint: strings.TrimSpace(endpoint),
		httpClient: telemetry.InstrumentClient(telemetry.UpstreamSolanaRPC, &http.Client{
			Timeout: 15 * time.Second,
		}),
	}
}

type solanaRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

type solanaSignatureStatus struct {
	Err                any    `json:"err"`
	ConfirmationStatus string `json:"confirmationStatus"`
}

type solanaSignatureStatusesResponse struct {
	Result struct {
		Value []*solanaSignatureStatus `json:"value"`
	} `json:"result"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type solanaBlockHeightResponse struct {
	Result uint64 `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type solanaBalanceResponse struct {
	Result struct {
		Value uint64 `json:"value"`
	} `json:"result"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type solanaTokenAccountsByOwnerResponse struct {
	Result struct {
		Value []struct {
			Account struct {
				Data struct {
					Parsed struct {
						Info struct {
							Mint        string `json:"mint"`
							TokenAmount struct {
								Amount string `json:"amount"`
							} `json:"tokenAmount"`
						} `json:"info"`
					} `json:"parsed"`
				} `json:"data"`
			} `json:"account"`
		} `json:"value"`
	} `json:"result"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// GetBalance returns lamports for a base58 Solana address via getBalance RPC.
func (r *HTTPSolanaRPC) GetBalance(ctx context.Context, address string) (uint64, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return 0, fmt.Errorf("address is required")
	}

	payload, err := json.Marshal(solanaRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "getBalance",
		Params:  []any{address},
	})
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("solana rpc status %d: %s", resp.StatusCode, string(respBody))
	}

	var rpcResp solanaBalanceResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return 0, err
	}
	if rpcResp.Error != nil {
		return 0, fmt.Errorf("solana rpc error: %s", rpcResp.Error.Message)
	}
	return rpcResp.Result.Value, nil
}

// GetSPLTokenBalance returns the raw token amount for ownerAddress and mint via getTokenAccountsByOwner.
func (r *HTTPSolanaRPC) GetSPLTokenBalance(ctx context.Context, ownerAddress, mint string) (uint64, error) {
	ownerAddress = strings.TrimSpace(ownerAddress)
	mint = strings.TrimSpace(mint)
	if ownerAddress == "" {
		return 0, fmt.Errorf("owner address is required")
	}
	if mint == "" {
		return 0, fmt.Errorf("mint is required")
	}

	payload, err := json.Marshal(solanaRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "getTokenAccountsByOwner",
		Params: []any{
			ownerAddress,
			map[string]string{"mint": mint},
			map[string]string{"encoding": "jsonParsed"},
		},
	})
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("solana rpc status %d: %s", resp.StatusCode, string(respBody))
	}

	var rpcResp solanaTokenAccountsByOwnerResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return 0, err
	}
	if rpcResp.Error != nil {
		return 0, fmt.Errorf("solana rpc error: %s", rpcResp.Error.Message)
	}

	var total uint64
	for _, entry := range rpcResp.Result.Value {
		raw := strings.TrimSpace(entry.Account.Data.Parsed.Info.TokenAmount.Amount)
		if raw == "" || raw == "0" {
			continue
		}
		amount, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid token amount %q for mint %s: %w", raw, mint, err)
		}
		total += amount
	}
	return total, nil
}

// IsConfirmed reports whether txSignature landed successfully. An on-chain failure is an error.
func (r *HTTPSolanaRPC) IsConfirmed(ctx context.Context, txSignature string) (bool, error) {
	status, err := r.SignatureStatus(ctx, txSignature)
	if err != nil {
		return false, err
	}
	if status.State == SignatureFailed {
		return false, fmt.Errorf("transaction failed on chain: %s", status.Err)
	}
	return status.State == SignatureConfirmed, nil
}

// SignatureStatus returns the chain outcome of txSignature via getSignatureStatuses.
// A transaction that failed on chain is a status, not an error: errors mean the RPC call
// itself did not produce an answer.
func (r *HTTPSolanaRPC) SignatureStatus(ctx context.Context, txSignature string) (SignatureStatus, error) {
	txSignature = strings.TrimSpace(txSignature)
	if txSignature == "" {
		err := fmt.Errorf("transaction signature is required")
		logSolanaRPCConfirmationCheck(txSignature, false, "", err)
		return SignatureStatus{}, err
	}

	payload, err := json.Marshal(solanaRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "getSignatureStatuses",
		Params: []any{
			[]string{txSignature},
			map[string]bool{"searchTransactionHistory": true},
		},
	})
	if err != nil {
		return SignatureStatus{}, err
	}

	respBody, err := r.post(ctx, payload)
	if err != nil {
		logSolanaRPCConfirmationCheck(txSignature, false, "", err)
		return SignatureStatus{}, err
	}

	var rpcResp solanaSignatureStatusesResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return SignatureStatus{}, err
	}
	if rpcResp.Error != nil {
		err := fmt.Errorf("solana rpc error: %s", rpcResp.Error.Message)
		logSolanaRPCConfirmationCheck(txSignature, false, "", err)
		return SignatureStatus{}, err
	}
	if len(rpcResp.Result.Value) == 0 || rpcResp.Result.Value[0] == nil {
		logSolanaRPCConfirmationCheck(txSignature, false, "", nil)
		return SignatureStatus{State: SignatureNotFound}, nil
	}

	// Below confirmed commitment the block can still be forked away, so neither success nor
	// failure is an outcome yet.
	status := rpcResp.Result.Value[0]
	switch status.ConfirmationStatus {
	case "confirmed", "finalized":
	default:
		logSolanaRPCConfirmationCheck(txSignature, false, status.ConfirmationStatus, nil)
		return SignatureStatus{State: SignaturePending}, nil
	}
	if status.Err != nil {
		chainErr := fmt.Sprintf("%v", status.Err)
		logSolanaRPCConfirmationCheck(txSignature, false, status.ConfirmationStatus, fmt.Errorf("transaction failed on chain: %s", chainErr))
		return SignatureStatus{State: SignatureFailed, Err: chainErr}, nil
	}
	logSolanaRPCConfirmationCheck(txSignature, true, status.ConfirmationStatus, nil)
	return SignatureStatus{State: SignatureConfirmed}, nil
}

// FinalizedBlockHeight returns the finalized block height via getBlockHeight. A transaction
// whose last valid block height is below it can no longer land.
func (r *HTTPSolanaRPC) FinalizedBlockHeight(ctx context.Context) (uint64, error) {
	payload, err := json.Marshal(solanaRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "getBlockHeight",
		Params:  []any{map[string]string{"commitment": "finalized"}},
	})
	if err != nil {
		return 0, err
	}

	respBody, err := r.post(ctx, payload)
	if err != nil {
		return 0, err
	}

	var rpcResp solanaBlockHeightResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return 0, err
	}
	if rpcResp.Error != nil {
		return 0, fmt.Errorf("solana rpc error: %s", rpcResp.Error.Message)
	}
	return rpcResp.Result, nil
}

func (r *HTTPSolanaRPC) post(ctx context.Context, payload []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("solana rpc status %d: %s", resp.StatusCode, string(respBody))
	}
	return respBody, nil
}

// fakeSolanaRPC is the locked test double for sweep confirmation.
type fakeSolanaRPC struct {
	mu          sync.Mutex
	statuses    map[string]SignatureStatus
	blockHeight uint64
	statusErr   error
	heightErr   error
}

// NewFakeSolanaRPC returns an in-memory RPC client for tests.
func NewFakeSolanaRPC() *fakeSolanaRPC {
	return &fakeSolanaRPC{statuses: make(map[string]SignatureStatus)}
}

// Confirm marks a signature confirmed for tests.
func (f *fakeSolanaRPC) Confirm(txSignature string) {
	f.setStatus(txSignature, SignatureStatus{State: SignatureConfirmed})
}

// FailOnChain marks a signature landed-and-failed for tests.
func (f *fakeSolanaRPC) FailOnChain(txSignature, chainErr string) {
	f.setStatus(txSignature, SignatureStatus{State: SignatureFailed, Err: chainErr})
}

// SetBlockHeight sets the finalized block height for tests.
func (f *fakeSolanaRPC) SetBlockHeight(height uint64) {
	f.mu.Lock()
	f.blockHeight = height
	f.mu.Unlock()
}

// SetErrors forces SignatureStatus and FinalizedBlockHeight to fail for tests.
func (f *fakeSolanaRPC) SetErrors(statusErr, heightErr error) {
	f.mu.Lock()
	f.statusErr = statusErr
	f.heightErr = heightErr
	f.mu.Unlock()
}

func (f *fakeSolanaRPC) setStatus(txSignature string, status SignatureStatus) {
	f.mu.Lock()
	f.statuses[txSignature] = status
	f.mu.Unlock()
}

func (f *fakeSolanaRPC) SignatureStatus(ctx context.Context, txSignature string) (SignatureStatus, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.statusErr != nil {
		return SignatureStatus{}, f.statusErr
	}
	status, ok := f.statuses[txSignature]
	if !ok {
		return SignatureStatus{State: SignatureNotFound}, nil
	}
	return status, nil
}

func (f *fakeSolanaRPC) FinalizedBlockHeight(ctx context.Context) (uint64, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.heightErr != nil {
		return 0, f.heightErr
	}
	return f.blockHeight, nil
}

package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SolanaRPC confirms on-chain transactions for sweep credit.
type SolanaRPC interface {
	IsConfirmed(ctx context.Context, txSignature string) (bool, error)
}

// HTTPSolanaRPC confirms transactions via Solana JSON-RPC.
type HTTPSolanaRPC struct {
	endpoint   string
	httpClient *http.Client
}

// NewHTTPSolanaRPC returns a mainnet RPC client for the given cluster name.
func NewHTTPSolanaRPC(cluster string) *HTTPSolanaRPC {
	cluster = strings.TrimSpace(cluster)
	if cluster == "" {
		cluster = "mainnet-beta"
	}
	return &HTTPSolanaRPC{
		endpoint: fmt.Sprintf("https://api.%s.solana.com", cluster),
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
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

func (r *HTTPSolanaRPC) IsConfirmed(ctx context.Context, txSignature string) (bool, error) {
	txSignature = strings.TrimSpace(txSignature)
	if txSignature == "" {
		return false, fmt.Errorf("transaction signature is required")
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
		return false, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(payload))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Errorf("solana rpc status %d: %s", resp.StatusCode, string(respBody))
	}

	var rpcResp solanaSignatureStatusesResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return false, err
	}
	if rpcResp.Error != nil {
		return false, fmt.Errorf("solana rpc error: %s", rpcResp.Error.Message)
	}
	if len(rpcResp.Result.Value) == 0 || rpcResp.Result.Value[0] == nil {
		return false, nil
	}

	status := rpcResp.Result.Value[0]
	if status.Err != nil {
		return false, fmt.Errorf("transaction failed on chain: %v", status.Err)
	}
	switch status.ConfirmationStatus {
	case "confirmed", "finalized":
		return true, nil
	default:
		return false, nil
	}
}

// fakeSolanaRPC is the locked test double for sweep confirmation.
type fakeSolanaRPC struct {
	confirmed map[string]bool
}

// NewFakeSolanaRPC returns an in-memory RPC client for tests.
func NewFakeSolanaRPC() *fakeSolanaRPC {
	return &fakeSolanaRPC{confirmed: make(map[string]bool)}
}

// Confirm marks a signature confirmed for tests.
func (f *fakeSolanaRPC) Confirm(txSignature string) {
	f.confirmed[txSignature] = true
}

func (f *fakeSolanaRPC) IsConfirmed(ctx context.Context, txSignature string) (bool, error) {
	_ = ctx
	return f.confirmed[txSignature], nil
}

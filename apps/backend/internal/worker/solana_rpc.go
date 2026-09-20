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

// NewHTTPSolanaRPC returns an RPC client for endpoint; pass config.Config.SolanaRPCEndpoint()
// so SOLANA_RPC_URL is honoured. This client confirms sweeps and payouts, so it must not
// silently fall back to the rate-limited public endpoint when a paid one is configured.
func NewHTTPSolanaRPC(endpoint string) *HTTPSolanaRPC {
	return &HTTPSolanaRPC{
		endpoint: strings.TrimSpace(endpoint),
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

func (r *HTTPSolanaRPC) IsConfirmed(ctx context.Context, txSignature string) (bool, error) {
	txSignature = strings.TrimSpace(txSignature)
	if txSignature == "" {
		err := fmt.Errorf("transaction signature is required")
		logSolanaRPCConfirmationCheck(txSignature, false, "", err)
		return false, err
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
		logSolanaRPCConfirmationCheck(txSignature, false, "", err)
		return false, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := fmt.Errorf("solana rpc status %d: %s", resp.StatusCode, string(respBody))
		logSolanaRPCConfirmationCheck(txSignature, false, "", err)
		return false, err
	}

	var rpcResp solanaSignatureStatusesResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return false, err
	}
	if rpcResp.Error != nil {
		err := fmt.Errorf("solana rpc error: %s", rpcResp.Error.Message)
		logSolanaRPCConfirmationCheck(txSignature, false, "", err)
		return false, err
	}
	if len(rpcResp.Result.Value) == 0 || rpcResp.Result.Value[0] == nil {
		logSolanaRPCConfirmationCheck(txSignature, false, "", nil)
		return false, nil
	}

	status := rpcResp.Result.Value[0]
	if status.Err != nil {
		err := fmt.Errorf("transaction failed on chain: %v", status.Err)
		logSolanaRPCConfirmationCheck(txSignature, false, status.ConfirmationStatus, err)
		return false, err
	}
	switch status.ConfirmationStatus {
	case "confirmed", "finalized":
		logSolanaRPCConfirmationCheck(txSignature, true, status.ConfirmationStatus, nil)
		return true, nil
	default:
		logSolanaRPCConfirmationCheck(txSignature, false, status.ConfirmationStatus, nil)
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

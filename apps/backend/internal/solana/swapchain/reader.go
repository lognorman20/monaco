// Package swapchain reads Solana JSON-RPC to decide whether a signed treasury swap
// transaction landed, failed, or can no longer land. It implements swapprovider.ChainReader.
package swapchain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/swapprovider"
	"github.com/monaco/monaco/apps/backend/internal/telemetry"
)

const commitmentFinalized = "finalized"

// HTTPReader is a swapprovider.ChainReader backed by Solana JSON-RPC.
type HTTPReader struct {
	endpoint   string
	httpClient *http.Client
}

// NewHTTPReader returns a reader for rpcURL. A blank rpcURL uses the public endpoint of cluster.
func NewHTTPReader(rpcURL, cluster string) *HTTPReader {
	rpcURL = strings.TrimSpace(rpcURL)
	if rpcURL == "" {
		cluster = strings.TrimSpace(cluster)
		if cluster == "" {
			cluster = "mainnet-beta"
		}
		rpcURL = fmt.Sprintf("https://api.%s.solana.com", cluster)
	}
	return &HTTPReader{
		endpoint:   rpcURL,
		httpClient: telemetry.InstrumentClient(telemetry.UpstreamSolanaRPC, &http.Client{Timeout: 15 * time.Second}),
	}
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcEnvelope struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type signatureStatusesResult struct {
	Value []*struct {
		Err                json.RawMessage `json:"err"`
		ConfirmationStatus string          `json:"confirmationStatus"`
	} `json:"value"`
}

type blockhashValidResult struct {
	Value bool `json:"value"`
}

type tokenBalance struct {
	Mint          string `json:"mint"`
	Owner         string `json:"owner"`
	UITokenAmount struct {
		Amount string `json:"amount"`
	} `json:"uiTokenAmount"`
}

type transactionResult struct {
	Meta *struct {
		Err               json.RawMessage `json:"err"`
		PreTokenBalances  []tokenBalance  `json:"preTokenBalances"`
		PostTokenBalances []tokenBalance  `json:"postTokenBalances"`
	} `json:"meta"`
}

// SignatureStatus searches full ledger history for signature.
func (r *HTTPReader) SignatureStatus(ctx context.Context, signature string) (swapprovider.SignatureStatus, error) {
	signature = strings.TrimSpace(signature)
	if signature == "" {
		return swapprovider.SignatureStatus{}, fmt.Errorf("swapchain: signature is required")
	}
	var result signatureStatusesResult
	err := r.call(ctx, "getSignatureStatuses", []any{
		[]string{signature},
		map[string]any{"searchTransactionHistory": true},
	}, &result)
	if err != nil {
		return swapprovider.SignatureStatus{}, err
	}
	if len(result.Value) != 1 {
		return swapprovider.SignatureStatus{}, fmt.Errorf("swapchain: getSignatureStatuses returned %d entries, want 1", len(result.Value))
	}
	entry := result.Value[0]
	if entry == nil {
		return swapprovider.SignatureStatus{}, nil
	}
	status := swapprovider.SignatureStatus{
		Found:     true,
		Finalized: entry.ConfirmationStatus == commitmentFinalized,
	}
	if isRPCErr(entry.Err) {
		status.Failed = true
		status.Err = string(entry.Err)
	}
	return status, nil
}

// IsBlockhashValid reports whether blockhash can still land as of the finalized bank.
func (r *HTTPReader) IsBlockhashValid(ctx context.Context, blockhash string) (bool, error) {
	blockhash = strings.TrimSpace(blockhash)
	if blockhash == "" {
		return false, fmt.Errorf("swapchain: blockhash is required")
	}
	var result blockhashValidResult
	err := r.call(ctx, "isBlockhashValid", []any{
		blockhash,
		map[string]any{"commitment": commitmentFinalized},
	}, &result)
	if err != nil {
		return false, err
	}
	return result.Value, nil
}

// TokenBalanceChanges returns post-minus-pre token balances per mint for owner.
func (r *HTTPReader) TokenBalanceChanges(ctx context.Context, signature, owner string) (map[string]int64, error) {
	signature = strings.TrimSpace(signature)
	owner = strings.TrimSpace(owner)
	if signature == "" || owner == "" {
		return nil, fmt.Errorf("swapchain: signature and owner are required")
	}
	var result *transactionResult
	err := r.call(ctx, "getTransaction", []any{
		signature,
		map[string]any{
			"encoding":                       "jsonParsed",
			"commitment":                     commitmentFinalized,
			"maxSupportedTransactionVersion": 0,
		},
	}, &result)
	if err != nil {
		return nil, err
	}
	if result == nil || result.Meta == nil {
		return nil, fmt.Errorf("swapchain: transaction %s has no finalized metadata", signature)
	}
	if isRPCErr(result.Meta.Err) {
		return nil, fmt.Errorf("swapchain: transaction %s failed on chain: %s", signature, string(result.Meta.Err))
	}

	changes := make(map[string]int64)
	for _, post := range result.Meta.PostTokenBalances {
		if post.Owner != owner {
			continue
		}
		amount, err := parseTokenAmount(post)
		if err != nil {
			return nil, err
		}
		changes[post.Mint] += amount
	}
	for _, pre := range result.Meta.PreTokenBalances {
		if pre.Owner != owner {
			continue
		}
		amount, err := parseTokenAmount(pre)
		if err != nil {
			return nil, err
		}
		changes[pre.Mint] -= amount
	}
	return changes, nil
}

func parseTokenAmount(balance tokenBalance) (int64, error) {
	amount, err := strconv.ParseInt(balance.UITokenAmount.Amount, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("swapchain: invalid token amount %q for mint %s: %w", balance.UITokenAmount.Amount, balance.Mint, err)
	}
	return amount, nil
}

func isRPCErr(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed != "" && trimmed != "null"
}

func (r *HTTPReader) call(ctx context.Context, method string, params []any, out any) error {
	payload, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		logRPCFailure(method, 0, err)
		return fmt.Errorf("swapchain: %s: %w", method, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logRPCFailure(method, resp.StatusCode, err)
		return fmt.Errorf("swapchain: %s: read body: %w", method, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := fmt.Errorf("swapchain: %s: rpc status %d", method, resp.StatusCode)
		logRPCFailure(method, resp.StatusCode, err)
		return err
	}

	var envelope rpcEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		logRPCFailure(method, resp.StatusCode, err)
		return fmt.Errorf("swapchain: %s: invalid json: %w", method, err)
	}
	if envelope.Error != nil {
		err := fmt.Errorf("swapchain: %s: rpc error %d: %s", method, envelope.Error.Code, envelope.Error.Message)
		logRPCFailure(method, resp.StatusCode, err)
		return err
	}
	if len(envelope.Result) == 0 {
		err := fmt.Errorf("swapchain: %s: response has no result", method)
		logRPCFailure(method, resp.StatusCode, err)
		return err
	}
	if err := json.Unmarshal(envelope.Result, out); err != nil {
		logRPCFailure(method, resp.StatusCode, err)
		return fmt.Errorf("swapchain: %s: invalid result: %w", method, err)
	}
	return nil
}

func logRPCFailure(method string, httpStatus int, err error) {
	slog.Error("swapchain rpc failed", "method", method, "http_status", httpStatus, "err", err)
}

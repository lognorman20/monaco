package privy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type solanaRPCEnvelope struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// callSolanaRPC posts one JSON-RPC call and decodes its result into out.
func (c *HTTPClient) callSolanaRPC(ctx context.Context, method string, params []any, out any) error {
	payload, err := json.Marshal(solanaRPCRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.solanaRPCEndpoint(), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		logSolanaRPC(method, 0, nil, err)
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		logSolanaRPC(method, resp.StatusCode, respBody, err)
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		rpcErr := fmt.Errorf("%w: solana rpc status %d: %s", ErrAPI, resp.StatusCode, string(respBody))
		logSolanaRPC(method, resp.StatusCode, respBody, rpcErr)
		return rpcErr
	}

	var envelope solanaRPCEnvelope
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		logSolanaRPC(method, resp.StatusCode, respBody, err)
		return err
	}
	if envelope.Error != nil {
		rpcErr := fmt.Errorf("%w: solana rpc error %d: %s", ErrAPI, envelope.Error.Code, envelope.Error.Message)
		logSolanaRPC(method, resp.StatusCode, respBody, rpcErr)
		return rpcErr
	}
	if len(envelope.Result) == 0 {
		rpcErr := fmt.Errorf("%w: solana rpc %s missing result", ErrAPI, method)
		logSolanaRPC(method, resp.StatusCode, respBody, rpcErr)
		return rpcErr
	}
	if err := json.Unmarshal(envelope.Result, out); err != nil {
		logSolanaRPC(method, resp.StatusCode, respBody, err)
		return err
	}
	logSolanaRPC(method, resp.StatusCode, nil, nil)
	return nil
}

// getLatestBlockhashWithExpiry returns a recent blockhash and the last block height at which
// a transaction carrying it can still land.
func (c *HTTPClient) getLatestBlockhashWithExpiry(ctx context.Context) ([]byte, int64, error) {
	var result struct {
		Value struct {
			Blockhash            string `json:"blockhash"`
			LastValidBlockHeight int64  `json:"lastValidBlockHeight"`
		} `json:"value"`
	}
	if err := c.callSolanaRPC(ctx, "getLatestBlockhash", []any{map[string]string{"commitment": "finalized"}}, &result); err != nil {
		return nil, 0, err
	}
	blockhash := strings.TrimSpace(result.Value.Blockhash)
	if blockhash == "" || result.Value.LastValidBlockHeight <= 0 {
		return nil, 0, fmt.Errorf("%w: solana rpc missing blockhash or last valid block height", ErrAPI)
	}
	decoded, err := decodeBase58Pubkey(blockhash)
	if err != nil {
		return nil, 0, err
	}
	return decoded, result.Value.LastValidBlockHeight, nil
}

func (c *HTTPClient) getFinalizedBlockHeight(ctx context.Context) (int64, error) {
	var height int64
	if err := c.callSolanaRPC(ctx, "getBlockHeight", []any{map[string]string{"commitment": "finalized"}}, &height); err != nil {
		return 0, err
	}
	if height <= 0 {
		return 0, fmt.Errorf("%w: solana rpc returned block height %d", ErrAPI, height)
	}
	return height, nil
}

// getSignatureStatus looks a signature up across the full ledger history. seen is false when
// the cluster has no record of it. A signature that is only `processed` is still pending:
// that block can be skipped.
func (c *HTTPClient) getSignatureStatus(ctx context.Context, txSignature string) (status PayoutStatus, seen bool, err error) {
	var result struct {
		Value []*struct {
			Err                any    `json:"err"`
			ConfirmationStatus string `json:"confirmationStatus"`
		} `json:"value"`
	}
	err = c.callSolanaRPC(ctx, "getSignatureStatuses", []any{
		[]string{txSignature},
		map[string]bool{"searchTransactionHistory": true},
	}, &result)
	if err != nil {
		return PayoutStatus{}, false, err
	}
	if len(result.Value) == 0 || result.Value[0] == nil {
		return PayoutStatus{}, false, nil
	}

	entry := result.Value[0]
	switch entry.ConfirmationStatus {
	case "confirmed", "finalized":
	default:
		return PayoutStatus{State: PayoutStatePending}, true, nil
	}
	if entry.Err != nil {
		return PayoutStatus{State: PayoutStateFailed, Reason: fmt.Sprintf("%v", entry.Err)}, true, nil
	}
	return PayoutStatus{State: PayoutStateConfirmed}, true, nil
}

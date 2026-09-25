package treasury

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
)

type solanaRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

type solanaRPCEnvelope struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// callSolanaRPC posts one JSON-RPC call and decodes its result into out. A JSON null result
// decodes to the zero value of out.
func (c *HTTPClient) callSolanaRPC(ctx context.Context, method string, params []any, out any) error {
	payload, err := json.Marshal(solanaRPCRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.RPCURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		slog.Warn("solana rpc failed", "method", method, "err", err)
		return err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: solana rpc %s status %d", ErrAPI, method, resp.StatusCode)
	}
	var envelope solanaRPCEnvelope
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return err
	}
	if envelope.Error != nil {
		return fmt.Errorf("%w: solana rpc %s error %d: %s", ErrAPI, method, envelope.Error.Code, envelope.Error.Message)
	}
	if len(envelope.Result) == 0 {
		return fmt.Errorf("%w: solana rpc %s missing result", ErrAPI, method)
	}
	return json.Unmarshal(envelope.Result, out)
}

// TreasuryUSDCBalance reads the owner's SPL USDC at confirmed commitment from Solana RPC.
func (c *HTTPClient) TreasuryUSDCBalance(ctx context.Context, treasuryAddress string) (int64, error) {
	treasuryAddress = strings.TrimSpace(treasuryAddress)
	if treasuryAddress == "" {
		return 0, fmt.Errorf("%w: missing wallet address", ErrAPI)
	}
	var result struct {
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
	}
	err := c.callSolanaRPC(ctx, "getTokenAccountsByOwner", []any{
		treasuryAddress,
		map[string]string{"mint": USDCMint},
		map[string]string{"encoding": "jsonParsed", "commitment": "confirmed"},
	}, &result)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, entry := range result.Value {
		info := entry.Account.Data.Parsed.Info
		if info.Mint != USDCMint {
			continue
		}
		amount, err := parseTokenAmount(info.TokenAmount.Amount)
		if err != nil {
			return 0, err
		}
		total += amount
	}
	return total, nil
}

func parseTokenAmount(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	amount, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || amount < 0 {
		return 0, fmt.Errorf("%w: invalid token amount %q", ErrAPI, raw)
	}
	return amount, nil
}

// getLatestBlockhashWithExpiry returns a finalized blockhash and the last block height at
// which a transaction built on it can land.
func (c *HTTPClient) getLatestBlockhashWithExpiry(ctx context.Context) ([]byte, uint64, error) {
	var result struct {
		Value struct {
			Blockhash            string `json:"blockhash"`
			LastValidBlockHeight uint64 `json:"lastValidBlockHeight"`
		} `json:"value"`
	}
	if err := c.callSolanaRPC(ctx, "getLatestBlockhash", []any{map[string]string{"commitment": "finalized"}}, &result); err != nil {
		return nil, 0, err
	}
	blockhash := strings.TrimSpace(result.Value.Blockhash)
	if blockhash == "" {
		return nil, 0, fmt.Errorf("%w: solana rpc missing blockhash", ErrAPI)
	}
	decoded, err := decodeBase58Pubkey(blockhash)
	if err != nil {
		return nil, 0, err
	}
	return decoded, result.Value.LastValidBlockHeight, nil
}

func (c *HTTPClient) getFinalizedBlockHeight(ctx context.Context) (uint64, error) {
	var height uint64
	if err := c.callSolanaRPC(ctx, "getBlockHeight", []any{map[string]string{"commitment": "finalized"}}, &height); err != nil {
		return 0, err
	}
	if height == 0 {
		return 0, fmt.Errorf("%w: solana rpc returned block height 0", ErrAPI)
	}
	return height, nil
}

// getSignatureStatus looks a signature up across the full ledger history. seen is false when
// the cluster has no record of it. A signature that is only `processed` is still pending.
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

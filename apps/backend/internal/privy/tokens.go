package privy

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

// SPLTokenBalance is an on-chain SPL token holding for a wallet owner.
type SPLTokenBalance struct {
	Mint   string
	Amount int64
}

// LookupWalletID returns the Privy wallet id for a Solana address.
func (c *HTTPClient) LookupWalletID(ctx context.Context, address string) (string, error) {
	wallet, err := c.getWalletByAddress(ctx, address)
	if err != nil {
		return "", err
	}
	return wallet.ID, nil
}

// ListSPLTokenBalances returns non-zero SPL token balances for ownerAddress via Solana RPC.
// Queries both classic SPL Token and Token-2022 programs (xStocks use Token-2022).
func (c *HTTPClient) ListSPLTokenBalances(ctx context.Context, ownerAddress string) ([]SPLTokenBalance, error) {
	ownerAddress = strings.TrimSpace(ownerAddress)
	if ownerAddress == "" {
		return nil, fmt.Errorf("%w: missing wallet address", ErrAPI)
	}

	var merged []SPLTokenBalance
	for _, programID := range []string{tokenProgramID, token2022ProgramID} {
		balances, err := c.listSPLTokenBalancesForProgram(ctx, ownerAddress, programID)
		if err != nil {
			return nil, err
		}
		merged = append(merged, balances...)
	}
	return merged, nil
}

func (c *HTTPClient) listSPLTokenBalancesForProgram(ctx context.Context, ownerAddress, programID string) ([]SPLTokenBalance, error) {
	respBody, status, err := c.postSolanaRPCWithRetry(ctx, "getTokenAccountsByOwner", []any{
		ownerAddress,
		map[string]string{"programId": programID},
		map[string]string{"encoding": "jsonParsed"},
	})
	if err != nil {
		logSolanaRPC("getTokenAccountsByOwner", status, respBody, err)
		return nil, err
	}

	balances, err := parseSPLTokenBalances(respBody)
	if err != nil {
		logSolanaRPC("getTokenAccountsByOwner", status, respBody, err)
		return nil, err
	}
	logSolanaRPC("getTokenAccountsByOwner", status, nil, nil)
	return balances, nil
}

func (c *HTTPClient) postSolanaRPCWithRetry(ctx context.Context, method string, params []any) ([]byte, int, error) {
	const maxAttempts = 6
	backoff := 200 * time.Millisecond

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, 0, ctx.Err()
			case <-timer.C:
			}
			if backoff < 2*time.Second {
				backoff *= 2
			}
		}

		respBody, status, err := c.postSolanaRPCOnce(ctx, method, params)
		if err == nil {
			return respBody, status, nil
		}
		if status != http.StatusTooManyRequests || attempt == maxAttempts-1 {
			return respBody, status, err
		}
	}
	return nil, 0, fmt.Errorf("%w: solana rpc retries exhausted", ErrAPI)
}

func (c *HTTPClient) postSolanaRPCOnce(ctx context.Context, method string, params []any) ([]byte, int, error) {
	payload, err := json.Marshal(solanaRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  method,
		Params:  params,
	})
	if err != nil {
		return nil, 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.solanaRPCEndpoint(), bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return respBody, resp.StatusCode, fmt.Errorf("%w: solana rpc status %d: %s", ErrAPI, resp.StatusCode, string(respBody))
	}
	return respBody, resp.StatusCode, nil
}

type tokenAccountsByOwnerResponse struct {
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

func parseSPLTokenBalances(body []byte) ([]SPLTokenBalance, error) {
	var rpcResp tokenAccountsByOwnerResponse
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return nil, fmt.Errorf("%w: solana rpc json: %v", ErrAPI, err)
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("%w: solana rpc error: %s", ErrAPI, rpcResp.Error.Message)
	}

	var balances []SPLTokenBalance
	for _, entry := range rpcResp.Result.Value {
		mint := strings.TrimSpace(entry.Account.Data.Parsed.Info.Mint)
		raw := strings.TrimSpace(entry.Account.Data.Parsed.Info.TokenAmount.Amount)
		if mint == "" || raw == "" || raw == "0" {
			continue
		}
		amount, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid token amount %q for mint %s", ErrAPI, raw, mint)
		}
		if amount <= 0 {
			continue
		}
		balances = append(balances, SPLTokenBalance{Mint: mint, Amount: amount})
	}
	return balances, nil
}

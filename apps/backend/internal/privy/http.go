package privy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

type createWalletRequest struct {
	ChainType   string       `json:"chain_type"`
	DisplayName string       `json:"display_name,omitempty"`
	ExternalID  string       `json:"external_id,omitempty"`
	Owner       *walletOwner `json:"owner,omitempty"`
}

type walletOwner struct {
	UserID string `json:"user_id"`
}

type createWalletResponse struct {
	ID      string `json:"id"`
	Address string `json:"address"`
}

type getWalletByAddressRequest struct {
	Address string `json:"address"`
}

type walletResponse struct {
	ID      string `json:"id"`
	Address string `json:"address"`
}

type walletBalanceResponse struct {
	Balances []walletBalanceEntry `json:"balances"`
}

type walletBalanceEntry struct {
	RawValue string `json:"raw_value"`
}

type walletRPCRequest struct {
	Method string          `json:"method"`
	CAIP2  string          `json:"caip2"`
	Params walletRPCParams `json:"params"`
}

type walletRPCParams struct {
	Transaction string `json:"transaction"`
	Encoding    string `json:"encoding"`
}

type walletRPCResponse struct {
	Data walletRPCData `json:"data"`
}

type walletRPCData struct {
	Hash string `json:"hash"`
}

func (c *HTTPClient) doPrivyRequest(ctx context.Context, method, path string, body []byte, idempotencyKey string) ([]byte, int, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("privy-app-id", c.appID)
	if idempotencyKey != "" {
		req.Header.Set("privy-idempotency-key", idempotencyKey)
	}
	req.SetBasicAuth(c.appID, c.appSecret)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return respBody, resp.StatusCode, nil
}

func (c *HTTPClient) createWallet(ctx context.Context, idempotencyKey string, body createWalletRequest) (createWalletResponse, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return createWalletResponse{}, err
	}

	respBody, status, err := c.doPrivyRequest(ctx, http.MethodPost, "/v1/wallets", payload, idempotencyKey)
	if err != nil {
		return createWalletResponse{}, err
	}
	if status < 200 || status >= 300 {
		return createWalletResponse{}, fmt.Errorf("%w: create wallet status %d: %s", ErrAPI, status, string(respBody))
	}

	var wallet createWalletResponse
	if err := json.Unmarshal(respBody, &wallet); err != nil {
		return createWalletResponse{}, err
	}
	if wallet.ID == "" || wallet.Address == "" {
		return createWalletResponse{}, fmt.Errorf("%w: create wallet missing id or address", ErrAPI)
	}
	return wallet, nil
}

func (c *HTTPClient) getWalletByAddress(ctx context.Context, address string) (walletResponse, error) {
	payload, err := json.Marshal(getWalletByAddressRequest{Address: address})
	if err != nil {
		return walletResponse{}, err
	}

	respBody, status, err := c.doPrivyRequest(ctx, http.MethodPost, "/v1/wallets/address", payload, "")
	if err != nil {
		return walletResponse{}, err
	}
	if status < 200 || status >= 300 {
		return walletResponse{}, fmt.Errorf("%w: get wallet by address status %d: %s", ErrAPI, status, string(respBody))
	}

	var wallet walletResponse
	if err := json.Unmarshal(respBody, &wallet); err != nil {
		return walletResponse{}, err
	}
	if wallet.ID == "" {
		return walletResponse{}, fmt.Errorf("%w: wallet lookup missing id", ErrAPI)
	}
	return wallet, nil
}

func (c *HTTPClient) getWalletUSDCBalance(ctx context.Context, walletID string) (int64, error) {
	query := url.Values{}
	query.Set("token", "solana:"+usdcMintAddress)
	path := fmt.Sprintf("/v1/wallets/%s/balance?%s", url.PathEscape(walletID), query.Encode())

	respBody, status, err := c.doPrivyRequest(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return 0, err
	}
	if status < 200 || status >= 300 {
		return 0, fmt.Errorf("%w: wallet balance status %d: %s", ErrAPI, status, string(respBody))
	}

	var balance walletBalanceResponse
	if err := json.Unmarshal(respBody, &balance); err != nil {
		return 0, err
	}
	if len(balance.Balances) == 0 {
		return 0, nil
	}
	return parseRawTokenAmount(balance.Balances[0].RawValue)
}

func (c *HTTPClient) signAndSendSolanaTransaction(ctx context.Context, walletID, txBase64 string) (string, error) {
	payload, err := json.Marshal(walletRPCRequest{
		Method: "signAndSendTransaction",
		CAIP2:  solanaMainnetCAIP2,
		Params: walletRPCParams{
			Transaction: txBase64,
			Encoding:    "base64",
		},
	})
	if err != nil {
		return "", err
	}

	path := fmt.Sprintf("/v1/wallets/%s/rpc", url.PathEscape(walletID))
	respBody, status, err := c.doPrivyRequest(ctx, http.MethodPost, path, payload, "")
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("%w: sign and send status %d: %s", ErrAPI, status, string(respBody))
	}

	var rpcResp walletRPCResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return "", err
	}
	if rpcResp.Data.Hash == "" {
		return "", fmt.Errorf("%w: sign and send missing hash", ErrAPI)
	}
	return rpcResp.Data.Hash, nil
}

package privy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type createWalletRequest struct {
	ChainType   string          `json:"chain_type"`
	DisplayName string          `json:"display_name,omitempty"`
	ExternalID  string          `json:"external_id,omitempty"`
	Owner       *walletOwner    `json:"owner,omitempty"`
}

type walletOwner struct {
	UserID string `json:"user_id"`
}

type createWalletResponse struct {
	ID      string `json:"id"`
	Address string `json:"address"`
}

func (c *HTTPClient) createWallet(ctx context.Context, idempotencyKey string, body createWalletRequest) (createWalletResponse, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return createWalletResponse{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/wallets", bytes.NewReader(payload))
	if err != nil {
		return createWalletResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("privy-app-id", c.appID)
	if idempotencyKey != "" {
		req.Header.Set("privy-idempotency-key", idempotencyKey)
	}
	req.SetBasicAuth(c.appID, c.appSecret)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return createWalletResponse{}, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return createWalletResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return createWalletResponse{}, fmt.Errorf("%w: create wallet status %d: %s", ErrAPI, resp.StatusCode, string(respBody))
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

package privy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type additionalSigner struct {
	SignerID string `json:"signer_id"`
}

type createWalletRequest struct {
	ChainType         string             `json:"chain_type"`
	DisplayName       string             `json:"display_name,omitempty"`
	ExternalID        string             `json:"external_id,omitempty"`
	Owner             *walletOwner       `json:"owner,omitempty"`
	AdditionalSigners []additionalSigner `json:"additional_signers,omitempty"`
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

type signTransactionRPCRequest struct {
	Method string          `json:"method"`
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
	Hash              string `json:"hash"`
	SignedTransaction string `json:"signed_transaction"`
	Encoding          string `json:"encoding"`
}

type solanaRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

type solanaBlockhashResponse struct {
	Result struct {
		Value struct {
			Blockhash string `json:"blockhash"`
		} `json:"value"`
	} `json:"result"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *HTTPClient) doPrivyRequest(ctx context.Context, method, path string, body []byte, idempotencyKey string) ([]byte, int, error) {
	return c.doPrivyRequestWithAuthorization(ctx, method, path, body, idempotencyKey, false)
}

func (c *HTTPClient) doPrivyRequestWithAuthorization(ctx context.Context, method, path string, body []byte, idempotencyKey string, requireAuthorization bool) ([]byte, int, error) {
	if requireAuthorization && c.privyAuthorizationPrivateKey == "" {
		return nil, 0, fmt.Errorf("%w: PRIVY_AUTHORIZATION_PRIVATE_KEY is required for wallet rpc", ErrAPI)
	}

	logAPIStart(method, path, idempotencyKey != "")

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
	if requireAuthorization {
		bodyMap, err := bodyMapForAuthorization(body)
		if err != nil {
			return nil, 0, err
		}
		payload := buildAuthorizationSignaturePayload(
			method,
			authorizationSignatureURL(c.baseURL, path),
			bodyMap,
			authorizationHeaders(c.appID, idempotencyKey),
		)
		signature, err := signAuthorizationPayload(c.privyAuthorizationPrivateKey, payload)
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set(authorizationSignatureHeader, signature)
	}
	req.SetBasicAuth(c.appID, c.appSecret)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		logAPIResult(method, path, 0, err)
		return nil, 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		logAPIResult(method, path, resp.StatusCode, err)
		return nil, resp.StatusCode, err
	}
	logAPIResult(method, path, resp.StatusCode, nil)
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

func (c *HTTPClient) solanaRPCEndpoint() string {
	if c.solanaRPCURL != "" {
		return c.solanaRPCURL
	}
	cluster := c.solanaCluster
	if cluster == "" {
		cluster = "mainnet-beta"
	}
	return fmt.Sprintf("https://api.%s.solana.com", cluster)
}

func (c *HTTPClient) getLatestBlockhash(ctx context.Context) ([]byte, error) {
	payload, err := json.Marshal(solanaRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "getLatestBlockhash",
		Params:  []any{map[string]string{"commitment": "finalized"}},
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.solanaRPCEndpoint(), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		logSolanaRPC("getLatestBlockhash", err)
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		logSolanaRPC("getLatestBlockhash", err)
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		rpcErr := fmt.Errorf("%w: solana rpc status %d: %s", ErrAPI, resp.StatusCode, string(respBody))
		logSolanaRPC("getLatestBlockhash", rpcErr)
		return nil, rpcErr
	}

	var rpcResp solanaBlockhashResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		logSolanaRPC("getLatestBlockhash", err)
		return nil, err
	}
	if rpcResp.Error != nil {
		rpcErr := fmt.Errorf("%w: solana rpc error: %s", ErrAPI, rpcResp.Error.Message)
		logSolanaRPC("getLatestBlockhash", rpcErr)
		return nil, rpcErr
	}
	blockhash := strings.TrimSpace(rpcResp.Result.Value.Blockhash)
	if blockhash == "" {
		rpcErr := fmt.Errorf("%w: solana rpc missing blockhash", ErrAPI)
		logSolanaRPC("getLatestBlockhash", rpcErr)
		return nil, rpcErr
	}
	logSolanaRPC("getLatestBlockhash", nil)
	return decodeBase58Pubkey(blockhash)
}

// SignSolanaTransaction signs a base64-encoded Solana transaction without broadcasting.
func (c *HTTPClient) SignSolanaTransaction(ctx context.Context, walletID, txBase64 string) (string, error) {
	return c.signSolanaTransaction(ctx, walletID, txBase64)
}

func (c *HTTPClient) signSolanaTransaction(ctx context.Context, walletID, txBase64 string) (string, error) {
	payload, err := json.Marshal(signTransactionRPCRequest{
		Method: "signTransaction",
		Params: walletRPCParams{
			Transaction: txBase64,
			Encoding:    "base64",
		},
	})
	if err != nil {
		return "", err
	}

	path := fmt.Sprintf("/v1/wallets/%s/rpc", url.PathEscape(walletID))
	respBody, status, err := c.doPrivyRequestWithAuthorization(ctx, http.MethodPost, path, payload, "", true)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("%w: sign transaction status %d: %s", ErrAPI, status, string(respBody))
	}

	var rpcResp walletRPCResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return "", err
	}
	if rpcResp.Data.SignedTransaction == "" {
		return "", fmt.Errorf("%w: sign transaction missing signed_transaction", ErrAPI)
	}
	return rpcResp.Data.SignedTransaction, nil
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
	respBody, status, err := c.doPrivyRequestWithAuthorization(ctx, http.MethodPost, path, payload, "", true)
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

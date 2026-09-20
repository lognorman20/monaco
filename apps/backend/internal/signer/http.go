package signer

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"
)

// ErrUnauthorized means the signer rejected the shared secret.
var ErrUnauthorized = errors.New("signer unauthorized")

// ErrSignerUnavailable means the signer sidecar is unreachable.
var ErrSignerUnavailable = errors.New("signer unavailable")

type httpClient struct {
	baseURL string
	secret  string
	http    *http.Client
}

// NewHTTPClient dials the signer sidecar.
func NewHTTPClient(baseURL, secret string) Client {
	return &httpClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		secret:  secret,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *httpClient) do(ctx context.Context, method, path string, body any, dest any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("x-signer-secret", c.secret)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSignerUnavailable, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%w: status %d", ErrSignerUnavailable, resp.StatusCode)
	}
	if dest == nil {
		return nil
	}
	return json.Unmarshal(raw, dest)
}

func (c *httpClient) Health(ctx context.Context) (HealthInfo, error) {
	var payload struct {
		RelayerAddress string `json:"relayerAddress"`
		ChainID        int64  `json:"chainId"`
	}
	if err := c.do(ctx, http.MethodGet, "/healthz", nil, &payload); err != nil {
		return HealthInfo{}, err
	}
	return HealthInfo{RelayerAddress: payload.RelayerAddress, ChainID: payload.ChainID}, nil
}

func (c *httpClient) CreateWallet(ctx context.Context) (CreatedWallet, error) {
	var payload struct {
		WalletID  string          `json:"walletId"`
		Address   string          `json:"address"`
		Metadata  json.RawMessage `json:"metadata"`
		KeyShares json.RawMessage `json:"keyShares"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/wallets", map[string]any{}, &payload); err != nil {
		return CreatedWallet{}, err
	}
	return CreatedWallet{WalletID: payload.WalletID, Address: strings.ToLower(payload.Address), Metadata: payload.Metadata, KeyShares: payload.KeyShares}, nil
}

func (c *httpClient) SignTypedData(ctx context.Context, req SignRequest) (string, error) {
	var payload struct {
		Signature string `json:"signature"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/wallets/sign-typed-data", map[string]any{
		"metadata":  req.Metadata,
		"keyShares": req.KeyShares,
		"typedData": req.TypedData,
	}, &payload); err != nil {
		return "", err
	}
	return payload.Signature, nil
}

func (c *httpClient) SendTransaction(ctx context.Context, req SendRequest) (string, error) {
	var payload struct {
		TxHash string `json:"txHash"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/wallets/send", map[string]any{
		"metadata":  req.Metadata,
		"keyShares": req.KeyShares,
		"to":        req.To,
		"data":      encodeData(req.Data),
		"valueWei":  valueString(req.ValueWei),
	}, &payload); err != nil {
		return "", err
	}
	return payload.TxHash, nil
}

func (c *httpClient) RelayerSend(ctx context.Context, req RelayerSendRequest) (string, error) {
	var payload struct {
		TxHash string `json:"txHash"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/relayer/send", map[string]any{
		"to":       req.To,
		"data":     encodeData(req.Data),
		"valueWei": valueString(req.ValueWei),
	}, &payload); err != nil {
		return "", err
	}
	return payload.TxHash, nil
}

func encodeData(data []byte) string {
	if len(data) == 0 {
		return "0x"
	}
	return "0x" + hex.EncodeToString(data)
}

func valueString(v *big.Int) string {
	if v == nil {
		return "0"
	}
	return v.String()
}

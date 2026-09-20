package signer

import (
	"context"
	"errors"
	"net/http"
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

// NewHTTPClient dials the signer sidecar (stub until M6-T4).
func NewHTTPClient(baseURL, secret string) Client {
	return &httpClient{
		baseURL: baseURL,
		secret:  secret,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *httpClient) unavailable() error {
	return ErrSignerUnavailable
}

func (c *httpClient) Health(ctx context.Context) (HealthInfo, error) {
	_ = ctx
	_ = c
	return HealthInfo{}, c.unavailable()
}

func (c *httpClient) CreateWallet(ctx context.Context) (CreatedWallet, error) {
	_ = ctx
	return CreatedWallet{}, c.unavailable()
}

func (c *httpClient) SignTypedData(ctx context.Context, req SignRequest) (string, error) {
	_ = ctx
	_ = req
	return "", c.unavailable()
}

func (c *httpClient) SendTransaction(ctx context.Context, req SendRequest) (string, error) {
	_ = ctx
	_ = req
	return "", c.unavailable()
}

func (c *httpClient) RelayerSend(ctx context.Context, req RelayerSendRequest) (string, error) {
	_ = ctx
	_ = req
	return "", c.unavailable()
}

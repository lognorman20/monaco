package xstocks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultBaseURL  = "https://api.xstocks.fi"
	solanaNetwork   = "Solana"
	assetsPath      = "/api/v2/public/assets/"
	defaultTimeout  = 15 * time.Second
)

// Resolver resolves an xStock symbol to its Solana mint address.
type Resolver interface {
	ResolveSolanaMint(ctx context.Context, symbol string) (string, error)
}

// HTTPResolver calls the xStocks public catalog API.
type HTTPResolver struct {
	baseURL    string
	httpClient *http.Client
}

// NewHTTPResolver returns a resolver that uses the production xStocks API.
func NewHTTPResolver() *HTTPResolver {
	return &HTTPResolver{
		baseURL: defaultBaseURL,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

// NewHTTPResolverWithClient is used in tests to inject an httptest server transport.
func NewHTTPResolverWithClient(baseURL string, httpClient *http.Client) *HTTPResolver {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &HTTPResolver{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

type assetResponse struct {
	Symbol      string       `json:"symbol"`
	Deployments []deployment `json:"deployments"`
}

type deployment struct {
	Address string `json:"address"`
	Network string `json:"network"`
}

// ResolveSolanaMint fetches asset metadata and returns the Solana mint address.
func (r *HTTPResolver) ResolveSolanaMint(ctx context.Context, symbol string) (string, error) {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return "", fmt.Errorf("%w: symbol is required", ErrInvalidResponse)
	}

	endpoint, err := url.JoinPath(r.baseURL, assetsPath, url.PathEscape(symbol))
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		logResolveMint(symbol, "", err)
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logResolveMint(symbol, "", err)
		return "", err
	}

	if resp.StatusCode == http.StatusNotFound {
		logResolveMint(symbol, "", ErrNotFound)
		return "", ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		resolveErr := fmt.Errorf("%w: status %d", ErrInvalidResponse, resp.StatusCode)
		logResolveMint(symbol, "", resolveErr)
		return "", resolveErr
	}

	mint, err := SolanaMintFromAssetResponse(body)
	if err != nil {
		logResolveMint(symbol, "", err)
		return "", err
	}
	logResolveMint(symbol, mint, nil)
	return mint, nil
}

// SolanaMintFromAssetResponse parses catalog JSON and returns the Solana mint.
func SolanaMintFromAssetResponse(body []byte) (string, error) {
	var asset assetResponse
	if err := json.Unmarshal(body, &asset); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	return solanaMintFromDeployments(asset.Deployments)
}

func solanaMintFromDeployments(deployments []deployment) (string, error) {
	for _, d := range deployments {
		if d.Network != solanaNetwork {
			continue
		}
		address := strings.TrimSpace(d.Address)
		if address == "" {
			continue
		}
		return address, nil
	}
	return "", ErrNoSolanaMint
}

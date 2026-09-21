package dex

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const kyberRoutesPath = "/base/api/v1/routes"
const kyberBuildPath = "/base/api/v1/route/build"
const defaultKyberBase = "https://aggregator-api.kyberswap.com"

// kyberSlippageBps is 2%. Thin B20 size + the ERC-20 approve wait stale a 0.5% route.
const kyberSlippageBps = 200

type kyberClient struct {
	http     *http.Client
	clientID string
	baseURL  string
}

// NewKyberClient talks to the KyberSwap aggregator on Base.
func NewKyberClient(httpClient *http.Client, clientID string) Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if strings.TrimSpace(clientID) == "" {
		clientID = "monaco"
	}
	return &kyberClient{http: httpClient, clientID: clientID, baseURL: defaultKyberBase}
}

func newKyberClient(httpClient *http.Client, clientID, baseURL string) *kyberClient {
	c := NewKyberClient(httpClient, clientID).(*kyberClient)
	if baseURL != "" {
		c.baseURL = strings.TrimRight(baseURL, "/")
	}
	return c
}

func (k *kyberClient) QuoteBuy(ctx context.Context, tokenOut string, usdcIn *big.Int) (Quote, error) {
	return k.quote(ctx, USDCAddress(), tokenOut, usdcIn)
}

func (k *kyberClient) QuoteSell(ctx context.Context, tokenIn string, amountIn *big.Int) (Quote, error) {
	return k.quote(ctx, tokenIn, USDCAddress(), amountIn)
}

type kyberRouteResponse struct {
	Code    int            `json:"code"`
	Data    kyberRouteData `json:"data"`
	Message string         `json:"message"`
}

type kyberRouteData struct {
	RouteSummary  json.RawMessage `json:"routeSummary"`
	RouterAddress string          `json:"routerAddress"`
}

func (k *kyberClient) quote(ctx context.Context, tokenIn, tokenOut string, amountIn *big.Int) (Quote, error) {
	q := Quote{TokenIn: tokenIn, TokenOut: tokenOut, AmountIn: amountIn, AmountOut: big.NewInt(0)}
	if amountIn == nil {
		return q, fmt.Errorf("amountIn is required")
	}
	u, err := url.Parse(k.baseURL + kyberRoutesPath)
	if err != nil {
		return q, err
	}
	query := u.Query()
	query.Set("tokenIn", tokenIn)
	query.Set("tokenOut", tokenOut)
	query.Set("amountIn", amountIn.String())
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return q, err
	}
	req.Header.Set("x-client-id", k.clientID)
	resp, err := k.http.Do(req)
	if err != nil {
		return q, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return q, nil
	}
	var parsed kyberRouteResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return q, err
	}
	if parsed.Code != 0 || len(parsed.Data.RouteSummary) == 0 || string(parsed.Data.RouteSummary) == "null" {
		return q, nil
	}
	q.Routable = true
	q.RouteSummary = parsed.Data.RouteSummary
	if out := amountOutFromSummary(parsed.Data.RouteSummary); out != nil {
		q.AmountOut = out
	}
	return q, nil
}

func amountOutFromSummary(raw json.RawMessage) *big.Int {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		return nil
	}
	for _, key := range []string{"amountOut", "outputAmount"} {
		if v, ok := obj[key]; ok {
			switch t := v.(type) {
			case string:
				n, ok := new(big.Int).SetString(t, 10)
				if ok {
					return n
				}
			case json.Number:
				n, ok := new(big.Int).SetString(t.String(), 10)
				if ok {
					return n
				}
			}
		}
	}
	return nil
}

type kyberBuildRequest struct {
	RouteSummary      json.RawMessage `json:"routeSummary"`
	Sender            string          `json:"sender"`
	Recipient         string          `json:"recipient"`
	SlippageTolerance int             `json:"slippageTolerance"`
	Deadline          int64           `json:"deadline"`
	Source            string          `json:"source"`
}

type kyberBuildResponse struct {
	Code int `json:"code"`
	Data struct {
		Data          string `json:"data"`
		RouterAddress string `json:"routerAddress"`
		AmountOutMin  string `json:"amountOutMin"`
	} `json:"data"`
}

func (k *kyberClient) BuildSwap(ctx context.Context, q Quote, sender, recipient string) (SwapCall, error) {
	payload, err := json.Marshal(kyberBuildRequest{
		RouteSummary:      q.RouteSummary,
		Sender:            sender,
		Recipient:         recipient,
		SlippageTolerance: kyberSlippageBps,
		Deadline:          time.Now().Add(20 * time.Minute).Unix(),
		Source:            k.clientID,
	})
	if err != nil {
		return SwapCall{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, k.baseURL+kyberBuildPath, bytes.NewReader(payload))
	if err != nil {
		return SwapCall{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-client-id", k.clientID)
	resp, err := k.http.Do(req)
	if err != nil {
		return SwapCall{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return SwapCall{}, fmt.Errorf("kyber build status %d", resp.StatusCode)
	}
	var parsed kyberBuildResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return SwapCall{}, err
	}
	if parsed.Code != 0 {
		return SwapCall{}, fmt.Errorf("kyber build code %d", parsed.Code)
	}
	data := parsed.Data.Data
	if strings.HasPrefix(data, "0x") {
		data = data[2:]
	}
	raw, err := hex.DecodeString(data)
	if err != nil || len(raw) == 0 || parsed.Data.RouterAddress == "" {
		return SwapCall{}, fmt.Errorf("kyber build missing calldata")
	}
	min := big.NewInt(1)
	if parsed.Data.AmountOutMin != "" {
		if n, ok := new(big.Int).SetString(parsed.Data.AmountOutMin, 10); ok {
			min = n
		}
	}
	return SwapCall{Router: parsed.Data.RouterAddress, Data: raw, AmountOutMin: min}, nil
}

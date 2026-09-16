package jupiter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// ExecuteBuyParams submits a signed treasury buy to Jupiter /execute.
type ExecuteBuyParams struct {
	GroupID           string
	UserID            string
	Symbol            string
	RequestID         string
	SignedTransaction string
}

// OrderBuyParams fetches an unsigned buy order for a treasury taker.
type OrderBuyParams struct {
	GroupID    string
	UserID     string
	Symbol     string
	InputMint  string
	OutputMint string
	Amount     int64
	Taker      string
}

// PollExecuteParams re-submits /execute to poll swap status.
type PollExecuteParams struct {
	GroupID           string
	UserID            string
	Symbol            string
	RequestID         string
	SignedTransaction string
}

type executeRequest struct {
	SignedTransaction string `json:"signedTransaction"`
	RequestID         string `json:"requestId"`
}

type executeResponse struct {
	Status             string `json:"status"`
	Code               int    `json:"code"`
	Signature          string `json:"signature"`
	Error              string `json:"error"`
	InputAmountResult  string `json:"inputAmountResult"`
	OutputAmountResult string `json:"outputAmountResult"`
	TotalOutputAmount  string `json:"totalOutputAmount"`
}

type orderResponse struct {
	Transaction string          `json:"transaction"`
	RequestID   string          `json:"requestId"`
	InputMint   string          `json:"inputMint"`
	OutputMint  string          `json:"outputMint"`
	InAmount    string          `json:"inAmount"`
	OutAmount   string          `json:"outAmount"`
	RoutePlan   json.RawMessage `json:"routePlan"`
	Error       string          `json:"error"`
}

// OrderBuy fetches an unsigned buy transaction for the treasury taker.
func (c *HTTPClient) OrderBuy(ctx context.Context, params OrderBuyParams) (BuyOrder, error) {
	if params.Amount <= 0 {
		return BuyOrder{}, fmt.Errorf("jupiter: amount must be positive")
	}
	if strings.TrimSpace(params.Taker) == "" {
		return BuyOrder{}, fmt.Errorf("jupiter: taker is required")
	}
	if strings.TrimSpace(params.OutputMint) == "" {
		return BuyOrder{}, fmt.Errorf("jupiter: output mint is required")
	}

	inputMint := params.InputMint
	if inputMint == "" {
		inputMint = USDCMint
	}

	query := url.Values{}
	query.Set("inputMint", inputMint)
	query.Set("outputMint", params.OutputMint)
	query.Set("amount", strconv.FormatInt(params.Amount, 10))
	query.Set("swapMode", "ExactIn")
	query.Set("slippageBps", strconv.Itoa(defaultSlippageBps))
	query.Set("taker", params.Taker)

	endpoint := c.baseURL + "/order?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return BuyOrder{}, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return BuyOrder{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return BuyOrder{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return BuyOrder{}, fmt.Errorf("jupiter: order status %d: %s", resp.StatusCode, string(body))
	}

	return ParseBuyOrderResponse(body)
}

// ParseBuyOrderResponse parses Jupiter /order JSON into a buy order.
func ParseBuyOrderResponse(body []byte) (BuyOrder, error) {
	var raw orderResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return BuyOrder{}, fmt.Errorf("jupiter: invalid order json: %w", err)
	}
	if strings.TrimSpace(raw.Transaction) == "" {
		return BuyOrder{}, ErrNoRoute
	}
	if raw.RequestID == "" {
		return BuyOrder{}, fmt.Errorf("jupiter: order missing requestId")
	}
	return BuyOrder{
		RequestID:   raw.RequestID,
		Transaction: raw.Transaction,
		InAmount:    raw.InAmount,
		OutAmount:   raw.OutAmount,
		InputMint:   raw.InputMint,
		OutputMint:  raw.OutputMint,
	}, nil
}

// ExecuteBuy POSTs a signed transaction to Jupiter /execute.
func (c *HTTPClient) ExecuteBuy(ctx context.Context, params ExecuteBuyParams) (ExecuteResult, error) {
	logExecuteSubmit(params.GroupID, params.UserID, params.Symbol, "", params.RequestID)
	return c.postExecute(ctx, params.GroupID, params.UserID, params.Symbol, params.RequestID, params.SignedTransaction)
}

// PollExecute re-submits /execute to poll for confirmation.
func (c *HTTPClient) PollExecute(ctx context.Context, params PollExecuteParams) (ExecuteResult, error) {
	return c.postExecute(ctx, params.GroupID, params.UserID, params.Symbol, params.RequestID, params.SignedTransaction)
}

func (c *HTTPClient) postExecute(ctx context.Context, groupID, userID, symbol, requestID, signedTx string) (ExecuteResult, error) {
	if requestID == "" || signedTx == "" {
		return ExecuteResult{}, fmt.Errorf("jupiter: requestId and signedTransaction are required")
	}

	payload, err := json.Marshal(executeRequest{
		SignedTransaction: signedTx,
		RequestID:         requestID,
	})
	if err != nil {
		return ExecuteResult{}, err
	}

	endpoint := c.baseURL + "/execute"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return ExecuteResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ExecuteResult{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ExecuteResult{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return ExecuteResult{}, fmt.Errorf("jupiter: execute status %d: %s", resp.StatusCode, string(body))
	}

	result, err := ParseExecuteResponse(body)
	if err != nil {
		return ExecuteResult{}, err
	}
	result.RequestID = requestID
	if result.Signature != "" {
		logExecuteSubmit(groupID, userID, symbol, result.Signature, requestID)
	}
	return result, nil
}

// ParseExecuteResponse parses Jupiter /execute JSON.
func ParseExecuteResponse(body []byte) (ExecuteResult, error) {
	var raw executeResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return ExecuteResult{}, fmt.Errorf("jupiter: invalid execute json: %w", err)
	}
	return ExecuteResult{
		Status:             raw.Status,
		Code:               raw.Code,
		Signature:          raw.Signature,
		InputAmountResult:  raw.InputAmountResult,
		OutputAmountResult: raw.OutputAmountResult,
		TotalOutputAmount:  raw.TotalOutputAmount,
		Error:              raw.Error,
	}, nil
}

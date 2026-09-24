package mintinfo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type rpcClient struct {
	url    string
	client *http.Client
}

func (c *rpcClient) call(ctx context.Context, method string, params []any) ([]byte, error) {
	reqBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mintinfo rpc: status %d", resp.StatusCode)
	}
	return body, nil
}

func (c *rpcClient) getAccountInfo(ctx context.Context, mint string) ([]byte, error) {
	return c.call(ctx, "getAccountInfo", []any{mint, map[string]string{"encoding": "jsonParsed"}})
}

func (c *rpcClient) getEpochInfo(ctx context.Context) ([]byte, error) {
	return c.call(ctx, "getEpochInfo", []any{})
}

package evm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"
)

type rpcClient struct {
	url  string
	http *http.Client
}

// NewJSONRPCClient dials a Base JSON-RPC HTTP endpoint.
func NewJSONRPCClient(url string) Client {
	return &rpcClient{url: url, http: &http.Client{Timeout: 20 * time.Second}}
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *rpcClient) callRPC(ctx context.Context, method string, params []any) (json.RawMessage, error) {
	payload, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var parsed rpcResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("rpc: %s", parsed.Error.Message)
	}
	return parsed.Result, nil
}

func (c *rpcClient) Call(ctx context.Context, to string, data []byte) ([]byte, error) {
	result, err := c.callRPC(ctx, "eth_call", []any{
		map[string]string{"to": to, "data": "0x" + fmt.Sprintf("%x", data)},
		"latest",
	})
	if err != nil {
		return nil, err
	}
	var hexStr string
	if err := json.Unmarshal(result, &hexStr); err != nil {
		return nil, err
	}
	return decodeHex(hexStr)
}

func (c *rpcClient) ERC20Balance(ctx context.Context, token, holder string) (*big.Int, error) {
	// balanceOf(address) selector 0x70a08231
	data := make([]byte, 36)
	copy(data[0:4], []byte{0x70, 0xa0, 0x82, 0x31})
	raw, err := c.Call(ctx, token, append([]byte{0x70, 0xa0, 0x82, 0x31}, leftPadAddress(holder)...))
	if err != nil {
		return nil, err
	}
	_ = data
	return new(big.Int).SetBytes(raw), nil
}

func (c *rpcClient) ETHBalance(ctx context.Context, addr string) (*big.Int, error) {
	result, err := c.callRPC(ctx, "eth_getBalance", []any{addr, "latest"})
	if err != nil {
		return nil, err
	}
	var hexStr string
	if err := json.Unmarshal(result, &hexStr); err != nil {
		return nil, err
	}
	n := new(big.Int)
	n.SetString(strings.TrimPrefix(hexStr, "0x"), 16)
	return n, nil
}

func (c *rpcClient) Allowance(ctx context.Context, token, owner, spender string) (*big.Int, error) {
	// allowance(owner,spender) 0xdd62ed3e
	payload := append([]byte{0xdd, 0x62, 0xed, 0x3e}, leftPadAddress(owner)...)
	payload = append(payload, leftPadAddress(spender)...)
	raw, err := c.Call(ctx, token, payload)
	if err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(raw), nil
}

func (c *rpcClient) Receipt(ctx context.Context, txHash string) (Receipt, error) {
	result, err := c.callRPC(ctx, "eth_getTransactionReceipt", []any{txHash})
	if err != nil {
		return Receipt{}, err
	}
	if string(result) == "null" {
		return Receipt{Found: false}, nil
	}
	var obj struct {
		Status string `json:"status"`
		Logs   []struct {
			Address string   `json:"address"`
			Topics  []string `json:"topics"`
			Data    string   `json:"data"`
		} `json:"logs"`
	}
	if err := json.Unmarshal(result, &obj); err != nil {
		return Receipt{}, err
	}
	status := uint64(0)
	if obj.Status == "0x1" || obj.Status == "1" {
		status = 1
	}
	var logs []Log
	for _, lg := range obj.Logs {
		data, _ := decodeHex(lg.Data)
		logs = append(logs, Log{Address: lg.Address, Topics: lg.Topics, Data: data})
	}
	return Receipt{Status: status, Logs: logs, Found: true}, nil
}

func (c *rpcClient) IsConfirmed(ctx context.Context, txHash string) (bool, error) {
	r, err := c.Receipt(ctx, txHash)
	if err != nil {
		return false, err
	}
	return r.Found && r.Status == 1, nil
}

func (c *rpcClient) ChainlinkLatestRoundData(ctx context.Context, feed string) (RoundData, error) {
	// latestRoundData() 0xfeaf968c
	raw, err := c.Call(ctx, feed, []byte{0xfe, 0xaf, 0x96, 0x8c})
	if err != nil {
		return RoundData{}, err
	}
	if len(raw) < 160 {
		return RoundData{Answer: big.NewInt(0), UpdatedAt: time.Now()}, nil
	}
	answer := new(big.Int).SetBytes(raw[32:64])
	updated := new(big.Int).SetBytes(raw[96:128])
	return RoundData{Answer: answer, UpdatedAt: time.Unix(updated.Int64(), 0)}, nil
}

func leftPadAddress(addr string) []byte {
	addr = strings.TrimPrefix(strings.ToLower(addr), "0x")
	b := make([]byte, 32)
	raw, _ := decodeHex("0x" + addr)
	copy(b[32-len(raw):], raw)
	return b
}

func decodeHex(s string) ([]byte, error) {
	s = strings.TrimPrefix(s, "0x")
	if len(s)%2 == 1 {
		s = "0" + s
	}
	out := make([]byte, len(s)/2)
	_, err := fmt.Sscanf(s, "%x", &out)
	if err != nil {
		b := make([]byte, 0, len(s)/2)
		for i := 0; i+1 < len(s); i += 2 {
			var v byte
			fmt.Sscanf(s[i:i+2], "%02x", &v)
			b = append(b, v)
		}
		return b, nil
	}
	return out, nil
}

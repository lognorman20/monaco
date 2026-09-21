package evm

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

type cachedRound struct {
	data RoundData
	at   time.Time
}

type cachedBalance struct {
	value *big.Int
	at    time.Time
}

type cachedHistory struct {
	rounds []RoundData
	at     time.Time
}

type rpcClient struct {
	url  string
	http *http.Client

	roundMu sync.Mutex
	rounds  map[string]cachedRound
	histMu  sync.Mutex
	hists   map[string]cachedHistory
	balMu   sync.Mutex
	bals    map[string]cachedBalance
}

const chainlinkRoundTTL = 90 * time.Second
const chainlinkHistoryTTL = 2 * time.Minute
const chainlinkHistoryLimit = 48
const erc20BalanceTTL = 15 * time.Second
const rpcRateLimitAttempts = 2

func isRPCRateLimited(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "rate limit") || strings.Contains(msg, "429")
}

func (c *rpcClient) callRPC(ctx context.Context, method string, params []any) (json.RawMessage, error) {
	var last error
	for attempt := 0; attempt < rpcRateLimitAttempts; attempt++ {
		result, err := c.callRPCOnce(ctx, method, params)
		if err == nil {
			return result, nil
		}
		last = err
		if !isRPCRateLimited(err) {
			return nil, err
		}
		delay := time.Duration(200*(1<<attempt)) * time.Millisecond
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	return nil, last
}

func (c *rpcClient) callRPCOnce(ctx context.Context, method string, params []any) (json.RawMessage, error) {
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
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("rpc: over rate limit")
	}
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
	key := strings.ToLower(strings.TrimSpace(token)) + ":" + strings.ToLower(strings.TrimSpace(holder))
	c.balMu.Lock()
	cached, ok := c.bals[key]
	if ok && time.Since(cached.at) < erc20BalanceTTL {
		out := new(big.Int).Set(cached.value)
		c.balMu.Unlock()
		return out, nil
	}
	c.balMu.Unlock()

	raw, err := c.Call(ctx, token, append([]byte{0x70, 0xa0, 0x82, 0x31}, leftPadAddress(holder)...))
	if err != nil {
		if isRPCRateLimited(err) && ok {
			return new(big.Int).Set(cached.value), nil
		}
		return nil, err
	}
	val := new(big.Int).SetBytes(raw)
	c.balMu.Lock()
	if c.bals == nil {
		c.bals = make(map[string]cachedBalance)
	}
	c.bals[key] = cachedBalance{value: new(big.Int).Set(val), at: time.Now()}
	c.balMu.Unlock()
	return val, nil
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

func (c *rpcClient) cachedRound(feed string) (RoundData, bool) {
	key := strings.ToLower(strings.TrimSpace(feed))
	c.roundMu.Lock()
	defer c.roundMu.Unlock()
	cached, ok := c.rounds[key]
	if !ok || time.Since(cached.at) >= chainlinkRoundTTL {
		return RoundData{}, false
	}
	return cached.data, true
}

func (c *rpcClient) storeRound(feed string, data RoundData) {
	key := strings.ToLower(strings.TrimSpace(feed))
	c.roundMu.Lock()
	if c.rounds == nil {
		c.rounds = make(map[string]cachedRound)
	}
	c.rounds[key] = cachedRound{data: data, at: time.Now()}
	c.roundMu.Unlock()
}

func parseLatestRoundData(raw []byte) (RoundData, error) {
	if len(raw) < 160 {
		return RoundData{}, fmt.Errorf("chainlink latestRoundData: short response")
	}
	roundID := new(big.Int).SetBytes(raw[0:32])
	answer := new(big.Int).SetBytes(raw[32:64])
	updated := new(big.Int).SetBytes(raw[96:128])
	return RoundData{RoundID: roundID, Answer: answer, UpdatedAt: time.Unix(updated.Int64(), 0)}, nil
}

func (c *rpcClient) ChainlinkLatestRoundData(ctx context.Context, feed string) (RoundData, error) {
	if data, ok := c.cachedRound(feed); ok {
		return data, nil
	}
	raw, err := c.Call(ctx, feed, latestRoundDataSelector)
	if err != nil {
		if isRPCRateLimited(err) {
			key := strings.ToLower(strings.TrimSpace(feed))
			c.roundMu.Lock()
			stale, hit := c.rounds[key]
			c.roundMu.Unlock()
			if hit {
				return stale.data, nil
			}
		}
		return RoundData{}, err
	}
	data, err := parseLatestRoundData(raw)
	if err != nil {
		return RoundData{}, err
	}
	c.storeRound(feed, data)
	return data, nil
}

func (c *rpcClient) ChainlinkLatestRoundDataMany(ctx context.Context, feeds []string) (map[string]RoundData, error) {
	out := make(map[string]RoundData, len(feeds))
	missing := make([]string, 0, len(feeds))
	seen := make(map[string]struct{}, len(feeds))
	for _, feed := range feeds {
		key := strings.ToLower(strings.TrimSpace(feed))
		if key == "" {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		if data, ok := c.cachedRound(key); ok {
			out[key] = data
			continue
		}
		missing = append(missing, key)
	}
	if len(missing) == 0 {
		return out, nil
	}
	payload, err := encodeAggregate3(missing)
	if err != nil {
		return out, err
	}
	raw, err := c.Call(ctx, Multicall3Address, payload)
	if err != nil {
		for _, feed := range missing {
			data, oneErr := c.ChainlinkLatestRoundData(ctx, feed)
			if oneErr != nil {
				continue
			}
			out[feed] = data
		}
		return out, nil
	}
	results, err := decodeAggregate3(raw)
	if err != nil {
		return out, err
	}
	for i, feed := range missing {
		if i >= len(results) || !results[i].Success {
			continue
		}
		data, parseErr := parseLatestRoundData(results[i].ReturnData)
		if parseErr != nil {
			continue
		}
		c.storeRound(feed, data)
		out[feed] = data
	}
	return out, nil
}

func (c *rpcClient) ChainlinkRoundHistory(ctx context.Context, feed string, limit int) ([]RoundData, error) {
	key := strings.ToLower(strings.TrimSpace(feed))
	if limit <= 0 {
		limit = chainlinkHistoryLimit
	}
	if limit > chainlinkHistoryLimit {
		limit = chainlinkHistoryLimit
	}
	c.histMu.Lock()
	if cached, ok := c.hists[key]; ok && time.Since(cached.at) < chainlinkHistoryTTL && len(cached.rounds) >= 2 {
		out := append([]RoundData(nil), cached.rounds...)
		c.histMu.Unlock()
		return out, nil
	}
	c.histMu.Unlock()

	latest, err := c.ChainlinkLatestRoundData(ctx, feed)
	if err != nil {
		return nil, err
	}
	rounds := []RoundData{latest}
	if latest.RoundID == nil || latest.RoundID.Sign() <= 0 || limit == 1 {
		return rounds, nil
	}

	targets := make([]string, 0, limit-1)
	datas := make([][]byte, 0, limit-1)
	for i := 1; i < limit; i++ {
		id := new(big.Int).Sub(latest.RoundID, big.NewInt(int64(i)))
		if id.Sign() <= 0 {
			break
		}
		targets = append(targets, key)
		datas = append(datas, encodeGetRoundData(id))
	}
	if len(targets) == 0 {
		return rounds, nil
	}
	payload, err := encodeAggregate3Calls(targets, datas)
	if err != nil {
		return rounds, nil
	}
	raw, err := c.Call(ctx, Multicall3Address, payload)
	if err != nil {
		return rounds, nil
	}
	results, err := decodeAggregate3(raw)
	if err != nil {
		return rounds, nil
	}
	for _, result := range results {
		if !result.Success {
			continue
		}
		data, parseErr := parseLatestRoundData(result.ReturnData)
		if parseErr != nil || data.Answer == nil || data.Answer.Sign() <= 0 {
			continue
		}
		rounds = append(rounds, data)
	}

	c.histMu.Lock()
	if c.hists == nil {
		c.hists = make(map[string]cachedHistory)
	}
	c.hists[key] = cachedHistory{rounds: append([]RoundData(nil), rounds...), at: time.Now()}
	c.histMu.Unlock()
	return rounds, nil
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
	return hex.DecodeString(s)
}

// ReceiptPollInterval is how often WaitReceipt re-queries eth_getTransactionReceipt.
const ReceiptPollInterval = 500 * time.Millisecond

// ReceiptWaitTimeout is how long WaitReceipt waits for a mined receipt.
const ReceiptWaitTimeout = 30 * time.Second

// WaitReceipt polls until the receipt exists or the timeout is reached.
func WaitReceipt(ctx context.Context, c Client, txHash string) (Receipt, error) {
	if c == nil {
		return Receipt{Found: true, Status: 1}, nil
	}
	deadline := time.Now().Add(ReceiptWaitTimeout)
	for {
		receipt, err := c.Receipt(ctx, txHash)
		if err != nil {
			return Receipt{}, err
		}
		if receipt.Found {
			return receipt, nil
		}
		if !time.Now().Before(deadline) {
			return Receipt{}, fmt.Errorf("receipt timeout")
		}
		select {
		case <-ctx.Done():
			return Receipt{}, ctx.Err()
		case <-time.After(ReceiptPollInterval):
		}
	}
}

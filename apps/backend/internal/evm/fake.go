package evm

import (
	"context"
	"math/big"
	"strings"
	"sync"
	"time"
)

type fakeClient struct {
	mu sync.Mutex

	balances   map[string]*big.Int
	allowances map[string]*big.Int
	receipts   map[string]Receipt
	roundData  map[string]RoundData
	roundHist  map[string][]RoundData
}

// NewFakeClient returns an in-memory EVM client for tests.
func NewFakeClient() *fakeClient {
	return &fakeClient{
		balances:   make(map[string]*big.Int),
		allowances: make(map[string]*big.Int),
		receipts:   make(map[string]Receipt),
		roundData:  make(map[string]RoundData),
		roundHist:  make(map[string][]RoundData),
	}
}

func balanceKey(token, holder string) string {
	return token + ":" + holder
}

func allowanceKey(token, owner, spender string) string {
	return token + ":" + owner + ":" + spender
}

func (f *fakeClient) SetETHBalance(addr string, amount *big.Int) {
	f.mu.Lock()
	f.balances["eth:"+addr] = new(big.Int).Set(amount)
	f.mu.Unlock()
}

func (f *fakeClient) SetERC20Balance(token, holder string, amount *big.Int) {
	f.mu.Lock()
	f.balances[balanceKey(token, holder)] = new(big.Int).Set(amount)
	f.mu.Unlock()
}

func (f *fakeClient) SetAllowance(token, owner, spender string, amount *big.Int) {
	f.mu.Lock()
	f.allowances[allowanceKey(token, owner, spender)] = new(big.Int).Set(amount)
	f.mu.Unlock()
}

func (f *fakeClient) SetReceipt(txHash string, receipt Receipt) {
	f.mu.Lock()
	receipt.Found = true
	f.receipts[txHash] = receipt
	f.mu.Unlock()
}

func (f *fakeClient) SetRoundHistory(feed string, rounds []RoundData) {
	f.mu.Lock()
	copied := append([]RoundData(nil), rounds...)
	f.roundHist[strings.ToLower(strings.TrimSpace(feed))] = copied
	if len(copied) > 0 {
		f.roundData[strings.ToLower(strings.TrimSpace(feed))] = copied[len(copied)-1]
	}
	f.mu.Unlock()
}

func (f *fakeClient) SetRoundData(feed string, data RoundData) {
	f.mu.Lock()
	f.roundData[strings.ToLower(strings.TrimSpace(feed))] = data
	f.mu.Unlock()
}

func (f *fakeClient) Call(ctx context.Context, to string, data []byte) ([]byte, error) {
	_ = ctx
	_ = to
	_ = data
	return nil, nil
}

func (f *fakeClient) ERC20Balance(ctx context.Context, token, holder string) (*big.Int, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	if b, ok := f.balances[balanceKey(token, holder)]; ok {
		return new(big.Int).Set(b), nil
	}
	return big.NewInt(0), nil
}

func (f *fakeClient) ETHBalance(ctx context.Context, addr string) (*big.Int, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	if b, ok := f.balances["eth:"+addr]; ok {
		return new(big.Int).Set(b), nil
	}
	return big.NewInt(0), nil
}

func (f *fakeClient) Allowance(ctx context.Context, token, owner, spender string) (*big.Int, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	if b, ok := f.allowances[allowanceKey(token, owner, spender)]; ok {
		return new(big.Int).Set(b), nil
	}
	return big.NewInt(0), nil
}

func (f *fakeClient) Receipt(ctx context.Context, txHash string) (Receipt, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.receipts[txHash]
	if !ok {
		return Receipt{Found: true, Status: 1}, nil
	}
	return r, nil
}

func (f *fakeClient) IsConfirmed(ctx context.Context, txHash string) (bool, error) {
	r, err := f.Receipt(ctx, txHash)
	if err != nil {
		return false, err
	}
	return r.Found && r.Status == 1, nil
}

func (f *fakeClient) ChainlinkLatestRoundData(ctx context.Context, feed string) (RoundData, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	if d, ok := f.roundData[strings.ToLower(strings.TrimSpace(feed))]; ok {
		return d, nil
	}
	return RoundData{Answer: big.NewInt(0), UpdatedAt: time.Now()}, nil
}

func (f *fakeClient) ChainlinkLatestRoundDataMany(ctx context.Context, feeds []string) (map[string]RoundData, error) {
	out := make(map[string]RoundData, len(feeds))
	for _, feed := range feeds {
		data, err := f.ChainlinkLatestRoundData(ctx, feed)
		if err != nil {
			continue
		}
		out[strings.ToLower(strings.TrimSpace(feed))] = data
	}
	return out, nil
}

func (f *fakeClient) ChainlinkRoundHistory(ctx context.Context, feed string, limit int) ([]RoundData, error) {
	_ = ctx
	key := strings.ToLower(strings.TrimSpace(feed))
	f.mu.Lock()
	hist := append([]RoundData(nil), f.roundHist[key]...)
	latest, hasLatest := f.roundData[key]
	f.mu.Unlock()
	if len(hist) > 0 {
		if limit > 0 && len(hist) > limit {
			hist = hist[len(hist)-limit:]
		}
		return hist, nil
	}
	if hasLatest && latest.Answer != nil && latest.Answer.Sign() > 0 {
		earlier := latest
		earlier.UpdatedAt = latest.UpdatedAt.Add(-time.Hour)
		return []RoundData{earlier, latest}, nil
	}
	return nil, nil
}

package evm

import (
	"context"
	"math/big"
	"sort"
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
	roundErrs  map[string]error
}

// NewFakeClient returns an in-memory EVM client for tests.
func NewFakeClient() *fakeClient {
	return &fakeClient{
		balances:   make(map[string]*big.Int),
		allowances: make(map[string]*big.Int),
		receipts:   make(map[string]Receipt),
		roundData:  make(map[string]RoundData),
		roundHist:  make(map[string][]RoundData),
		roundErrs:  make(map[string]error),
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
	if err, ok := f.roundErrs[strings.ToLower(strings.TrimSpace(feed))]; ok {
		return RoundData{}, err
	}
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

// ChainlinkRoundsSince replays the registered history, narrowed the way the live
// client narrows it: rounds at or after `since`, plus the one round before it so
// a caller can still find a previous close.
func (f *fakeClient) ChainlinkRoundsSince(ctx context.Context, feed string, since time.Time, maxRounds int) (RoundHistory, error) {
	_ = ctx
	key := strings.ToLower(strings.TrimSpace(feed))
	f.mu.Lock()
	if err, ok := f.roundErrs[key]; ok {
		f.mu.Unlock()
		return RoundHistory{}, err
	}
	hist := append([]RoundData(nil), f.roundHist[key]...)
	latest, hasLatest := f.roundData[key]
	f.mu.Unlock()

	if len(hist) == 0 {
		if !hasLatest || latest.Answer == nil || latest.Answer.Sign() <= 0 {
			return RoundHistory{}, nil
		}
		hist = []RoundData{latest}
	}
	sort.Slice(hist, func(i, j int) bool { return hist[i].UpdatedAt.Before(hist[j].UpdatedAt) })
	out := RoundHistory{Complete: true}
	if len(hist) > 0 {
		out.FirstRoundAt = hist[0].UpdatedAt.UTC()
	}
	for i, round := range hist {
		if round.Answer == nil || round.Answer.Sign() <= 0 || round.UpdatedAt.IsZero() {
			continue
		}
		round.UpdatedAt = round.UpdatedAt.UTC()
		if !since.IsZero() && round.UpdatedAt.Before(since) {
			// Keep the last round before the window, the way a live walk overshoots
			// its cutoff by one round.
			if i+1 < len(hist) && !hist[i+1].UpdatedAt.Before(since) {
				out.Rounds = append(out.Rounds, round)
			}
			continue
		}
		out.Rounds = append(out.Rounds, round)
	}
	if maxRounds > 0 && len(out.Rounds) > maxRounds {
		out.Rounds = out.Rounds[len(out.Rounds)-maxRounds:]
		out.Complete = false
	}
	return out, nil
}

// SetRoundError makes every round read for a feed fail, for the RPC-down paths.
func (f *fakeClient) SetRoundError(feed string, err error) {
	f.mu.Lock()
	f.roundErrs[strings.ToLower(strings.TrimSpace(feed))] = err
	f.mu.Unlock()
}

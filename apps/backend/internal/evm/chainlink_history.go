package evm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Reading a feed's history is a read per round: Chainlink aggregators expose
// getRoundData(roundId) and nothing else. Done literally that is one eth_call per
// round, which for seven weeks of an equity feed is a few hundred sequential
// requests and a 429 from any public endpoint.
//
// Two layers of batching keep it to a handful of round trips:
//   - roundsPerCall reads ride in one Multicall3 aggregate3, so one eth_call
//     returns a hundred rounds;
//   - callsPerRequest of those eth_calls ride in one JSON-RPC batch, so one HTTP
//     request returns several hundred rounds.
//
// A whole feed's history is therefore one or two requests cold, and none warm.
const (
	// roundsPerCall is how many getRoundData reads go into one aggregate3.
	roundsPerCall = 100
	// callsPerRequest is how many eth_calls go into one JSON-RPC batch request.
	callsPerRequest = 8
	// historyConcurrency bounds how many of those requests are in flight for one
	// history read, so a deep read cannot itself become the rate limit.
	historyConcurrency = 2
	// maxHistoryRounds caps one history read. A feed that somehow has more rounds
	// than this is read back this far and reported as incomplete rather than
	// walked forever.
	maxHistoryRounds = 4000
)

// phaseRoundMask is the low 64 bits of a proxy round id: the aggregator's own
// round number. The high bits are the phase, which changes when the feed is
// migrated to a new aggregator, and phase boundaries are not walkable — round 1
// of the current phase is as far back as a proxy answers.
const phaseRoundBits = 64

// RoundHistory is a feed's rounds and how far back they go.
type RoundHistory struct {
	// Rounds are oldest first, and hold only rounds the aggregator actually
	// answered: a priced round with a non-zero updatedAt.
	Rounds []RoundData
	// FirstRoundAt is when round 1 of the current aggregator phase was struck —
	// the earliest instant this feed can price. Zero when it could not be read.
	FirstRoundAt time.Time
	// Complete reports that Rounds reaches round 1 of the phase, so nothing older
	// is missing from them.
	Complete bool
	// Truncated reports that the walk stopped on a failed read rather than on its
	// own terms. The rounds in hand are real, but they are not all the rounds in
	// the window, so a caller that promised a window length must not pretend this
	// is it.
	Truncated bool
}

// Oldest is the time of the earliest round in the history, or zero when empty.
func (h RoundHistory) Oldest() time.Time {
	if len(h.Rounds) == 0 {
		return time.Time{}
	}
	return h.Rounds[0].UpdatedAt
}

type cachedFirstRound struct {
	data  RoundData
	phase string
	at    time.Time
}

// chainlinkFirstRoundTTL is how long round 1 of a phase is reused. It only
// changes when the feed migrates to a new aggregator, which is also a phase
// change, and the cache is keyed by phase.
const chainlinkFirstRoundTTL = 6 * time.Hour

// splitRoundID splits a proxy round id into its phase and the aggregator's own
// round number.
func splitRoundID(id *big.Int) (phase *big.Int, round uint64, ok bool) {
	if id == nil || id.Sign() <= 0 {
		return nil, 0, false
	}
	mask := new(big.Int).Lsh(big.NewInt(1), phaseRoundBits)
	mask.Sub(mask, big.NewInt(1))
	low := new(big.Int).And(id, mask)
	if !low.IsUint64() || low.Uint64() == 0 {
		return nil, 0, false
	}
	return new(big.Int).Rsh(id, phaseRoundBits), low.Uint64(), true
}

// composeRoundID is the proxy round id for one aggregator round inside a phase.
func composeRoundID(phase *big.Int, round uint64) *big.Int {
	id := new(big.Int).Lsh(phase, phaseRoundBits)
	return id.Or(id, new(big.Int).SetUint64(round))
}

// ChainlinkRoundsSince reads a feed's rounds back to `since`, newest first on the
// wire and oldest first in the result.
//
// It stops at the first round older than `since`, at maxRounds, or at round 1 of
// the current phase, whichever comes first, and it always reports where the
// feed's history starts so a caller can tell "no rounds in this window" from
// "this window predates the feed". A zero `since` means the whole phase.
func (c *rpcClient) ChainlinkRoundsSince(ctx context.Context, feed string, since time.Time, maxRounds int) (RoundHistory, error) {
	key := strings.ToLower(strings.TrimSpace(feed))
	if key == "" {
		return RoundHistory{}, fmt.Errorf("chainlink history: empty feed address")
	}
	if maxRounds <= 0 || maxRounds > maxHistoryRounds {
		maxRounds = maxHistoryRounds
	}
	if cached, ok := c.cachedHistory(key, since); ok {
		return cached, nil
	}

	latest, err := c.ChainlinkLatestRoundData(ctx, feed)
	if err != nil {
		return RoundHistory{}, fmt.Errorf("chainlink latest round: %w", err)
	}
	if !roundAnswered(latest) {
		return RoundHistory{}, fmt.Errorf("chainlink history: feed %s has no priced round", key)
	}
	phase, newest, ok := splitRoundID(latest.RoundID)
	if !ok {
		// No phase-encoded round id: the proxy answers latestRoundData but nothing
		// can be walked back from it. One round is not a series, and saying so is
		// better than pretending the feed has no history at all.
		return RoundHistory{Rounds: []RoundData{normalizeRound(latest)}}, nil
	}

	history, err := c.walkRounds(ctx, key, phase, latest, newest, since, maxRounds)
	if err != nil {
		return RoundHistory{}, err
	}
	c.storeHistory(key, history)
	return history, nil
}

func (c *rpcClient) walkRounds(ctx context.Context, feed string, phase *big.Int, latest RoundData, newest uint64, since time.Time, maxRounds int) (RoundHistory, error) {
	history := RoundHistory{Rounds: []RoundData{normalizeRound(latest)}}

	remaining := int(newest) - 1
	if remaining > maxRounds-1 {
		remaining = maxRounds - 1
	}
	if remaining <= 0 {
		history.Complete = newest == 1
		history.FirstRoundAt = history.Rounds[0].UpdatedAt
		return history, nil
	}

	// Round 1 rides along with the first wave, so the feed's start costs no extra
	// request. Once known it is cached for the phase.
	needFirst := true
	if first, ok := c.cachedFirstRound(feed, phase); ok {
		history.FirstRoundAt = first.UpdatedAt
		needFirst = false
	}

	next := newest - 1
	wave := roundsPerCall
	if since.IsZero() {
		// The whole phase is wanted: ask for all of it at once rather than
		// discovering round by round that nothing is old enough.
		wave = remaining
	}
	for remaining > 0 {
		if wave > remaining {
			wave = remaining
		}
		ids := make([]uint64, 0, wave+1)
		for i := 0; i < wave; i++ {
			ids = append(ids, next-uint64(i))
		}
		if needFirst {
			ids = append(ids, 1)
		}

		fetched, err := c.roundsByID(ctx, feed, phase, ids)
		if err != nil {
			if len(history.Rounds) >= 2 {
				// Part of the walk is in hand. Hand it back, flagged, so a caller
				// that can use a short series does and one that cannot says why.
				history.Truncated = true
				break
			}
			return RoundHistory{}, err
		}
		if needFirst {
			if first, ok := fetched[1]; ok {
				history.FirstRoundAt = first.UpdatedAt
				c.storeFirstRound(feed, phase, first)
				needFirst = false
			}
		}

		oldest := latest.UpdatedAt
		added := 0
		for _, id := range ids[:wave] {
			round, ok := fetched[id]
			if !ok {
				continue
			}
			history.Rounds = append(history.Rounds, round)
			added++
			if round.UpdatedAt.Before(oldest) {
				oldest = round.UpdatedAt
			}
		}
		next -= uint64(wave)
		remaining -= wave
		if next == 0 {
			history.Complete = true
			break
		}
		if !since.IsZero() && added > 0 && oldest.Before(since) {
			break
		}
		if added == 0 {
			// The aggregator answered nothing across a whole wave. Walking further
			// back would be guessing; stop and report what is in hand.
			break
		}
		// Each wave that fails to reach far enough back doubles the next one, so a
		// dense feed costs a handful of waves rather than one per hundred rounds.
		wave *= 2
	}

	sort.Slice(history.Rounds, func(i, j int) bool {
		return history.Rounds[i].UpdatedAt.Before(history.Rounds[j].UpdatedAt)
	})
	if history.Complete && history.FirstRoundAt.IsZero() && len(history.Rounds) > 0 {
		history.FirstRoundAt = history.Rounds[0].UpdatedAt
	}
	return history, nil
}

// roundsByID reads a set of aggregator rounds, batched into multicalls and then
// into JSON-RPC batch requests. Rounds the aggregator does not answer — a reverted
// call, a zero updatedAt, an unpriced round — are simply absent from the result.
func (c *rpcClient) roundsByID(ctx context.Context, feed string, phase *big.Int, ids []uint64) (map[uint64]RoundData, error) {
	out := make(map[uint64]RoundData, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	chunks := make([][]uint64, 0, (len(ids)/roundsPerCall)+1)
	for start := 0; start < len(ids); start += roundsPerCall {
		end := start + roundsPerCall
		if end > len(ids) {
			end = len(ids)
		}
		chunks = append(chunks, ids[start:end])
	}

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		firstErr error
		sem      = make(chan struct{}, historyConcurrency)
	)
	for start := 0; start < len(chunks); start += callsPerRequest {
		end := start + callsPerRequest
		if end > len(chunks) {
			end = len(chunks)
		}
		group := chunks[start:end]
		wg.Add(1)
		go func(group [][]uint64) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			calls := make([]ethCall, 0, len(group))
			for _, chunk := range group {
				targets := make([]string, len(chunk))
				datas := make([][]byte, len(chunk))
				for i, id := range chunk {
					targets[i] = feed
					datas[i] = encodeGetRoundData(composeRoundID(phase, id))
				}
				payload, err := encodeAggregate3Calls(targets, datas)
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
					return
				}
				calls = append(calls, ethCall{To: Multicall3Address, Data: payload})
			}

			raws, err := c.ethCallBatch(ctx, calls)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			for i, raw := range raws {
				if i >= len(group) {
					break
				}
				results, decodeErr := decodeAggregate3(raw)
				if decodeErr != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = decodeErr
					}
					mu.Unlock()
					return
				}
				mu.Lock()
				for j, id := range group[i] {
					if j >= len(results) || !results[j].Success {
						continue
					}
					round, parseErr := parseLatestRoundData(results[j].ReturnData)
					if parseErr != nil || !roundAnswered(round) {
						continue
					}
					out[id] = normalizeRound(round)
				}
				mu.Unlock()
			}
		}(group)
	}
	wg.Wait()
	if firstErr != nil && len(out) == 0 {
		return nil, firstErr
	}
	return out, nil
}

// roundAnswered rejects the rounds an aggregator returns for ids it does not
// hold: a zero price, or a price with no time on it. Neither can go on a chart,
// and a zero updatedAt read as a timestamp is 1 January 1970.
func roundAnswered(round RoundData) bool {
	if round.Answer == nil || round.Answer.Sign() <= 0 {
		return false
	}
	return !round.UpdatedAt.IsZero() && round.UpdatedAt.Unix() > 0
}

func normalizeRound(round RoundData) RoundData {
	round.UpdatedAt = round.UpdatedAt.UTC()
	return round
}

type ethCall struct {
	To   string
	Data []byte
}

// ethCallBatch sends several eth_calls as one JSON-RPC batch request and returns
// their results in the order asked. An endpoint that refuses batches (a single
// object instead of an array) is retried one call at a time, so batching is an
// optimisation and never a requirement.
func (c *rpcClient) ethCallBatch(ctx context.Context, calls []ethCall) ([][]byte, error) {
	if len(calls) == 0 {
		return nil, nil
	}
	if len(calls) == 1 {
		raw, err := c.Call(ctx, calls[0].To, calls[0].Data)
		if err != nil {
			return nil, err
		}
		return [][]byte{raw}, nil
	}

	var last error
	for attempt := 0; attempt < rpcRateLimitAttempts; attempt++ {
		out, err := c.ethCallBatchOnce(ctx, calls)
		if err == nil {
			return out, nil
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

func (c *rpcClient) ethCallBatchOnce(ctx context.Context, calls []ethCall) ([][]byte, error) {
	batch := make([]rpcRequest, 0, len(calls))
	for i, call := range calls {
		batch = append(batch, rpcRequest{
			JSONRPC: "2.0",
			ID:      i,
			Method:  "eth_call",
			Params: []any{
				map[string]string{"to": call.To, "data": "0x" + fmt.Sprintf("%x", call.Data)},
				"latest",
			},
		})
	}
	payload, err := json.Marshal(batch)
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

	var parsed []struct {
		ID     int             `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		// Not a batch response: the endpoint does not batch, or refused this one
		// with a single error object. Either way the calls are answered one at a
		// time instead — batching is an optimisation, never a requirement.
		//
		// The exception is a rate limit: retrying the same work as N separate
		// requests is the fastest way to deserve one.
		var single rpcResponse
		if singleErr := json.Unmarshal(body, &single); singleErr == nil && single.Error != nil {
			if isRPCRateLimited(fmt.Errorf("%s", single.Error.Message)) {
				return nil, fmt.Errorf("rpc: %s", single.Error.Message)
			}
		}
		return c.ethCallsSequential(ctx, calls)
	}

	out := make([][]byte, len(calls))
	var firstErr error
	for _, entry := range parsed {
		if entry.ID < 0 || entry.ID >= len(calls) {
			continue
		}
		if entry.Error != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("rpc: %s", entry.Error.Message)
			}
			continue
		}
		var hexStr string
		if err := json.Unmarshal(entry.Result, &hexStr); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		decoded, err := decodeHex(hexStr)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		out[entry.ID] = decoded
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

func (c *rpcClient) ethCallsSequential(ctx context.Context, calls []ethCall) ([][]byte, error) {
	out := make([][]byte, 0, len(calls))
	for _, call := range calls {
		raw, err := c.Call(ctx, call.To, call.Data)
		if err != nil {
			return nil, err
		}
		out = append(out, raw)
	}
	return out, nil
}

func (c *rpcClient) cachedHistory(feed string, since time.Time) (RoundHistory, bool) {
	c.histMu.Lock()
	defer c.histMu.Unlock()
	cached, ok := c.hists[feed]
	if !ok || time.Since(cached.at) >= chainlinkHistoryTTL || len(cached.history.Rounds) == 0 {
		return RoundHistory{}, false
	}
	// A cached read only answers a request it actually covers: one that reaches
	// the whole phase, or back past what this caller asked for.
	if !cached.history.Complete && !since.IsZero() && cached.history.Oldest().After(since) {
		return RoundHistory{}, false
	}
	if !cached.history.Complete && since.IsZero() {
		return RoundHistory{}, false
	}
	out := cached.history
	out.Rounds = append([]RoundData(nil), cached.history.Rounds...)
	return out, true
}

func (c *rpcClient) storeHistory(feed string, history RoundHistory) {
	if len(history.Rounds) == 0 || history.Truncated {
		// A short answer is worth returning once; it is not worth serving for the
		// next two minutes to everyone who asks.
		return
	}
	c.histMu.Lock()
	if c.hists == nil {
		c.hists = make(map[string]cachedHistory)
	}
	stored := history
	stored.Rounds = append([]RoundData(nil), history.Rounds...)
	c.hists[feed] = cachedHistory{history: stored, at: time.Now()}
	c.histMu.Unlock()
}

func (c *rpcClient) cachedFirstRound(feed string, phase *big.Int) (RoundData, bool) {
	c.firstMu.Lock()
	defer c.firstMu.Unlock()
	cached, ok := c.firsts[feed]
	if !ok || cached.phase != phase.String() || time.Since(cached.at) >= chainlinkFirstRoundTTL {
		return RoundData{}, false
	}
	return cached.data, true
}

func (c *rpcClient) storeFirstRound(feed string, phase *big.Int, data RoundData) {
	c.firstMu.Lock()
	if c.firsts == nil {
		c.firsts = make(map[string]cachedFirstRound)
	}
	c.firsts[feed] = cachedFirstRound{data: data, phase: phase.String(), at: time.Now()}
	c.firstMu.Unlock()
}

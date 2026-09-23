package evm

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
)

const testFeed = "0x787f13dea48db0897cbcdd985de77809d837f988"

// fakeNode is a Base endpoint that answers latestRoundData and multicalled
// getRoundData out of a generated round set, and counts what it was asked.
type fakeNode struct {
	rounds map[uint64]RoundData
	newest uint64
	phase  *big.Int

	// requests counts HTTP round trips; calls counts the eth_calls inside them.
	requests atomic.Int64
	calls    atomic.Int64
	// failAfter makes every request past the nth answer 429, for the rate-limit
	// paths. Zero never fails.
	failAfter int64
	// noBatch refuses JSON-RPC batches the way a minimal endpoint does.
	noBatch bool
	// shortRound returns an unparseable payload for one round id.
	shortRound uint64
}

// newFakeNode builds `count` rounds ending now, `spacing` apart, walking a price
// up one cent a round from $300.
func newFakeNode(count int, spacing time.Duration, end time.Time) *fakeNode {
	node := &fakeNode{rounds: make(map[uint64]RoundData, count), newest: uint64(count), phase: big.NewInt(2)}
	for i := 1; i <= count; i++ {
		id := uint64(i)
		node.rounds[id] = RoundData{
			RoundID:   composeRoundID(node.phase, id),
			Answer:    big.NewInt(30_000_000_000 + int64(i)*1_000_000),
			UpdatedAt: end.Add(-time.Duration(count-i) * spacing),
		}
	}
	return node
}

func (n *fakeNode) serve(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.requests.Add(1)
		if n.failAfter > 0 && n.requests.Load() > n.failAfter {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		var raw json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		trimmed := strings.TrimSpace(string(raw))
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(trimmed, "[") {
			if n.noBatch {
				_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"message":"batch requests are not supported"}}`))
				return
			}
			var batch []rpcRequest
			if err := json.Unmarshal(raw, &batch); err != nil {
				t.Errorf("decode batch: %v", err)
				return
			}
			out := make([]string, 0, len(batch))
			for _, req := range batch {
				n.calls.Add(1)
				out = append(out, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":"0x%x"}`, req.ID, n.answer(t, req)))
			}
			_, _ = w.Write([]byte("[" + strings.Join(out, ",") + "]"))
			return
		}
		var req rpcRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		n.calls.Add(1)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":"0x%x"}`, req.ID, n.answer(t, req))))
	}))
	t.Cleanup(server.Close)
	return server
}

// answer decodes one eth_call and encodes the aggregator's reply.
func (n *fakeNode) answer(t *testing.T, req rpcRequest) []byte {
	t.Helper()
	if req.Method != "eth_call" || len(req.Params) == 0 {
		t.Fatalf("unexpected method %q", req.Method)
	}
	params, ok := req.Params[0].(map[string]any)
	if !ok {
		// Re-marshalled params come back as a generic map only after a round trip;
		// in-process they are the map we sent.
		encoded, _ := json.Marshal(req.Params[0])
		var decoded map[string]any
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("params: %v", err)
		}
		params = decoded
	}
	data, err := decodeHex(params["data"].(string))
	if err != nil {
		t.Fatalf("data: %v", err)
	}
	to := strings.ToLower(params["to"].(string))

	if strings.EqualFold(to, Multicall3Address) {
		calls, err := decodeAggregate3Calls(data)
		if err != nil {
			t.Fatalf("aggregate3 calls: %v", err)
		}
		results := make([]multicall3Result, 0, len(calls))
		for _, call := range calls {
			results = append(results, n.roundResult(call))
		}
		packed, err := multicall3ABI.Methods["aggregate3"].Outputs.Pack(results)
		if err != nil {
			t.Fatalf("pack: %v", err)
		}
		return packed
	}
	if len(data) >= 4 && string(data[:4]) == string(latestRoundDataSelector) {
		return encodeRound(n.rounds[n.newest])
	}
	t.Fatalf("unexpected call to %s", to)
	return nil
}

func (n *fakeNode) roundResult(call []byte) multicall3Result {
	if len(call) < 36 || string(call[:4]) != string(getRoundDataSelector) {
		return multicall3Result{Success: false}
	}
	id := new(big.Int).SetBytes(call[4:36])
	_, round, ok := splitRoundID(id)
	if !ok {
		return multicall3Result{Success: false}
	}
	if n.shortRound != 0 && round == n.shortRound {
		return multicall3Result{Success: true, ReturnData: []byte{0x12, 0x34}}
	}
	data, found := n.rounds[round]
	if !found {
		// What a real aggregator does for a round it does not hold: a zeroed answer
		// rather than a revert.
		return multicall3Result{Success: true, ReturnData: make([]byte, 160)}
	}
	return multicall3Result{Success: true, ReturnData: encodeRound(data)}
}

func encodeRound(round RoundData) []byte {
	raw := make([]byte, 160)
	if round.RoundID != nil {
		round.RoundID.FillBytes(raw[0:32])
	}
	if round.Answer != nil {
		round.Answer.FillBytes(raw[32:64])
	}
	if !round.UpdatedAt.IsZero() {
		big.NewInt(round.UpdatedAt.Unix()).FillBytes(raw[96:128])
	}
	return raw
}

// decodeAggregate3Calls unpacks the call array a client sent, so the fake node
// answers exactly what was asked rather than assuming an order.
func decodeAggregate3Calls(data []byte) ([][]byte, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("short aggregate3 payload")
	}
	values, err := multicall3ABI.Methods["aggregate3"].Inputs.Unpack(data[4:])
	if err != nil {
		return nil, err
	}
	if len(values) != 1 {
		return nil, fmt.Errorf("aggregate3: unexpected inputs")
	}
	converted, ok := abi.ConvertType(values[0], new([]multicall3Call)).(*[]multicall3Call)
	if !ok || converted == nil {
		return nil, fmt.Errorf("aggregate3: unexpected input shape %T", values[0])
	}
	out := make([][]byte, 0, len(*converted))
	for _, call := range *converted {
		out = append(out, call.CallData)
	}
	return out, nil
}

// TestChainlinkRoundsSince_costsAHandfulOfRequestsNotOnePerRound is the guard on
// the thing that made this unusable: reading a quarter of history one eth_call at
// a time. The numbers are deliberately generous — what must never come back is a
// cost that grows with the number of rounds.
func TestChainlinkRoundsSince_costsAHandfulOfRequestsNotOnePerRound(t *testing.T) {
	t.Parallel()
	const rounds = 240
	node := newFakeNode(rounds, 30*time.Minute, time.Now())
	client := NewJSONRPCClient(node.serve(t).URL)

	history, err := client.ChainlinkRoundsSince(context.Background(), testFeed, time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Rounds) != rounds {
		t.Fatalf("rounds = %d, want %d", len(history.Rounds), rounds)
	}
	if !history.Complete {
		t.Fatal("history should reach round 1")
	}

	cold := node.requests.Load()
	if cold > 4 {
		t.Fatalf("cold read cost %d HTTP requests, want a handful", cold)
	}
	// The real regression guard: one request per round would be 240 of them, and
	// anything proportional to the round count fails here long before that.
	if cold >= rounds/10 {
		t.Fatalf("cold read cost %d HTTP requests for %d rounds: this scales per round", cold, rounds)
	}

	if _, err := client.ChainlinkRoundsSince(context.Background(), testFeed, time.Time{}, 0); err != nil {
		t.Fatal(err)
	}
	if warm := node.requests.Load() - cold; warm != 0 {
		t.Fatalf("warm read cost %d HTTP requests, want 0", warm)
	}
}

func TestChainlinkRoundsSince_stopsAtTheWindowInsteadOfReadingEverything(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	node := newFakeNode(2000, 15*time.Minute, now)
	client := NewJSONRPCClient(node.serve(t).URL)

	since := now.Add(-24 * time.Hour)
	history, err := client.ChainlinkRoundsSince(context.Background(), testFeed, since, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Rounds) >= 2000 {
		t.Fatalf("rounds = %d: a day should not cost the whole feed", len(history.Rounds))
	}
	if history.Oldest().After(since) {
		t.Fatalf("oldest round %s is inside the window, so the window is not covered", history.Oldest())
	}
	if history.FirstRoundAt.IsZero() {
		t.Fatal("a windowed read still has to report where the feed starts")
	}
	if requests := node.requests.Load(); requests > 3 {
		t.Fatalf("windowed read cost %d requests", requests)
	}
}

func TestChainlinkRoundsSince_reportsTheFeedsFirstRound(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	node := newFakeNode(120, time.Hour, now)
	client := NewJSONRPCClient(node.serve(t).URL)

	history, err := client.ChainlinkRoundsSince(context.Background(), testFeed, now.Add(-2*time.Hour), 0)
	if err != nil {
		t.Fatal(err)
	}
	want := now.Add(-119 * time.Hour)
	if !history.FirstRoundAt.Equal(want) {
		t.Fatalf("first round = %s, want %s", history.FirstRoundAt, want)
	}
}

func TestChainlinkRoundsSince_rateLimitedMidWalkIsTruncatedNotSilent(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	node := newFakeNode(900, 10*time.Minute, now)
	node.failAfter = 2
	client := NewJSONRPCClient(node.serve(t).URL)

	history, err := client.ChainlinkRoundsSince(context.Background(), testFeed, now.AddDate(0, -1, 0), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !history.Truncated {
		t.Fatal("a walk cut short by a rate limit has to say so")
	}
	if len(history.Rounds) < 2 {
		t.Fatalf("rounds = %d: what was read should still come back", len(history.Rounds))
	}

	// A truncated answer is never cached: the next caller gets a real attempt.
	node.failAfter = 0
	before := node.requests.Load()
	if _, err := client.ChainlinkRoundsSince(context.Background(), testFeed, now.AddDate(0, -1, 0), 0); err != nil {
		t.Fatal(err)
	}
	if node.requests.Load() == before {
		t.Fatal("a truncated history was served from the cache")
	}
}

func TestChainlinkRoundsSince_rpcDownIsAnError(t *testing.T) {
	t.Parallel()
	node := newFakeNode(10, time.Hour, time.Now())
	node.failAfter = -1
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)
	client := NewJSONRPCClient(server.URL)

	if _, err := client.ChainlinkRoundsSince(context.Background(), testFeed, time.Time{}, 0); err == nil {
		t.Fatal("a dead endpoint must not read as an empty history")
	}
}

func TestChainlinkRoundsSince_rateLimitedOnTheFirstReadIsAnError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)
	client := NewJSONRPCClient(server.URL)

	_, err := client.ChainlinkRoundsSince(context.Background(), testFeed, time.Time{}, 0)
	if err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("err = %v, want a rate limit", err)
	}
}

func TestChainlinkRoundsSince_skipsUnpricedAndUndatedRounds(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	node := newFakeNode(60, time.Hour, now)
	// A round with no price and a round with no time: both are what an aggregator
	// returns for an id it does not hold, and neither can go on a chart.
	node.rounds[30] = RoundData{RoundID: composeRoundID(node.phase, 30), Answer: big.NewInt(0), UpdatedAt: now.Add(-30 * time.Hour)}
	node.rounds[31] = RoundData{RoundID: composeRoundID(node.phase, 31), Answer: big.NewInt(30_000_000_000), UpdatedAt: time.Unix(0, 0)}
	client := NewJSONRPCClient(node.serve(t).URL)

	history, err := client.ChainlinkRoundsSince(context.Background(), testFeed, time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Rounds) != 58 {
		t.Fatalf("rounds = %d, want 58 (60 minus the unpriced and the undated)", len(history.Rounds))
	}
	for _, round := range history.Rounds {
		if round.Answer.Sign() <= 0 || round.UpdatedAt.Unix() <= 0 {
			t.Fatalf("kept a round that cannot be drawn: %+v", round)
		}
	}
}

func TestChainlinkRoundsSince_feedWithFewerRoundsThanAsked(t *testing.T) {
	t.Parallel()
	// Round times survive the chain as whole seconds, so the fixture uses them.
	now := time.Now().UTC().Truncate(time.Second)
	node := newFakeNode(3, time.Hour, now)
	client := NewJSONRPCClient(node.serve(t).URL)

	history, err := client.ChainlinkRoundsSince(context.Background(), testFeed, now.AddDate(-1, 0, 0), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Rounds) != 3 || !history.Complete {
		t.Fatalf("rounds = %d complete = %v, want all 3 and complete", len(history.Rounds), history.Complete)
	}
	if !history.FirstRoundAt.Equal(now.Add(-2 * time.Hour)) {
		t.Fatalf("first round = %s", history.FirstRoundAt)
	}
}

func TestChainlinkRoundsSince_worksWithoutJSONRPCBatching(t *testing.T) {
	t.Parallel()
	node := newFakeNode(150, 30*time.Minute, time.Now())
	node.noBatch = true
	client := NewJSONRPCClient(node.serve(t).URL)

	history, err := client.ChainlinkRoundsSince(context.Background(), testFeed, time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Rounds) != 150 {
		t.Fatalf("rounds = %d, want 150 — batching is an optimisation, not a requirement", len(history.Rounds))
	}
}

func TestChainlinkRoundsSince_malformedRoundIsSkippedNotDrawn(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	node := newFakeNode(40, time.Hour, now)
	// One round comes back too short to be a round. It must vanish from the
	// series rather than take the other 39 with it or be read as a price.
	node.shortRound = 30
	client := NewJSONRPCClient(node.serve(t).URL)

	history, err := client.ChainlinkRoundsSince(context.Background(), testFeed, time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Rounds) != 39 {
		t.Fatalf("rounds = %d, want 39: the malformed one is dropped and the rest survive", len(history.Rounds))
	}
}

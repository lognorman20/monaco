package mintinfo

import (
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

const (
	mintSPACEX  = "PreANxuXjsy2pvisWWMNB6YaJNzr7681wJJr2rHsfTh"
	mintOPENAI  = "PreweJYECqtQwBtpxHL171nL2K6umo692gTm7Q3rpgF"
	mintKALSHI  = "PreLWGkkeqG1s4HEfFZSy9moCrJ7btsHuUtfcCeoRua"
	mintTSpaceX = "TSPXcLV76s6V2zDiZQ18kBfcbnjaE2ZzNT3ga2Pd99v"
	mintZero    = "ZeroMultiplierMint1111111111111111111111111"
)

//go:embed testdata/spacex_mint.json
var spacexMintFixture []byte

//go:embed testdata/openai_mint.json
var openaiMintFixture []byte

//go:embed testdata/kalshi_mint.json
var kalshiMintFixture []byte

//go:embed testdata/tspacex_mint.json
var tspacexMintFixture []byte

//go:embed testdata/zero_multiplier_mint.json
var zeroMultiplierFixture []byte

//go:embed testdata/epoch_info.json
var epochInfoFixture []byte

//go:embed testdata/epoch_info_before_fee.json
var epochBeforeFeeFixture []byte

var spacexMultiplierEffective = time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)

type rpcHarness struct {
	t             *testing.T
	accountByMint map[string][]byte
	epochBody     []byte
	failRPC       atomic.Bool
	epochCalls    atomic.Int32
	accountCalls  atomic.Int32
}

func newRPCHarness(t *testing.T, accounts map[string][]byte, epoch []byte) *rpcHarness {
	t.Helper()
	return &rpcHarness{
		t:             t,
		accountByMint: accounts,
		epochBody:     epoch,
	}
}

func (h *rpcHarness) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if h.failRPC.Load() {
			http.Error(w, "upstream down", 503)
			return
		}
		switch req.Method {
		case "getEpochInfo":
			h.epochCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(h.epochBody)
		case "getAccountInfo":
			h.accountCalls.Add(1)
			var envelope struct {
				Params []json.RawMessage `json:"params"`
			}
			if err := json.Unmarshal(body, &envelope); err != nil || len(envelope.Params) < 1 {
				http.Error(w, "bad params", 400)
				return
			}
			var mint string
			if err := json.Unmarshal(envelope.Params[0], &mint); err != nil {
				http.Error(w, "bad mint", 400)
				return
			}
			resp, ok := h.accountByMint[mint]
			if !ok {
				http.Error(w, "unknown mint", 404)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(resp)
		default:
			http.Error(w, "unknown method", 400)
		}
	}))
}

func (h *rpcHarness) reader(at time.Time) *HTTPReader {
	srv := h.server()
	t := h.t
	t.Cleanup(srv.Close)
	return NewHTTPReaderWithClient(srv.URL, srv.Client(), func() time.Time { return at })
}

func ratEq(a, b *big.Rat, label string, t *testing.T) {
	t.Helper()
	if a == nil || b == nil {
		if a != b {
			t.Fatalf("%s: nil mismatch", label)
		}
		return
	}
	if a.Cmp(b) != 0 {
		t.Fatalf("%s: got %s want %s", label, a.FloatString(8), b.FloatString(8))
	}
}

func TestReader_spacex_multiplier5_fee100_afterEffective(t *testing.T) {
	t.Parallel()
	at := spacexMultiplierEffective.Add(24 * time.Hour)
	h := newRPCHarness(t, map[string][]byte{mintSPACEX: spacexMintFixture}, epochInfoFixture)
	r := h.reader(at)

	info, err := r.Info(context.Background(), mintSPACEX)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Decimals != 9 {
		t.Fatalf("decimals = %d", info.Decimals)
	}
	if info.TransferFeeBps != 100 {
		t.Fatalf("fee bps = %d, want 100", info.TransferFeeBps)
	}
	ratEq(info.UiMultiplier, big.NewRat(5, 1), "multiplier", t)
	if !info.PermanentDelegate {
		t.Fatal("expected permanent delegate")
	}
}

func TestReader_spacex_beforeEffective_multiplier1(t *testing.T) {
	t.Parallel()
	at := spacexMultiplierEffective.Add(-24 * time.Hour)
	h := newRPCHarness(t, map[string][]byte{mintSPACEX: spacexMintFixture}, epochInfoFixture)
	r := h.reader(at)

	info, err := r.Info(context.Background(), mintSPACEX)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	ratEq(info.UiMultiplier, big.NewRat(1, 1), "multiplier", t)
	if info.TransferFeeBps != 100 {
		t.Fatalf("fee bps = %d, want 100", info.TransferFeeBps)
	}
}

func TestReader_feeEpoch_picksOlderBeforeNewerEpoch(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	h := newRPCHarness(t, map[string][]byte{mintSPACEX: spacexMintFixture}, epochBeforeFeeFixture)
	r := h.reader(at)

	info, err := r.Info(context.Background(), mintSPACEX)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.TransferFeeBps != 50 {
		t.Fatalf("fee bps = %d, want 50", info.TransferFeeBps)
	}
}

func TestReader_tSpaceX_noScaleExtension_multiplier1_fee20(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	h := newRPCHarness(t, map[string][]byte{mintTSpaceX: tspacexMintFixture}, epochInfoFixture)
	r := h.reader(at)

	info, err := r.Info(context.Background(), mintTSpaceX)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	ratEq(info.UiMultiplier, big.NewRat(1, 1), "multiplier", t)
	if info.TransferFeeBps != 20 {
		t.Fatalf("fee bps = %d, want 20", info.TransferFeeBps)
	}
}

func TestReader_rpcDown_returnsLastGoodThenStatic(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	h := newRPCHarness(t, map[string][]byte{mintKALSHI: kalshiMintFixture}, epochInfoFixture)
	srv := h.server()
	t.Cleanup(srv.Close)

	clock := at
	r := NewHTTPReaderWithClient(srv.URL, srv.Client(), func() time.Time { return clock })

	first, err := r.Info(context.Background(), mintKALSHI)
	if err != nil {
		t.Fatalf("first Info: %v", err)
	}
	if first.TransferFeeBps != 100 {
		t.Fatalf("first fee = %d", first.TransferFeeBps)
	}

	h.failRPC.Store(true)
	clock = at.Add(11 * time.Minute)
	second, err := r.Info(context.Background(), mintKALSHI)
	if err != nil {
		t.Fatalf("second Info: %v", err)
	}
	if second.TransferFeeBps != first.TransferFeeBps {
		t.Fatalf("last-good fee = %d want %d", second.TransferFeeBps, first.TransferFeeBps)
	}

	clock = at.Add(25 * time.Hour)
	third, err := r.Info(context.Background(), mintKALSHI)
	if err != nil {
		t.Fatalf("third Info: %v", err)
	}
	if third.TransferFeeBps != 100 {
		t.Fatalf("static fee = %d, want 100", third.TransferFeeBps)
	}
	ratEq(third.UiMultiplier, big.NewRat(1, 1), "static multiplier", t)
}

func TestReader_zeroMultiplier_isUnresolved(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	h := newRPCHarness(t, map[string][]byte{mintZero: zeroMultiplierFixture}, epochInfoFixture)
	r := h.reader(at)

	info, err := r.Info(context.Background(), mintZero)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.UiMultiplier != nil {
		t.Fatalf("expected nil multiplier, got %v", info.UiMultiplier)
	}
}

func TestReader_emptyRPCURL_usesStatic(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	r := NewHTTPReaderWithClient("", nil, func() time.Time { return at })

	info, err := r.Info(context.Background(), mintSPACEX)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	ratEq(info.UiMultiplier, big.NewRat(5, 1), "multiplier", t)
	if info.TransferFeeBps != 100 {
		t.Fatalf("fee = %d", info.TransferFeeBps)
	}

	_, err = r.Info(context.Background(), "UnknownMint1111111111111111111111111111")
	if err != ErrUnknownMint {
		t.Fatalf("err = %v, want ErrUnknownMint", err)
	}
}

func TestReader_openai_staticMultiplierString(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	h := newRPCHarness(t, map[string][]byte{mintOPENAI: openaiMintFixture}, epochInfoFixture)
	r := h.reader(at)

	info, err := r.Info(context.Background(), mintOPENAI)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	want := new(big.Rat)
	_, _ = want.SetString("1.4861347")
	ratEq(info.UiMultiplier, want, "openai multiplier", t)
}

func TestReader_cacheInvalidatesAtMultiplierEffective(t *testing.T) {
	t.Parallel()
	before := spacexMultiplierEffective.Add(-time.Hour)
	h := newRPCHarness(t, map[string][]byte{mintSPACEX: spacexMintFixture}, epochInfoFixture)
	srv := h.server()
	t.Cleanup(srv.Close)

	clock := before
	r := NewHTTPReaderWithClient(srv.URL, srv.Client(), func() time.Time { return clock })

	info, err := r.Info(context.Background(), mintSPACEX)
	if err != nil {
		t.Fatalf("before: %v", err)
	}
	ratEq(info.UiMultiplier, big.NewRat(1, 1), "before effective", t)

	after := spacexMultiplierEffective.Add(time.Hour)
	clock = after
	info2, err := r.Info(context.Background(), mintSPACEX)
	if err != nil {
		t.Fatalf("after: %v", err)
	}
	ratEq(info2.UiMultiplier, big.NewRat(5, 1), "after effective", t)
	if h.accountCalls.Load() < 2 {
		t.Fatalf("expected refetch after effective, account calls = %d", h.accountCalls.Load())
	}
}

func TestFakeReader(t *testing.T) {
	t.Parallel()
	want := Info{Mint: mintSPACEX, Decimals: 9, UiMultiplier: big.NewRat(1, 1)}
	fr := NewFakeReader(map[string]Info{mintSPACEX: want})
	got, err := fr.Info(context.Background(), mintSPACEX)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mint != want.Mint {
		t.Fatalf("mint = %q", got.Mint)
	}
}

func TestParseAccountInfo_nonToken2022(t *testing.T) {
	t.Parallel()
	body := []byte(`{
	  "result": {
	    "value": {
	      "owner": "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA",
	      "data": {"parsed": {"info": {"decimals": 6, "extensions": []}}}
	    }
	  }
	}`)
	now := time.Now()
	info, err := parseAccountInfo(body, "So11111111111111111111111111111111111111112", 0, now)
	if err != nil {
		t.Fatal(err)
	}
	if info.Decimals != 6 {
		t.Fatalf("decimals = %d", info.Decimals)
	}
	ratEq(info.UiMultiplier, big.NewRat(1, 1), "multiplier", t)
}

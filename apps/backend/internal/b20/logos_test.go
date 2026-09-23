package b20

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"math/big"
	"sync"
	"testing"
	"time"
)

// appleContractURI is AAPLc's contractURI() as the token returned it on Base
// mainnet (read 2026-09-21).
const appleContractURI = "data:application/json;base64,eyJuYW1lIjoiQXBwbGUgSW5jLiIsInN5bWJvbCI6IkFBUExjIiwiaW1hZ2UiOiJodHRwczovL21ldGFkYXRhLmNvaW5iYXNlLmNvbS9lcXVpdHlfaWNvbnMvODczODE5ZjRiMTRlZmU0NGI5NGFiZWNiYzhlODg2NGQyOTk4MTYzYWJkMGNhNTViNDQ5ZWU2ZWIwN2QwZDk0Yy5wbmcifQ=="

const appleLogo = "https://metadata.coinbase.com/equity_icons/873819f4b14efe44b94abecbc8e8864d2998163abd0ca55b449ee6eb07d0d94c.png"

func abiString(s string) []byte {
	word := func(n int) []byte {
		b := make([]byte, 32)
		big.NewInt(int64(n)).FillBytes(b)
		return b
	}
	padded := make([]byte, (len(s)+31)/32*32)
	copy(padded, s)
	return append(append(word(32), word(len(s))...), padded...)
}

func jsonURI(doc string) string {
	return "data:application/json;base64," + base64.StdEncoding.EncodeToString([]byte(doc))
}

type stubCaller struct {
	mu    sync.Mutex
	calls int
	ret   []byte
	err   error
	wait  time.Duration
}

func (s *stubCaller) Call(ctx context.Context, to string, data []byte) ([]byte, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	if !bytes.Equal(data, contractURISelector) {
		return nil, errors.New("unexpected calldata")
	}
	if s.wait > 0 {
		select {
		case <-time.After(s.wait):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return s.ret, s.err
}

func TestLogoFromContractURI_readsTheIssuersImage(t *testing.T) {
	got, err := LogoFromContractURI(appleContractURI)
	if err != nil || got != appleLogo {
		t.Fatalf("logo = %q, %v; want %q", got, err, appleLogo)
	}
}

func TestLogoFromContractURI_refusesWhatTheAppShouldNotLoad(t *testing.T) {
	cases := map[string]string{
		"empty":           "",
		"remote document": "https://example.com/meta.json",
		"not base64":      "data:application/json;base64,%%%",
		"not json":        jsonURI(`{"image":`),
		"no image":        jsonURI(`{"name":"Apple Inc."}`),
		"http":            jsonURI(`{"image":"http://metadata.coinbase.com/equity_icons/a.png"}`),
		"other host":      jsonURI(`{"image":"https://evil.example/a.png"}`),
		"lookalike host":  jsonURI(`{"image":"https://metadata.coinbase.com.evil.example/a.png"}`),
		"userinfo":        jsonURI(`{"image":"https://x@metadata.coinbase.com/a.png"}`),
		"port":            jsonURI(`{"image":"https://metadata.coinbase.com:8443/a.png"}`),
		"javascript":      jsonURI(`{"image":"javascript:alert(1)"}`),
	}
	for name, uri := range cases {
		if got, err := LogoFromContractURI(uri); err == nil || got != "" {
			t.Errorf("%s: logo = %q, err = %v; want refused", name, got, err)
		}
	}
}

func TestDecodeABIString_rejectsMalformedReturnData(t *testing.T) {
	if got, err := decodeABIString(abiString(appleContractURI)); err != nil || got != appleContractURI {
		t.Fatalf("round trip: %q, %v", got, err)
	}
	bad := [][]byte{
		nil,
		make([]byte, 31),
		append(append(make([]byte, 31), 0xff), make([]byte, 32)...),      // offset past the end
		append(abiString("abc")[:32], append(make([]byte, 31), 0x7f)...), // length past the end
	}
	for i, raw := range bad {
		if _, err := decodeABIString(raw); err == nil {
			t.Errorf("case %d decoded", i)
		}
	}
}

func TestContractLogos_cachesAnAnswerForADay(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	chain := &stubCaller{ret: abiString(appleContractURI)}
	logos := NewContractLogos(chain, func() time.Time { return now })

	for i := 0; i < 3; i++ {
		if got := logos.LogoURL(context.Background(), "0xB200000000000000000000C2E324D24D7EECD1FB"); got != appleLogo {
			t.Fatalf("logo = %q", got)
		}
	}
	if chain.calls != 1 {
		t.Fatalf("chain read %d times, want 1", chain.calls)
	}
	now = now.Add(logoTTL + time.Second)
	logos.LogoURL(context.Background(), "0xb200000000000000000000c2e324d24d7eecd1fb")
	if chain.calls != 2 {
		t.Fatalf("chain read %d times after expiry, want 2", chain.calls)
	}
}

func TestContractLogos_aFailedReadIsNoLogoAndIsRetriedLater(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	chain := &stubCaller{err: errors.New("rpc down")}
	logos := NewContractLogos(chain, func() time.Time { return now })

	if got := logos.LogoURL(context.Background(), "0xb2"); got != "" {
		t.Fatalf("logo = %q, want none", got)
	}
	logos.LogoURL(context.Background(), "0xb2")
	if chain.calls != 1 {
		t.Fatalf("a failure was retried %d times inside its back-off", chain.calls)
	}
	chain.err, chain.ret = nil, abiString(appleContractURI)
	now = now.Add(logoFailureTTL + time.Second)
	if got := logos.LogoURL(context.Background(), "0xb2"); got != appleLogo {
		t.Fatalf("logo after back-off = %q", got)
	}
}

func TestContractLogos_aMalformedPayloadIsNoLogo(t *testing.T) {
	logos := NewContractLogos(&stubCaller{ret: []byte{1, 2, 3}}, nil)
	if got := logos.LogoURL(context.Background(), "0xb2"); got != "" {
		t.Fatalf("logo = %q, want none", got)
	}
}

func TestContractLogos_aCallerDeadlineIsNotCachedAsAFailure(t *testing.T) {
	chain := &stubCaller{ret: abiString(appleContractURI), wait: time.Second}
	logos := NewContractLogos(chain, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if got := logos.LogoURL(ctx, "0xb2"); got != "" {
		t.Fatalf("logo = %q under an expired deadline", got)
	}
	chain.wait = 0
	if got := logos.LogoURL(context.Background(), "0xb2"); got != appleLogo {
		t.Fatalf("the next caller got %q; the deadline was cached as a failure", got)
	}
}

func TestNewContractLogos_withoutAChainThereAreNoLogos(t *testing.T) {
	if NewContractLogos(nil, nil) != nil {
		t.Fatal("want nil")
	}
}

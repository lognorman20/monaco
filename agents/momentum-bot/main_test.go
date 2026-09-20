package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func env(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

var fullEnv = map[string]string{"MONACO_API": "http://127.0.0.1:1", "MONACO_GROUP_ID": "group-1", "MONACO_AGENT_KEY": testKey}

func TestParseOptions_defaultsToDryRun(t *testing.T) {
	opts, err := parseOptions(nil, env(fullEnv))
	if err != nil {
		t.Fatal(err)
	}
	if opts.cfg.Live {
		t.Fatal("must not be live without --live")
	}
	if opts.tradeUSD != 1 || opts.maxUSD != 5 {
		t.Fatalf("caps %v/%v", opts.tradeUSD, opts.maxUSD)
	}
}

func TestParseOptions_rejections(t *testing.T) {
	cases := map[string]struct {
		args []string
		env  map[string]string
		want string
	}{
		"missing key":            {nil, map[string]string{"MONACO_API": "x", "MONACO_GROUP_ID": "g"}, "MONACO_AGENT_KEY is not set"},
		"missing api":            {nil, map[string]string{"MONACO_GROUP_ID": "g", "MONACO_AGENT_KEY": "k"}, "MONACO_API is not set"},
		"key is never a flag":    {[]string{"--key", testKey}, fullEnv, "flag provided but not defined"},
		"live and dry-run":       {[]string{"--live", "--dry-run"}, fullEnv, "mutually exclusive"},
		"cap below trade size":   {[]string{"--trade-usd", "2", "--max-spend-usd", "1"}, fullEnv, "--max-spend-usd"},
		"lookback too short":     {[]string{"--interval", "30s", "--lookback", "45s"}, fullEnv, "--lookback"},
		"negative threshold":     {[]string{"--buy-pct", "-1"}, fullEnv, "must not be negative"},
		"stray positional input": {[]string{"buy"}, fullEnv, "unexpected argument"},
	}
	for name, tc := range cases {
		_, err := parseOptions(tc.args, env(tc.env))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want %q", name, err, tc.want)
		}
	}
}

// fakeStack serves both the Monaco catalog/intent routes and a Jupiter price feed
// whose GOOGLx price climbs 1% per request.
func fakeStack(t *testing.T) (envFn func(string) string, posts *atomic.Int32) {
	t.Helper()
	posts = &atomic.Int32{}
	var priceCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/groups/group-1/assets", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"assets":[{"symbol":"GOOGLx","solanaMint":"mintG","routable":true},{"symbol":"DEADx","solanaMint":"mintD","routable":false}]}`))
	})
	mux.HandleFunc("POST /v1/groups/group-1/agents/intents", func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		_, _ = w.Write([]byte(`{"intentId":"i","status":"executed","transactionId":"tx"}`))
	})
	mux.HandleFunc("GET /price/v3", func(w http.ResponseWriter, r *http.Request) {
		n := priceCalls.Add(1)
		_, _ = w.Write([]byte(`{"mintG":{"usdPrice":` + []string{"100", "101", "102", "103", "104"}[min(int(n)-1, 4)] + `}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return env(map[string]string{
		"MONACO_API": srv.URL, "MONACO_GROUP_ID": "group-1", "MONACO_AGENT_KEY": testKey,
		"JUPITER_PRICE_URL": srv.URL + "/price/v3",
	}), posts
}

var fastOnce = []string{"--once", "--interval", "1s", "--lookback", "2s"}

func TestRun_dryRunOnceEndToEnd(t *testing.T) {
	envFn, posts := fakeStack(t)
	var out bytes.Buffer
	if err := run(fastOnce, envFn, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if posts.Load() != 0 {
		t.Fatalf("dry run posted %d intents", posts.Load())
	}
	log := out.String()
	for _, want := range []string{"DRY RUN", "watching  GOOGLx\n", "key       set (hidden)", "→ would buy $1.00 of GOOGLx"} {
		if !strings.Contains(log, want) {
			t.Errorf("output missing %q:\n%s", want, log)
		}
	}
	if strings.Contains(log, testKey) {
		t.Fatal("the key must never be printed")
	}
}

func TestRun_liveNeedsAYes(t *testing.T) {
	envFn, posts := fakeStack(t)
	for _, answer := range []string{"n\n", "\n", ""} {
		var out bytes.Buffer
		err := run(append([]string{"--live"}, fastOnce...), envFn, strings.NewReader(answer), &out)
		if err == nil || !strings.Contains(err.Error(), "not confirmed") {
			t.Fatalf("answer %q: got %v", answer, err)
		}
	}
	if posts.Load() != 0 {
		t.Fatalf("posted %d intents without confirmation", posts.Load())
	}
}

func TestRun_liveConfirmedTradesAgainstTheFake(t *testing.T) {
	envFn, posts := fakeStack(t)
	var out bytes.Buffer
	if err := run(append([]string{"--live"}, fastOnce...), envFn, strings.NewReader("y\n"), &out); err != nil {
		t.Fatal(err)
	}
	if posts.Load() != 1 || !strings.Contains(out.String(), "✓ filled  tx tx") {
		t.Fatalf("posts=%d\n%s", posts.Load(), out.String())
	}
}

func TestRun_unknownSymbolFailsBeforeTrading(t *testing.T) {
	envFn, _ := fakeStack(t)
	err := run(append([]string{"--symbols", "NOPEx"}, fastOnce...), envFn, strings.NewReader(""), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "NOPEx is not in the cabal's catalog") {
		t.Fatalf("got %v", err)
	}
}

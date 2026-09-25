package telemetry

import (
	"net/http"
	"time"
)

// Upstream service names: the `service` label on outbound-call metrics.
const (
	UpstreamJupiter   = "jupiter"
	UpstreamPyth      = "pyth"
	UpstreamPrivy     = "privy"
	UpstreamSolanaRPC = "solana_rpc"
	UpstreamXStocks   = "xstocks"
	UpstreamTessera   = "tessera"
	UpstreamPreStocks = "prestocks"
	UpstreamFlash     = "flash"
	UpstreamSupabase  = "supabase_storage"
)

type instrumentedTransport struct {
	classify func(*http.Request) string
	inner    http.RoundTripper
}

// Transport wraps inner so every call to an upstream is counted by outcome and timed.
// A nil inner uses http.DefaultTransport. 429s get their own outcome: rate limiting is the
// failure these upstreams actually produce, and it needs a different response than a 5xx.
func Transport(service string, inner http.RoundTripper) http.RoundTripper {
	if inner == nil {
		inner = http.DefaultTransport
	}
	return TransportFunc(func(*http.Request) string { return service }, inner)
}

// TransportFunc is Transport for a client that talks to more than one upstream: classify
// names the service per request. It must return one of a fixed set of names, never
// anything derived from the path or query.
func TransportFunc(classify func(*http.Request) string, inner http.RoundTripper) http.RoundTripper {
	if inner == nil {
		inner = http.DefaultTransport
	}
	return instrumentedTransport{classify: classify, inner: inner}
}

// InstrumentClient returns a copy of client whose transport records into this package.
func InstrumentClient(service string, client *http.Client) *http.Client {
	if client == nil {
		client = &http.Client{}
	}
	wrapped := *client
	wrapped.Transport = Transport(service, client.Transport)
	return &wrapped
}

func (t instrumentedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := t.inner.RoundTrip(req)
	ObserveUpstream(t.classify(req), upstreamOutcome(resp, err), time.Since(start))
	return resp, err
}

func upstreamOutcome(resp *http.Response, err error) string {
	switch {
	case err != nil:
		return "transport_error"
	case resp.StatusCode == http.StatusTooManyRequests:
		return OutcomeRateLimited
	case resp.StatusCode >= http.StatusInternalServerError:
		return "server_error"
	case resp.StatusCode >= http.StatusBadRequest:
		return "client_error"
	default:
		return OutcomeOK
	}
}

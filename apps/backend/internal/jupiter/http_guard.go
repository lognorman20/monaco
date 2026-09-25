package jupiter

import (
	"flag"
	"fmt"
	"net/http"
	"strings"
)

func runningGoTest() bool {
	return flag.Lookup("test.v") != nil
}

type blockLiveJupiterTransport struct {
	inner http.RoundTripper
}

func (t blockLiveJupiterTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	host := strings.ToLower(req.URL.Host)
	if strings.Contains(host, "jup.ag") {
		return nil, fmt.Errorf("jupiter: live API HTTP blocked during go test: %s", req.URL)
	}
	inner := t.inner
	if inner == nil {
		inner = http.DefaultTransport
	}
	return inner.RoundTrip(req)
}

func wrapHTTPClientForTests(client *http.Client) *http.Client {
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}
	if !runningGoTest() {
		return client
	}
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	wrapped := *client
	if wrapped.Timeout == 0 {
		wrapped.Timeout = defaultTimeout
	}
	wrapped.Transport = blockLiveJupiterTransport{inner: transport}
	return &wrapped
}

// WrapHTTPClientForTests applies the same guard to a Jupiter client that lives in
// its own package, so a live call from a test fails there too.
func WrapHTTPClientForTests(client *http.Client) *http.Client {
	return wrapHTTPClientForTests(client)
}

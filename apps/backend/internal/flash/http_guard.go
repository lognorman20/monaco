package flash

import (
	"flag"
	"fmt"
	"net/http"
	"strings"
)

func runningGoTest() bool {
	return flag.Lookup("test.v") != nil
}

type blockLiveFlashTransport struct {
	inner http.RoundTripper
}

func (t blockLiveFlashTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	host := strings.ToLower(req.URL.Host)
	if strings.Contains(host, "definitive.fi") {
		return nil, fmt.Errorf("flash: live API HTTP blocked during go test: %s", req.URL)
	}
	inner := t.inner
	if inner == nil {
		inner = http.DefaultTransport
	}
	return inner.RoundTrip(req)
}

func wrapHTTPClientForTests(client *http.Client) *http.Client {
	if !runningGoTest() {
		return client
	}
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	wrapped := *client
	wrapped.Transport = blockLiveFlashTransport{inner: transport}
	return &wrapped
}

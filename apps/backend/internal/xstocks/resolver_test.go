package xstocks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	aaplxSolanaMint = "XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp"
	tslaxSolanaMint = "XsDoVfqeBukxuZHWhdvWHBhgEHjGNst4MLodqsJHzoB"
)

// fakeXStocksServer serves fixture JSON for catalog asset lookups in unit tests.
func fakeXStocksServer(t *testing.T, fixtures map[string][]byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		const prefix = "/api/v2/public/assets/"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}

		symbol := strings.TrimPrefix(r.URL.Path, prefix)
		body, ok := fixtures[symbol]
		if !ok {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
}

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("testdata", name)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return body
}

func TestXStocksResolver_AAPLx_returnsSolanaMintFromDeployments(t *testing.T) {
	// Arrange
	server := fakeXStocksServer(t, map[string][]byte{
		"AAPLx": loadFixture(t, "aaplx.json"),
	})
	defer server.Close()

	resolver := NewHTTPResolverWithClient(server.URL, server.Client())
	ctx := context.Background()

	// Act
	mint, err := resolver.ResolveSolanaMint(ctx, "AAPLx")

	// Assert
	if err != nil {
		t.Fatalf("ResolveSolanaMint: %v", err)
	}
	if mint != aaplxSolanaMint {
		t.Fatalf("mint = %q, want %q", mint, aaplxSolanaMint)
	}
}

func TestXStocksResolver_TSLAx_returnsSolanaMintFromDeployments(t *testing.T) {
	// Arrange
	server := fakeXStocksServer(t, map[string][]byte{
		"TSLAx": loadFixture(t, "tslax.json"),
	})
	defer server.Close()

	resolver := NewHTTPResolverWithClient(server.URL, server.Client())
	ctx := context.Background()

	// Act
	mint, err := resolver.ResolveSolanaMint(ctx, "TSLAx")

	// Assert
	if err != nil {
		t.Fatalf("ResolveSolanaMint: %v", err)
	}
	if mint != tslaxSolanaMint {
		t.Fatalf("mint = %q, want %q", mint, tslaxSolanaMint)
	}
}

func TestXStocksResolver_missingSolanaDeployment_returnsError(t *testing.T) {
	// Arrange
	payload := []byte(`{
		"symbol": "NOCHAINx",
		"deployments": [
			{
				"address": "0x0000000000000000000000000000000000000003",
				"network": "Ethereum"
			}
		]
	}`)
	server := fakeXStocksServer(t, map[string][]byte{
		"NOCHAINx": payload,
	})
	defer server.Close()

	resolver := NewHTTPResolverWithClient(server.URL, server.Client())
	ctx := context.Background()

	// Act
	_, err := resolver.ResolveSolanaMint(ctx, "NOCHAINx")

	// Assert
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != ErrNoSolanaMint {
		t.Fatalf("err = %v, want %v", err, ErrNoSolanaMint)
	}
}

func TestXStocksResolver_malformedPayload_returnsError(t *testing.T) {
	// Arrange
	server := fakeXStocksServer(t, map[string][]byte{
		"BADx": []byte(`{"symbol":"BADx","deployments":[`),
	})
	defer server.Close()

	resolver := NewHTTPResolverWithClient(server.URL, server.Client())
	ctx := context.Background()

	// Act
	_, err := resolver.ResolveSolanaMint(ctx, "BADx")

	// Assert
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), ErrInvalidResponse.Error()) {
		t.Fatalf("err = %v, want wrapped %v", err, ErrInvalidResponse)
	}
}

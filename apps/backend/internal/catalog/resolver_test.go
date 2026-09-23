package catalog

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

func TestCompositeResolver_tSpaceX_and_T_SpaceX_resolveSameMint(t *testing.T) {
	tessera := NewFakeSource(tesseraSpaceX())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	resolver := NewResolver(xstocks.NewHTTPResolverWithClient(server.URL, server.Client()), tessera)
	ctx := context.Background()

	m1, err := resolver.ResolveSolanaMint(ctx, "tSpaceX")
	if err != nil {
		t.Fatal(err)
	}
	m2, err := resolver.ResolveSolanaMint(ctx, "T-SpaceX")
	if err != nil {
		t.Fatal(err)
	}
	if m1 != tSpaceXMint || m2 != tSpaceXMint {
		t.Fatalf("mints = %q %q, want %q", m1, m2, tSpaceXMint)
	}
}

func TestCompositeResolver_unknown_returnsErrNotFound(t *testing.T) {
	tessera := NewFakeSource()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	resolver := NewResolver(xstocks.NewHTTPResolverWithClient(server.URL, server.Client()), tessera)
	_, err := resolver.ResolveSolanaMint(context.Background(), "NOTREAL")
	if !errors.Is(err, xstocks.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

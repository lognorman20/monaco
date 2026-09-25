package catalog

import (
	"context"
	"sync"

	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

type fakeSource struct {
	mu      sync.Mutex
	rows    []xstocks.CatalogAsset
	listErr error
}

// NewFakeSource returns an in-memory catalog Source for tests.
func NewFakeSource(rows ...xstocks.CatalogAsset) Source {
	return &fakeSource{rows: append([]xstocks.CatalogAsset(nil), rows...)}
}

// SetFakeSourceListError forces List to return err on the fake source.
func SetFakeSourceListError(src Source, err error) {
	f, ok := src.(*fakeSource)
	if !ok {
		panic("catalog: SetFakeSourceListError requires NewFakeSource")
	}
	f.mu.Lock()
	f.listErr = err
	f.mu.Unlock()
}

func (f *fakeSource) List(_ context.Context) ([]xstocks.CatalogAsset, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	return append([]xstocks.CatalogAsset(nil), f.rows...), nil
}

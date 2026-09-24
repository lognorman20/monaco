package prestocks

import (
	"context"

	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

type fakeCatalog struct {
	rows []xstocks.CatalogAsset
	err  error
}

// NewFakeCatalog returns an in-memory PreStocks catalog for tests.
func NewFakeCatalog(rows ...xstocks.CatalogAsset) *fakeCatalog {
	cp := append([]xstocks.CatalogAsset(nil), rows...)
	return &fakeCatalog{rows: cp}
}

// SetFakeCatalogError forces List to return err.
func SetFakeCatalogError(c *fakeCatalog, err error) {
	c.err = err
}

func (f *fakeCatalog) List(ctx context.Context) ([]xstocks.CatalogAsset, error) {
	_ = ctx
	if f.err != nil {
		return nil, f.err
	}
	return append([]xstocks.CatalogAsset(nil), f.rows...), nil
}

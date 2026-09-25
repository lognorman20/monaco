package tessera

import (
	"context"

	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

type fakeCatalog struct {
	rows []xstocks.CatalogAsset
	err  error
}

// NewFakeCatalog returns an in-memory Tessera catalog for tests.
func NewFakeCatalog(rows ...xstocks.CatalogAsset) Catalog {
	cp := append([]xstocks.CatalogAsset(nil), rows...)
	return &fakeCatalog{rows: cp}
}

// SetFakeCatalogError forces List to return err.
func SetFakeCatalogError(c Catalog, err error) {
	f, ok := c.(*fakeCatalog)
	if !ok {
		panic("tessera: SetFakeCatalogError requires NewFakeCatalog")
	}
	f.err = err
}

func (f *fakeCatalog) List(ctx context.Context) ([]xstocks.CatalogAsset, error) {
	_ = ctx
	if f.err != nil {
		return nil, f.err
	}
	return append([]xstocks.CatalogAsset(nil), f.rows...), nil
}

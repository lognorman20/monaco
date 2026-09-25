package catalog

import (
	"context"

	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

// Source lists Tessera (or other supplemental) catalog rows.
type Source interface {
	List(ctx context.Context) ([]xstocks.CatalogAsset, error)
}

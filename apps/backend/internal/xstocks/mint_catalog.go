package xstocks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// MintCatalog resolves a Solana mint address to catalog metadata.
type MintCatalog interface {
	LookupByMint(ctx context.Context, mint string) (CatalogAsset, bool, error)
}

type mintIndex struct {
	mu         sync.RWMutex
	byMint     map[string]CatalogAsset
	allAssets  []CatalogAsset
	loaded     bool
}

func (s *HTTPCatalogSearcher) LookupByMint(ctx context.Context, mint string) (CatalogAsset, bool, error) {
	mint = strings.TrimSpace(mint)
	if mint == "" {
		return CatalogAsset{}, false, nil
	}
	if err := s.ensureMintIndex(ctx); err != nil {
		return CatalogAsset{}, false, err
	}
	s.mintIndex.mu.RLock()
	asset, ok := s.mintIndex.byMint[mint]
	s.mintIndex.mu.RUnlock()
	return asset, ok, nil
}

func (s *HTTPCatalogSearcher) ensureMintIndex(ctx context.Context) error {
	s.mintIndex.mu.RLock()
	if s.mintIndex.loaded {
		s.mintIndex.mu.RUnlock()
		return nil
	}
	s.mintIndex.mu.RUnlock()

	s.mintIndex.mu.Lock()
	defer s.mintIndex.mu.Unlock()
	if s.mintIndex.loaded {
		return nil
	}

	byMint := make(map[string]CatalogAsset)
	allAssets := make([]CatalogAsset, 0)
	hasNextPage := true
	for page := 0; hasNextPage; page++ {
		body, err := s.fetchCatalogListPage(ctx, page)
		if err != nil {
			return err
		}
		var list catalogListResponse
		if err := json.Unmarshal(body, &list); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidResponse, err)
		}
		for _, node := range list.Nodes {
			mint, err := solanaMintFromDeployments(node.Deployments)
			if err != nil {
				continue
			}
			asset := CatalogAsset{
				Symbol:     strings.TrimSpace(node.Symbol),
				Name:       strings.TrimSpace(node.Name),
				SolanaMint: mint,
			}
			byMint[mint] = asset
			allAssets = append(allAssets, asset)
		}
		hasNextPage = list.Page.HasNextPage
	}

	s.mintIndex.byMint = byMint
	s.mintIndex.allAssets = allAssets
	s.mintIndex.loaded = true
	return nil
}

func (f *fakeCatalogSearcher) LookupByMint(ctx context.Context, mint string) (CatalogAsset, bool, error) {
	_ = ctx
	mint = strings.TrimSpace(mint)
	if mint == "" {
		return CatalogAsset{}, false, nil
	}

	f.mu.Lock()
	assets := append([]CatalogAsset(nil), f.assets...)
	f.mu.Unlock()

	for _, asset := range assets {
		if strings.TrimSpace(asset.SolanaMint) == mint {
			return asset, true, nil
		}
	}
	return CatalogAsset{}, false, nil
}

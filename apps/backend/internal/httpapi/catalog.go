package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

// CatalogHandlers serves catalog search HTTP routes.
type CatalogHandlers struct {
	Store   *postgres.Store
	Privy   privy.Client
	Catalog xstocks.CatalogSearcher
	Price   jupiter.PriceClient
	// KeyGuard throttles wrong agent keys. Nil disables throttling.
	KeyGuard *AgentKeyGuard
}

type catalogAssetResponse struct {
	Symbol     string `json:"symbol"`
	Name       string `json:"name"`
	SolanaMint string `json:"solanaMint"`
	Routable   bool   `json:"routable"`
	assetCatalogJSONFields
}

type searchAssetsResponse struct {
	Assets  []catalogAssetResponse `json:"assets"`
	HasMore bool                   `json:"hasMore"`
}

// SearchAssetsHandler handles GET /v1/groups/{id}/assets?query=.
func (h *CatalogHandlers) SearchAssetsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/groups/{id}/assets")

	groupID := strings.TrimSpace(r.PathValue("id"))
	if groupID == "" {
		logJSONError(ctx, log, "missing_group_id", w, http.StatusNotFound, "group not found")
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("query"))
	limit := parseCatalogLimit(r.URL.Query().Get("limit"))
	offset := parseCatalogOffset(r.URL.Query().Get("offset"))

	agentKey := strings.TrimSpace(r.Header.Get(agentKeyHeader))
	if agentKey != "" {
		if over, wait := h.KeyGuard.blocked(r, groupID, agentKey); over {
			writeAgentKeyThrottled(ctx, log, w, wait, groupID)
			return
		}
		if _, err := resolveAgentForGroup(ctx, h.Store, groupID, agentKey); err != nil {
			h.KeyGuard.recordFailure(r, groupID, err)
			writeAgentAuthError(ctx, log, w, err, groupID)
			return
		}
	} else {
		token, ok := bearerToken(r)
		if !ok {
			logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
			return
		}
		if _, err := h.authorizeGroupMember(ctx, token, groupID); err != nil {
			writeCatalogError(ctx, log, w, err, "group_id", groupID, "query", query)
			return
		}
	}

	kind, kindErr := parseCatalogKindQuery(r.URL.Query().Get("kind"))
	if kindErr != nil {
		logJSONError(ctx, log, "invalid_catalog_kind", w, http.StatusBadRequest, "invalid catalog kind", "group_id", groupID, "query", query)
		return
	}

	page, err := catalogSearch(ctx, h.Catalog, query, kind, limit, offset)
	if err != nil {
		if errors.Is(err, xstocks.ErrInvalidResponse) {
			logJSONError(ctx, log, "invalid_catalog_query", w, http.StatusBadRequest, "invalid catalog query", "group_id", groupID, "query", query)
			return
		}
		logJSONError(ctx, log, "catalog_search_failed", w, http.StatusInternalServerError, "internal server error", "group_id", groupID, "query", query, "err", err.Error())
		return
	}

	prices := fetchCatalogPrices(ctx, h.Price, page.Assets)
	resp := searchAssetsResponse{
		Assets:  make([]catalogAssetResponse, 0, len(page.Assets)),
		HasMore: page.HasMore,
	}
	for _, asset := range page.Assets {
		n := asset.Normalize()
		row := catalogAssetResponse{
			Symbol:     n.Symbol,
			Name:       n.Name,
			SolanaMint: n.SolanaMint,
			Routable:   n.Routable,
		}
		var pricePtr *jupiter.TokenPrice
		if price, ok := prices[n.SolanaMint]; ok {
			priceCopy := price
			pricePtr = &priceCopy
		}
		row.assetCatalogJSONFields = catalogJSONFields(n, pricePtr, variantCountFor(ctx, h.Catalog, n))
		resp.Assets = append(resp.Assets, row)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
	logJSONOK(ctx, log, "ok", "group_id", groupID, "query", query, "limit", limit, "offset", offset, "result_count", len(resp.Assets), "has_more", resp.HasMore)
}

func fetchCatalogPrices(ctx context.Context, price jupiter.PriceClient, assets []xstocks.CatalogAsset) map[string]jupiter.TokenPrice {
	if price == nil || len(assets) == 0 {
		return nil
	}
	mints := make([]string, 0, len(assets))
	for _, asset := range assets {
		if mint := strings.TrimSpace(asset.SolanaMint); mint != "" {
			mints = append(mints, mint)
		}
	}
	prices, err := price.Prices(ctx, mints)
	if err != nil {
		return nil
	}
	return prices
}

func parseCatalogLimit(raw string) int {
	const defaultLimit = 25
	const maxLimit = 100
	limit, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || limit <= 0 {
		return defaultLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}

func parseCatalogOffset(raw string) int {
	offset, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || offset < 0 {
		return 0
	}
	return offset
}

func (h *CatalogHandlers) authorizeGroupMember(ctx context.Context, accessToken, groupID string) (string, error) {
	identity, err := h.Privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			return "", privy.ErrInvalidToken
		}
		return "", fmt.Errorf("verify session: %w", err)
	}

	user, found, err := h.Store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		return "", err
	}
	if !found {
		return "", app.ErrUserNotFound
	}

	group, found, err := h.Store.GetGroupByID(ctx, groupID)
	if err != nil {
		return "", err
	}
	if !found {
		return "", app.ErrGroupNotFound
	}
	_ = group

	member, err := h.Store.IsGroupMember(ctx, groupID, user.ID)
	if err != nil {
		return "", err
	}
	if !member {
		return "", app.ErrNotGroupMember
	}

	treasury, found, err := h.Store.GetTreasuryByGroupID(ctx, groupID)
	if err != nil {
		return "", err
	}
	if !found {
		return "", app.ErrGroupNotFound
	}
	_ = treasury
	return user.ID, nil
}

func writeCatalogError(ctx context.Context, log *requestLog, w http.ResponseWriter, err error, attrs ...any) {
	switch {
	case errors.Is(err, privy.ErrInvalidToken):
		logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token", attrs...)
	case errors.Is(err, app.ErrUserNotFound):
		logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found", attrs...)
	case errors.Is(err, app.ErrGroupNotFound):
		logJSONError(ctx, log, "group_not_found", w, http.StatusNotFound, "group not found", attrs...)
	case errors.Is(err, app.ErrNotGroupMember):
		logJSONError(ctx, log, "not_group_member", w, http.StatusForbidden, "not a group member", attrs...)
	default:
		all := append(attrs, "err", err.Error())
		logJSONError(ctx, log, "internal_error", w, http.StatusInternalServerError, "internal server error", all...)
	}
}

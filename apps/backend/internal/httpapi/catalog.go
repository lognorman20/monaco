package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

// CatalogHandlers serves catalog search HTTP routes.
type CatalogHandlers struct {
	Store   *postgres.Store
	Privy   privy.Client
	Catalog xstocks.CatalogSearcher
}

type catalogAssetResponse struct {
	Symbol     string `json:"symbol"`
	Name       string `json:"name"`
	SolanaMint string `json:"solanaMint"`
}

type searchAssetsResponse struct {
	Assets []catalogAssetResponse `json:"assets"`
}

// SearchAssetsHandler handles GET /v1/groups/{id}/assets?query=.
func (h *CatalogHandlers) SearchAssetsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/groups/{id}/assets")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	groupID := strings.TrimSpace(r.PathValue("id"))
	if groupID == "" {
		logJSONError(ctx, log, "missing_group_id", w, http.StatusNotFound, "group not found")
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("query"))
	if query == "" {
		logJSONError(ctx, log, "missing_query", w, http.StatusBadRequest, "query is required", "group_id", groupID)
		return
	}

	if _, err := h.authorizeGroupMember(ctx, token, groupID); err != nil {
		writeCatalogError(ctx, log, w, err, "group_id", groupID, "query", query)
		return
	}

	assets, err := h.Catalog.Search(ctx, query)
	if err != nil {
		if errors.Is(err, xstocks.ErrInvalidResponse) {
			logJSONError(ctx, log, "invalid_catalog_query", w, http.StatusBadRequest, "invalid catalog query", "group_id", groupID, "query", query)
			return
		}
		logJSONError(ctx, log, "catalog_search_failed", w, http.StatusInternalServerError, "internal server error", "group_id", groupID, "query", query, "err", err.Error())
		return
	}

	resp := searchAssetsResponse{
		Assets: make([]catalogAssetResponse, 0, len(assets)),
	}
	for _, asset := range assets {
		resp.Assets = append(resp.Assets, catalogAssetResponse{
			Symbol:     asset.Symbol,
			Name:       asset.Name,
			SolanaMint: asset.SolanaMint,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
	logJSONOK(ctx, log, "ok", "group_id", groupID, "query", query, "result_count", len(resp.Assets))
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
	if !found || group.CreatorUserID != user.ID {
		return "", app.ErrNotGroupMember
	}

	treasury, found, err := h.Store.GetTreasuryByGroupID(ctx, groupID)
	if err != nil {
		return "", err
	}
	if !found {
		return "", app.ErrGroupNotFound
	}
	return treasury.SolanaAddress, nil
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

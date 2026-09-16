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
	token, ok := bearerToken(r)
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	groupID := strings.TrimSpace(r.PathValue("id"))
	if groupID == "" {
		writeJSONError(w, http.StatusNotFound, "group not found")
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("query"))
	if query == "" {
		writeJSONError(w, http.StatusBadRequest, "query is required")
		return
	}

	if _, err := h.authorizeGroupMember(r.Context(), token, groupID); err != nil {
		writeCatalogError(w, err)
		return
	}

	assets, err := h.Catalog.Search(r.Context(), query)
	if err != nil {
		if errors.Is(err, xstocks.ErrInvalidResponse) {
			writeJSONError(w, http.StatusBadRequest, "invalid catalog query")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
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

func writeCatalogError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, privy.ErrInvalidToken):
		writeJSONError(w, http.StatusUnauthorized, "invalid or expired access token")
	case errors.Is(err, app.ErrUserNotFound):
		writeJSONError(w, http.StatusNotFound, "user not found")
	case errors.Is(err, app.ErrGroupNotFound):
		writeJSONError(w, http.StatusNotFound, "group not found")
	case errors.Is(err, app.ErrNotGroupMember):
		writeJSONError(w, http.StatusForbidden, "not a group member")
	default:
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
	}
}

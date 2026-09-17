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
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

// QuoteHandlers serves buy quote HTTP routes.
type QuoteHandlers struct {
	Store *postgres.Store
	Privy privy.Client
	Buy   *app.BuyService
}

type quoteRequest struct {
	Symbol string `json:"symbol"`
	USDC   int64  `json:"usdc"`
}

type quoteResponse struct {
	Symbol     string `json:"symbol"`
	USDCMicros string `json:"usdcMicros"`
	Routable   bool   `json:"routable"`
}

// ProposalQuoteInput is the quote gate input shared with proposal create (M4-T13).
type ProposalQuoteInput struct {
	GroupID    string
	UserID     string
	Symbol     string
	USDCAmount int64
}

// ProposalQuoteOK reports whether Jupiter can quote USDC to the symbol output mint.
// T13 should call this before inserting an open proposal.
func ProposalQuoteOK(ctx context.Context, buy *app.BuyService, in ProposalQuoteInput) (bool, error) {
	if buy == nil {
		return false, fmt.Errorf("buy service is required")
	}
	if strings.TrimSpace(in.Symbol) == "" {
		return false, fmt.Errorf("symbol is required")
	}
	if in.USDCAmount <= 0 {
		return false, fmt.Errorf("usdc must be positive")
	}

	_, err := buy.StartBuy(ctx, app.StartBuyRequest{
		GroupID:    in.GroupID,
		UserID:     in.UserID,
		Symbol:     in.Symbol,
		USDCAmount: in.USDCAmount,
	})
	if err != nil {
		if errors.Is(err, app.ErrQuoteNotRoutable) || errors.Is(err, xstocks.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// QuoteHandler handles POST /v1/groups/{id}/quotes.
func (h *QuoteHandlers) QuoteHandler(w http.ResponseWriter, r *http.Request) {
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

	var req quoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Symbol) == "" {
		writeJSONError(w, http.StatusBadRequest, "symbol is required")
		return
	}
	if req.USDC <= 0 {
		writeJSONError(w, http.StatusBadRequest, "usdc must be positive")
		return
	}

	userID, err := h.authorizeGroupMember(r.Context(), token, groupID)
	if err != nil {
		writeQuoteError(w, err)
		return
	}

	routable, err := ProposalQuoteOK(r.Context(), h.Buy, ProposalQuoteInput{
		GroupID:    groupID,
		UserID:     userID,
		Symbol:     req.Symbol,
		USDCAmount: req.USDC,
	})
	if err != nil {
		if errors.Is(err, xstocks.ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, "symbol not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(quoteResponse{
		Symbol:     req.Symbol,
		USDCMicros: strconv.FormatInt(req.USDC, 10),
		Routable:   routable,
	})
}

func (h *QuoteHandlers) authorizeGroupMember(ctx context.Context, accessToken, groupID string) (string, error) {
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
	if group.CreatorUserID != user.ID {
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

func writeQuoteError(w http.ResponseWriter, err error) {
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

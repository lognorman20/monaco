package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

// QuoteHandlers serves buy quote HTTP routes.
type QuoteHandlers struct {
	Store      *postgres.Store
	Privy      privy.Client
	Buy        *app.BuyService
	Governance *app.GovernanceService
	Price      jupiter.PriceClient
}

type quoteRequest struct {
	Kind              string `json:"kind"`
	Symbol            string `json:"symbol"`
	USDC              int64  `json:"usdc"`
	TokenAmount       int64  `json:"tokenAmount"`
	SelectBestVariant bool   `json:"selectBestVariant"`
}

type quoteProviderResponse struct {
	Issuer     string `json:"issuer"`
	IssuerName string `json:"issuerName"`
}

type quoteComparisonCandidateResponse struct {
	Symbol             string `json:"symbol"`
	Issuer             string `json:"issuer,omitempty"`
	IssuerName         string `json:"issuerName,omitempty"`
	ExposureUsdcMicros string `json:"exposureUsdcMicros,omitempty"`
	CostRatioBps       int64  `json:"costRatioBps,omitempty"`
	DeltaBps           int64  `json:"deltaBps,omitempty"`
	Reason             string `json:"reason,omitempty"`
}

type quoteComparisonResponse struct {
	Basis      string                             `json:"basis"`
	Candidates []quoteComparisonCandidateResponse `json:"candidates,omitempty"`
}

type quoteResponse struct {
	Kind             string `json:"kind,omitempty"`
	Symbol           string `json:"symbol"`
	USDCMicros       string `json:"usdcMicros,omitempty"`
	TokenAmount      string `json:"tokenAmount,omitempty"`
	Routable         bool   `json:"routable"`
	Reason           string `json:"reason,omitempty"`
	OutputAmount     string `json:"outputAmount,omitempty"`
	OutputUsdcMicros string `json:"outputUsdcMicros,omitempty"`
	PriceUsdcMicros  string `json:"priceUsdcMicros,omitempty"`
	TokenDecimals    int    `json:"tokenDecimals,omitempty"`
	PremiumBps       *int   `json:"premiumBps,omitempty"`
	// AssetKind is stock or pre_ipo. Kind stays buy/sell.
	AssetKind          string                   `json:"assetKind,omitempty"`
	UiAmountMultiplier string                   `json:"uiAmountMultiplier,omitempty"`
	Provider           *quoteProviderResponse   `json:"provider,omitempty"`
	PriceComparison    *quoteComparisonResponse `json:"priceComparison,omitempty"`
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
	ctx := r.Context()
	log := newRequestLog(r, "POST /v1/groups/{id}/quotes")

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

	var req quoteRequest
	if !decodeJSONBody(ctx, log, w, r, &req, "group_id", groupID) {
		return
	}
	if strings.TrimSpace(req.Symbol) == "" {
		logJSONError(ctx, log, "missing_symbol", w, http.StatusBadRequest, "symbol is required", "group_id", groupID)
		return
	}
	kind := strings.TrimSpace(req.Kind)
	if kind == "" {
		kind = "buy"
	}
	if kind != "buy" && kind != "sell" {
		logJSONError(ctx, log, "invalid_kind", w, http.StatusBadRequest, "kind must be buy or sell", "group_id", groupID)
		return
	}

	userID, err := h.authorizeGroupMember(ctx, token, groupID)
	if err != nil {
		writeQuoteError(ctx, log, w, err, "group_id", groupID, "symbol", req.Symbol)
		return
	}

	if kind == "sell" {
		if req.TokenAmount <= 0 {
			logJSONError(ctx, log, "invalid_token_amount", w, http.StatusBadRequest, "tokenAmount must be positive", "group_id", groupID, "symbol", req.Symbol)
			return
		}
		if h.Governance == nil {
			logJSONError(ctx, log, "quote_check_failed", w, http.StatusInternalServerError, "internal server error", "group_id", groupID)
			return
		}
		quoted, err := h.Governance.QuoteProposal(ctx, app.QuoteProposalInput{
			GroupID:     groupID,
			UserID:      userID,
			Symbol:      req.Symbol,
			Kind:        app.ProposalKindSell,
			TokenAmount: req.TokenAmount,
		})
		if err != nil {
			if errors.Is(err, app.ErrExceedsTreasuryHolding) {
				logJSONError(ctx, log, "exceeds_treasury_holding", w, http.StatusBadRequest, "amount exceeds treasury holding", "group_id", groupID, "symbol", req.Symbol, "user_id", userID)
				return
			}
			if errors.Is(err, xstocks.ErrNotFound) {
				logJSONError(ctx, log, "symbol_not_found", w, http.StatusNotFound, "symbol not found", "group_id", groupID, "symbol", req.Symbol, "user_id", userID)
				return
			}
			if errors.Is(err, app.ErrQuoteNotRoutable) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(quoteResponse{
					Kind:        "sell",
					Symbol:      req.Symbol,
					TokenAmount: strconv.FormatInt(req.TokenAmount, 10),
					Routable:    false,
				})
				return
			}
			writeQuoteError(ctx, log, w, err, "group_id", groupID, "symbol", req.Symbol)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(quoteResponse{
			Kind:             "sell",
			Symbol:           req.Symbol,
			TokenAmount:      strconv.FormatInt(req.TokenAmount, 10),
			Routable:         quoted.Routable,
			OutputUsdcMicros: quoted.OutputUsdcMicros,
		})
		return
	}

	if req.USDC <= 0 {
		logJSONError(ctx, log, "invalid_usdc", w, http.StatusBadRequest, "usdc must be positive", "group_id", groupID, "symbol", req.Symbol)
		return
	}

	asset, assetErr := h.Buy.ResolveAsset(ctx, req.Symbol)
	if assetErr != nil {
		if errors.Is(assetErr, xstocks.ErrNotFound) {
			logJSONError(ctx, log, "symbol_not_found", w, http.StatusNotFound, "symbol not found", "group_id", groupID, "symbol", req.Symbol, "user_id", userID)
			return
		}
		logJSONError(ctx, log, "quote_check_failed", w, http.StatusInternalServerError, "internal server error", "group_id", groupID, "symbol", req.Symbol, "user_id", userID, "err", assetErr.Error())
		return
	}

	result, err := h.Buy.StartBuy(ctx, app.StartBuyRequest{
		GroupID:           groupID,
		UserID:            userID,
		Symbol:            req.Symbol,
		USDCAmount:        req.USDC,
		SelectBestVariant: req.SelectBestVariant,
	})
	if err != nil {
		if errors.Is(err, xstocks.ErrNotFound) {
			logJSONError(ctx, log, "symbol_not_found", w, http.StatusNotFound, "symbol not found", "group_id", groupID, "symbol", req.Symbol, "user_id", userID)
			return
		}
		if errors.Is(err, app.ErrIssuerPaused) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(quoteResponse{
				Symbol:     req.Symbol,
				USDCMicros: strconv.FormatInt(req.USDC, 10),
				Routable:   false,
				Reason:     "issuer_paused",
			})
			return
		}
		if errors.Is(err, app.ErrQuoteNotRoutable) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(quoteResponse{
				Symbol:     req.Symbol,
				USDCMicros: strconv.FormatInt(req.USDC, 10),
				Routable:   false,
			})
			logJSONOK(ctx, log, "quoted",
				"group_id", groupID,
				"user_id", userID,
				"symbol", req.Symbol,
				"usdc", req.USDC,
				"routable", false,
			)
			return
		}
		logJSONError(ctx, log, "quote_check_failed", w, http.StatusInternalServerError, "internal server error", "group_id", groupID, "symbol", req.Symbol, "user_id", userID, "err", err.Error())
		return
	}

	n := asset.Normalize()
	quoteSymbol := req.Symbol
	if strings.TrimSpace(result.Symbol) != "" {
		quoteSymbol = result.Symbol
		if picked, err := h.Buy.ResolveAsset(ctx, quoteSymbol); err == nil {
			n = picked.Normalize()
		}
	}
	resp := quoteResponse{
		Symbol:             quoteSymbol,
		USDCMicros:         strconv.FormatInt(req.USDC, 10),
		Routable:           result.Quote.Routable,
		OutputAmount:       strings.TrimSpace(result.Quote.OutAmount),
		TokenDecimals:      n.Decimals,
		AssetKind:          string(n.Kind),
		UiAmountMultiplier: uiAmountMultiplierForAsset(n),
	}
	if price, ok := quotePriceUsdcMicros(req.USDC, resp.OutputAmount, n.Decimals, n.UiAmountMultiplier, n.Kind); ok {
		resp.PriceUsdcMicros = strconv.FormatInt(price, 10)
	}
	if h.Price != nil {
		if prices, err := h.Price.Prices(ctx, []string{n.SolanaMint}); err == nil {
			if p, ok := prices[n.SolanaMint]; ok {
				fields := catalogJSONFields(n, &p, 0)
				resp.PremiumBps = fields.PremiumBps
			}
		}
	}
	if result.Provider != nil {
		resp.Provider = &quoteProviderResponse{
			Issuer:     result.Provider.Issuer,
			IssuerName: result.Provider.IssuerName,
		}
	}
	if result.PriceComparison != nil {
		resp.PriceComparison = quoteComparisonToJSON(*result.PriceComparison)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
	logJSONOK(ctx, log, "quoted",
		"group_id", groupID,
		"user_id", userID,
		"symbol", req.Symbol,
		"usdc", req.USDC,
		"routable", resp.Routable,
	)
}

func quotePriceUsdcMicros(usdcMicros int64, outputAmount string, decimals int, uiMultiplier *big.Rat, kind xstocks.AssetKind) (int64, bool) {
	outputAmount = strings.TrimSpace(outputAmount)
	if usdcMicros <= 0 || outputAmount == "" {
		return 0, false
	}
	outAtomics, err := strconv.ParseInt(outputAmount, 10, 64)
	if err != nil || outAtomics <= 0 {
		return 0, false
	}
	if decimals == 0 {
		decimals = jupiter.XStockDecimals
	}
	mult := uiMultiplier
	if mult == nil {
		mult = big.NewRat(1, 1)
	}
	if mult.Sign() <= 0 {
		return 0, false
	}
	scale := jupiter.AtomicScale(decimals)
	num := new(big.Int).SetInt64(usdcMicros)
	num.Mul(num, big.NewInt(scale))
	num.Mul(num, mult.Denom())
	den := new(big.Int).SetInt64(outAtomics)
	den.Mul(den, mult.Num())
	if den.Sign() <= 0 {
		return 0, false
	}
	price := new(big.Int).Quo(num, den)
	if !price.IsInt64() || price.Int64() <= 0 {
		return 0, false
	}
	return price.Int64(), true
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
	if group.IsFaker {
		return "", app.ErrFakerGroupReadOnly
	}
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

func writeQuoteError(ctx context.Context, log *requestLog, w http.ResponseWriter, err error, attrs ...any) {
	if writeFakerReadOnly(ctx, log, w, err, attrs...) {
		return
	}
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

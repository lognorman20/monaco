package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

// AssetSocialHandlers serves GET /v1/assets/{symbol}/social: what the caller's own
// cabals are doing with one stock.
type AssetSocialHandlers struct {
	Home *app.HomeService
}

type assetSocialHoldingResponse struct {
	GroupID     string `json:"groupId"`
	Name        string `json:"name"`
	Units       string `json:"units"`
	TokenAmount string `json:"tokenAmount"`
	MarkUsd     string `json:"markUsd"`
	ValueUsd    string `json:"valueUsd"`
	// CostBasisUsd is what the cabal paid for the units it still holds; DollarPnL is
	// ValueUsd minus that.
	CostBasisUsd string `json:"costBasisUsd"`
	DollarPnL    string `json:"dollarPnl"`
	// PercentReturn is null when there is no cost basis to measure a return against.
	PercentReturn  *string `json:"percentReturn"`
	MySliceUsd     string  `json:"mySliceUsd"`
	MySlicePercent string  `json:"mySlicePercent"`
	AfterHours     bool    `json:"afterHours"`
}

type assetSocialVoterResponse struct {
	UserID          string `json:"userId"`
	DisplayName     string `json:"displayName"`
	ProfilePhotoURL string `json:"profilePhotoUrl,omitempty"`
	Choice          string `json:"choice"`
}

type assetSocialProposalResponse struct {
	ID          string `json:"id"`
	GroupID     string `json:"groupId"`
	GroupName   string `json:"groupName"`
	Kind        string `json:"kind"`
	Status      string `json:"status"`
	UsdcMicros  int64  `json:"usdcMicros"`
	TokenAmount int64  `json:"tokenAmount"`
	Thesis      string `json:"thesis,omitempty"`
	Yes         int    `json:"yes"`
	No          int    `json:"no"`
	MemberCount int    `json:"memberCount"`
	// MyVote is absent when the viewer has not voted yet.
	MyVote string                     `json:"myVote,omitempty"`
	Voters []assetSocialVoterResponse `json:"voters"`
	// Timestamps are RFC3339 in UTC; the app converts for display.
	ExpiresAt string `json:"expiresAt"`
	CreatedAt string `json:"createdAt"`
}

type assetSocialActivityResponse struct {
	ID          string `json:"id"`
	GroupID     string `json:"groupId"`
	GroupName   string `json:"groupName"`
	Kind        string `json:"kind"`
	Action      string `json:"action,omitempty"`
	Status      string `json:"status,omitempty"`
	UsdcMicros  int64  `json:"usdcMicros"`
	TokenAmount int64  `json:"tokenAmount"`
	ActorName   string `json:"actorName,omitempty"`
	TxSignature string `json:"txSignature,omitempty"`
	CreatedAt   string `json:"createdAt"`
}

type assetSocialResponse struct {
	Symbol        string                        `json:"symbol"`
	Holdings      []assetSocialHoldingResponse  `json:"holdings"`
	OpenProposals []assetSocialProposalResponse `json:"openProposals"`
	Activity      []assetSocialActivityResponse `json:"activity"`
	HolderCount   int                           `json:"holderCount"`
	// UnvaluedGroups is how many of the viewer's cabals could not be priced on this
	// pass. The card says so rather than implying those cabals hold nothing.
	UnvaluedGroups int `json:"unvaluedGroups"`
}

// GetAssetSocialHandler handles GET /v1/assets/{symbol}/social.
func (h *AssetSocialHandlers) GetAssetSocialHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/assets/{symbol}/social")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	symbol := strings.TrimSpace(r.PathValue("symbol"))
	if symbol == "" {
		logJSONError(ctx, log, "missing_symbol", w, http.StatusNotFound, "asset not found")
		return
	}
	// The symbol is only ever a filter on the caller's own rows, but it still has to
	// look like a ticker: an unbounded string here would end up in logs and in a
	// LIKE-free equality test whose only protection is that it is parameterised.
	if !isPlausibleTicker(symbol) {
		logJSONError(ctx, log, "invalid_symbol", w, http.StatusBadRequest, "invalid symbol")
		return
	}

	result, err := h.Home.GetAssetSocial(ctx, token, symbol)
	if err != nil {
		switch {
		case errors.Is(err, privy.ErrInvalidToken):
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token")
		case errors.Is(err, app.ErrUserNotFound):
			logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found")
		default:
			logJSONError(ctx, log, "asset_social_failed", w, http.StatusInternalServerError, "internal server error", "symbol", symbol, "err", err.Error())
		}
		return
	}

	resp := assetSocialResponseFor(result)
	writeMarketJSON(ctx, log, w, http.StatusOK, resp, "ok",
		"symbol", symbol,
		"holding_count", len(resp.Holdings),
		"open_proposal_count", len(resp.OpenProposals),
		"activity_count", len(resp.Activity),
		"unvalued_groups", resp.UnvaluedGroups,
	)
}

// isPlausibleTicker keeps the path segment inside the shape a ticker can have.
// xStock symbols are letters and digits with an optional "x" suffix; nothing else
// reaches the store.
func isPlausibleTicker(symbol string) bool {
	if len(symbol) == 0 || len(symbol) > 16 {
		return false
	}
	for _, r := range symbol {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '-':
		default:
			return false
		}
	}
	return true
}

func assetSocialResponseFor(result app.AssetSocialResult) assetSocialResponse {
	resp := assetSocialResponse{
		Symbol:         result.Symbol,
		Holdings:       make([]assetSocialHoldingResponse, 0, len(result.Holdings)),
		OpenProposals:  make([]assetSocialProposalResponse, 0, len(result.OpenProposals)),
		Activity:       make([]assetSocialActivityResponse, 0, len(result.Activity)),
		HolderCount:    result.HolderCount,
		UnvaluedGroups: result.UnvaluedGroups,
	}
	for _, holding := range result.Holdings {
		resp.Holdings = append(resp.Holdings, assetSocialHoldingResponse{
			GroupID:        holding.GroupID,
			Name:           holding.Name,
			Units:          holding.Units,
			TokenAmount:    holding.TokenAmount,
			MarkUsd:        holding.MarkUsd,
			ValueUsd:       holding.ValueUsd,
			CostBasisUsd:   holding.CostBasisUsd,
			DollarPnL:      holding.DollarPnL,
			PercentReturn:  holding.PercentReturn,
			MySliceUsd:     holding.MySliceUsd,
			MySlicePercent: holding.MySlicePercent,
			AfterHours:     holding.AfterHours,
		})
	}
	for _, proposal := range result.OpenProposals {
		voters := make([]assetSocialVoterResponse, 0, len(proposal.Voters))
		for _, voter := range proposal.Voters {
			voters = append(voters, assetSocialVoterResponse{
				UserID:          voter.UserID,
				DisplayName:     voter.DisplayName,
				ProfilePhotoURL: voter.ProfilePhotoURL,
				Choice:          voter.Choice,
			})
		}
		resp.OpenProposals = append(resp.OpenProposals, assetSocialProposalResponse{
			ID:          proposal.ID,
			GroupID:     proposal.GroupID,
			GroupName:   proposal.GroupName,
			Kind:        proposal.Kind,
			Status:      proposal.Status,
			UsdcMicros:  proposal.UsdcMicros,
			TokenAmount: proposal.TokenAmount,
			Thesis:      proposal.Thesis,
			Yes:         proposal.Yes,
			No:          proposal.No,
			MemberCount: proposal.MemberCount,
			MyVote:      proposal.MyVote,
			Voters:      voters,
			ExpiresAt:   proposal.ExpiresAt.UTC().Format(time.RFC3339),
			CreatedAt:   proposal.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	for _, item := range result.Activity {
		resp.Activity = append(resp.Activity, assetSocialActivityResponse{
			ID:          item.ID,
			GroupID:     item.GroupID,
			GroupName:   item.GroupName,
			Kind:        item.Kind,
			Action:      item.Action,
			Status:      item.Status,
			UsdcMicros:  item.UsdcMicros,
			TokenAmount: item.TokenAmount,
			ActorName:   item.ActorName,
			TxSignature: item.TxSignature,
			CreatedAt:   item.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return resp
}

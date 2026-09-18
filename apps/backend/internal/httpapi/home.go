package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

// HomeHandlers serves GET /v1/home.
type HomeHandlers struct {
	Home *app.HomeService
}

type homeGroupBoardRowResponse struct {
	GroupID       string  `json:"groupId"`
	Name          string  `json:"name"`
	PotValueUsd   string  `json:"potValueUsd"`
	PercentReturn *string `json:"percentReturn"`
	DollarPnL     string  `json:"dollarPnl"`
	IsJoined      bool    `json:"isJoined"`
}

type homePeopleBoardRowResponse struct {
	UserID        string  `json:"userId"`
	DisplayName   string  `json:"displayName"`
	PercentReturn *string `json:"percentReturn"`
	DollarPnL     string  `json:"dollarPnl"`
}

type homeResponse struct {
	Groups []homeGroupBoardRowResponse `json:"groups"`
	People []homePeopleBoardRowResponse `json:"people"`
}

// HomeHandler handles GET /v1/home.
func (h *HomeHandlers) HomeHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/home")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	result, err := h.Home.GetHome(ctx, token)
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token")
			return
		}
		if errors.Is(err, app.ErrUserNotFound) {
			logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found")
			return
		}
		logJSONError(ctx, log, "get_home_failed", w, http.StatusInternalServerError, "internal server error", "err", err.Error())
		return
	}

	groups := make([]homeGroupBoardRowResponse, 0, len(result.Groups))
	for _, row := range result.Groups {
		groups = append(groups, homeGroupBoardRowResponse{
			GroupID:       row.GroupID,
			Name:          row.Name,
			PotValueUsd:   row.PotValueUsd,
			PercentReturn: row.PercentReturn,
			DollarPnL:     row.DollarPnL,
			IsJoined:      row.IsJoined,
		})
	}
	people := make([]homePeopleBoardRowResponse, 0, len(result.People))
	for _, row := range result.People {
		people = append(people, homePeopleBoardRowResponse{
			UserID:        row.UserID,
			DisplayName:   row.DisplayName,
			PercentReturn: row.PercentReturn,
			DollarPnL:     row.DollarPnL,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(homeResponse{
		Groups: groups,
		People: people,
	})
	logJSONOK(ctx, log, "ok", "group_count", len(groups), "people_count", len(people))
}

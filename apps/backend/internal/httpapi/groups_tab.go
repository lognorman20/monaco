package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

// GroupsTabHandlers serves the Groups tab discovery routes:
//
//	GET /v1/groups/search?q=&limit=&cursor=
//	GET /v1/groups/leaderboard?limit=
//	GET /v1/groups/pnl-history?range=          (every cabal the viewer belongs to)
//	GET /v1/groups/{id}/pnl-history?range=
type GroupsTabHandlers struct {
	GroupsTab *app.GroupsTabService
}

type groupDiscoveryRowResponse struct {
	GroupID       string  `json:"groupId"`
	Name          string  `json:"name"`
	MemberCount   int     `json:"memberCount"`
	PotValueUsd   string  `json:"potValueUsd"`
	PercentReturn *string `json:"percentReturn"`
	DollarPnL     string  `json:"dollarPnl"`
	IsJoined      bool    `json:"isJoined"`
	JoinMode      string  `json:"joinMode"`
	PictureURL    *string `json:"pictureUrl"`
}

type groupSearchResponse struct {
	Groups     []groupDiscoveryRowResponse `json:"groups"`
	NextCursor *string                     `json:"nextCursor"`
}

type groupLeaderboardRowResponse struct {
	Rank int `json:"rank"`
	groupDiscoveryRowResponse
}

type groupLeaderboardResponse struct {
	Groups []groupLeaderboardRowResponse `json:"groups"`
}

type groupPnLPointResponse struct {
	At          string `json:"at"`
	PotValueUsd string `json:"potValueUsd"`
	NetInUsd    string `json:"netInUsd"`
	DollarPnL   string `json:"dollarPnl"`
}

type groupPnLSeriesResponse struct {
	GroupID string                  `json:"groupId"`
	Name    string                  `json:"name"`
	Range   string                  `json:"range"`
	Points  []groupPnLPointResponse `json:"points"`
}

type myGroupsPnLResponse struct {
	Range  string                   `json:"range"`
	Series []groupPnLSeriesResponse `json:"series"`
}

// parseGroupsTabLimit reads ?limit=. Absent means the default; a non-integer or
// value below 1 is a client error; values above the max are capped.
func parseGroupsTabLimit(r *http.Request) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return app.GroupsTabDefaultLimit, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 1 {
		return 0, false
	}
	return app.ClampGroupsTabLimit(parsed), true
}

func discoveryRowResponse(row app.GroupDiscoveryRow) groupDiscoveryRowResponse {
	return groupDiscoveryRowResponse{
		GroupID:       row.GroupID,
		Name:          row.Name,
		MemberCount:   row.MemberCount,
		PotValueUsd:   row.PotValueUsd,
		PercentReturn: row.PercentReturn,
		DollarPnL:     row.DollarPnL,
		IsJoined:      row.IsJoined,
		JoinMode:      string(row.JoinMode),
		PictureURL:    optionalString(row.PictureURL),
	}
}

func seriesResponse(series app.GroupPnLSeries) groupPnLSeriesResponse {
	points := make([]groupPnLPointResponse, 0, len(series.Points))
	for _, p := range series.Points {
		points = append(points, groupPnLPointResponse{
			At:          p.At.UTC().Format(time.RFC3339),
			PotValueUsd: p.PotValueUsd(),
			NetInUsd:    p.NetInUsd(),
			DollarPnL:   p.DollarPnL(),
		})
	}
	return groupPnLSeriesResponse{
		GroupID: series.GroupID,
		Name:    series.Name,
		Range:   string(series.Range),
		Points:  points,
	}
}

func writeJSONOK(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(payload)
}

// SearchGroupsHandler handles GET /v1/groups/search.
func (h *GroupsTabHandlers) SearchGroupsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/groups/search")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	limit, ok := parseGroupsTabLimit(r)
	if !ok {
		logJSONError(ctx, log, "invalid_limit", w, http.StatusBadRequest, "limit must be a positive integer")
		return
	}
	query := r.URL.Query().Get("q")
	cursor := strings.TrimSpace(r.URL.Query().Get("cursor"))

	result, err := h.GroupsTab.SearchGroups(ctx, token, query, limit, cursor)
	if err != nil {
		switch {
		case errors.Is(err, privy.ErrInvalidToken):
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token")
		case errors.Is(err, app.ErrUserNotFound):
			logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found")
		case errors.Is(err, app.ErrInvalidGroupSearchQuery):
			logJSONError(ctx, log, "invalid_query", w, http.StatusBadRequest, "search needs 2 to 64 characters")
		case errors.Is(err, app.ErrInvalidGroupSearchCursor):
			logJSONError(ctx, log, "invalid_cursor", w, http.StatusBadRequest, "invalid cursor")
		default:
			logJSONError(ctx, log, "search_groups_failed", w, http.StatusInternalServerError, "internal server error", "err", err.Error())
		}
		return
	}

	groups := make([]groupDiscoveryRowResponse, 0, len(result.Groups))
	for _, row := range result.Groups {
		groups = append(groups, discoveryRowResponse(row))
	}
	writeJSONOK(w, groupSearchResponse{Groups: groups, NextCursor: result.NextCursor})
	logJSONOK(ctx, log, "ok", "group_count", len(groups), "has_more", result.NextCursor != nil)
}

// GroupLeaderboardHandler handles GET /v1/groups/leaderboard.
func (h *GroupsTabHandlers) GroupLeaderboardHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/groups/leaderboard")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	limit, ok := parseGroupsTabLimit(r)
	if !ok {
		logJSONError(ctx, log, "invalid_limit", w, http.StatusBadRequest, "limit must be a positive integer")
		return
	}

	rows, err := h.GroupsTab.Leaderboard(ctx, token, limit)
	if err != nil {
		switch {
		case errors.Is(err, privy.ErrInvalidToken):
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token")
		case errors.Is(err, app.ErrUserNotFound):
			logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found")
		default:
			logJSONError(ctx, log, "group_leaderboard_failed", w, http.StatusInternalServerError, "internal server error", "err", err.Error())
		}
		return
	}

	groups := make([]groupLeaderboardRowResponse, 0, len(rows))
	for _, row := range rows {
		groups = append(groups, groupLeaderboardRowResponse{
			Rank:                      row.Rank,
			groupDiscoveryRowResponse: discoveryRowResponse(row.GroupDiscoveryRow),
		})
	}
	writeJSONOK(w, groupLeaderboardResponse{Groups: groups})
	logJSONOK(ctx, log, "ok", "group_count", len(groups))
}

// MyGroupsPnLHistoryHandler handles GET /v1/groups/pnl-history: one series per
// cabal the viewer belongs to, in a single round trip for the Groups tab chart.
func (h *GroupsTabHandlers) MyGroupsPnLHistoryHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/groups/pnl-history")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	rng, err := app.ParseGroupPnLRange(r.URL.Query().Get("range"))
	if err != nil {
		logJSONError(ctx, log, "invalid_range", w, http.StatusBadRequest, "range must be one of 1D, 1W, 1M, 3M")
		return
	}

	series, err := h.GroupsTab.MyGroupsPnLHistory(ctx, token, rng)
	if err != nil {
		switch {
		case errors.Is(err, privy.ErrInvalidToken):
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token")
		case errors.Is(err, app.ErrUserNotFound):
			logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found")
		default:
			logJSONError(ctx, log, "my_groups_pnl_history_failed", w, http.StatusInternalServerError, "internal server error", "err", err.Error())
		}
		return
	}

	out := make([]groupPnLSeriesResponse, 0, len(series))
	for _, s := range series {
		out = append(out, seriesResponse(s))
	}
	writeJSONOK(w, myGroupsPnLResponse{Range: string(rng), Series: out})
	logJSONOK(ctx, log, "ok", "series_count", len(out))
}

// GroupPnLHistoryHandler handles GET /v1/groups/{id}/pnl-history. Any signed-in
// user may read a group's P&L history; the leaderboard already makes group-level
// pot and P&L public.
func (h *GroupsTabHandlers) GroupPnLHistoryHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/groups/{id}/pnl-history")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	groupID := strings.TrimSpace(r.PathValue("id"))
	rng, err := app.ParseGroupPnLRange(r.URL.Query().Get("range"))
	if err != nil {
		logJSONError(ctx, log, "invalid_range", w, http.StatusBadRequest, "range must be one of 1D, 1W, 1M, 3M", "group_id", groupID)
		return
	}

	series, err := h.GroupsTab.GroupPnLHistory(ctx, token, groupID, rng)
	if err != nil {
		switch {
		case errors.Is(err, privy.ErrInvalidToken):
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token", "group_id", groupID)
		case errors.Is(err, app.ErrUserNotFound):
			logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found", "group_id", groupID)
		case errors.Is(err, app.ErrGroupNotFound):
			logJSONError(ctx, log, "group_not_found", w, http.StatusNotFound, "group not found", "group_id", groupID)
		default:
			logJSONError(ctx, log, "group_pnl_history_failed", w, http.StatusInternalServerError, "internal server error", "group_id", groupID, "err", err.Error())
		}
		return
	}

	writeJSONOK(w, seriesResponse(series))
	logJSONOK(ctx, log, "ok", "group_id", groupID, "point_count", len(series.Points))
}

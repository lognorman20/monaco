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

// GroupsTabHandlers serves Groups tab discovery and P&L history routes.
type GroupsTabHandlers struct {
	Home *app.HomeService
}

type groupDiscoveryRowResponse struct {
	GroupID       string  `json:"groupId"`
	Name          string  `json:"name"`
	PotValueUsd   string  `json:"potValueUsd"`
	PercentReturn *string `json:"percentReturn"`
	DollarPnL     string  `json:"dollarPnl"`
	IsJoined      bool    `json:"isJoined"`
	JoinMode      string  `json:"joinMode"`
}

type groupLeaderboardRowResponse struct {
	Rank          int     `json:"rank"`
	GroupID       string  `json:"groupId"`
	Name          string  `json:"name"`
	PotValueUsd   string  `json:"potValueUsd"`
	PercentReturn *string `json:"percentReturn"`
	DollarPnL     string  `json:"dollarPnl"`
	IsJoined      bool    `json:"isJoined"`
}

type groupPnLHistoryPointResponse struct {
	At          string `json:"at"`
	PotValueUsd string `json:"potValueUsd"`
	DollarPnL   string `json:"dollarPnl"`
}

type groupPnLHistoryResponse struct {
	GroupID string                         `json:"groupId"`
	Name    string                         `json:"name"`
	Points  []groupPnLHistoryPointResponse `json:"points"`
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

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	limit, offset := parseGroupsTabPagination(r)

	result, err := h.Home.SearchGroups(ctx, token, query, limit, offset)
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token")
			return
		}
		if errors.Is(err, app.ErrUserNotFound) {
			logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found")
			return
		}
		logJSONError(ctx, log, "search_groups_failed", w, http.StatusInternalServerError, "internal server error", "err", err.Error())
		return
	}

	groups := make([]groupDiscoveryRowResponse, 0, len(result))
	for _, row := range result {
		groups = append(groups, groupDiscoveryRowResponse{
			GroupID:       row.GroupID,
			Name:          row.Name,
			PotValueUsd:   row.PotValueUsd,
			PercentReturn: row.PercentReturn,
			DollarPnL:     row.DollarPnL,
			IsJoined:      row.IsJoined,
			JoinMode:      row.JoinMode,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string][]groupDiscoveryRowResponse{"groups": groups})
	logJSONOK(ctx, log, "ok", "group_count", len(groups))
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

	limit, offset := parseGroupsTabPagination(r)

	result, err := h.Home.GetGroupLeaderboard(ctx, token, limit, offset)
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token")
			return
		}
		if errors.Is(err, app.ErrUserNotFound) {
			logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found")
			return
		}
		logJSONError(ctx, log, "group_leaderboard_failed", w, http.StatusInternalServerError, "internal server error", "err", err.Error())
		return
	}

	groups := make([]groupLeaderboardRowResponse, 0, len(result))
	for _, row := range result {
		groups = append(groups, groupLeaderboardRowResponse{
			Rank:          row.Rank,
			GroupID:       row.GroupID,
			Name:          row.Name,
			PotValueUsd:   row.PotValueUsd,
			PercentReturn: row.PercentReturn,
			DollarPnL:     row.DollarPnL,
			IsJoined:      row.IsJoined,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string][]groupLeaderboardRowResponse{"groups": groups})
	logJSONOK(ctx, log, "ok", "group_count", len(groups))
}

// GroupPnLHistoryHandler handles GET /v1/groups/{id}/pnl-history.
func (h *GroupsTabHandlers) GroupPnLHistoryHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/groups/{id}/pnl-history")

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

	days := 90
	if raw := strings.TrimSpace(r.URL.Query().Get("days")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			logJSONError(ctx, log, "invalid_days", w, http.StatusBadRequest, "days must be a positive integer")
			return
		}
		days = parsed
	}

	result, err := h.Home.GetGroupPnLHistory(ctx, token, groupID, days)
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token", "group_id", groupID)
			return
		}
		if errors.Is(err, app.ErrUserNotFound) {
			logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found", "group_id", groupID)
			return
		}
		if errors.Is(err, app.ErrGroupNotFound) {
			logJSONError(ctx, log, "group_not_found", w, http.StatusNotFound, "group not found", "group_id", groupID)
			return
		}
		logJSONError(ctx, log, "group_pnl_history_failed", w, http.StatusInternalServerError, "internal server error", "group_id", groupID, "err", err.Error())
		return
	}

	points := make([]groupPnLHistoryPointResponse, 0, len(result.Points))
	for _, point := range result.Points {
		points = append(points, groupPnLHistoryPointResponse{
			At:          point.At.Format(time.RFC3339),
			PotValueUsd: point.PotValueUsd,
			DollarPnL:   point.DollarPnL,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(groupPnLHistoryResponse{
		GroupID: result.GroupID,
		Name:    result.Name,
		Points:  points,
	})
	logJSONOK(ctx, log, "ok", "group_id", groupID, "point_count", len(points))
}

func parseGroupsTabPagination(r *http.Request) (limit, offset int) {
	limit = 25
	offset = 0
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 0 {
			offset = parsed
		}
	}
	return limit, offset
}

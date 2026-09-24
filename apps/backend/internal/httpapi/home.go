package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

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
	PictureURL    *string `json:"pictureUrl"`
}

type homePeopleBoardRowResponse struct {
	UserID          string  `json:"userId"`
	DisplayName     string  `json:"displayName"`
	ProfilePhotoURL *string `json:"profilePhotoUrl"`
	PercentReturn   *string `json:"percentReturn"`
	DollarPnL       string  `json:"dollarPnl"`
}

type homeResponse struct {
	Groups []homeGroupBoardRowResponse  `json:"groups"`
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
			PictureURL:    optionalString(row.PictureURL),
		})
	}
	people := make([]homePeopleBoardRowResponse, 0, len(result.People))
	for _, row := range result.People {
		people = append(people, homePeopleBoardRowResponse{
			UserID:          row.UserID,
			DisplayName:     row.DisplayName,
			ProfilePhotoURL: optionalString(row.ProfilePhotoURL),
			PercentReturn:   row.PercentReturn,
			DollarPnL:       row.DollarPnL,
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

// UserSharedGroupsHandler handles GET /v1/users/{id}/groups.
func (h *HomeHandlers) UserSharedGroupsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/users/{id}/groups")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	targetUserID := r.PathValue("id")
	if strings.TrimSpace(targetUserID) == "" {
		logJSONError(ctx, log, "missing_user_id", w, http.StatusNotFound, "user not found")
		return
	}

	result, err := h.Home.GetUserSharedGroups(ctx, token, targetUserID)
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token")
			return
		}
		if errors.Is(err, app.ErrUserNotFound) {
			logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found")
			return
		}
		logJSONError(ctx, log, "get_user_shared_groups_failed", w, http.StatusInternalServerError, "internal server error", "err", err.Error())
		return
	}

	groups := make([]homeGroupBoardRowResponse, 0, len(result))
	for _, row := range result {
		groups = append(groups, homeGroupBoardRowResponse{
			GroupID:       row.GroupID,
			Name:          row.Name,
			PotValueUsd:   row.PotValueUsd,
			PercentReturn: row.PercentReturn,
			DollarPnL:     row.DollarPnL,
			IsJoined:      row.IsJoined,
			PictureURL:    optionalString(row.PictureURL),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string][]homeGroupBoardRowResponse{"groups": groups})
	logJSONOK(ctx, log, "ok", "group_count", len(groups))
}

type homeMyGroupRowResponse struct {
	GroupID       string  `json:"groupId"`
	Name          string  `json:"name"`
	EquityUsd     string  `json:"equityUsd"`
	SlicePercent  string  `json:"slicePercent"`
	DollarPnL     string  `json:"dollarPnl"`
	PercentReturn *string `json:"percentReturn"`
	PictureURL    *string `json:"pictureUrl"`
}

// Timestamps on the home DTOs are strings formatted as UTC RFC 3339, like every other
// route. A time.Time field would be encoded with nanoseconds and the value's own offset.
type homePnLSeriesPointResponse struct {
	TS        string `json:"ts"`
	EquityUsd string `json:"equityUsd"`
	DollarPnl string `json:"dollarPnl"`
}

type homeLeaderboardSectionResponse struct {
	Range  string                       `json:"range"`
	People []homePeopleBoardRowResponse `json:"people"`
}

type homeMissedProposalRowResponse struct {
	GroupID    string `json:"groupId"`
	GroupName  string `json:"groupName"`
	ProposalID string `json:"proposalId"`
	Symbol     string `json:"symbol"`
	Status     string `json:"status"`
	CreatedAt  string `json:"createdAt"`
	ExpiresAt  string `json:"expiresAt"`
}

type homeDashboardResponse struct {
	NetWorthUsd           string                          `json:"netWorthUsd"`
	NetWorthDollarPnl     string                          `json:"netWorthDollarPnl"`
	NetWorthPercentReturn *string                         `json:"netWorthPercentReturn"`
	MyGroups              []homeMyGroupRowResponse        `json:"myGroups"`
	PnlSeries1H           []homePnLSeriesPointResponse    `json:"pnlSeries1H"`
	Leaderboard           homeLeaderboardSectionResponse  `json:"leaderboard"`
	MissedProposals       []homeMissedProposalRowResponse `json:"missedProposals"`
}

type homePnLSeriesResponse struct {
	Points []homePnLSeriesPointResponse `json:"points"`
}

type homeMissedProposalsResponse struct {
	Proposals []homeMissedProposalRowResponse `json:"proposals"`
}

// HomeDashboardHandler handles GET /v1/home/dashboard.
func (h *HomeHandlers) HomeDashboardHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/home/dashboard")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	leaderboardRange, err := app.ParseHomeLeaderboardRange(r.URL.Query().Get("leaderboardRange"))
	if err != nil {
		logJSONError(ctx, log, "invalid_range", w, http.StatusBadRequest, "invalid leaderboardRange")
		return
	}

	result, err := h.Home.GetHomeDashboard(ctx, token, leaderboardRange)
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token")
			return
		}
		if errors.Is(err, app.ErrUserNotFound) {
			logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found")
			return
		}
		logJSONError(ctx, log, "get_home_dashboard_failed", w, http.StatusInternalServerError, "internal server error", "err", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(mapHomeDashboardResponse(result))
	logJSONOK(ctx, log, "ok", "my_groups", len(result.MyGroups), "missed_proposals", len(result.MissedProposals))
}

// HomePnLSeriesHandler handles GET /v1/home/pnl-series.
func (h *HomeHandlers) HomePnLSeriesHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/home/pnl-series")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	seriesRange, err := app.ParseHomeLeaderboardRange(r.URL.Query().Get("range"))
	if err != nil {
		logJSONError(ctx, log, "invalid_range", w, http.StatusBadRequest, "invalid range")
		return
	}
	if seriesRange == app.HomeLeaderboardRangeALL {
		seriesRange = app.HomeLeaderboardRange1H
	}

	points, err := h.Home.GetHomePnLSeries(ctx, token, seriesRange)
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token")
			return
		}
		if errors.Is(err, app.ErrUserNotFound) {
			logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found")
			return
		}
		logJSONError(ctx, log, "get_home_pnl_series_failed", w, http.StatusInternalServerError, "internal server error", "err", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(homePnLSeriesResponse{Points: mapHomePnLSeriesPoints(points)})
	logJSONOK(ctx, log, "ok", "point_count", len(points))
}

// HomeMissedProposalsHandler handles GET /v1/home/missed-proposals.
func (h *HomeHandlers) HomeMissedProposalsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/home/missed-proposals")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	proposals, err := h.Home.GetHomeMissedProposals(ctx, token)
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token")
			return
		}
		if errors.Is(err, app.ErrUserNotFound) {
			logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found")
			return
		}
		logJSONError(ctx, log, "get_home_missed_proposals_failed", w, http.StatusInternalServerError, "internal server error", "err", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(homeMissedProposalsResponse{Proposals: mapHomeMissedProposals(proposals)})
	logJSONOK(ctx, log, "ok", "proposal_count", len(proposals))
}

func mapHomeDashboardResponse(result app.HomeDashboardResult) homeDashboardResponse {
	myGroups := make([]homeMyGroupRowResponse, 0, len(result.MyGroups))
	for _, row := range result.MyGroups {
		myGroups = append(myGroups, homeMyGroupRowResponse{
			GroupID:       row.GroupID,
			Name:          row.Name,
			EquityUsd:     row.EquityUsd,
			SlicePercent:  row.SlicePercent,
			DollarPnL:     row.DollarPnL,
			PercentReturn: row.PercentReturn,
			PictureURL:    optionalString(row.PictureURL),
		})
	}
	people := make([]homePeopleBoardRowResponse, 0, len(result.Leaderboard.People))
	for _, row := range result.Leaderboard.People {
		people = append(people, homePeopleBoardRowResponse{
			UserID:          row.UserID,
			DisplayName:     row.DisplayName,
			ProfilePhotoURL: optionalString(row.ProfilePhotoURL),
			PercentReturn:   row.PercentReturn,
			DollarPnL:       row.DollarPnL,
		})
	}
	return homeDashboardResponse{
		NetWorthUsd:           result.NetWorthUsd,
		NetWorthDollarPnl:     result.NetWorthDollarPnL,
		NetWorthPercentReturn: result.NetWorthPercentReturn,
		MyGroups:              myGroups,
		PnlSeries1H:           mapHomePnLSeriesPoints(result.PnlSeries1H),
		Leaderboard: homeLeaderboardSectionResponse{
			Range:  string(result.Leaderboard.Range),
			People: people,
		},
		MissedProposals: mapHomeMissedProposals(result.MissedProposals),
	}
}

func mapHomePnLSeriesPoints(points []app.HomePnLSeriesPoint) []homePnLSeriesPointResponse {
	out := make([]homePnLSeriesPointResponse, 0, len(points))
	for _, point := range points {
		out = append(out, homePnLSeriesPointResponse{
			TS:        point.TS.UTC().Format(time.RFC3339),
			EquityUsd: point.EquityUsd,
			DollarPnl: point.DollarPnL,
		})
	}
	return out
}

func mapHomeMissedProposals(rows []app.HomeMissedProposalRow) []homeMissedProposalRowResponse {
	out := make([]homeMissedProposalRowResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, homeMissedProposalRowResponse{
			GroupID:    row.GroupID,
			GroupName:  row.GroupName,
			ProposalID: row.ProposalID,
			Symbol:     row.Symbol,
			Status:     row.Status,
			CreatedAt:  row.CreatedAt.UTC().Format(time.RFC3339),
			ExpiresAt:  row.ExpiresAt.UTC().Format(time.RFC3339),
		})
	}
	return out
}

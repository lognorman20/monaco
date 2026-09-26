package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

// MatchupHandlers serves weekly matchups:
//
//	GET  /v1/groups/{id}/matchup
//	GET  /v1/home/matchups
//	GET  /v1/matchups/table?limit=
//	POST /v1/groups/{id}/matchups/challenge
//	POST /v1/groups/{id}/matchups/challenges/{challengeId}/accept
type MatchupHandlers struct {
	Matchups *app.MatchupService
}

// maxMatchupChallengeRequestBytes caps the challenge body, which carries one group id.
const maxMatchupChallengeRequestBytes = 4 << 10

type matchupFaceResponse struct {
	GroupID     string  `json:"groupId"`
	Name        string  `json:"name"`
	PictureURL  *string `json:"pictureUrl"`
	MemberCount int     `json:"memberCount"`
}

type matchupSideResponse struct {
	matchupFaceResponse
	// Score is the week's return as a ratio string ("0.0123" is +1.23%); null on a bye.
	Score *string `json:"score"`
}

type matchupCurrentResponse struct {
	ID            string               `json:"id"`
	WeekStart     string               `json:"weekStart"`
	WeekEnd       string               `json:"weekEnd"`
	DaysLeft      int                  `json:"daysLeft"`
	A             matchupSideResponse  `json:"a"`
	B             *matchupSideResponse `json:"b"`
	Leading       *string              `json:"leading"`
	FromChallenge bool                 `json:"fromChallenge"`
}

type matchupRecordResponse struct {
	Wins   int `json:"wins"`
	Losses int `json:"losses"`
	Ties   int `json:"ties"`
}

type matchupPastResultResponse struct {
	ID            string               `json:"id"`
	WeekStart     string               `json:"weekStart"`
	Result        string               `json:"result"`
	Opponent      *matchupFaceResponse `json:"opponent"`
	Score         *string              `json:"score"`
	OpponentScore *string              `json:"opponentScore"`
	Winner        *string              `json:"winner"`
}

type matchupChallengeResponse struct {
	ID         string              `json:"id"`
	WeekStart  string              `json:"weekStart"`
	Direction  string              `json:"direction"`
	Status     string              `json:"status"`
	Opponent   matchupFaceResponse `json:"opponent"`
	CreatedAt  string              `json:"createdAt"`
	AcceptedAt *string             `json:"acceptedAt"`
}

type groupMatchupResponse struct {
	NextDrawAt   string                      `json:"nextDrawAt"`
	Current      *matchupCurrentResponse     `json:"current"`
	Record       matchupRecordResponse       `json:"record"`
	Recent       []matchupPastResultResponse `json:"recent"`
	Challenges   []matchupChallengeResponse  `json:"challenges"`
	CanChallenge bool                        `json:"canChallenge"`
}

type homeMatchupsResponse struct {
	WeekStart  string                   `json:"weekStart"`
	WeekEnd    string                   `json:"weekEnd"`
	DaysLeft   int                      `json:"daysLeft"`
	NextDrawAt string                   `json:"nextDrawAt"`
	Drawn      bool                     `json:"drawn"`
	HasCabals  bool                     `json:"hasCabals"`
	Matchups   []matchupCurrentResponse `json:"matchups"`
}

type matchupTableRowResponse struct {
	Rank       int     `json:"rank"`
	GroupID    string  `json:"groupId"`
	Name       string  `json:"name"`
	PictureURL *string `json:"pictureUrl"`
	Wins       int     `json:"wins"`
	Losses     int     `json:"losses"`
	Ties       int     `json:"ties"`
	// Points is the sum of weekly returns as a ratio string, the tiebreak after wins.
	Points string `json:"points"`
	// Streak is the current run ("W3", "L1", "T2"); null before a first result.
	Streak *string `json:"streak"`
	IsMine bool    `json:"isMine"`
}

type matchupTableResponse struct {
	ThroughWeek *string                   `json:"throughWeek"`
	Cabals      []matchupTableRowResponse `json:"cabals"`
}

type matchupChallengeRequest struct {
	GroupID string `json:"groupId"`
}

func matchupTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func matchupScoreString(score *float64) *string {
	if score == nil {
		return nil
	}
	formatted := strconv.FormatFloat(*score, 'f', -1, 64)
	return &formatted
}

func matchupFaceJSON(face app.MatchupFace) matchupFaceResponse {
	return matchupFaceResponse{
		GroupID:     face.GroupID,
		Name:        face.Name,
		PictureURL:  optionalString(face.PictureURL),
		MemberCount: face.MemberCount,
	}
}

func matchupCurrentJSON(c app.MatchupCurrent) matchupCurrentResponse {
	out := matchupCurrentResponse{
		ID:            c.ID,
		WeekStart:     matchupTime(c.WeekStart),
		WeekEnd:       matchupTime(c.WeekEnd),
		DaysLeft:      c.DaysLeft,
		A:             matchupSideResponse{matchupFaceResponse: matchupFaceJSON(c.A.MatchupFace), Score: matchupScoreString(c.A.Score)},
		FromChallenge: c.FromChallenge,
	}
	if c.B != nil {
		out.B = &matchupSideResponse{matchupFaceResponse: matchupFaceJSON(c.B.MatchupFace), Score: matchupScoreString(c.B.Score)}
	}
	if c.Leading != "" {
		leading := c.Leading
		out.Leading = &leading
	}
	return out
}

func matchupChallengeJSON(c app.MatchupChallenge) matchupChallengeResponse {
	out := matchupChallengeResponse{
		ID:        c.ID,
		WeekStart: matchupTime(c.WeekStart),
		Direction: c.Direction,
		Status:    c.Status,
		Opponent:  matchupFaceJSON(c.Opponent),
		CreatedAt: matchupTime(c.CreatedAt),
	}
	if c.AcceptedAt != nil {
		at := matchupTime(*c.AcceptedAt)
		out.AcceptedAt = &at
	}
	return out
}

// writeMatchupError maps the matchup service's errors onto responses.
func writeMatchupError(ctx context.Context, log *requestLog, w http.ResponseWriter, fallbackBranch string, err error, attrs ...any) {
	var conflict *app.MatchupConflictError
	switch {
	case errors.Is(err, privy.ErrInvalidToken):
		logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token", attrs...)
	case errors.Is(err, app.ErrUserNotFound):
		logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found", attrs...)
	case errors.Is(err, app.ErrGroupNotFound):
		logJSONError(ctx, log, "group_not_found", w, http.StatusNotFound, "group not found", attrs...)
	case errors.Is(err, app.ErrNotGroupMember):
		logJSONError(ctx, log, "not_group_member", w, http.StatusForbidden, "not a group member", attrs...)
	case errors.Is(err, app.ErrMatchupOpponentNotFound):
		logJSONError(ctx, log, "opponent_not_found", w, http.StatusNotFound, "cabal to challenge not found", attrs...)
	case errors.Is(err, app.ErrMatchupChallengeSelf):
		logJSONError(ctx, log, "challenge_self", w, http.StatusBadRequest, "a cabal cannot challenge itself", attrs...)
	case errors.Is(err, app.ErrMatchupChallengeNotFound):
		logJSONError(ctx, log, "challenge_not_found", w, http.StatusNotFound, "challenge not found", attrs...)
	case errors.As(err, &conflict):
		logJSONErrorWithReason(ctx, log, "challenge_conflict", w, http.StatusConflict, matchupConflictMessage(conflict.Reason), conflict.Reason, attrs...)
	default:
		logJSONError(ctx, log, fallbackBranch, w, http.StatusInternalServerError, "internal server error", append(attrs, "err", err.Error())...)
	}
}

func matchupConflictMessage(reason string) string {
	switch reason {
	case app.MatchupConflictAlreadyMatched:
		return "one of these cabals already has next week's opponent"
	case app.MatchupConflictIncomingChallenge:
		return "that cabal already challenged yours; accept it instead"
	case app.MatchupConflictTooManyChallenges:
		return "too many open challenges this week"
	case app.MatchupConflictChallengeClosed:
		return "this challenge is closed"
	default:
		return "challenge refused"
	}
}

// GroupMatchupHandler handles GET /v1/groups/{id}/matchup.
func (h *MatchupHandlers) GroupMatchupHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/groups/{id}/matchup")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	groupID := strings.TrimSpace(r.PathValue("id"))

	view, err := h.Matchups.GroupMatchup(ctx, token, groupID)
	if err != nil {
		writeMatchupError(ctx, log, w, "group_matchup_failed", err, "group_id", groupID)
		return
	}

	out := groupMatchupResponse{
		NextDrawAt:   matchupTime(view.NextDrawAt),
		Record:       matchupRecordResponse{Wins: view.Record.Wins, Losses: view.Record.Losses, Ties: view.Record.Ties},
		Recent:       make([]matchupPastResultResponse, 0, len(view.Recent)),
		Challenges:   make([]matchupChallengeResponse, 0, len(view.Challenges)),
		CanChallenge: view.CanChallenge,
	}
	if view.Current != nil {
		current := matchupCurrentJSON(*view.Current)
		out.Current = &current
	}
	for _, past := range view.Recent {
		row := matchupPastResultResponse{
			ID:            past.ID,
			WeekStart:     matchupTime(past.WeekStart),
			Result:        past.Outcome,
			Score:         matchupScoreString(past.Score),
			OpponentScore: matchupScoreString(past.OpponentScore),
			Winner:        optionalString(past.WinnerGroupID),
		}
		if past.Opponent != nil {
			face := matchupFaceJSON(*past.Opponent)
			row.Opponent = &face
		}
		out.Recent = append(out.Recent, row)
	}
	for _, c := range view.Challenges {
		out.Challenges = append(out.Challenges, matchupChallengeJSON(c))
	}
	writeJSONOK(w, out)
	logJSONOK(ctx, log, "ok", "group_id", groupID, "has_current", view.Current != nil, "challenges", len(out.Challenges))
}

// HomeMatchupsHandler handles GET /v1/home/matchups.
func (h *MatchupHandlers) HomeMatchupsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/home/matchups")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	view, err := h.Matchups.HomeMatchups(ctx, token)
	if err != nil {
		writeMatchupError(ctx, log, w, "home_matchups_failed", err)
		return
	}
	out := homeMatchupsResponse{
		WeekStart:  matchupTime(view.WeekStart),
		WeekEnd:    matchupTime(view.WeekEnd),
		DaysLeft:   view.DaysLeft,
		NextDrawAt: matchupTime(view.NextDrawAt),
		Drawn:      view.Drawn,
		HasCabals:  view.HasCabals,
		Matchups:   make([]matchupCurrentResponse, 0, len(view.Matchups)),
	}
	for _, m := range view.Matchups {
		out.Matchups = append(out.Matchups, matchupCurrentJSON(m))
	}
	writeJSONOK(w, out)
	logJSONOK(ctx, log, "ok", "matchup_count", len(out.Matchups), "drawn", view.Drawn)
}

// MatchupTableHandler handles GET /v1/matchups/table.
func (h *MatchupHandlers) MatchupTableHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/matchups/table")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	limit := app.MatchupTableDefaultLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			logJSONError(ctx, log, "invalid_limit", w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = app.ClampMatchupTableLimit(parsed)
	}

	view, err := h.Matchups.SeasonTable(ctx, token, limit)
	if err != nil {
		writeMatchupError(ctx, log, w, "matchup_table_failed", err)
		return
	}
	out := matchupTableResponse{Cabals: make([]matchupTableRowResponse, 0, len(view.Rows))}
	if !view.ThroughWeek.IsZero() {
		through := matchupTime(view.ThroughWeek)
		out.ThroughWeek = &through
	}
	for _, row := range view.Rows {
		points := strconv.FormatFloat(row.Record.Points, 'f', -1, 64)
		out.Cabals = append(out.Cabals, matchupTableRowResponse{
			Rank:       row.Rank,
			GroupID:    row.GroupID,
			Name:       row.Name,
			PictureURL: optionalString(row.PictureURL),
			Wins:       row.Record.Wins,
			Losses:     row.Record.Losses,
			Ties:       row.Record.Ties,
			Points:     points,
			Streak:     optionalString(row.Record.Streak),
			IsMine:     row.IsMine,
		})
	}
	writeJSONOK(w, out)
	logJSONOK(ctx, log, "ok", "cabal_count", len(out.Cabals))
}

// CreateMatchupChallengeHandler handles POST /v1/groups/{id}/matchups/challenge.
func (h *MatchupHandlers) CreateMatchupChallengeHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "POST /v1/groups/{id}/matchups/challenge")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	groupID := strings.TrimSpace(r.PathValue("id"))

	var req matchupChallengeRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxMatchupChallengeRequestBytes)
	if !decodeJSONBody(ctx, log, w, r, &req, "group_id", groupID) {
		return
	}
	opponentID := strings.TrimSpace(req.GroupID)
	if opponentID == "" {
		logJSONError(ctx, log, "missing_opponent", w, http.StatusBadRequest, "groupId is required", "group_id", groupID)
		return
	}

	challenge, created, err := h.Matchups.Challenge(ctx, token, groupID, opponentID)
	if err != nil {
		writeMatchupError(ctx, log, w, "create_matchup_challenge_failed", err, "group_id", groupID, "opponent_id", opponentID)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(matchupChallengeJSON(challenge))
	log.done(ctx, "ok", status, "group_id", groupID, "opponent_id", opponentID, "challenge_id", challenge.ID, "created", created)
}

// AcceptMatchupChallengeHandler handles POST /v1/groups/{id}/matchups/challenges/{challengeId}/accept.
func (h *MatchupHandlers) AcceptMatchupChallengeHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "POST /v1/groups/{id}/matchups/challenges/{challengeId}/accept")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	groupID := strings.TrimSpace(r.PathValue("id"))
	challengeID := strings.TrimSpace(r.PathValue("challengeId"))

	challenge, err := h.Matchups.AcceptChallenge(ctx, token, groupID, challengeID)
	if err != nil {
		writeMatchupError(ctx, log, w, "accept_matchup_challenge_failed", err, "group_id", groupID, "challenge_id", challengeID)
		return
	}
	writeJSONOK(w, matchupChallengeJSON(challenge))
	logJSONOK(ctx, log, "ok", "group_id", groupID, "challenge_id", challengeID)
}

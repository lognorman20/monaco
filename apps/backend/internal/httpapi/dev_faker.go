package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/config"
	"github.com/monaco/monaco/apps/backend/internal/faker"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// DevFakerHandlers serves POST /v1/dev/faker (#153): local-only demo/test seeding.
//
// Guards, in order: FAKER_ENABLED (else 404), loopback caller without proxy headers (403),
// local DATABASE_URL (403), bearer auth (401). mixed requires a group_id the caller created.
type DevFakerHandlers struct {
	Enabled     bool
	DatabaseURL string
	Store       *postgres.Store
	Privy       privy.Client
	Seeder      *faker.Seeder
}

type devFakerRequest struct {
	Profile string `json:"profile"`
	GroupID string `json:"group_id"`
}

type devFakerResponse struct {
	Profile string             `json:"profile"`
	Mixed   *faker.MixedResult `json:"mixed,omitempty"`
	Scale   *faker.ScaleResult `json:"scale,omitempty"`
}

// FakerHandler handles POST /v1/dev/faker.
func (h *DevFakerHandlers) FakerHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if h == nil || !h.Enabled {
		// Indistinguishable from an unknown route when disabled.
		http.NotFound(w, r)
		return
	}
	log := newRequestLog(r, "POST /v1/dev/faker")

	if !isLoopbackRequest(r) {
		logJSONError(ctx, log, "faker_not_loopback", w, http.StatusForbidden, "faker seeding is only allowed from localhost")
		return
	}
	if !config.IsLocalDatabaseURL(h.DatabaseURL) {
		logJSONError(ctx, log, "faker_non_local_db", w, http.StatusForbidden, "faker seeding requires a local DATABASE_URL")
		return
	}

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	userID, err := h.authorizeUser(ctx, token)
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token")
			return
		}
		if errors.Is(err, app.ErrUserNotFound) {
			logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found")
			return
		}
		logJSONError(ctx, log, "faker_auth_failed", w, http.StatusInternalServerError, "internal server error", "err", err.Error())
		return
	}

	var req devFakerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logJSONError(ctx, log, "invalid_body", w, http.StatusBadRequest, "invalid request body")
		return
	}
	profile := strings.ToLower(strings.TrimSpace(req.Profile))
	groupID := strings.TrimSpace(req.GroupID)
	switch profile {
	case "mixed", "all":
		if groupID == "" {
			logJSONError(ctx, log, "missing_group_id", w, http.StatusBadRequest, "group_id is required for profile "+profile)
			return
		}
		if status, code, msg := h.checkMixedTarget(ctx, userID, groupID); status != 0 {
			logJSONError(ctx, log, code, w, status, msg, "group_id", groupID, "user_id", userID)
			return
		}
	case "scale":
		groupID = ""
	default:
		logJSONError(ctx, log, "invalid_profile", w, http.StatusBadRequest, `profile must be "mixed", "scale", or "all"`)
		return
	}

	resp := devFakerResponse{Profile: profile}
	if profile == "mixed" || profile == "all" {
		mixed, err := h.Seeder.SeedMixed(ctx, groupID)
		if err != nil {
			logJSONError(ctx, log, "faker_mixed_failed", w, http.StatusInternalServerError, "internal server error", "group_id", groupID, "err", err.Error())
			return
		}
		resp.Mixed = &mixed
	}
	if profile == "scale" || profile == "all" {
		scale, err := h.Seeder.SeedScale(ctx)
		if err != nil {
			logJSONError(ctx, log, "faker_scale_failed", w, http.StatusInternalServerError, "internal server error", "err", err.Error())
			return
		}
		resp.Scale = &scale
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
	logJSONOK(ctx, log, "faker_seeded", "profile", profile, "group_id", groupID, "user_id", userID)
}

func (h *DevFakerHandlers) authorizeUser(ctx context.Context, token string) (string, error) {
	identity, err := h.Privy.VerifySession(ctx, privy.AccessToken(token))
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
	return user.ID, nil
}

// checkMixedTarget returns a non-zero HTTP status when the mixed target is not the caller's real club.
func (h *DevFakerHandlers) checkMixedTarget(ctx context.Context, userID, groupID string) (int, string, string) {
	if !uuidPattern.MatchString(groupID) {
		return http.StatusBadRequest, "invalid_group_id", "group_id must be a uuid"
	}
	group, found, err := h.Store.GetGroupByID(ctx, groupID)
	if err != nil {
		return http.StatusInternalServerError, "group_lookup_failed", "internal server error"
	}
	if !found {
		return http.StatusNotFound, "group_not_found", "group not found"
	}
	if group.IsFaker {
		return http.StatusBadRequest, "faker_group", "mixed profile requires a real group"
	}
	if group.CreatorUserID != userID {
		return http.StatusForbidden, "not_group_creator", "only the group creator can seed ghost members"
	}
	return 0, "", ""
}

// isLoopbackRequest accepts direct loopback connections only. Any proxy forwarding header is
// rejected so a tunnel or reverse proxy on localhost cannot expose the endpoint.
func isLoopbackRequest(r *http.Request) bool {
	for _, header := range []string{"X-Forwarded-For", "X-Real-Ip", "Forwarded"} {
		if strings.TrimSpace(r.Header.Get(header)) != "" {
			return false
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return config.IsLoopbackHost(host)
}

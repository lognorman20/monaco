package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/monaco/monaco/apps/backend/internal/auth"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
)

// maxPatchMeBodyBytes bounds PATCH /v1/me bodies. A 32-character name is at most
// 128 bytes of UTF-8; the rest is JSON framing and headroom.
const maxPatchMeBodyBytes = 4 << 10

// MeHandlers serves GET/PATCH /v1/me and POST /v1/me/profile-photo.
type MeHandlers struct {
	Sessions     *app.SessionService
	ProfilePhoto *app.ProfilePhotoService
}

// meResponse is the signed-in profile returned by GET/PATCH /v1/me,
// POST /v1/me/profile-photo, and POST /v1/auth/session.
type meResponse struct {
	UserID              string  `json:"userId"`
	DisplayName         string  `json:"displayName"`
	MemberWalletAddress string  `json:"memberWalletAddress"`
	ProfilePhotoURL     *string `json:"profilePhotoUrl"`
	CreatedAt           string  `json:"createdAt"`
}

// patchMeRequest is the PATCH /v1/me body. displayName is required; a pointer
// distinguishes a missing field from an empty string.
type patchMeRequest struct {
	DisplayName *string `json:"displayName"`
}

// MeHandler handles GET /v1/me.
func (h *MeHandlers) MeHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/me")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	result, err := h.Sessions.GetMe(ctx, token)
	if err != nil {
		writeMeError(ctx, log, w, err)
		return
	}

	writeMeResponse(ctx, log, w, http.StatusOK, result, "ok", "user_id", result.UserID)
}

// PatchMeHandler handles PATCH /v1/me: update the signed-in user's display name.
func (h *MeHandlers) PatchMeHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "PATCH /v1/me")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxPatchMeBodyBytes)
	decoder := json.NewDecoder(r.Body)
	var req patchMeRequest
	err := decoder.Decode(&req)
	if err == nil {
		// Exactly one JSON value: anything after it is a malformed body.
		if _, trailingErr := decoder.Token(); !errors.Is(trailingErr, io.EOF) {
			err = trailingErr
			if err == nil {
				err = errors.New("trailing data after JSON body")
			}
		}
	}
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			logJSONError(ctx, log, "body_too_large", w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		logJSONError(ctx, log, "invalid_body", w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.DisplayName == nil {
		logJSONError(ctx, log, "invalid_display_name", w, http.StatusBadRequest, "displayName is required")
		return
	}

	result, err := h.Sessions.SetDisplayName(ctx, token, *req.DisplayName)
	if err != nil {
		var nameErr *app.DisplayNameError
		switch {
		case errors.As(err, &nameErr):
			logJSONError(ctx, log, "invalid_display_name", w, http.StatusBadRequest, nameErr.Message(), "reason", string(nameErr.Reason))
		case errors.Is(err, app.ErrRateLimited):
			writeRateLimited(ctx, log, w, err)
		default:
			writeMeError(ctx, log, w, err)
		}
		return
	}

	writeMeResponse(ctx, log, w, http.StatusOK, result, "updated", "user_id", result.UserID)
}

// UploadProfilePhotoHandler handles POST /v1/me/profile-photo.
func (h *MeHandlers) UploadProfilePhotoHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "POST /v1/me/profile-photo")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	if h.ProfilePhoto == nil {
		logJSONError(ctx, log, "not_configured", w, http.StatusServiceUnavailable, "profile photo upload is not configured")
		return
	}

	const maxBodyBytes = 2<<20 + (1 << 10)
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	if err := r.ParseMultipartForm(maxBodyBytes); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			logJSONError(ctx, log, "photo_too_large", w, http.StatusBadRequest, "photo must be at most 2MB")
			return
		}
		logJSONError(ctx, log, "invalid_multipart", w, http.StatusBadRequest, "invalid multipart form", "err", err.Error())
		return
	}

	file, _, err := r.FormFile("photo")
	if err != nil {
		logJSONError(ctx, log, "missing_photo", w, http.StatusBadRequest, "photo field is required")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		logJSONError(ctx, log, "read_photo_failed", w, http.StatusBadRequest, "could not read photo")
		return
	}

	result, err := h.ProfilePhoto.UploadProfilePhoto(ctx, token, data)
	if err != nil {
		writeProfilePhotoUploadError(ctx, log, w, err)
		return
	}

	writeMeResponse(ctx, log, w, http.StatusOK, result, "uploaded", "user_id", result.UserID)
}

func writeMeResponse(ctx context.Context, log *requestLog, w http.ResponseWriter, status int, result app.MeResult, okCode string, kv ...any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(meResponseFromResult(result))
	logJSONOK(ctx, log, okCode, kv...)
}

func meResponseFromResult(result app.MeResult) meResponse {
	createdAt := ""
	if !result.CreatedAt.IsZero() {
		createdAt = result.CreatedAt.UTC().Format(time.RFC3339)
	}
	return meResponse{
		UserID:              result.UserID,
		DisplayName:         result.DisplayName,
		MemberWalletAddress: result.MemberWalletAddress,
		ProfilePhotoURL:     optionalString(result.ProfilePhotoURL),
		CreatedAt:           createdAt,
	}
}

// optionalString maps blank strings to JSON null.
func optionalString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func writeMeError(ctx context.Context, log *requestLog, w http.ResponseWriter, err error) {
	if errors.Is(err, auth.ErrUnauthorized) {
		logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token")
		return
	}
	if errors.Is(err, app.ErrUserNotFound) {
		logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found")
		return
	}
	logJSONError(ctx, log, "get_me_failed", w, http.StatusInternalServerError, "internal server error", "err", err.Error())
}

// writeRateLimited responds 429 with a whole-second Retry-After header.
func writeRateLimited(ctx context.Context, log *requestLog, w http.ResponseWriter, err error) {
	retryAfter := time.Second
	var limited *app.RateLimitError
	if errors.As(err, &limited) && limited.RetryAfter > retryAfter {
		retryAfter = limited.RetryAfter
	}
	seconds := int(math.Ceil(retryAfter.Seconds()))
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	logJSONError(ctx, log, "rate_limited", w, http.StatusTooManyRequests, "too many requests, try again shortly", "retry_after_s", seconds)
}

func writeProfilePhotoUploadError(ctx context.Context, log *requestLog, w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrUnauthorized):
		logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token")
	case errors.Is(err, app.ErrUserNotFound):
		logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found")
	case errors.Is(err, app.ErrRateLimited):
		writeRateLimited(ctx, log, w, err)
	case errors.Is(err, app.ErrProfilePhotoTooLarge):
		logJSONError(ctx, log, "photo_too_large", w, http.StatusBadRequest, "photo must be at most 2MB")
	case errors.Is(err, app.ErrProfilePhotoInvalid):
		logJSONError(ctx, log, "invalid_photo", w, http.StatusBadRequest, "photo must be jpeg, png, or webp")
	case errors.Is(err, app.ErrProfilePhotoNotConfigured):
		logJSONError(ctx, log, "not_configured", w, http.StatusServiceUnavailable, "profile photo upload is not configured")
	default:
		logJSONError(ctx, log, "upload_failed", w, http.StatusInternalServerError, "internal server error", "err", err.Error())
	}
}

func bearerToken(r *http.Request) (string, bool) {
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(auth, prefix))
	if token == "" {
		return "", false
	}
	return token, true
}

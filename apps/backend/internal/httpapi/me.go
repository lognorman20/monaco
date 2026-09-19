package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

// MeHandlers serves GET /v1/me and POST /v1/me/profile-photo.
type MeHandlers struct {
	Sessions     *app.SessionService
	ProfilePhoto *app.ProfilePhotoService
}

type meResponse struct {
	UserID              string  `json:"userId"`
	DisplayName         string  `json:"displayName"`
	MemberWalletAddress string  `json:"memberWalletAddress"`
	ProfilePhotoURL     *string `json:"profilePhotoUrl"`
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
		logJSONError(ctx, log, "invalid_multipart", w, http.StatusBadRequest, "invalid multipart form")
		return
	}

	file, header, err := r.FormFile("photo")
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
	_ = header

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
	var profilePhotoURL *string
	trimmed := strings.TrimSpace(result.ProfilePhotoURL)
	if trimmed != "" {
		profilePhotoURL = &trimmed
	}
	return meResponse{
		UserID:              result.UserID,
		DisplayName:         result.DisplayName,
		MemberWalletAddress: result.MemberWalletAddress,
		ProfilePhotoURL:     profilePhotoURL,
	}
}

func writeMeError(ctx context.Context, log *requestLog, w http.ResponseWriter, err error) {
	if errors.Is(err, privy.ErrInvalidToken) {
		logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token")
		return
	}
	if errors.Is(err, app.ErrUserNotFound) {
		logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found")
		return
	}
	logJSONError(ctx, log, "get_me_failed", w, http.StatusInternalServerError, "internal server error", "err", err.Error())
}

func writeProfilePhotoUploadError(ctx context.Context, log *requestLog, w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, privy.ErrInvalidToken):
		logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token")
	case errors.Is(err, app.ErrUserNotFound):
		logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found")
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

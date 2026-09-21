package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/auth"
)

// GroupPictureHandlers serves POST and DELETE /v1/groups/{id}/picture.
type GroupPictureHandlers struct {
	Pictures *app.GroupPictureService
}

// groupPictureResponse is the cabal's picture after a write. pictureUrl is null
// once the picture is removed, which is how the app knows to fall back to the
// cabal's initials.
type groupPictureResponse struct {
	GroupID    string  `json:"groupId"`
	PictureURL *string `json:"pictureUrl"`
}

// maxGroupPictureBodyBytes bounds the whole multipart body. The image itself is
// capped at 2MB inside the app layer; the extra kilobyte is multipart framing.
const maxGroupPictureBodyBytes = 2<<20 + (1 << 10)

// UploadGroupPictureHandler handles POST /v1/groups/{id}/picture.
func (h *GroupPictureHandlers) UploadGroupPictureHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "POST /v1/groups/{id}/picture")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	if h.Pictures == nil {
		logJSONError(ctx, log, "not_configured", w, http.StatusServiceUnavailable, "cabal picture upload is not configured")
		return
	}

	groupID := r.PathValue("id")
	if strings.TrimSpace(groupID) == "" {
		logJSONError(ctx, log, "missing_group_id", w, http.StatusNotFound, "cabal not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxGroupPictureBodyBytes)

	if err := r.ParseMultipartForm(maxGroupPictureBodyBytes); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			logJSONError(ctx, log, "picture_too_large", w, http.StatusRequestEntityTooLarge, "picture must be at most 2MB", "group_id", groupID)
			return
		}
		logJSONError(ctx, log, "invalid_multipart", w, http.StatusBadRequest, "invalid multipart form", "group_id", groupID, "err", err.Error())
		return
	}

	file, _, err := r.FormFile("picture")
	if err != nil {
		logJSONError(ctx, log, "missing_picture", w, http.StatusBadRequest, "picture field is required", "group_id", groupID)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		logJSONError(ctx, log, "read_picture_failed", w, http.StatusBadRequest, "could not read picture", "group_id", groupID)
		return
	}

	result, err := h.Pictures.SetGroupPicture(ctx, token, groupID, data)
	if err != nil {
		writeGroupPictureError(ctx, log, w, err, groupID)
		return
	}

	writeGroupPictureResponse(ctx, log, w, result, "uploaded")
}

// RemoveGroupPictureHandler handles DELETE /v1/groups/{id}/picture.
func (h *GroupPictureHandlers) RemoveGroupPictureHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "DELETE /v1/groups/{id}/picture")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	if h.Pictures == nil {
		logJSONError(ctx, log, "not_configured", w, http.StatusServiceUnavailable, "cabal picture upload is not configured")
		return
	}

	groupID := r.PathValue("id")
	if strings.TrimSpace(groupID) == "" {
		logJSONError(ctx, log, "missing_group_id", w, http.StatusNotFound, "cabal not found")
		return
	}

	result, err := h.Pictures.RemoveGroupPicture(ctx, token, groupID)
	if err != nil {
		writeGroupPictureError(ctx, log, w, err, groupID)
		return
	}

	writeGroupPictureResponse(ctx, log, w, result, "removed")
}

func writeGroupPictureResponse(ctx context.Context, log *requestLog, w http.ResponseWriter, result app.GroupPictureResult, okCode string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(groupPictureResponse{
		GroupID:    result.GroupID,
		PictureURL: optionalString(result.PictureURL),
	})
	logJSONOK(ctx, log, okCode, "group_id", result.GroupID)
}

func writeGroupPictureError(ctx context.Context, log *requestLog, w http.ResponseWriter, err error, groupID string) {
	switch {
	case errors.Is(err, auth.ErrUnauthorized):
		logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token", "group_id", groupID)
	case errors.Is(err, app.ErrUserNotFound):
		logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found", "group_id", groupID)
	// A non-member is told the same thing a bad id is told: nothing.
	case errors.Is(err, app.ErrGroupNotFound):
		logJSONError(ctx, log, "group_not_found", w, http.StatusNotFound, "cabal not found", "group_id", groupID)
	case errors.Is(err, app.ErrNotGroupCreator):
		logJSONError(ctx, log, "not_group_creator", w, http.StatusForbidden, "only the cabal's creator can change its picture", "group_id", groupID)
	case errors.Is(err, app.ErrRateLimited):
		writeRateLimited(ctx, log, w, err)
	case errors.Is(err, app.ErrImageTooLarge):
		logJSONError(ctx, log, "picture_too_large", w, http.StatusRequestEntityTooLarge, "picture must be at most 2MB", "group_id", groupID)
	case errors.Is(err, app.ErrImageInvalid):
		logJSONError(ctx, log, "invalid_picture", w, http.StatusBadRequest, "picture must be a jpeg, png, or webp image", "group_id", groupID)
	case errors.Is(err, app.ErrImageUploadNotConfigured):
		logJSONError(ctx, log, "not_configured", w, http.StatusServiceUnavailable, "cabal picture upload is not configured", "group_id", groupID)
	default:
		if writeFakerReadOnly(ctx, log, w, err, "group_id", groupID) {
			return
		}
		logJSONError(ctx, log, "set_group_picture_failed", w, http.StatusInternalServerError, "internal server error", "group_id", groupID, "err", err.Error())
	}
}

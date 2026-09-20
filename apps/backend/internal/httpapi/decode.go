package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// bodyTooLargeMessage is the one message every route sends with a 413.
const bodyTooLargeMessage = "request body too large"

// isBodyTooLarge reports whether err came from a tripped http.MaxBytesReader:
// the LimitRequestBody middleware cap or a tighter per-route one.
func isBodyTooLarge(err error) bool {
	var maxBytesErr *http.MaxBytesError
	return errors.As(err, &maxBytesErr)
}

// decodeJSONBody reads the request's JSON body into dst. On failure it writes
// the response and returns false: 413 when a body cap tripped, otherwise the
// 400 every handler already answered for a body it cannot parse. A client can
// therefore tell "too big" from "malformed" on every route.
func decodeJSONBody(ctx context.Context, log *requestLog, w http.ResponseWriter, r *http.Request, dst any, attrs ...any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeBodyDecodeError(ctx, log, w, err, attrs...)
		return false
	}
	return true
}

// decodeOptionalJSONBody is decodeJSONBody for routes whose body may be absent
// or empty; dst is left untouched in that case.
func decodeOptionalJSONBody(ctx context.Context, log *requestLog, w http.ResponseWriter, r *http.Request, dst any, attrs ...any) bool {
	if r.Body == nil {
		return true
	}
	body, err := io.ReadAll(r.Body)
	if err == nil && len(body) > 0 {
		err = json.Unmarshal(body, dst)
	}
	if err != nil {
		writeBodyDecodeError(ctx, log, w, err, attrs...)
		return false
	}
	return true
}

// writeBodyDecodeError maps a body read/parse failure to its response. It is
// shared with handlers that decode by hand (PATCH /v1/me rejects trailing data)
// so they still share the 413 mapping.
func writeBodyDecodeError(ctx context.Context, log *requestLog, w http.ResponseWriter, err error, attrs ...any) {
	if isBodyTooLarge(err) {
		logJSONError(ctx, log, "body_too_large", w, http.StatusRequestEntityTooLarge, bodyTooLargeMessage, attrs...)
		return
	}
	logJSONError(ctx, log, "invalid_body", w, http.StatusBadRequest, "invalid request body", attrs...)
}

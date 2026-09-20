package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/monaco/monaco/apps/backend/internal/app"
)

// writeFakerReadOnly maps app.ErrFakerGroupReadOnly to 403 for mutations on faker
// scale clubs or ghost proposals (#153). Returns true when it wrote the response.
func writeFakerReadOnly(ctx context.Context, log *requestLog, w http.ResponseWriter, err error, attrs ...any) bool {
	if !errors.Is(err, app.ErrFakerGroupReadOnly) {
		return false
	}
	logJSONError(ctx, log, "faker_group_read_only", w, http.StatusForbidden, "demo club is read-only", attrs...)
	return true
}

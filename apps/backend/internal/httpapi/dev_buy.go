package httpapi

import (
	"net/http"
)

// DevBuyHandlers is the removed M3 dev-only buy route shell (M4-T19).
type DevBuyHandlers struct{}

// DevBuyHandler handles POST /v1/dev/groups/{id}/buy. Deleted in M4-T19 — always 404.
func (h *DevBuyHandlers) DevBuyHandler(w http.ResponseWriter, r *http.Request) {
	writeJSONError(w, http.StatusNotFound, "not found")
}

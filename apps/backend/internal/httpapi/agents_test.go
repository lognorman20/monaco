package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/app"
)

func TestSubmitAgentIntentHandler_rejectsOversizedIdempotencyKey(t *testing.T) {
	t.Parallel()

	body := fmt.Sprintf(`{"side":"buy","symbol":"AAPLx","usdcMicros":1000000,"idempotencyKey":%q}`,
		strings.Repeat("k", app.MaxAgentIdempotencyKeyLength+1))
	req := httptest.NewRequest(http.MethodPost, "/v1/groups/g/agents/intents", strings.NewReader(body))
	req.SetPathValue("id", "00000000-0000-4000-8000-000000000000")
	req.Header.Set(agentKeyHeader, wrongCurrentFormatKey)
	rec := httptest.NewRecorder()

	// No intent service: the request must be refused before it would be reached.
	(&AgentHandlers{}).SubmitAgentIntentHandler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestWriteAgentIntentError_inFlightReplayIsConflict(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/v1/groups/g/agents/intents", nil)
	rec := httptest.NewRecorder()
	writeAgentIntentError(req.Context(), newRequestLog(req, "test"), rec, app.ErrAgentIntentInFlight, "g")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/packages/domain"
)

const agentKeyHeader = "X-Monaco-Agent-Key"

// AgentHandlers serves agent intent routes.
type AgentHandlers struct {
	Intents *app.AgentIntentService
	// KeyGuard throttles wrong agent keys. Nil disables throttling.
	KeyGuard *AgentKeyGuard
}

type submitAgentIntentRequest struct {
	Side        string `json:"side"`
	Symbol      string `json:"symbol"`
	UsdcMicros  int64  `json:"usdcMicros"`
	TokenAmount int64  `json:"tokenAmount"`
	// IdempotencyKey is optional. A resend under the same key gets the first intent's answer.
	IdempotencyKey string `json:"idempotencyKey"`
}

type submitAgentIntentResponse struct {
	IntentID      string `json:"intentId"`
	Status        string `json:"status"`
	TransactionID string `json:"transactionId,omitempty"`
	RejectReason  string `json:"rejectReason,omitempty"`
}

// SubmitAgentIntentHandler handles POST /v1/groups/{id}/agents/intents.
func (h *AgentHandlers) SubmitAgentIntentHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "POST /v1/groups/{id}/agents/intents")

	groupID := strings.TrimSpace(r.PathValue("id"))
	if groupID == "" {
		logJSONError(ctx, log, "missing_group_id", w, http.StatusNotFound, "group not found")
		return
	}

	agentKey := strings.TrimSpace(r.Header.Get(agentKeyHeader))
	if agentKey == "" {
		logJSONError(ctx, log, "missing_agent_key", w, http.StatusUnauthorized, "missing agent api key", "group_id", groupID)
		return
	}
	if over, wait := h.KeyGuard.blocked(r, groupID, agentKey); over {
		writeAgentKeyThrottled(ctx, log, w, wait, groupID)
		return
	}

	var req submitAgentIntentRequest
	if !decodeJSONBody(ctx, log, w, r, &req, "group_id", groupID) {
		return
	}
	side, err := domain.ParseAgentIntentSide(strings.TrimSpace(req.Side))
	if err != nil {
		logJSONError(ctx, log, "invalid_side", w, http.StatusBadRequest, "side must be buy or sell", "group_id", groupID)
		return
	}
	if strings.TrimSpace(req.Symbol) == "" {
		logJSONError(ctx, log, "missing_symbol", w, http.StatusBadRequest, "symbol is required", "group_id", groupID)
		return
	}
	if side == domain.AgentIntentBuy && req.UsdcMicros <= 0 {
		logJSONError(ctx, log, "invalid_usdc", w, http.StatusBadRequest, "usdcMicros must be positive", "group_id", groupID)
		return
	}
	if side == domain.AgentIntentSell && req.TokenAmount <= 0 {
		logJSONError(ctx, log, "invalid_token_amount", w, http.StatusBadRequest, "tokenAmount must be positive", "group_id", groupID)
		return
	}

	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if len(idempotencyKey) > app.MaxAgentIdempotencyKeyLength {
		logJSONError(ctx, log, "invalid_idempotency_key", w, http.StatusBadRequest, "idempotencyKey is too long", "group_id", groupID)
		return
	}

	result, err := h.Intents.SubmitAgentIntent(ctx, app.SubmitAgentIntentInput{
		GroupID:        groupID,
		AgentKey:       agentKey,
		Side:           side,
		Symbol:         strings.TrimSpace(req.Symbol),
		UsdcMicros:     req.UsdcMicros,
		TokenAmount:    req.TokenAmount,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		h.KeyGuard.recordFailure(r, groupID, err)
		writeAgentIntentError(r.Context(), log, w, err, groupID)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(submitAgentIntentResponse{
		IntentID:      result.IntentID,
		Status:        result.Status,
		TransactionID: result.TransactionID,
		RejectReason:  result.RejectReason,
	})
	logJSONOK(ctx, log, "ok", "group_id", groupID, "intent_id", result.IntentID, "status", result.Status)
}

func writeAgentIntentError(ctx context.Context, log *requestLog, w http.ResponseWriter, err error, groupID string) {
	if writeFakerReadOnly(ctx, log, w, err, "group_id", groupID) {
		return
	}
	switch {
	case errors.Is(err, app.ErrInvalidAgentAPIKey):
		logJSONError(ctx, log, "invalid_agent_key", w, http.StatusUnauthorized, "invalid agent api key", "group_id", groupID)
	case errors.Is(err, app.ErrAgentGroupMismatch):
		// Same response as an unknown key: a distinct one would confirm the key is live elsewhere.
		logJSONError(ctx, log, "agent_group_mismatch", w, http.StatusUnauthorized, "invalid agent api key", "group_id", groupID)
	case errors.Is(err, app.ErrAgentPaused):
		logJSONError(ctx, log, "agent_paused", w, http.StatusForbidden, "agent is paused", "group_id", groupID)
	case errors.Is(err, app.ErrAgentIntentInFlight):
		logJSONError(ctx, log, "intent_in_flight", w, http.StatusConflict, "intent with this idempotencyKey is still executing", "group_id", groupID)
	case errors.Is(err, app.ErrAgentIntentRejected):
		logJSONError(ctx, log, "intent_rejected", w, http.StatusUnprocessableEntity, err.Error(), "group_id", groupID)
	default:
		logJSONError(ctx, log, "intent_failed", w, http.StatusInternalServerError, "internal server error", "group_id", groupID, "err", err.Error())
	}
}

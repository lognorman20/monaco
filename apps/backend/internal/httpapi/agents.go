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
}

type submitAgentIntentRequest struct {
	Side        string `json:"side"`
	Symbol      string `json:"symbol"`
	UsdcMicros  int64  `json:"usdcMicros"`
	TokenAmount int64  `json:"tokenAmount"`
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

	var req submitAgentIntentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logJSONError(ctx, log, "invalid_body", w, http.StatusBadRequest, "invalid request body", "group_id", groupID)
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

	result, err := h.Intents.SubmitAgentIntent(ctx, app.SubmitAgentIntentInput{
		GroupID:     groupID,
		AgentKey:    agentKey,
		Side:        side,
		Symbol:      strings.TrimSpace(req.Symbol),
		UsdcMicros:  req.UsdcMicros,
		TokenAmount: req.TokenAmount,
	})
	if err != nil {
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
	switch {
	case errors.Is(err, app.ErrInvalidAgentAPIKey):
		logJSONError(ctx, log, "invalid_agent_key", w, http.StatusUnauthorized, "invalid agent api key", "group_id", groupID)
	case errors.Is(err, app.ErrAgentGroupMismatch):
		logJSONError(ctx, log, "agent_group_mismatch", w, http.StatusForbidden, "agent key does not match group", "group_id", groupID)
	case errors.Is(err, app.ErrAgentPaused):
		logJSONError(ctx, log, "agent_paused", w, http.StatusForbidden, "agent is paused", "group_id", groupID)
	case errors.Is(err, app.ErrAgentIntentRejected):
		logJSONError(ctx, log, "intent_rejected", w, http.StatusUnprocessableEntity, err.Error(), "group_id", groupID)
	default:
		logJSONError(ctx, log, "intent_failed", w, http.StatusInternalServerError, "internal server error", "group_id", groupID, "err", err.Error())
	}
}

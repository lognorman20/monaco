package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/packages/domain"
)

const agentKeyHeader = "X-Monaco-Agent-Key"

// AgentHandlers serves the routes an agent calls with its X-Monaco-Agent-Key.
type AgentHandlers struct {
	Store   *postgres.Store
	Intents *app.AgentIntentService
	// KeyGuard throttles wrong agent keys. Nil disables throttling.
	KeyGuard *AgentKeyGuard
	// Limits caps each agent's intents and reads. Nil disables the caps.
	Limits *AgentRateLimits
	// Docs serves skill.md and its URL for PUBLIC_API_BASE_URL.
	Docs *app.AgentDocs
}

type submitAgentIntentRequest struct {
	Side   string `json:"side"`
	Symbol string `json:"symbol"`
	// A buy sends exactly one of UsdcMicros and Usd; a sell exactly one of TokenAmount and Shares.
	UsdcMicros  *int64         `json:"usdcMicros"`
	Usd         *decimalAmount `json:"usd"`
	TokenAmount *int64         `json:"tokenAmount"`
	Shares      *decimalAmount `json:"shares"`
	// IdempotencyKey is optional. A resend under the same key gets the first intent's answer.
	IdempotencyKey string `json:"idempotencyKey"`
	Reason         string `json:"reason"`
}

// decimalAmount is a decimal amount sent as a JSON string ("10.50") or a bare JSON number
// (10.50). It keeps the text as sent, so it is parsed exactly and never through a float.
type decimalAmount string

func (d *decimalAmount) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*d = decimalAmount(strings.TrimSpace(text))
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err != nil {
		return fmt.Errorf("amount must be a decimal string such as \"10.50\"")
	}
	*d = decimalAmount(number.String())
	return nil
}

type submitAgentIntentResponse struct {
	IntentID      string `json:"intentId"`
	Status        string `json:"status"`
	TransactionID string `json:"transactionId,omitempty"`
	RejectReason  string `json:"rejectReason,omitempty"`
}

// agentIntentRequestError is a request refused before it reached the intent service.
type agentIntentRequestError struct {
	status  int
	branch  string
	message string
}

// parseAgentIntentRequest turns a request body into an intent with atomic amounts. Amount
// mistakes are 422 with a rejected status, the same shape the service uses to refuse an
// intent, so a bot handles both the same way.
func parseAgentIntentRequest(req submitAgentIntentRequest) (app.SubmitAgentIntentInput, *agentIntentRequestError) {
	side, err := domain.ParseAgentIntentSide(strings.TrimSpace(req.Side))
	if err != nil {
		return app.SubmitAgentIntentInput{}, &agentIntentRequestError{http.StatusBadRequest, "invalid_side", "side must be buy or sell"}
	}
	symbol := strings.TrimSpace(req.Symbol)
	if symbol == "" {
		return app.SubmitAgentIntentInput{}, &agentIntentRequestError{http.StatusBadRequest, "missing_symbol", "symbol is required"}
	}
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if len(idempotencyKey) > app.MaxAgentIdempotencyKeyLength {
		return app.SubmitAgentIntentInput{}, &agentIntentRequestError{http.StatusBadRequest, "invalid_idempotency_key", "idempotencyKey is too long"}
	}
	reason := strings.TrimSpace(req.Reason)
	if utf8.RuneCountInString(reason) > app.MaxAgentIntentReasonRunes {
		return app.SubmitAgentIntentInput{}, invalidAgentIntent(fmt.Sprintf("reason is longer than %d characters", app.MaxAgentIntentReasonRunes))
	}

	in := app.SubmitAgentIntentInput{Side: side, Symbol: symbol, IdempotencyKey: idempotencyKey, Reason: reason}
	var amountErr *agentIntentRequestError
	if side == domain.AgentIntentBuy {
		in.UsdcMicros, amountErr = parseAgentAmount(req.UsdcMicros, req.Usd, req.TokenAmount != nil || req.Shares != nil,
			"usdcMicros", "usd", "tokenAmount or shares", domain.ParseUSDDecimal)
	} else {
		in.TokenAmount, amountErr = parseAgentAmount(req.TokenAmount, req.Shares, req.UsdcMicros != nil || req.Usd != nil,
			"tokenAmount", "shares", "usdcMicros or usd", func(s string) (int64, error) {
				return domain.ParseTokenDecimal(s, jupiter.XStockDecimals)
			})
	}
	if amountErr != nil {
		return app.SubmitAgentIntentInput{}, amountErr
	}
	return in, nil
}

// parseAgentAmount reads a side's amount from exactly one of its atomic and decimal fields.
func parseAgentAmount(atomic *int64, decimal *decimalAmount, otherSideSent bool, atomicName, decimalName, otherSideNames string, parse func(string) (int64, error)) (int64, *agentIntentRequestError) {
	if otherSideSent {
		return 0, invalidAgentIntent(fmt.Sprintf("this side takes %s or %s, not %s", atomicName, decimalName, otherSideNames))
	}
	switch {
	case atomic != nil && decimal != nil, atomic == nil && decimal == nil:
		return 0, invalidAgentIntent(fmt.Sprintf("send exactly one of %s or %s", atomicName, decimalName))
	case atomic != nil:
		if *atomic <= 0 {
			return 0, invalidAgentIntent(atomicName + " must be positive")
		}
		return *atomic, nil
	default:
		amount, err := parse(string(*decimal))
		if err != nil {
			return 0, invalidAgentIntent(decimalName + ": " + err.Error())
		}
		return amount, nil
	}
}

func invalidAgentIntent(message string) *agentIntentRequestError {
	return &agentIntentRequestError{http.StatusUnprocessableEntity, "invalid_intent", message}
}

func writeAgentIntentRequestError(ctx context.Context, log *requestLog, w http.ResponseWriter, reqErr *agentIntentRequestError, attrs ...any) {
	if reqErr.status == http.StatusUnprocessableEntity {
		logJSONIntentError(ctx, log, reqErr.branch, w, reqErr.status, reqErr.message,
			&intentErrorDetail{Status: "rejected", RejectReason: reqErr.message}, attrs...)
		return
	}
	logJSONError(ctx, log, reqErr.branch, w, reqErr.status, reqErr.message, attrs...)
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
	in, reqErr := parseAgentIntentRequest(req)
	if reqErr != nil {
		writeAgentIntentRequestError(ctx, log, w, reqErr, "group_id", groupID)
		return
	}

	agent, err := resolveAgentForGroup(ctx, h.Store, groupID, agentKey)
	if err != nil {
		h.KeyGuard.recordFailure(r, groupID, err)
		writeAgentAuthError(ctx, log, w, err, groupID)
		return
	}
	h.submitIntent(w, r, log, agent.ID, groupID, agentKey, in)
}

// submitIntent runs an authenticated agent's intent under the agent's intent rate limit.
func (h *AgentHandlers) submitIntent(w http.ResponseWriter, r *http.Request, log *requestLog, agentID, groupID, agentKey string, in app.SubmitAgentIntentInput) {
	ctx := r.Context()
	if !h.Limits.allowIntent(ctx, log, w, agentID) {
		return
	}
	in.GroupID = groupID
	in.AgentKey = agentKey
	result, err := h.Intents.SubmitAgentIntent(ctx, in)
	if err != nil {
		h.KeyGuard.recordFailure(r, groupID, err)
		writeAgentIntentError(ctx, log, w, result, err, groupID)
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

func writeAgentIntentError(ctx context.Context, log *requestLog, w http.ResponseWriter, result app.SubmitAgentIntentResult, err error, groupID string) {
	if writeFakerReadOnly(ctx, log, w, err, "group_id", groupID) {
		return
	}
	// Whatever the service knows about the intent travels with the refusal, so the bot can
	// tell over budget from a failed swap and quote the intent id. Auth failures and errors
	// from before the intent was decided carry no outcome and keep the plain error shape.
	intent := agentIntentErrorDetail(result)
	switch {
	case errors.Is(err, app.ErrInvalidAgentAPIKey):
		logJSONError(ctx, log, "invalid_agent_key", w, http.StatusUnauthorized, "invalid agent api key", "group_id", groupID)
	case errors.Is(err, app.ErrAgentGroupMismatch):
		// Same response as an unknown key: a distinct one would confirm the key is live elsewhere.
		logJSONError(ctx, log, "agent_group_mismatch", w, http.StatusUnauthorized, "invalid agent api key", "group_id", groupID)
	case errors.Is(err, app.ErrAgentPaused):
		logJSONIntentError(ctx, log, "agent_paused", w, http.StatusForbidden, "agent is paused", intent, "group_id", groupID)
	case errors.Is(err, app.ErrAgentIntentInFlight):
		logJSONIntentError(ctx, log, "intent_in_flight", w, http.StatusConflict, "intent with this idempotencyKey is still executing", intent, "group_id", groupID)
	case errors.Is(err, app.ErrAgentIntentRejected):
		logJSONIntentError(ctx, log, "intent_rejected", w, http.StatusUnprocessableEntity, err.Error(), intent, "group_id", groupID)
	default:
		logJSONIntentError(ctx, log, "intent_failed", w, http.StatusInternalServerError, "internal server error", intent, "group_id", groupID, "err", err.Error())
	}
}

// agentIntentErrorDetail is nil when the service returned no outcome with its error.
func agentIntentErrorDetail(result app.SubmitAgentIntentResult) *intentErrorDetail {
	if result.IntentID == "" && result.Status == "" {
		return nil
	}
	return &intentErrorDetail{
		IntentID:     result.IntentID,
		Status:       result.Status,
		RejectReason: result.RejectReason,
	}
}

package httpapi

import (
	"errors"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

// The /v1/agent routes take only the agent key: it names the agent and its cabal, so an agent
// needs nothing else to trade. Every keyed route answers a revoked key with 401.

type agentBudgetResponse struct {
	AllocationUsd        string `json:"allocationUsd"`
	AllocationUsdcMicros int64  `json:"allocationUsdcMicros"`
	AvailableUsd         string `json:"availableUsd"`
	AvailableUsdcMicros  int64  `json:"availableUsdcMicros"`
}

type agentHoldingResponse struct {
	Symbol          string  `json:"symbol"`
	Name            string  `json:"name"`
	Shares          string  `json:"shares"`
	SharesAtomic    int64   `json:"sharesAtomic"`
	MarkUsd         *string `json:"markUsd"`
	MarkUsdcMicros  *int64  `json:"markUsdcMicros"`
	ValueUsd        *string `json:"valueUsd"`
	ValueUsdcMicros *int64  `json:"valueUsdcMicros"`
}

type agentLimitsResponse struct {
	IntentsPerHour int `json:"intentsPerHour"`
	ReasonMaxChars int `json:"reasonMaxChars"`
}

type agentAccountResponse struct {
	CabalName               string                 `json:"cabalName"`
	AgentName               string                 `json:"agentName"`
	Status                  string                 `json:"status"`
	Budget                  agentBudgetResponse    `json:"budget"`
	CashAvailableUsd        string                 `json:"cashAvailableUsd"`
	CashAvailableUsdcMicros int64                  `json:"cashAvailableUsdcMicros"`
	Holdings                []agentHoldingResponse `json:"holdings"`
	Limits                  agentLimitsResponse    `json:"limits"`
	DocsURL                 string                 `json:"docsUrl"`
}

type agentAssetResponse struct {
	Symbol         string  `json:"symbol"`
	Name           string  `json:"name"`
	SolanaMint     string  `json:"solanaMint"`
	Routable       bool    `json:"routable"`
	MarkUsd        *string `json:"markUsd"`
	MarkUsdcMicros *int64  `json:"markUsdcMicros"`
}

type agentAssetsResponse struct {
	Assets  []agentAssetResponse `json:"assets"`
	HasMore bool                 `json:"hasMore"`
}

type agentIntentResponse struct {
	IntentID          string  `json:"intentId"`
	Side              string  `json:"side"`
	Symbol            string  `json:"symbol"`
	Status            string  `json:"status"`
	RejectReason      *string `json:"rejectReason"`
	Reason            *string `json:"reason"`
	IdempotencyKey    *string `json:"idempotencyKey"`
	UsdcMicros        *int64  `json:"usdcMicros"`
	TokenAmount       *int64  `json:"tokenAmount"`
	TransactionID     *string `json:"transactionId"`
	TxSignature       *string `json:"txSignature"`
	FilledTokenAmount *int64  `json:"filledTokenAmount"`
	FilledUsdcMicros  *int64  `json:"filledUsdcMicros"`
	CreatedAt         string  `json:"createdAt"`
}

// authenticateAgentKey resolves the calling agent from its key alone and spends one read or
// intent from its per-agent limit. It writes the refusal itself and reports false.
func (h *AgentHandlers) authenticateAgentKey(w http.ResponseWriter, r *http.Request, log *requestLog, intent bool) (postgres.GroupAgentRow, string, bool) {
	ctx := r.Context()
	agentKey := strings.TrimSpace(r.Header.Get(agentKeyHeader))
	if agentKey == "" {
		logJSONError(ctx, log, "missing_agent_key", w, http.StatusUnauthorized, "missing agent api key")
		return postgres.GroupAgentRow{}, "", false
	}
	if over, wait := h.KeyGuard.blocked(r, keyOnlyGuardScope, agentKey); over {
		writeAgentKeyThrottled(ctx, log, w, wait, "")
		return postgres.GroupAgentRow{}, "", false
	}
	agent, err := resolveAgentByKey(ctx, h.Store, agentKey)
	if err != nil {
		h.KeyGuard.recordFailure(r, keyOnlyGuardScope, err)
		writeAgentAuthError(ctx, log, w, err, "")
		return postgres.GroupAgentRow{}, "", false
	}
	if !intent && !h.Limits.allowRead(ctx, log, w, agent.ID) {
		return postgres.GroupAgentRow{}, "", false
	}
	return agent, agentKey, true
}

// AgentAccountHandler handles GET /v1/agent.
func (h *AgentHandlers) AgentAccountHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/agent")
	agent, _, ok := h.authenticateAgentKey(w, r, log, false)
	if !ok {
		return
	}
	account, err := h.Intents.Account(ctx, agent)
	if err != nil {
		if writeFakerReadOnly(ctx, log, w, err, "group_id", agent.GroupID) {
			return
		}
		logJSONError(ctx, log, "agent_account_failed", w, http.StatusInternalServerError, "internal server error", "group_id", agent.GroupID, "err", err.Error())
		return
	}

	resp := agentAccountResponse{
		CabalName: account.CabalName,
		AgentName: account.AgentName,
		Status:    string(account.Status),
		Budget: agentBudgetResponse{
			AllocationUsd:        formatUsdcMicros(account.AllocationUsdcMicros),
			AllocationUsdcMicros: account.AllocationUsdcMicros,
			AvailableUsd:         formatUsdcMicros(account.AvailableUsdcMicros),
			AvailableUsdcMicros:  account.AvailableUsdcMicros,
		},
		CashAvailableUsd:        formatUsdcMicros(account.CashAvailableUsdcMicros),
		CashAvailableUsdcMicros: account.CashAvailableUsdcMicros,
		Holdings:                make([]agentHoldingResponse, 0, len(account.Holdings)),
		Limits:                  agentLimitsResponse{IntentsPerHour: app.AgentIntentsPerHour, ReasonMaxChars: app.MaxAgentIntentReasonRunes},
	}
	if h.Docs != nil {
		resp.DocsURL = h.Docs.SkillURL()
	}
	for _, holding := range account.Holdings {
		resp.Holdings = append(resp.Holdings, agentHoldingResponse{
			Symbol:          holding.Symbol,
			Name:            holding.Name,
			Shares:          formatAtomic(holding.TokenAmount, jupiter.XStockAtomicScale, jupiter.XStockDecimals),
			SharesAtomic:    holding.TokenAmount,
			MarkUsd:         optionalUsd(holding.MarkUsdcMicros),
			MarkUsdcMicros:  holding.MarkUsdcMicros,
			ValueUsd:        optionalUsd(holding.ValueUsdcMicros),
			ValueUsdcMicros: holding.ValueUsdcMicros,
		})
	}
	writeMarketJSON(ctx, log, w, http.StatusOK, resp, "ok", "group_id", agent.GroupID, "agent_id", agent.ID, "holding_count", len(resp.Holdings))
}

// AgentAssetsHandler handles GET /v1/agent/assets.
func (h *AgentHandlers) AgentAssetsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/agent/assets")
	agent, _, ok := h.authenticateAgentKey(w, r, log, false)
	if !ok {
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	limit := parseCatalogLimit(r.URL.Query().Get("limit"))
	offset := parseCatalogOffset(r.URL.Query().Get("offset"))

	page, err := h.Intents.ListAssets(ctx, query, limit, offset)
	if err != nil {
		if errors.Is(err, xstocks.ErrInvalidResponse) {
			logJSONError(ctx, log, "invalid_catalog_query", w, http.StatusBadRequest, "invalid catalog query", "query", query)
			return
		}
		logJSONError(ctx, log, "catalog_search_failed", w, http.StatusInternalServerError, "internal server error", "query", query, "err", err.Error())
		return
	}
	resp := agentAssetsResponse{Assets: make([]agentAssetResponse, 0, len(page.Assets)), HasMore: page.HasMore}
	for _, asset := range page.Assets {
		resp.Assets = append(resp.Assets, agentAssetResponse{
			Symbol:         asset.Symbol,
			Name:           asset.Name,
			SolanaMint:     asset.SolanaMint,
			Routable:       asset.Routable,
			MarkUsd:        optionalUsd(asset.MarkUsdcMicros),
			MarkUsdcMicros: asset.MarkUsdcMicros,
		})
	}
	writeMarketJSON(ctx, log, w, http.StatusOK, resp, "ok", "agent_id", agent.ID, "query", query, "result_count", len(resp.Assets))
}

// SubmitKeyAgentIntentHandler handles POST /v1/agent/intents.
func (h *AgentHandlers) SubmitKeyAgentIntentHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "POST /v1/agent/intents")
	agent, agentKey, ok := h.authenticateAgentKey(w, r, log, true)
	if !ok {
		return
	}
	var req submitAgentIntentRequest
	if !decodeJSONBody(ctx, log, w, r, &req, "group_id", agent.GroupID) {
		return
	}
	in, reqErr := parseAgentIntentRequest(req)
	if reqErr != nil {
		writeAgentIntentRequestError(ctx, log, w, reqErr, "group_id", agent.GroupID)
		return
	}
	h.submitIntent(w, r, log, agent.ID, agent.GroupID, agentKey, in)
}

// GetAgentIntentHandler handles GET /v1/agent/intents/{intentId}.
func (h *AgentHandlers) GetAgentIntentHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/agent/intents/{intentId}")
	agent, _, ok := h.authenticateAgentKey(w, r, log, false)
	if !ok {
		return
	}
	intentID := strings.TrimSpace(r.PathValue("intentId"))
	view, err := h.Intents.GetIntent(ctx, agent.ID, intentID)
	if errors.Is(err, app.ErrAgentIntentNotFound) {
		logJSONError(ctx, log, "intent_not_found", w, http.StatusNotFound, "intent not found", "agent_id", agent.ID)
		return
	}
	if err != nil {
		logJSONError(ctx, log, "get_intent_failed", w, http.StatusInternalServerError, "internal server error", "agent_id", agent.ID, "err", err.Error())
		return
	}
	resp := agentIntentResponse{
		IntentID:          view.ID,
		Side:              string(view.Side),
		Symbol:            view.Symbol,
		Status:            view.Status,
		RejectReason:      optionalText(view.RejectReason),
		Reason:            optionalText(view.Reason),
		IdempotencyKey:    optionalText(view.IdempotencyKey),
		UsdcMicros:        view.UsdcMicros,
		TokenAmount:       view.TokenAmount,
		TransactionID:     optionalText(view.TransactionID),
		TxSignature:       optionalText(view.TxSignature),
		FilledTokenAmount: view.FilledTokenAmount,
		FilledUsdcMicros:  view.FilledUsdcMicros,
		CreatedAt:         view.CreatedAt.UTC().Format(time.RFC3339),
	}
	writeMarketJSON(ctx, log, w, http.StatusOK, resp, "ok", "agent_id", agent.ID, "intent_id", view.ID, "status", view.Status)
}

// AgentSkillHandler handles GET /v1/agent/skill.md. It is public: it holds no secrets and an
// agent reads it before it has been handed a key.
func (h *AgentHandlers) AgentSkillHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/agent/skill.md")
	if h.Docs == nil {
		logJSONError(ctx, log, "skill_unavailable", w, http.StatusNotFound, "not found")
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(h.Docs.SkillMarkdown()))
	logJSONOK(ctx, log, "ok")
}

// formatAtomic renders an atomic amount as an exact decimal with the given places.
func formatAtomic(atomic, scale int64, places int) string {
	return new(big.Rat).SetFrac64(atomic, scale).FloatString(places)
}

func formatUsdcMicros(micros int64) string {
	return formatAtomic(micros, 1_000_000, 2)
}

func optionalUsd(micros *int64) *string {
	if micros == nil {
		return nil
	}
	text := formatUsdcMicros(*micros)
	return &text
}

func optionalText(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

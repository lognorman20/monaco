package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
	"github.com/monaco/monaco/packages/domain"
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
	writeAgentIntentError(req.Context(), newRequestLog(req, "test"), rec,
		app.SubmitAgentIntentResult{IntentID: "intent-1", Status: "accepted"}, app.ErrAgentIntentInFlight, "g")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	body := decodeAgentIntentErrorBody(t, rec)
	if body.IntentID != "intent-1" || body.Status != "accepted" || body.RejectReason != "" {
		t.Fatalf("body = %+v, want the in-flight intent's id and status", body)
	}
}

// A refusal that never reached an intent (bad key, database down) stays the plain error shape.
func TestWriteAgentIntentError_withoutIntentOutcomeKeepsPlainErrorShape(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/v1/groups/g/agents/intents", nil)
	rec := httptest.NewRecorder()
	writeAgentIntentError(req.Context(), newRequestLog(req, "test"), rec, app.SubmitAgentIntentResult{}, app.ErrInvalidAgentAPIKey, "g")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	var fields map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &fields); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	for _, name := range []string{"intentId", "status", "rejectReason"} {
		if _, present := fields[name]; present {
			t.Fatalf("body = %s, want no %q on a 401", rec.Body.String(), name)
		}
	}
}

type agentIntentErrorBody struct {
	Error        string `json:"error"`
	RequestID    string `json:"requestId"`
	IntentID     string `json:"intentId"`
	Status       string `json:"status"`
	RejectReason string `json:"rejectReason"`
}

func decodeAgentIntentErrorBody(t *testing.T, rec *httptest.ResponseRecorder) agentIntentErrorBody {
	t.Helper()
	var body agentIntentErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	return body
}

type agentIntentTestApp struct {
	Handlers   *AgentHandlers
	Governance *app.GovernanceService
	Jupiter    jupiter.Client
	GroupID    string
	CreatorID  string
	Key        string
}

// integrationAgentIntentApp is a cabal with $100 in its treasury and an active agent that
// was voted a $1 allocation, behind the real intent handler and the request id middleware.
func integrationAgentIntentApp(t *testing.T) agentIntentTestApp {
	t.Helper()

	quoteHandlers, groupHandlers, authHandlers, privyClient, jupiterClient, resolver, iso := integrationQuotesApp(t)
	token, groupID, creatorID := createGroupForQuotes(t, iso, groupHandlers, authHandlers, privyClient)
	key := installGuardTestAgent(t, groupHandlers.Governance, string(token), groupID, creatorID)
	xstocks.RegisterSolanaMint(resolver, "AAPLx", jupiter.AAPLxMint)

	symbols := app.NewSymbolResolver(xstocks.NewFakeCatalogSearcher())
	swap := app.NewSwapService(quoteHandlers.Store, quoteHandlers.Buy, jupiterClient, privyClient, app.NewFakePrivyTreasurySigner(), "", symbols)
	return agentIntentTestApp{
		Handlers:   &AgentHandlers{Intents: app.NewAgentIntentService(quoteHandlers.Store, swap, symbols)},
		Governance: groupHandlers.Governance,
		Jupiter:    jupiterClient,
		GroupID:    groupID,
		CreatorID:  creatorID,
		Key:        key,
	}
}

func (a agentIntentTestApp) postBuy(usdcMicros int64, idempotencyKey string) *httptest.ResponseRecorder {
	body := fmt.Sprintf(`{"side":"buy","symbol":"AAPLx","usdcMicros":%d,"idempotencyKey":%q}`, usdcMicros, idempotencyKey)
	req := httptest.NewRequest(http.MethodPost, "/v1/groups/"+a.GroupID+"/agents/intents", strings.NewReader(body))
	req.SetPathValue("id", a.GroupID)
	req.Header.Set(agentKeyHeader, a.Key)
	rec := httptest.NewRecorder()
	RequestID()(http.HandlerFunc(a.Handlers.SubmitAgentIntentHandler)).ServeHTTP(rec, req)
	return rec
}

func TestSubmitAgentIntentHandler_overBudgetBuy_returnsRejectedIntentInBody(t *testing.T) {
	t.Parallel()
	// Arrange
	a := integrationAgentIntentApp(t)

	// Act: $2 against a $1 allocation.
	rec := a.postBuy(2_000_000, "")

	// Assert
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body = %s", rec.Code, rec.Body.String())
	}
	body := decodeAgentIntentErrorBody(t, rec)
	if body.Status != "rejected" {
		t.Fatalf("status = %q, want rejected; body = %s", body.Status, rec.Body.String())
	}
	if body.RejectReason != "trade exceeds agent allocation" {
		t.Fatalf("rejectReason = %q, want the allocation breach", body.RejectReason)
	}
	if body.IntentID == "" {
		t.Fatalf("missing intentId; body = %s", rec.Body.String())
	}
	if !strings.Contains(body.Error, body.RejectReason) || body.RequestID == "" {
		t.Fatalf("body = %s, want error and requestId kept next to the intent fields", rec.Body.String())
	}
}

func TestSubmitAgentIntentHandler_replayedRejection_returnsSameBody(t *testing.T) {
	t.Parallel()
	// Arrange
	a := integrationAgentIntentApp(t)
	first := a.postBuy(2_000_000, "tick-1")

	// Act
	replay := a.postBuy(2_000_000, "tick-1")

	// Assert: only the request id belongs to the call and not to the intent.
	if first.Code != http.StatusUnprocessableEntity || replay.Code != first.Code {
		t.Fatalf("first = %d, replay = %d, want 422 twice", first.Code, replay.Code)
	}
	firstBody, replayBody := decodeAgentIntentErrorBody(t, first), decodeAgentIntentErrorBody(t, replay)
	if firstBody.IntentID == "" || firstBody.RejectReason == "" {
		t.Fatalf("first body = %s, want intentId and rejectReason", first.Body.String())
	}
	firstBody.RequestID, replayBody.RequestID = "", ""
	if replayBody != firstBody {
		t.Fatalf("replay body = %+v, want the first answer %+v", replayBody, firstBody)
	}
}

func TestSubmitAgentIntentHandler_pausedAgent_returnsRejectedWithoutIntent(t *testing.T) {
	t.Parallel()
	// Arrange
	a := integrationAgentIntentApp(t)
	pause, err := a.Governance.CreateProposal(context.Background(), app.CreateProposalInput{
		GroupID: a.GroupID, ProposerID: a.CreatorID, Kind: domain.ProposalKindPauseAgent,
	})
	if err != nil {
		t.Fatalf("create pause proposal: %v", err)
	}
	if _, err := a.Governance.CastVote(context.Background(), app.CastVoteInput{ProposalID: pause.ID, VoterID: a.CreatorID, Choice: domain.VoteYes}); err != nil {
		t.Fatalf("cast vote (pause): %v", err)
	}

	// Act
	rec := a.postBuy(500_000, "")

	// Assert: a paused agent's intent is refused before it is recorded, so there is no id.
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %s", rec.Code, rec.Body.String())
	}
	body := decodeAgentIntentErrorBody(t, rec)
	if body.Status != "rejected" || body.RejectReason != "agent is paused" || body.IntentID != "" {
		t.Fatalf("body = %s, want status rejected, the pause as reason and no intentId", rec.Body.String())
	}
	if body.Error != "agent is paused" || body.RequestID == "" {
		t.Fatalf("body = %s, want error and requestId kept", rec.Body.String())
	}
}

func TestSubmitAgentIntentHandler_failedSwap_returnsFailedIntentWithoutInternalError(t *testing.T) {
	t.Parallel()
	// Arrange: the quote routes, the execute call blows up.
	a := integrationAgentIntentApp(t)
	const usdc = 500_000
	requestID := "agent-intent-body-failed-swap-" + a.GroupID
	jupiter.RegisterQuoteBuy(a.Jupiter, jupiter.AAPLxMint, usdc, jupiter.BuyQuote{
		Routable:   true,
		OutputMint: jupiter.AAPLxMint,
		InAmount:   strconv.Itoa(usdc),
		OutAmount:  strconv.Itoa(usdc),
		RequestID:  requestID,
	})
	jupiter.RegisterExecuteError(a.Jupiter, requestID, errors.New("upstream secret detail"))

	// Act
	rec := a.postBuy(usdc, "tick-1")

	// Assert
	if rec.Code < 500 {
		t.Fatalf("status = %d, want 5xx; body = %s", rec.Code, rec.Body.String())
	}
	body := decodeAgentIntentErrorBody(t, rec)
	if body.Status != "failed" || body.IntentID == "" {
		t.Fatalf("body = %s, want status failed and the intent id", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "upstream secret detail") {
		t.Fatalf("body leaks the internal error: %s", rec.Body.String())
	}

	// The replay reports the same intent, status and reason (as a 200, per the replay rules).
	replay := a.postBuy(usdc, "tick-1")
	if replay.Code != http.StatusOK {
		t.Fatalf("replay status = %d, want 200; body = %s", replay.Code, replay.Body.String())
	}
	replayBody := decodeAgentIntentErrorBody(t, replay)
	if replayBody.IntentID != body.IntentID || replayBody.Status != body.Status || replayBody.RejectReason != body.RejectReason {
		t.Fatalf("replay = %s, first = %s; want the same intent, status and reason", replay.Body.String(), rec.Body.String())
	}
}

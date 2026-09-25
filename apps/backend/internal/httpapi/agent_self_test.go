package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
	"github.com/monaco/monaco/packages/domain"
)

// agentRoute sends one request to a key-only /v1/agent route through the mux, so path values
// and method routing are the ones production uses.
func (a agentIntentTestApp) agentRoute(method, path, key, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/agent", a.Handlers.AgentAccountHandler)
	mux.HandleFunc("GET /v1/agent/assets", a.Handlers.AgentAssetsHandler)
	mux.HandleFunc("POST /v1/agent/intents", a.Handlers.SubmitKeyAgentIntentHandler)
	mux.HandleFunc("GET /v1/agent/intents/{intentId}", a.Handlers.GetAgentIntentHandler)
	mux.HandleFunc("GET /v1/agent/skill.md", a.Handlers.AgentSkillHandler)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if key != "" {
		req.Header.Set(agentKeyHeader, key)
	}
	rec := httptest.NewRecorder()
	RequestID()(mux).ServeHTTP(rec, req)
	return rec
}

func (a agentIntentTestApp) postKeyIntent(body string) *httptest.ResponseRecorder {
	return a.agentRoute(http.MethodPost, "/v1/agent/intents", a.Key, body)
}

func (a agentIntentTestApp) vote(t *testing.T, kind domain.ProposalKind) {
	t.Helper()
	proposal, err := a.Governance.CreateProposal(context.Background(), app.CreateProposalInput{
		GroupID: a.GroupID, ProposerID: a.CreatorID, Kind: kind,
	})
	if err != nil {
		t.Fatalf("create %s proposal: %v", kind, err)
	}
	if _, err := a.Governance.CastVote(context.Background(), app.CastVoteInput{ProposalID: proposal.ID, VoterID: a.CreatorID, Choice: domain.VoteYes}); err != nil {
		t.Fatalf("cast vote (%s): %v", kind, err)
	}
}

// registerAAPLxBuyFill makes a buy of usdc micros fill for tokens atomics.
func registerAAPLxBuyFill(a agentIntentTestApp, usdc, tokens int64) {
	requestID := "agent-self-buy-" + a.GroupID + "-" + strconv.FormatInt(usdc, 10)
	jupiter.RegisterQuoteBuy(a.Jupiter, jupiter.AAPLxMint, usdc, jupiter.BuyQuote{
		Routable:   true,
		OutputMint: jupiter.AAPLxMint,
		InAmount:   strconv.FormatInt(usdc, 10),
		OutAmount:  strconv.FormatInt(tokens, 10),
		RequestID:  requestID,
	})
	jupiter.RegisterExecutePoll(a.Jupiter, requestID, []jupiter.ExecuteResult{{
		Status:             jupiter.ExecuteStatusSuccess,
		Signature:          "sig-" + requestID,
		InputAmountResult:  strconv.FormatInt(usdc, 10),
		OutputAmountResult: strconv.FormatInt(tokens, 10),
	}})
}

func decodeJSON[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return out
}

func TestAgentAccount_keyAloneResolvesTheCabal(t *testing.T) {
	t.Parallel()
	a := integrationAgentIntentApp(t)

	rec := a.agentRoute(http.MethodGet, "/v1/agent", a.Key, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	account := decodeJSON[agentAccountResponse](t, rec)
	if account.CabalName != "Quotes Fund" || account.AgentName != "Guard Bot" || account.Status != "active" {
		t.Fatalf("account = %+v, want the key's cabal and agent", account)
	}
	want := agentBudgetResponse{AllocationUsd: "1.00", AllocationUsdcMicros: 1_000_000, AvailableUsd: "1.00", AvailableUsdcMicros: 1_000_000}
	if account.Budget != want {
		t.Fatalf("budget = %+v, want %+v", account.Budget, want)
	}
	// $100 in the treasury, $1 of budget: cash is the smaller.
	if account.CashAvailableUsd != "1.00" || account.CashAvailableUsdcMicros != 1_000_000 {
		t.Fatalf("cash = %q / %d, want the $1 budget", account.CashAvailableUsd, account.CashAvailableUsdcMicros)
	}
	if len(account.Holdings) != 0 {
		t.Fatalf("holdings = %+v, want none before any trade", account.Holdings)
	}
	if account.Limits != (agentLimitsResponse{IntentsPerHour: 30, ReasonMaxChars: 280}) {
		t.Fatalf("limits = %+v", account.Limits)
	}
	if account.DocsURL != agentTestBaseURL+"/v1/agent/skill.md" {
		t.Fatalf("docsUrl = %q", account.DocsURL)
	}
}

func TestAgentRoutes_revokedKeyIsUnauthorizedOnEveryKeyedRoute(t *testing.T) {
	t.Parallel()
	a := integrationAgentIntentApp(t)
	// An intent to read back afterwards, recorded while the key was live.
	first := a.postKeyIntent(`{"side":"buy","symbol":"AAPLx","usd":"2","idempotencyKey":"before-revoke"}`)
	intentID := decodeAgentIntentErrorBody(t, first).IntentID
	if intentID == "" {
		t.Fatalf("setup intent: %d %s", first.Code, first.Body.String())
	}

	a.vote(t, domain.ProposalKindRevokeAgent)

	for _, route := range []struct{ method, path, body string }{
		{http.MethodGet, "/v1/agent", ""},
		{http.MethodGet, "/v1/agent/assets", ""},
		{http.MethodPost, "/v1/agent/intents", `{"side":"buy","symbol":"AAPLx","usd":"0.10"}`},
		{http.MethodGet, "/v1/agent/intents/" + intentID, ""},
	} {
		rec := a.agentRoute(route.method, route.path, a.Key, route.body)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s with a revoked key: status = %d, want 401; body = %s", route.method, route.path, rec.Code, rec.Body.String())
		}
	}
}

func TestAgentRoutes_pausedAgentReadsButCannotTrade(t *testing.T) {
	t.Parallel()
	a := integrationAgentIntentApp(t)
	a.vote(t, domain.ProposalKindPauseAgent)

	read := a.agentRoute(http.MethodGet, "/v1/agent", a.Key, "")
	if read.Code != http.StatusOK || decodeJSON[agentAccountResponse](t, read).Status != "paused" {
		t.Fatalf("paused read: %d %s, want 200 with status paused", read.Code, read.Body.String())
	}
	trade := a.postKeyIntent(`{"side":"buy","symbol":"AAPLx","usd":"0.50"}`)
	if trade.Code != http.StatusForbidden || decodeAgentIntentErrorBody(t, trade).RejectReason != "agent is paused" {
		t.Fatalf("paused intent: %d %s, want 403 agent is paused", trade.Code, trade.Body.String())
	}
}

func TestAgentIntentRoute_readsBackFillReasonAndHoldings(t *testing.T) {
	t.Parallel()
	a := integrationAgentIntentApp(t)
	pyth.RegisterAssetMark(a.Marks, "AAPLx", pyth.AssetMark{PriceUsdcMicros: 200_000_000})
	xstocks.RegisterCatalogAsset(a.Catalog, xstocks.CatalogAsset{Symbol: "AAPLx", Name: "Apple xStock", SolanaMint: jupiter.AAPLxMint, Routable: true})
	// $0.50 buys 0.0025 shares.
	registerAAPLxBuyFill(a, 500_000, 250_000)

	posted := a.postKeyIntent(`{"side":"buy","symbol":"AAPLx","usd":"0.50","idempotencyKey":"k1","reason":"breakout"}`)
	if posted.Code != http.StatusOK {
		t.Fatalf("post: %d %s", posted.Code, posted.Body.String())
	}
	intentID := decodeJSON[submitAgentIntentResponse](t, posted).IntentID

	rec := a.agentRoute(http.MethodGet, "/v1/agent/intents/"+intentID, a.Key, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("read: %d %s", rec.Code, rec.Body.String())
	}
	intent := decodeJSON[agentIntentResponse](t, rec)
	if intent.Status != "executed" || intent.Side != "buy" || intent.Symbol != "AAPLx" {
		t.Fatalf("intent = %+v, want an executed AAPLx buy", intent)
	}
	if intent.Reason == nil || *intent.Reason != "breakout" || intent.IdempotencyKey == nil || *intent.IdempotencyKey != "k1" {
		t.Fatalf("intent reason/key = %v/%v, want breakout/k1", intent.Reason, intent.IdempotencyKey)
	}
	if intent.UsdcMicros == nil || *intent.UsdcMicros != 500_000 || intent.TokenAmount != nil {
		t.Fatalf("intent amounts = %v/%v, want usdcMicros 500000 from usd 0.50", intent.UsdcMicros, intent.TokenAmount)
	}
	if intent.TxSignature == nil || *intent.TxSignature == "" || intent.TransactionID == nil {
		t.Fatalf("intent = %s, want the swap's transaction and signature", rec.Body.String())
	}
	if intent.FilledUsdcMicros == nil || *intent.FilledUsdcMicros != 500_000 || intent.FilledTokenAmount == nil || *intent.FilledTokenAmount != 250_000 {
		t.Fatalf("fill = %v usdc / %v tokens, want 500000 / 250000", intent.FilledUsdcMicros, intent.FilledTokenAmount)
	}

	account := decodeJSON[agentAccountResponse](t, a.agentRoute(http.MethodGet, "/v1/agent", a.Key, ""))
	if account.Budget.AvailableUsdcMicros != 500_000 {
		t.Fatalf("available = %d, want $1 - $0.50", account.Budget.AvailableUsdcMicros)
	}
	if len(account.Holdings) != 1 {
		t.Fatalf("holdings = %+v, want one AAPLx position", account.Holdings)
	}
	h := account.Holdings[0]
	if h.Symbol != "AAPLx" || h.Name != "Apple xStock" || h.Shares != "0.00250000" || h.SharesAtomic != 250_000 {
		t.Fatalf("holding = %+v", h)
	}
	if h.MarkUsd == nil || *h.MarkUsd != "200.00" || h.ValueUsd == nil || *h.ValueUsd != "0.50" || *h.ValueUsdcMicros != 500_000 {
		t.Fatalf("holding value = %s", rec.Body.String())
	}
}

func TestAgentIntentRoute_anotherAgentsIntentIsNotFound(t *testing.T) {
	t.Parallel()
	a := integrationAgentIntentApp(t)
	b := integrationAgentIntentApp(t)
	posted := a.postKeyIntent(`{"side":"buy","symbol":"AAPLx","usd":"2"}`)
	intentID := decodeAgentIntentErrorBody(t, posted).IntentID
	if intentID == "" {
		t.Fatalf("setup intent: %d %s", posted.Code, posted.Body.String())
	}

	if rec := a.agentRoute(http.MethodGet, "/v1/agent/intents/"+intentID, a.Key, ""); rec.Code != http.StatusOK {
		t.Fatalf("own intent: %d %s, want 200", rec.Code, rec.Body.String())
	}
	for _, id := range []string{intentID, "not-a-uuid", "00000000-0000-4000-8000-000000000000"} {
		if rec := a.agentRoute(http.MethodGet, "/v1/agent/intents/"+id, b.Key, ""); rec.Code != http.StatusNotFound {
			t.Errorf("intent %q read by another agent: %d %s, want 404", id, rec.Code, rec.Body.String())
		}
	}
}

func TestAgentIntentRoute_reasonIsStoredAndCapped(t *testing.T) {
	t.Parallel()
	a := integrationAgentIntentApp(t)
	reason := strings.Repeat("é", app.MaxAgentIntentReasonRunes)

	// Over the $1 budget, so it is recorded as rejected without a swap.
	posted := a.postKeyIntent(`{"side":"buy","symbol":"AAPLx","usd":"2","reason":"` + reason + `"}`)
	intentID := decodeAgentIntentErrorBody(t, posted).IntentID
	if posted.Code != http.StatusUnprocessableEntity || intentID == "" {
		t.Fatalf("post: %d %s", posted.Code, posted.Body.String())
	}
	intent := decodeJSON[agentIntentResponse](t, a.agentRoute(http.MethodGet, "/v1/agent/intents/"+intentID, a.Key, ""))
	if intent.Reason == nil || *intent.Reason != reason {
		t.Fatalf("reason = %v, want the 280 characters sent", intent.Reason)
	}
	if intent.Status != "rejected" || intent.RejectReason == nil || *intent.RejectReason != "trade exceeds agent allocation" {
		t.Fatalf("intent = %+v, want the allocation rejection", intent)
	}

	tooLong := a.postKeyIntent(`{"side":"buy","symbol":"AAPLx","usd":"0.10","reason":"` + reason + `x"}`)
	body := decodeAgentIntentErrorBody(t, tooLong)
	if tooLong.Code != http.StatusUnprocessableEntity || body.Status != "rejected" || body.IntentID != "" || !strings.Contains(body.RejectReason, "280") {
		t.Fatalf("281-character reason: %d %s, want 422 rejected before any intent", tooLong.Code, tooLong.Body.String())
	}
}

func TestAgentIntentRoute_amountFormErrorsAreUnprocessable(t *testing.T) {
	t.Parallel()
	a := integrationAgentIntentApp(t)
	for _, body := range []string{
		`{"side":"buy","symbol":"AAPLx","usd":"0.50","usdcMicros":500000}`,
		`{"side":"buy","symbol":"AAPLx"}`,
		`{"side":"buy","symbol":"AAPLx","usd":"0.0000001"}`,
		`{"side":"sell","symbol":"AAPLx","shares":"0.000000001"}`,
		`{"side":"sell","symbol":"AAPLx","usd":"1"}`,
	} {
		rec := a.postKeyIntent(body)
		got := decodeAgentIntentErrorBody(t, rec)
		if rec.Code != http.StatusUnprocessableEntity || got.Status != "rejected" || got.RejectReason == "" {
			t.Errorf("%s: %d %s, want 422 rejected with a reason", body, rec.Code, rec.Body.String())
		}
	}
}

func TestGroupIntentRoute_acceptsDecimalUsd(t *testing.T) {
	t.Parallel()
	a := integrationAgentIntentApp(t)
	registerAAPLxBuyFill(a, 750_000, 1_000)

	req := httptest.NewRequest(http.MethodPost, "/v1/groups/"+a.GroupID+"/agents/intents", strings.NewReader(`{"side":"buy","symbol":"AAPLx","usd":0.75}`))
	req.SetPathValue("id", a.GroupID)
	req.Header.Set(agentKeyHeader, a.Key)
	rec := httptest.NewRecorder()
	a.Handlers.SubmitAgentIntentHandler(rec, req)

	if rec.Code != http.StatusOK || decodeJSON[submitAgentIntentResponse](t, rec).Status != "executed" {
		t.Fatalf("group route buy of usd 0.75: %d %s, want executed", rec.Code, rec.Body.String())
	}
}

func TestAgentAssets_marksEachStockAndNullsAFailedMark(t *testing.T) {
	t.Parallel()
	a := integrationAgentIntentApp(t)
	for _, asset := range []xstocks.CatalogAsset{
		{Symbol: "AAPLx", Name: "Apple xStock", SolanaMint: jupiter.AAPLxMint, Routable: true},
		{Symbol: "TSLAx", Name: "Tesla xStock", SolanaMint: jupiter.TSLAxMint, Routable: true},
		{Symbol: "NOPEx", Name: "Unroutable xStock", SolanaMint: "MintNOPEx", Routable: false},
	} {
		xstocks.RegisterCatalogAsset(a.Catalog, asset)
	}
	xstocks.SetFakeCatalogRoutabilityProber(a.Catalog, unroutableMints{"MintNOPEx": true})
	pyth.RegisterAssetMark(a.Marks, "AAPLx", pyth.AssetMark{PriceUsdcMicros: 230_120_000})
	pyth.RegisterAssetMarkError(a.Marks, "TSLAx", errors.New("feed down"))

	rec := a.agentRoute(http.MethodGet, "/v1/agent/assets?limit=10", a.Key, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}
	raw := decodeJSON[struct {
		Assets []map[string]any `json:"assets"`
	}](t, rec)
	bySymbol := map[string]map[string]any{}
	for _, asset := range raw.Assets {
		bySymbol[asset["symbol"].(string)] = asset
	}
	if _, listed := bySymbol["NOPEx"]; listed || len(bySymbol) != 2 {
		t.Fatalf("assets = %s, want only the two routable stocks", rec.Body.String())
	}
	if bySymbol["AAPLx"]["markUsd"] != "230.12" || bySymbol["AAPLx"]["markUsdcMicros"] != float64(230_120_000) {
		t.Fatalf("AAPLx = %v, want its mark", bySymbol["AAPLx"])
	}
	tsla := bySymbol["TSLAx"]
	if markUsd, present := tsla["markUsd"]; !present || markUsd != nil || tsla["markUsdcMicros"] != nil {
		t.Fatalf("TSLAx = %v, want markUsd present and null", tsla)
	}
}

// unroutableMints reports every mint in it as having no swap route.
type unroutableMints map[string]bool

func (u unroutableMints) IsRoutable(_ context.Context, asset xstocks.CatalogAsset) bool {
	return !u[asset.SolanaMint]
}

func TestAgentRoutes_perAgentLimitAnswers429WithRetryAfter(t *testing.T) {
	t.Parallel()
	a := integrationAgentIntentApp(t)
	b := integrationAgentIntentApp(t)
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	limits := &AgentRateLimits{
		Intents: app.NewKeyedRateLimiter(1, time.Hour, clock),
		Reads:   app.NewKeyedRateLimiter(1, 30*time.Second, clock),
	}
	a.Handlers.Limits, b.Handlers.Limits = limits, limits

	if rec := a.postKeyIntent(`{"side":"buy","symbol":"AAPLx","usd":"2"}`); rec.Code == http.StatusTooManyRequests {
		t.Fatalf("first intent limited: %s", rec.Body.String())
	}
	over := a.postKeyIntent(`{"side":"buy","symbol":"AAPLx","usd":"2"}`)
	if over.Code != http.StatusTooManyRequests || over.Header().Get("Retry-After") != "3600" {
		t.Fatalf("second intent: %d Retry-After=%q, want 429 with 3600", over.Code, over.Header().Get("Retry-After"))
	}
	// The limit is the agent's own: another agent still trades.
	if rec := b.postKeyIntent(`{"side":"buy","symbol":"AAPLx","usd":"2"}`); rec.Code == http.StatusTooManyRequests {
		t.Fatalf("another agent limited by the first one's intents: %s", rec.Body.String())
	}

	if rec := a.agentRoute(http.MethodGet, "/v1/agent", a.Key, ""); rec.Code != http.StatusOK {
		t.Fatalf("first read: %d", rec.Code)
	}
	read := a.agentRoute(http.MethodGet, "/v1/agent/assets", a.Key, "")
	if read.Code != http.StatusTooManyRequests || read.Header().Get("Retry-After") != "30" {
		t.Fatalf("second read: %d Retry-After=%q, want 429 with 30", read.Code, read.Header().Get("Retry-After"))
	}
}

func TestAgentSkill_isPublicAndNamesTheBaseURL(t *testing.T) {
	t.Parallel()
	docs, err := app.NewAgentDocs("https://agents.monaco.example")
	if err != nil {
		t.Fatalf("docs: %v", err)
	}
	a := agentIntentTestApp{Handlers: &AgentHandlers{Docs: docs}}

	rec := a.agentRoute(http.MethodGet, "/v1/agent/skill.md", "", "")

	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/markdown; charset=utf-8" {
		t.Fatalf("status = %d, content type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	body := rec.Body.String()
	for _, want := range []string{"curl -s https://agents.monaco.example/v1/agent ", "https://agents.monaco.example/v1/agent/intents", "AAPLx", "idempotencyKey", "sell exceeds agent position", "Retry-After"} {
		if !strings.Contains(body, want) {
			t.Errorf("skill.md is missing %q", want)
		}
	}
	if strings.Contains(body, "{{") {
		t.Error("skill.md has an unrendered template action")
	}
}

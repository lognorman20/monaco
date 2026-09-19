package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

func TestPOST_proposals_happyPath_returnsProposalID(t *testing.T) {
	t.Parallel()

	proposalHandlers, groupHandlers, authHandlers, privyClient, jupiterClient, resolver, iso := integrationProposalsApp(t)
	token, groupID, _ := createGroupForQuotes(t, iso, groupHandlers, authHandlers, privyClient)
	xstocks.RegisterSolanaMint(resolver, "AAPLx", jupiter.AAPLxMint)
	jupiter.RegisterQuoteBuy(jupiterClient, jupiter.AAPLxMint, 5_000_000, jupiter.BuyQuote{
		Routable:   true,
		InputMint:  jupiter.USDCMint,
		OutputMint: jupiter.AAPLxMint,
		InAmount:   "5000000",
		OutAmount:  "2500000",
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/groups/"+groupID+"/proposals", strings.NewReader(`{"symbol":"AAPLx","usdc":5000000}`))
	req.SetPathValue("id", groupID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()

	proposalHandlers.CreateProposalHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var payload createProposalResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if payload.ProposalID == "" {
		t.Fatal("expected proposalId in response")
	}
}

func TestPOST_proposals_exceedsTreasuryUSDC_returns400(t *testing.T) {
	t.Parallel()

	proposalHandlers, groupHandlers, authHandlers, privyClient, jupiterClient, resolver, iso := integrationProposalsApp(t)
	token, groupID, _ := createGroupForQuotes(t, iso, groupHandlers, authHandlers, privyClient)
	treasury, found, err := proposalHandlers.Store.GetTreasuryByGroupID(context.Background(), groupID)
	if err != nil || !found {
		t.Fatalf("get treasury: found=%v err=%v", found, err)
	}
	privy.SetTreasuryUSDCBalance(privyClient, treasury.SolanaAddress, 1_000_000)
	xstocks.RegisterSolanaMint(resolver, "AAPLx", jupiter.AAPLxMint)
	jupiter.RegisterQuoteBuy(jupiterClient, jupiter.AAPLxMint, 5_000_000, jupiter.BuyQuote{
		Routable:   true,
		InputMint:  jupiter.USDCMint,
		OutputMint: jupiter.AAPLxMint,
		InAmount:   "5000000",
		OutAmount:  "2500000",
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/groups/"+groupID+"/proposals", strings.NewReader(`{"symbol":"AAPLx","usdc":5000000}`))
	req.SetPathValue("id", groupID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()
	proposalHandlers.CreateProposalHandler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
}

func TestPOST_quotes_routable_returnsOutputAndPrice(t *testing.T) {
	t.Parallel()

	quoteHandlers, groupHandlers, authHandlers, privyClient, jupiterClient, resolver, iso := integrationQuotesApp(t)
	token, groupID, _ := createGroupForQuotes(t, iso, groupHandlers, authHandlers, privyClient)
	xstocks.RegisterSolanaMint(resolver, "AAPLx", jupiter.AAPLxMint)
	jupiter.RegisterQuoteBuy(jupiterClient, jupiter.AAPLxMint, 5_000_000, jupiter.BuyQuote{
		Routable:   true,
		InputMint:  jupiter.USDCMint,
		OutputMint: jupiter.AAPLxMint,
		InAmount:   "5000000",
		OutAmount:  "2500000",
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/groups/"+groupID+"/quotes", strings.NewReader(`{"symbol":"AAPLx","usdc":5000000}`))
	req.SetPathValue("id", groupID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()
	quoteHandlers.QuoteHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var payload quoteResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if !payload.Routable {
		t.Fatal("expected routable=true")
	}
	if payload.OutputAmount != "2500000" {
		t.Fatalf("outputAmount = %q, want 2500000", payload.OutputAmount)
	}
	wantPrice := strconv.FormatInt((5_000_000*jupiter.XStockAtomicScale)/2_500_000, 10)
	if payload.PriceUsdcMicros != wantPrice {
		t.Fatalf("priceUsdcMicros = %q, want %q", payload.PriceUsdcMicros, wantPrice)
	}
}

func TestGET_groupProposals_openTab_returnsCreatedProposal(t *testing.T) {
	t.Parallel()

	proposalHandlers, groupHandlers, authHandlers, privyClient, jupiterClient, resolver, iso := integrationProposalsApp(t)
	token, groupID, _ := createGroupForQuotes(t, iso, groupHandlers, authHandlers, privyClient)
	xstocks.RegisterSolanaMint(resolver, "AAPLx", jupiter.AAPLxMint)
	jupiter.RegisterQuoteBuy(jupiterClient, jupiter.AAPLxMint, 5_000_000, jupiter.BuyQuote{
		Routable:   true,
		InputMint:  jupiter.USDCMint,
		OutputMint: jupiter.AAPLxMint,
		InAmount:   "5000000",
		OutAmount:  "2500000",
	})

	createReq := httptest.NewRequest(http.MethodPost, "/v1/groups/"+groupID+"/proposals", strings.NewReader(`{"symbol":"AAPLx","usdc":5000000}`))
	createReq.SetPathValue("id", groupID)
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+string(token))
	createRec := httptest.NewRecorder()
	proposalHandlers.CreateProposalHandler(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create status = %d, want 200; body = %s", createRec.Code, createRec.Body.String())
	}

	var created createProposalResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create json: %v", err)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/v1/groups/"+groupID+"/proposals?tab=open", nil)
	listReq.SetPathValue("id", groupID)
	listReq.Header.Set("Authorization", "Bearer "+string(token))
	listRec := httptest.NewRecorder()
	proposalHandlers.ListGroupProposalsHandler(listRec, listReq)

	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200; body = %s", listRec.Code, listRec.Body.String())
	}

	var payload listGroupProposalsResponse
	if err := json.Unmarshal(listRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode list json: %v", err)
	}
	if len(payload.Proposals) != 1 {
		t.Fatalf("proposals len = %d, want 1", len(payload.Proposals))
	}
	if payload.Proposals[0].ID != created.ProposalID {
		t.Fatalf("proposal id = %q, want %q", payload.Proposals[0].ID, created.ProposalID)
	}
	if payload.Proposals[0].ExpiresAt == "" {
		t.Fatal("expected expiresAt on open proposal")
	}
}

func TestGET_proposalDetail_returnsProposerAndVotes(t *testing.T) {
	t.Parallel()

	proposalHandlers, groupHandlers, authHandlers, privyClient, jupiterClient, resolver, iso := integrationProposalsApp(t)
	token, groupID, _ := createGroupForQuotes(t, iso, groupHandlers, authHandlers, privyClient)
	xstocks.RegisterSolanaMint(resolver, "AAPLx", jupiter.AAPLxMint)
	jupiter.RegisterQuoteBuy(jupiterClient, jupiter.AAPLxMint, 5_000_000, jupiter.BuyQuote{
		Routable:   true,
		InputMint:  jupiter.USDCMint,
		OutputMint: jupiter.AAPLxMint,
		InAmount:   "5000000",
		OutAmount:  "2500000",
	})

	createReq := httptest.NewRequest(http.MethodPost, "/v1/groups/"+groupID+"/proposals", strings.NewReader(`{"symbol":"AAPLx","usdc":5000000}`))
	createReq.SetPathValue("id", groupID)
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+string(token))
	createRec := httptest.NewRecorder()
	proposalHandlers.CreateProposalHandler(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create status = %d, want 200; body = %s", createRec.Code, createRec.Body.String())
	}

	var created createProposalResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create json: %v", err)
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/v1/proposals/"+created.ProposalID, nil)
	detailReq.SetPathValue("id", created.ProposalID)
	detailReq.Header.Set("Authorization", "Bearer "+string(token))
	detailRec := httptest.NewRecorder()
	proposalHandlers.GetProposalDetailHandler(detailRec, detailReq)

	if detailRec.Code != http.StatusOK {
		t.Fatalf("detail status = %d, want 200; body = %s", detailRec.Code, detailRec.Body.String())
	}

	var payload proposalDetailResponse
	if err := json.Unmarshal(detailRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode detail json: %v", err)
	}
	if payload.ID != created.ProposalID {
		t.Fatalf("id = %q, want %q", payload.ID, created.ProposalID)
	}
	if payload.ProposerName == "" {
		t.Fatal("expected proposerName")
	}
	if payload.CreatedAt == "" || payload.ExpiresAt == "" {
		t.Fatalf("expected createdAt and expiresAt, got created=%q expires=%q", payload.CreatedAt, payload.ExpiresAt)
	}
	if payload.VoteSummary.EligibleCount != 1 {
		t.Fatalf("eligibleCount = %d, want 1", payload.VoteSummary.EligibleCount)
	}
	if payload.Execution.State != "not_applicable" {
		t.Fatalf("execution.state = %q, want not_applicable", payload.Execution.State)
	}
}

func TestPOST_proposals_withThesis_roundTripsOnListAndDetail(t *testing.T) {
	t.Parallel()

	proposalHandlers, groupHandlers, authHandlers, privyClient, jupiterClient, resolver, iso := integrationProposalsApp(t)
	token, groupID, _ := createGroupForQuotes(t, iso, groupHandlers, authHandlers, privyClient)
	xstocks.RegisterSolanaMint(resolver, "AAPLx", jupiter.AAPLxMint)
	jupiter.RegisterQuoteBuy(jupiterClient, jupiter.AAPLxMint, 5_000_000, jupiter.BuyQuote{
		Routable:   true,
		InputMint:  jupiter.USDCMint,
		OutputMint: jupiter.AAPLxMint,
		InAmount:   "5000000",
		OutAmount:  "2500000",
	})

	createReq := httptest.NewRequest(
		http.MethodPost,
		"/v1/groups/"+groupID+"/proposals",
		strings.NewReader(`{"symbol":"AAPLx","usdc":5000000,"thesis":"Strong earnings beat, raising guidance."}`),
	)
	createReq.SetPathValue("id", groupID)
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+string(token))
	createRec := httptest.NewRecorder()
	proposalHandlers.CreateProposalHandler(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create status = %d, want 200; body = %s", createRec.Code, createRec.Body.String())
	}

	var created createProposalResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create json: %v", err)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/v1/groups/"+groupID+"/proposals?tab=open", nil)
	listReq.SetPathValue("id", groupID)
	listReq.Header.Set("Authorization", "Bearer "+string(token))
	listRec := httptest.NewRecorder()
	proposalHandlers.ListGroupProposalsHandler(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200; body = %s", listRec.Code, listRec.Body.String())
	}
	var listPayload listGroupProposalsResponse
	if err := json.Unmarshal(listRec.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode list json: %v", err)
	}
	if len(listPayload.Proposals) != 1 || listPayload.Proposals[0].Thesis != "Strong earnings beat, raising guidance." {
		t.Fatalf("list thesis mismatch: %+v", listPayload.Proposals)
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/v1/proposals/"+created.ProposalID, nil)
	detailReq.SetPathValue("id", created.ProposalID)
	detailReq.Header.Set("Authorization", "Bearer "+string(token))
	detailRec := httptest.NewRecorder()
	proposalHandlers.GetProposalDetailHandler(detailRec, detailReq)
	if detailRec.Code != http.StatusOK {
		t.Fatalf("detail status = %d, want 200; body = %s", detailRec.Code, detailRec.Body.String())
	}
	var detailPayload proposalDetailResponse
	if err := json.Unmarshal(detailRec.Body.Bytes(), &detailPayload); err != nil {
		t.Fatalf("decode detail json: %v", err)
	}
	if detailPayload.Thesis != "Strong earnings beat, raising guidance." {
		t.Fatalf("detail thesis = %q, want %q", detailPayload.Thesis, "Strong earnings beat, raising guidance.")
	}
}

func TestPOST_proposals_thesisTooLong_returns400(t *testing.T) {
	t.Parallel()

	proposalHandlers, groupHandlers, authHandlers, privyClient, _, resolver, iso := integrationProposalsApp(t)
	token, groupID, _ := createGroupForQuotes(t, iso, groupHandlers, authHandlers, privyClient)
	xstocks.RegisterSolanaMint(resolver, "AAPLx", jupiter.AAPLxMint)

	body, err := json.Marshal(map[string]any{
		"symbol": "AAPLx",
		"usdc":   5_000_000,
		"thesis": strings.Repeat("a", 501),
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/groups/"+groupID+"/proposals", strings.NewReader(string(body)))
	req.SetPathValue("id", groupID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()
	proposalHandlers.CreateProposalHandler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
}

func integrationProposalsApp(t *testing.T) (*ProposalHandlers, *GroupHandlers, *AuthHandlers, privy.Client, jupiter.Client, xstocks.Resolver, *postgres.TestIsolation) {
	t.Helper()

	quoteHandlers, groupHandlers, authHandlers, privyClient, jupiterClient, resolver, iso := integrationQuotesApp(t)
	groupHandlers.Governance.SetBuyService(quoteHandlers.Buy)
	return &ProposalHandlers{
		Store:      quoteHandlers.Store,
		Privy:      quoteHandlers.Privy,
		Governance: groupHandlers.Governance,
	}, groupHandlers, authHandlers, privyClient, jupiterClient, resolver, iso
}

package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/packages/domain"
)

func agentKeyRequest(groupID, key, remoteAddr string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/v1/groups/"+groupID+"/assets?limit=1", nil)
	req.SetPathValue("id", groupID)
	req.Header.Set(agentKeyHeader, key)
	req.RemoteAddr = remoteAddr
	return req
}

func TestAgentKeyGuard_throttlesWrongKeysPerGroupAndAddress(t *testing.T) {
	t.Parallel()

	catalogHandlers, groupHandlers, authHandlers, _, iso := integrationCatalogApp(t)
	catalogHandlers.KeyGuard = NewAgentKeyGuard()
	_, groupID, _ := createGroupForQuotes(t, iso, groupHandlers, authHandlers, catalogHandlers.Privy)

	for i := 0; i < agentKeyFailureBurst; i++ {
		rec := httptest.NewRecorder()
		catalogHandlers.SearchAssetsHandler(rec, agentKeyRequest(groupID, "zzzzz", "203.0.113.7:4000"))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("wrong key %d: status = %d, want 401", i+1, rec.Code)
		}
	}

	// Same group from a fresh address: the group allowance is spent.
	rec := httptest.NewRecorder()
	catalogHandlers.SearchAssetsHandler(rec, agentKeyRequest(groupID, "zzzzz", "198.51.100.9:4000"))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 once the group's wrong-key allowance is spent", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("429 without Retry-After")
	}

	// Same address against another group: the address allowance is spent too.
	rec = httptest.NewRecorder()
	catalogHandlers.SearchAssetsHandler(rec, agentKeyRequest("00000000-0000-4000-8000-000000000000", "zzzzz", "203.0.113.7:5000"))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 for an address that spent its allowance on another group", rec.Code)
	}
}

func TestAgentKeyGuard_validKeyNeverSpendsAndMismatchLooksLikeUnknownKey(t *testing.T) {
	t.Parallel()

	catalogHandlers, groupHandlers, authHandlers, _, iso := integrationCatalogApp(t)
	catalogHandlers.KeyGuard = NewAgentKeyGuard()
	token, groupID, creatorID := createGroupForQuotes(t, iso, groupHandlers, authHandlers, catalogHandlers.Privy)

	governance := groupHandlers.Governance
	proposal, err := governance.CreateProposal(context.Background(), app.CreateProposalInput{
		GroupID:              groupID,
		ProposerID:           creatorID,
		Kind:                 domain.ProposalKindAddAgent,
		AgentDisplayName:     "Guard Bot",
		AllocationUsdcMicros: 1_000_000,
	})
	if err != nil {
		t.Fatalf("create add agent proposal: %v", err)
	}
	if _, err := governance.CastVote(context.Background(), app.CastVoteInput{
		ProposalID: proposal.ID,
		VoterID:    creatorID,
		Choice:     domain.VoteYes,
	}); err != nil {
		t.Fatalf("cast vote: %v", err)
	}
	detail, err := governance.GetProposalDetail(context.Background(), string(token), proposal.ID)
	if err != nil {
		t.Fatalf("get proposal detail: %v", err)
	}
	key := detail.MintedAgentKey
	if key == "" {
		t.Fatal("expected minted agent key for proposer")
	}

	for i := 0; i < agentKeyFailureBurst*3; i++ {
		rec := httptest.NewRecorder()
		catalogHandlers.SearchAssetsHandler(rec, agentKeyRequest(groupID, key, "203.0.113.7:4000"))
		if rec.Code != http.StatusOK {
			t.Fatalf("valid key call %d: status = %d, want 200 (valid calls must not be throttled)", i+1, rec.Code)
		}
	}

	// A live key presented to the wrong group must be indistinguishable from an unknown key.
	otherGroup := "00000000-0000-4000-8000-000000000000"
	mismatch := httptest.NewRecorder()
	catalogHandlers.SearchAssetsHandler(mismatch, agentKeyRequest(otherGroup, key, "198.51.100.9:4000"))
	unknown := httptest.NewRecorder()
	catalogHandlers.SearchAssetsHandler(unknown, agentKeyRequest(otherGroup, "zzzzz", "198.51.100.10:4000"))
	if mismatch.Code != http.StatusUnauthorized || mismatch.Code != unknown.Code || mismatch.Body.String() != unknown.Body.String() {
		t.Fatalf("mismatch = %d %q, unknown = %d %q; want identical 401s", mismatch.Code, mismatch.Body.String(), unknown.Code, unknown.Body.String())
	}
}

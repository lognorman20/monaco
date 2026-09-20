package httpapi

import (
	"context"
	"fmt"
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
	catalogHandlers.KeyGuard = NewAgentKeyGuard(false)
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
	catalogHandlers.KeyGuard = NewAgentKeyGuard(false)
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

// wrongCurrentFormatKey has the shape of a minted key and matches no agent.
const wrongCurrentFormatKey = app.AgentKeyPrefix + "22222222222222222222222222222222"

func installGuardTestAgent(t *testing.T, governance *app.GovernanceService, token, groupID, creatorID string) string {
	t.Helper()
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
	detail, err := governance.GetProposalDetail(context.Background(), token, proposal.ID)
	if err != nil {
		t.Fatalf("get proposal detail: %v", err)
	}
	if detail.MintedAgentKey == "" {
		t.Fatal("expected minted agent key for proposer")
	}
	return detail.MintedAgentKey
}

// Ten bad keys aimed at a cabal used to lock its real bot out for as long as the attacker
// kept sending one a minute.
func TestAgentKeyGuard_validKeyPassesWhileGroupAllowanceIsSpentByBadKeys(t *testing.T) {
	t.Parallel()

	catalogHandlers, groupHandlers, authHandlers, _, iso := integrationCatalogApp(t)
	catalogHandlers.KeyGuard = NewAgentKeyGuard(false)
	token, groupID, creatorID := createGroupForQuotes(t, iso, groupHandlers, authHandlers, catalogHandlers.Privy)
	key := installGuardTestAgent(t, groupHandlers.Governance, string(token), groupID, creatorID)

	// Two attacker addresses, so neither address allowance hides what the group one does.
	attackers := []string{"203.0.113.7:4000", "203.0.113.8:4000"}
	badKeys := []string{"zzzzz", wrongCurrentFormatKey}
	for i := 0; i < agentKeyFailureBurst*2; i++ {
		rec := httptest.NewRecorder()
		catalogHandlers.SearchAssetsHandler(rec, agentKeyRequest(groupID, badKeys[i%2], attackers[i%2]))
		if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusTooManyRequests {
			t.Fatalf("bad key %d: status = %d, want 401 or 429", i+1, rec.Code)
		}
	}

	// The group's allowance is spent: a short guess from a fresh address is refused...
	rec := httptest.NewRecorder()
	catalogHandlers.SearchAssetsHandler(rec, agentKeyRequest(groupID, "zzzzz", "198.51.100.9:4000"))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("short guess once the group allowance is spent: status = %d, want 429", rec.Code)
	}
	// ...and the cabal's bot, from its own address, is not.
	for i := 0; i < 3; i++ {
		rec = httptest.NewRecorder()
		catalogHandlers.SearchAssetsHandler(rec, agentKeyRequest(groupID, key, "192.0.2.50:4000"))
		if rec.Code != http.StatusOK {
			t.Fatalf("valid key call %d while the group allowance is spent: status = %d, want 200", i+1, rec.Code)
		}
	}
}

// An address that burned its allowance gets no answer about any key, right or wrong:
// a 200 for the right one would make the throttle an oracle.
func TestAgentKeyGuard_spentAddressIsRefusedBeforeItsKeyIsChecked(t *testing.T) {
	t.Parallel()

	catalogHandlers, groupHandlers, authHandlers, _, iso := integrationCatalogApp(t)
	catalogHandlers.KeyGuard = NewAgentKeyGuard(false)
	token, groupID, creatorID := createGroupForQuotes(t, iso, groupHandlers, authHandlers, catalogHandlers.Privy)
	key := installGuardTestAgent(t, groupHandlers.Governance, string(token), groupID, creatorID)

	otherGroup := "00000000-0000-4000-8000-000000000000"
	for i := 0; i < agentKeyFailureBurst; i++ {
		rec := httptest.NewRecorder()
		catalogHandlers.SearchAssetsHandler(rec, agentKeyRequest(otherGroup, wrongCurrentFormatKey, "203.0.113.7:4000"))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("wrong key %d: status = %d, want 401", i+1, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	catalogHandlers.SearchAssetsHandler(rec, agentKeyRequest(groupID, key, "203.0.113.7:5000"))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("right key from a spent address: status = %d, want 429", rec.Code)
	}
}

// Behind a proxy every caller shares RemoteAddr. The guard follows the same
// TRUST_PROXY_HEADERS switch as the request rate limiter.
func TestAgentKeyGuard_addressFollowsTrustedProxyHeader(t *testing.T) {
	t.Parallel()

	groupA := "00000000-0000-4000-8000-00000000000a"
	groupB := "00000000-0000-4000-8000-00000000000b"
	forwarded := func(groupID, clientAddr string) *http.Request {
		req := agentKeyRequest(groupID, wrongCurrentFormatKey, "10.0.0.1:4000")
		req.Header.Set("X-Forwarded-For", clientAddr)
		return req
	}

	trusting := NewAgentKeyGuard(true)
	for i := 0; i < agentKeyFailureBurst; i++ {
		trusting.recordFailure(forwarded(groupA, "203.0.113.7"), groupA, app.ErrInvalidAgentAPIKey)
	}
	if over, _ := trusting.blocked(forwarded(groupB, "203.0.113.7"), groupB, wrongCurrentFormatKey); !over {
		t.Fatal("the client that spent its allowance is not blocked behind the proxy")
	}
	if over, _ := trusting.blocked(forwarded(groupB, "198.51.100.9"), groupB, wrongCurrentFormatKey); over {
		t.Fatal("another client behind the same proxy is blocked by its neighbour's failures")
	}

	// Without a trusted proxy the header is caller-controlled and must be ignored.
	direct := NewAgentKeyGuard(false)
	for i := 0; i < agentKeyFailureBurst; i++ {
		direct.recordFailure(forwarded(groupA, fmt.Sprintf("203.0.113.%d", i)), groupA, app.ErrInvalidAgentAPIKey)
	}
	if over, _ := direct.blocked(forwarded(groupB, "198.51.100.9"), groupB, wrongCurrentFormatKey); !over {
		t.Fatal("rotating X-Forwarded-For dodged the address allowance")
	}
}

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/auth"
	"github.com/monaco/monaco/apps/backend/internal/chainlink"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
	"github.com/monaco/monaco/packages/domain"
)

type assetSocialHarness struct {
	social   *AssetSocialHandlers
	auth     *AuthHandlers
	groups   *GroupHandlers
	wallets  wallets.Client
	verifier auth.Verifier
	store    *postgres.Store
	iso      *postgres.TestIsolation
}

func newAssetSocialHarness(t *testing.T) assetSocialHarness {
	t.Helper()
	authHandlers, walletClient, db, iso := integrationApp(t)
	store := postgres.NewStore(db)
	verifier := authHandlers.Verifier
	marksClient := chainlink.NewFakeClient()
	symbols := app.NewSymbolResolver(nil)
	deposits := app.NewDepositService(store, verifier, walletClient, marksClient, symbols)
	home := app.NewHomeService(store, verifier, walletClient, marksClient, deposits, symbols)
	return assetSocialHarness{
		social: &AssetSocialHandlers{Home: home},
		auth:   authHandlers,
		groups: &GroupHandlers{
			Groups:     app.NewGroupService(store, verifier, walletClient),
			Governance: app.NewGovernanceService(store, verifier, walletClient),
		},
		wallets:  walletClient,
		verifier: verifier,
		store:    store,
		iso:      iso,
	}
}

func (h assetSocialHarness) get(t *testing.T, token auth.AccessToken, symbol string) (*httptest.ResponseRecorder, assetSocialResponse) {
	t.Helper()
	// A fixed target with the symbol set as a path value: the handler reads the path
	// value, and a symbol that is not URL-safe is exactly the case being tested.
	req := httptest.NewRequest(http.MethodGet, "/v1/assets/symbol/social", nil)
	req.SetPathValue("symbol", symbol)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+string(token))
	}
	rec := httptest.NewRecorder()
	h.social.GetAssetSocialHandler(rec, req)

	var payload assetSocialResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode social json: %v; body = %s", err, rec.Body.String())
		}
	}
	return rec, payload
}

// seedSymbolProposal opens a vote about symbol in groupID and casts one yes ballot.
func (h assetSocialHarness) seedSymbolProposal(t *testing.T, groupID, proposerID, symbol string) string {
	t.Helper()
	ctx := context.Background()
	tx, err := h.store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	proposal, err := h.store.InsertProposalTx(ctx, tx, postgres.InsertProposalParams{
		GroupID:    groupID,
		ProposerID: proposerID,
		Symbol:     symbol,
		Kind:       domain.ProposalKindBuy,
		UsdcMicros: 5_000_000,
		Thesis:     "Earnings next week",
		ExpiresAt:  time.Now().UTC().Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("InsertProposalTx: %v", err)
	}
	if _, _, err := h.store.InsertVoteTx(ctx, tx, proposal.ID, proposerID, domain.VoteYes); err != nil {
		t.Fatalf("InsertVoteTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return proposal.ID
}

func (h assetSocialHarness) createGroup(t *testing.T, token auth.AccessToken, name string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"`+name+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()
	h.groups.CreateGroupHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create group status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var created createGroupResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create group json: %v", err)
	}
	trackCreatedGroup(h.iso, created.GroupID)
	return created.GroupID
}

func TestGET_assetSocial_showsOwnCabalsOpenVoteAndActivity(t *testing.T) {
	t.Parallel()

	h := newAssetSocialHarness(t)
	session, token := seedAuthenticatedUser(t, h.iso, h.auth, h.wallets, "social-member", "Social Member")
	groupID := h.createGroup(t, token, "Weekend investors")
	proposalID := h.seedSymbolProposal(t, groupID, session.UserID, "AAPLc")

	rec, payload := h.get(t, token, "AAPLc")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	if len(payload.OpenProposals) != 1 {
		t.Fatalf("openProposals = %d, want 1; body = %s", len(payload.OpenProposals), rec.Body.String())
	}
	open := payload.OpenProposals[0]
	if open.ID != proposalID {
		t.Fatalf("proposal id = %q, want %q", open.ID, proposalID)
	}
	if open.GroupName != "Weekend investors" {
		t.Fatalf("groupName = %q, want the cabal's name", open.GroupName)
	}
	if open.Yes != 1 || open.No != 0 {
		t.Fatalf("tally = %d/%d, want 1 yes and 0 no", open.Yes, open.No)
	}
	if open.MyVote != string(domain.VoteYes) {
		t.Fatalf("myVote = %q, want yes", open.MyVote)
	}
	if len(open.Voters) != 1 || open.Voters[0].DisplayName != "Social Member" {
		t.Fatalf("voters = %+v, want the one member who voted, by name", open.Voters)
	}
	if open.MemberCount != 1 {
		t.Fatalf("memberCount = %d, want 1", open.MemberCount)
	}

	if len(payload.Activity) != 1 || payload.Activity[0].ID != proposalID {
		t.Fatalf("activity = %+v, want the proposal", payload.Activity)
	}
	if payload.Activity[0].Kind != app.AssetActivityProposed {
		t.Fatalf("activity kind = %q, want %q", payload.Activity[0].Kind, app.AssetActivityProposed)
	}
}

// The authz case issue #341 asks for: another cabal's vote on the same symbol must
// never appear in a non-member's answer.
func TestGET_assetSocial_neverLeaksAnotherCabalsVote(t *testing.T) {
	t.Parallel()

	h := newAssetSocialHarness(t)
	insiderSession, insiderToken := seedAuthenticatedUser(t, h.iso, h.auth, h.wallets, "social-insider", "Insider")
	insiderGroup := h.createGroup(t, insiderToken, "Private cabal")
	h.seedSymbolProposal(t, insiderGroup, insiderSession.UserID, "AAPLc")

	// An outsider with a cabal of their own, and no vote on this symbol anywhere.
	_, outsiderToken := seedAuthenticatedUser(t, h.iso, h.auth, h.wallets, "social-outsider", "Outsider")
	h.createGroup(t, outsiderToken, "Other cabal")

	rec, payload := h.get(t, outsiderToken, "AAPLc")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if len(payload.OpenProposals) != 0 {
		t.Fatalf("openProposals = %+v, want none: the outsider is not in that cabal", payload.OpenProposals)
	}
	if len(payload.Activity) != 0 {
		t.Fatalf("activity = %+v, want none", payload.Activity)
	}
	if len(payload.Holdings) != 0 || payload.HolderCount != 0 {
		t.Fatalf("holdings = %+v, holderCount = %d, want empty", payload.Holdings, payload.HolderCount)
	}
}

// A member with no cabals gets a well-formed empty answer, not a 404 and not nulls:
// the card hides itself on empty, and a null array would crash the decoder.
func TestGET_assetSocial_noCabals_returnsEmptyLists(t *testing.T) {
	t.Parallel()

	h := newAssetSocialHarness(t)
	_, token := seedAuthenticatedUser(t, h.iso, h.auth, h.wallets, "social-lonely", "Lonely")

	rec, _ := h.get(t, token, "AAPLc")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`"holdings":[]`, `"openProposals":[]`, `"activity":[]`, `"holderCount":0`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body = %s, want it to contain %s", body, want)
		}
	}
}

func TestGET_assetSocial_missingToken_returns401(t *testing.T) {
	t.Parallel()

	h := newAssetSocialHarness(t)
	rec, _ := h.get(t, "", "AAPLc")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestGET_assetSocial_symbolIsValidatedServerSide(t *testing.T) {
	t.Parallel()

	h := newAssetSocialHarness(t)
	_, token := seedAuthenticatedUser(t, h.iso, h.auth, h.wallets, "social-validate", "Validate")

	for _, symbol := range []string{"AAPL x", "'; DROP TABLE proposals; --", "AAAAAAAAAAAAAAAAAAAAAAAA"} {
		rec, _ := h.get(t, token, symbol)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("symbol %q status = %d, want 400", symbol, rec.Code)
		}
	}
}

func TestIsPlausibleTicker(t *testing.T) {
	t.Parallel()

	for _, ok := range []string{"AAPLc", "aaplc", "BRK.B", "TSLAc", "X", "SPY-1"} {
		if !isPlausibleTicker(ok) {
			t.Fatalf("isPlausibleTicker(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "AAPL x", "AAPL/USD", "AAPL;", "ünicode", "0123456789ABCDEFG"} {
		if isPlausibleTicker(bad) {
			t.Fatalf("isPlausibleTicker(%q) = true, want false", bad)
		}
	}
}

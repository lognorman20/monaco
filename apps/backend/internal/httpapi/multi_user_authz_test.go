package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// Cross-user authorization (issue #147): a signed-in user who is not in a club must
// not read its private surfaces or act on it, and a member must not act outside the
// rights the club's rules give them.

func TestMultiUserAuthz_nonMember_cannotReadOrMutateClub(t *testing.T) {
	t.Parallel()

	// Arrange: Alice and Bob share a funded club with an open proposal; Carol is outside it.
	a := newMultiUserApp(t)
	alice := a.signIn(t, "authz-alice", "Alice")
	bob := a.signIn(t, "authz-bob", "Bob")
	carol := a.signIn(t, "authz-carol", "Carol")
	a.topUpBalance(t, carol, usdc10) // Carol can afford to fund; membership alone must stop her.
	c := a.createClub(t, alice, "Private Club")
	a.join(t, bob, c)
	aliceDeposit := a.deposit(t, alice, c, usdc10)
	a.registerRoutableAAPLx(usdc5)
	proposalID := a.propose(t, alice, c, usdc5)

	groupPath := "/v1/groups/" + c.ID
	proposalBody := `{"symbol":"AAPLx","usdc":5000000}`
	cases := []struct {
		name    string
		handler http.HandlerFunc
		method  string
		target  string
		body    string
		pathKey string
		pathVal string
		want    int
	}{
		{"GET /v1/groups/{id}", a.Groups.GetGroupHandler, http.MethodGet, groupPath, "", "id", c.ID, http.StatusNotFound},
		{"GET /v1/groups/{id}/view", a.Groups.GetGroupViewHandler, http.MethodGet, groupPath + "/view", "", "id", c.ID, http.StatusNotFound},
		{"GET /v1/groups/{id}/proposals", a.Proposals.ListGroupProposalsHandler, http.MethodGet, groupPath + "/proposals?tab=open", "", "id", c.ID, http.StatusNotFound},
		{"GET /v1/groups/{id}/treasury/usdc", a.Deposits.GetTreasuryUsdcBalanceHandler, http.MethodGet, groupPath + "/treasury/usdc", "", "id", c.ID, http.StatusNotFound},
		{"GET /v1/groups/{id}/assets", a.Catalog.SearchAssetsHandler, http.MethodGet, groupPath + "/assets?query=AAPL", "", "id", c.ID, http.StatusForbidden},
		{"GET /v1/proposals/{id}", a.Proposals.GetProposalDetailHandler, http.MethodGet, "/v1/proposals/" + proposalID, "", "id", proposalID, http.StatusNotFound},
		{"GET /v1/deposits/{id}", a.Deposits.GetDepositHandler, http.MethodGet, "/v1/deposits/" + aliceDeposit.DepositID, "", "id", aliceDeposit.DepositID, http.StatusNotFound},
		{"POST /v1/groups/{id}/fund", a.Deposits.FundGroupHandler, http.MethodPost, groupPath + "/fund", `{"amount":1000000}`, "id", c.ID, http.StatusForbidden},
		{"POST /v1/groups/{id}/quotes", a.Quotes.QuoteHandler, http.MethodPost, groupPath + "/quotes", proposalBody, "id", c.ID, http.StatusForbidden},
		{"POST /v1/groups/{id}/proposals", a.Proposals.CreateProposalHandler, http.MethodPost, groupPath + "/proposals", proposalBody, "id", c.ID, http.StatusForbidden},
		{"POST /v1/proposals/{id}/votes", a.Proposals.CastVoteHandler, http.MethodPost, "/v1/proposals/" + proposalID + "/votes", `{"choice":"yes"}`, "id", proposalID, http.StatusForbidden},
		{"GET /v1/groups/{id}/join-requests", a.Groups.ListJoinRequestsHandler, http.MethodGet, groupPath + "/join-requests", "", "id", c.ID, http.StatusForbidden},
	}

	// Act + Assert: every club-scoped route refuses Carol.
	for _, tc := range cases {
		rec := a.call(t, tc.handler, tc.method, tc.target, carol.Token, tc.body, tc.pathKey, tc.pathVal)
		requireStatus(t, rec, tc.want, "Carol "+tc.name)
	}

	// Carol's refused vote recorded no ballot and did not move the tally.
	detail := a.proposalDetail(t, alice, proposalID)
	if detail.Status != "open" || detail.VoteSummary.YesCount != 0 || len(detail.Votes) != 0 {
		t.Fatalf("after Carol's vote attempt: status=%s summary=%+v votes=%+v, want untouched", detail.Status, detail.VoteSummary, detail.Votes)
	}
	// Carol's refused deposit left no pending row that the sweep poller would pick up.
	if pending, err := a.Store.HasPendingDepositsForGroup(context.Background(), c.ID); err != nil || pending {
		t.Fatalf("pending deposits for club after Carol's attempt = %v (err %v), want none", pending, err)
	}

	// Carol's home surfaces carry nothing from Alice and Bob's club.
	carolHome := a.home(t, carol)
	requireNoPerson(t, carolHome.People, alice, "Carol /v1/home people")
	requireNoPerson(t, carolHome.People, bob, "Carol /v1/home people")
	if row := findHomeGroupRow(t, carolHome.Groups, c.ID); row.IsJoined {
		t.Fatal("Carol /v1/home: club isJoined = true, want false")
	}
	carolDash := a.dashboard(t, carol)
	if len(carolDash.MyGroups) != 0 || len(carolDash.Leaderboard.People) != 0 || len(carolDash.MissedProposals) != 0 {
		t.Fatalf("Carol dashboard = myGroups %d, leaderboard %d, missed %d; want all empty",
			len(carolDash.MyGroups), len(carolDash.Leaderboard.People), len(carolDash.MissedProposals))
	}

	// Bob, a member, still sees the proposal as missed until he votes.
	if !missedProposal(a.dashboard(t, bob).MissedProposals, proposalID) {
		t.Fatal("Bob dashboard missedProposals lacks the open proposal")
	}
	requireStatus(t, a.vote(t, bob, proposalID, "no"), http.StatusNoContent, "Bob POST /v1/proposals/{id}/votes")
	if missedProposal(a.dashboard(t, bob).MissedProposals, proposalID) {
		t.Fatal("Bob dashboard still lists the proposal after voting")
	}
}

func TestMultiUserAuthz_member_cannotActOutsideClubRules(t *testing.T) {
	t.Parallel()

	// Arrange: Alice's club names only Alice as a voter; Bob joins and is funded.
	a := newMultiUserApp(t)
	alice := a.signIn(t, "rules-alice", "Alice")
	bob := a.signIn(t, "rules-bob", "Bob")
	body, err := json.Marshal(map[string]any{
		"name":     "Named Voters " + a.ISO.Suffix(),
		"voterSet": map[string]any{"mode": "named_subset", "memberIds": []string{alice.UserID}},
	})
	if err != nil {
		t.Fatalf("marshal create body: %v", err)
	}
	createRec := a.call(t, a.Groups.CreateGroupHandler, http.MethodPost, "/v1/groups", alice.Token, string(body))
	requireStatus(t, createRec, http.StatusOK, "Alice POST /v1/groups (named voters)")
	created := decodeBody[createGroupResponse](t, createRec)
	trackCreatedGroup(a.ISO, created.GroupID)
	c := club{ID: created.GroupID, Name: created.Name, TreasuryAddress: created.TreasuryAddress}
	a.join(t, bob, c)
	a.deposit(t, bob, c, usdc10)
	a.registerRoutableAAPLx(usdc5)
	proposalID := a.propose(t, alice, c, usdc5)

	// Act + Assert: Bob is a funded member but not a named voter or the admin.
	requireStatus(t, a.vote(t, bob, proposalID, "yes"), http.StatusForbidden, "Bob vote outside voter set")
	requireStatus(t,
		a.call(t, a.Proposals.CreateProposalHandler, http.MethodPost, "/v1/groups/"+c.ID+"/proposals", bob.Token, `{"symbol":"AAPLx","usdc":5000000}`, "id", c.ID),
		http.StatusForbidden, "Bob propose outside voter set")
	requireStatus(t,
		a.call(t, a.Groups.ListJoinRequestsHandler, http.MethodGet, "/v1/groups/"+c.ID+"/join-requests", bob.Token, "", "id", c.ID),
		http.StatusForbidden, "Bob list join requests as non-admin")

	detail := a.proposalDetail(t, bob, proposalID)
	if detail.CanVote {
		t.Fatal("Bob canVote = true outside the named voter set")
	}
	if detail.VoteSummary.EligibleCount != 1 || len(detail.Votes) != 0 {
		t.Fatalf("summary=%+v votes=%+v, want 1 eligible and no ballots", detail.VoteSummary, detail.Votes)
	}

	// Alice, the only named voter, decides alone.
	requireStatus(t, a.vote(t, alice, proposalID, "yes"), http.StatusNoContent, "Alice vote")
	if status := a.proposalDetail(t, bob, proposalID).Status; status != "passed" {
		t.Fatalf("status = %s, want passed on the sole named voter's yes", status)
	}
}

func missedProposal(rows []homeMissedProposalRowResponse, proposalID string) bool {
	for _, row := range rows {
		if row.ProposalID == proposalID {
			return true
		}
	}
	return false
}

package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
	"github.com/monaco/monaco/packages/domain"
)

// commentFixture is one cabal with a proposer (Ada), a second member (Ben), an outsider (Cy), and one open proposal.
type commentFixture struct {
	handlers    *ProposalHandlers
	groupID     string
	proposalID  string
	adaToken    privy.AccessToken
	benToken    privy.AccessToken
	cyToken     privy.AccessToken
	newProposal func() string
}

func newCommentFixture(t *testing.T) commentFixture {
	t.Helper()

	proposalHandlers, groupHandlers, authHandlers, privyClient, jupiterClient, resolver, iso := integrationProposalsApp(t)
	adaToken, groupID, _ := createGroupForQuotes(t, iso, groupHandlers, authHandlers, privyClient)
	xstocks.RegisterSolanaMint(resolver, "AAPLx", jupiter.AAPLxMint)
	jupiter.RegisterQuoteBuy(jupiterClient, jupiter.AAPLxMint, 5_000_000, jupiter.BuyQuote{
		Routable:   true,
		InputMint:  jupiter.USDCMint,
		OutputMint: jupiter.AAPLxMint,
		InAmount:   "5000000",
		OutAmount:  "2500000",
	})

	_, benToken := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "ben", "Ben Ortiz")
	_, cyToken := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "cy", "Cy Park")

	joinReq := httptest.NewRequest(http.MethodPost, "/v1/groups/"+groupID+"/join", strings.NewReader(`{}`))
	joinReq.SetPathValue("id", groupID)
	joinReq.Header.Set("Content-Type", "application/json")
	joinReq.Header.Set("Authorization", "Bearer "+string(benToken))
	joinRec := httptest.NewRecorder()
	groupHandlers.JoinGroupHandler(joinRec, joinReq)
	if joinRec.Code != http.StatusNoContent {
		t.Fatalf("join status = %d, want 204; body = %s", joinRec.Code, joinRec.Body.String())
	}

	newProposal := func() string {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/v1/groups/"+groupID+"/proposals", strings.NewReader(`{"symbol":"AAPLx","usdc":5000000}`))
		req.SetPathValue("id", groupID)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+string(adaToken))
		rec := httptest.NewRecorder()
		proposalHandlers.CreateProposalHandler(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("create proposal status = %d, want 200; body = %s", rec.Code, rec.Body.String())
		}
		var created createProposalResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
			t.Fatalf("decode create proposal: %v", err)
		}
		return created.ProposalID
	}

	return commentFixture{
		handlers:    proposalHandlers,
		groupID:     groupID,
		proposalID:  newProposal(),
		adaToken:    adaToken,
		benToken:    benToken,
		cyToken:     cyToken,
		newProposal: newProposal,
	}
}

func (f commentFixture) postComment(t *testing.T, token privy.AccessToken, proposalID, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/proposals/"+proposalID+"/comments", strings.NewReader(body))
	req.SetPathValue("id", proposalID)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+string(token))
	}
	rec := httptest.NewRecorder()
	f.handlers.CreateProposalCommentHandler(rec, req)
	return rec
}

func (f commentFixture) listComments(t *testing.T, token privy.AccessToken, proposalID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/proposals/"+proposalID+"/comments", nil)
	req.SetPathValue("id", proposalID)
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()
	f.handlers.ListProposalCommentsHandler(rec, req)
	return rec
}

func (f commentFixture) castVote(t *testing.T, token privy.AccessToken, proposalID, choice string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/proposals/"+proposalID+"/votes", strings.NewReader(`{"choice":"`+choice+`"}`))
	req.SetPathValue("id", proposalID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()
	f.handlers.CastVoteHandler(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("vote status = %d, want 204; body = %s", rec.Code, rec.Body.String())
	}
}

func (f commentFixture) listOpenProposals(t *testing.T, token privy.AccessToken) listGroupProposalsResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/groups/"+f.groupID+"/proposals?tab=open", nil)
	req.SetPathValue("id", f.groupID)
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()
	f.handlers.ListGroupProposalsHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list proposals status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var payload listGroupProposalsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode list proposals: %v", err)
	}
	return payload
}

func decodeComment(t *testing.T, rec *httptest.ResponseRecorder) proposalCommentResponse {
	t.Helper()
	var payload proposalCommentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode comment: %v; body = %s", err, rec.Body.String())
	}
	return payload
}

func jsonCommentBody(t *testing.T, body, parentID string) string {
	t.Helper()
	raw, err := json.Marshal(createProposalCommentRequest{Body: body, ParentID: parentID})
	if err != nil {
		t.Fatalf("marshal comment body: %v", err)
	}
	return string(raw)
}

func TestPOST_proposalComments_memberComment_returns201WithAuthor(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newCommentFixture(t)

	// Act
	rec := f.postComment(t, f.benToken, f.proposalID, jsonCommentBody(t, "  Apple into earnings feels early.  ", ""))

	// Assert
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body = %s", rec.Code, rec.Body.String())
	}
	comment := decodeComment(t, rec)
	if comment.Body != "Apple into earnings feels early." {
		t.Fatalf("body = %q, want trimmed text", comment.Body)
	}
	if comment.AuthorName != "Ben Ortiz" || comment.ProposalID != f.proposalID || comment.ParentID != "" {
		t.Fatalf("unexpected comment: %+v", comment)
	}
	if comment.ID == "" || comment.CreatedAt == "" {
		t.Fatalf("expected id and createdAt: %+v", comment)
	}
}

func TestGET_proposalComments_replyThread_returnsOldestFirstWithParentIDs(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newCommentFixture(t)
	top := decodeComment(t, f.postComment(t, f.benToken, f.proposalID, jsonCommentBody(t, "Why Apple over Nvidia?", "")))
	reply := decodeComment(t, f.postComment(t, f.adaToken, f.proposalID, jsonCommentBody(t, "Lower drawdown for a first buy.", top.ID)))

	// Act
	rec := f.listComments(t, f.adaToken, f.proposalID)

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var payload listProposalCommentsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(payload.Comments) != 2 {
		t.Fatalf("comments len = %d, want 2", len(payload.Comments))
	}
	if payload.Comments[0].ID != top.ID || payload.Comments[1].ID != reply.ID {
		t.Fatalf("order = [%s %s], want [%s %s]", payload.Comments[0].ID, payload.Comments[1].ID, top.ID, reply.ID)
	}
	if payload.Comments[1].ParentID != top.ID {
		t.Fatalf("reply parentId = %q, want %q", payload.Comments[1].ParentID, top.ID)
	}
	if payload.Comments[1].AuthorName != "Quotes User" {
		t.Fatalf("reply authorName = %q, want proposer display name", payload.Comments[1].AuthorName)
	}
}

func TestGET_proposalComments_noComments_returnsEmptyArray(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newCommentFixture(t)

	// Act
	rec := f.listComments(t, f.benToken, f.proposalID)

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != `{"comments":[]}` {
		t.Fatalf("body = %s, want empty comments array", rec.Body.String())
	}
}

func TestPOST_proposalComments_nonMember_returns404(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newCommentFixture(t)

	// Act
	rec := f.postComment(t, f.cyToken, f.proposalID, jsonCommentBody(t, "Let me in on this.", ""))

	// Assert
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
	}
}

func TestGET_proposalComments_nonMember_returns404(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newCommentFixture(t)
	f.postComment(t, f.benToken, f.proposalID, jsonCommentBody(t, "Members only thought.", ""))

	// Act
	rec := f.listComments(t, f.cyToken, f.proposalID)

	// Assert
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "Members only thought.") {
		t.Fatal("non-member response leaked comment text")
	}
}

func TestPOST_proposalComments_missingAuth_returns401(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newCommentFixture(t)

	// Act
	rec := f.postComment(t, "", f.proposalID, jsonCommentBody(t, "Anonymous take.", ""))

	// Assert
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body = %s", rec.Code, rec.Body.String())
	}
}

func TestPOST_proposalComments_invalidBodies_return400(t *testing.T) {
	t.Parallel()
	f := newCommentFixture(t)

	cases := []struct {
		name string
		body string
	}{
		{name: "whitespace only", body: jsonCommentBody(t, " \n\t ", "")},
		{name: "missing body field", body: `{}`},
		{name: "one rune over limit", body: jsonCommentBody(t, strings.Repeat("é", domain.MaxProposalCommentRunes+1), "")},
		{name: "nul character", body: `{"body":"hi\u0000there"}`},
		{name: "malformed json", body: `{"body":`},
		{name: "malformed parent id", body: jsonCommentBody(t, "Reply to nothing.", "not-a-uuid")},
		{name: "unknown parent id", body: jsonCommentBody(t, "Reply to a ghost.", "8b0c2f3e-6f59-4a57-9d8e-2b0a4c1e9f11")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			rec := f.postComment(t, f.benToken, f.proposalID, tc.body)

			// Assert
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
			}
		})
	}

	// Assert: none of the rejected requests stored a row.
	list := f.listComments(t, f.benToken, f.proposalID)
	if strings.TrimSpace(list.Body.String()) != `{"comments":[]}` {
		t.Fatalf("rejected comments were stored: %s", list.Body.String())
	}
}

func TestPOST_proposalComments_bodyOverByteCap_returns413(t *testing.T) {
	t.Parallel()
	// Arrange: the route's byte cap trips before the comment can be measured.
	f := newCommentFixture(t)
	body := jsonCommentBody(t, strings.Repeat("x", maxCommentRequestBytes), "")

	// Act
	rec := f.postComment(t, f.benToken, f.proposalID, body)

	// Assert
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body = %s", rec.Code, rec.Body.String())
	}
	list := f.listComments(t, f.benToken, f.proposalID)
	if strings.TrimSpace(list.Body.String()) != `{"comments":[]}` {
		t.Fatalf("rejected comment was stored: %s", list.Body.String())
	}
}

func TestPOST_proposalComments_exactlyMaxRunes_returns201(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newCommentFixture(t)
	body := strings.Repeat("é", domain.MaxProposalCommentRunes)

	// Act
	rec := f.postComment(t, f.benToken, f.proposalID, jsonCommentBody(t, body, ""))

	// Assert
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body = %s", rec.Code, rec.Body.String())
	}
}

func TestPOST_proposalComments_parentOnOtherProposal_returns400(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newCommentFixture(t)
	otherProposalID := f.newProposal()
	foreign := decodeComment(t, f.postComment(t, f.benToken, otherProposalID, jsonCommentBody(t, "Different buy entirely.", "")))

	// Act
	rec := f.postComment(t, f.adaToken, f.proposalID, jsonCommentBody(t, "Cross-thread reply.", foreign.ID))

	// Assert
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
}

func TestPOST_proposalComments_missingProposal_returns404(t *testing.T) {
	t.Parallel()
	f := newCommentFixture(t)

	for _, proposalID := range []string{"3f1d7a52-0c4e-4b8a-a1f0-5d9e2c7b6a10", "not-a-uuid"} {
		t.Run(proposalID, func(t *testing.T) {
			// Act
			rec := f.postComment(t, f.benToken, proposalID, jsonCommentBody(t, "Where did it go?", ""))

			// Assert
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestGET_proposalComments_missingProposal_returns404(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newCommentFixture(t)

	// Act
	rec := f.listComments(t, f.benToken, "3f1d7a52-0c4e-4b8a-a1f0-5d9e2c7b6a10")

	// Assert
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
	}
}

func TestPOST_proposalComments_burstOverLimit_returns429(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newCommentFixture(t)
	for i := 0; i < 10; i++ {
		rec := f.postComment(t, f.benToken, f.proposalID, jsonCommentBody(t, "Point number "+string(rune('A'+i)), ""))
		if rec.Code != http.StatusCreated {
			t.Fatalf("comment %d status = %d, want 201; body = %s", i, rec.Code, rec.Body.String())
		}
	}

	// Act
	rec := f.postComment(t, f.benToken, f.proposalID, jsonCommentBody(t, "One more thing.", ""))

	// Assert
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body = %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header")
	}
}

func TestGET_groupProposals_cardFields_reflectVotesAndComments(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newCommentFixture(t)
	f.postComment(t, f.benToken, f.proposalID, jsonCommentBody(t, "In.", ""))
	f.postComment(t, f.adaToken, f.proposalID, jsonCommentBody(t, "Same.", ""))
	f.castVote(t, f.adaToken, f.proposalID, "yes")

	// Act
	benView := f.listOpenProposals(t, f.benToken)

	// Assert
	if len(benView.Proposals) != 1 {
		t.Fatalf("proposals len = %d, want 1", len(benView.Proposals))
	}
	card := benView.Proposals[0]
	if card.CommentCount != 2 {
		t.Fatalf("commentCount = %d, want 2", card.CommentCount)
	}
	if card.VoteSummary.YesCount != 1 || card.VoteSummary.NoCount != 0 || card.VoteSummary.EligibleCount != 2 {
		t.Fatalf("voteSummary = %+v, want 1 yes / 0 no / 2 eligible", card.VoteSummary)
	}
	if card.VoteSummary.Threshold != "majority" {
		t.Fatalf("threshold = %q, want majority", card.VoteSummary.Threshold)
	}
	if !card.CanVote {
		t.Fatal("expected canVote=true for member who has not voted")
	}
}

func TestGET_groupProposals_afterViewerVotes_canVoteIsFalse(t *testing.T) {
	t.Parallel()
	// Arrange: two open proposals so the viewer's vote on one must not leak onto the other.
	f := newCommentFixture(t)
	secondID := f.newProposal()
	f.castVote(t, f.benToken, f.proposalID, "yes")

	// Act
	benView := f.listOpenProposals(t, f.benToken)

	// Assert
	canVote := map[string]bool{}
	for _, card := range benView.Proposals {
		canVote[card.ID] = card.CanVote
	}
	if canVote[f.proposalID] {
		t.Fatal("expected canVote=false on the proposal Ben already voted on")
	}
	if !canVote[secondID] {
		t.Fatal("expected canVote=true on the proposal Ben has not voted on")
	}
}

func TestGET_proposalDetail_includesCommentCount(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newCommentFixture(t)
	f.postComment(t, f.benToken, f.proposalID, jsonCommentBody(t, "First.", ""))

	req := httptest.NewRequest(http.MethodGet, "/v1/proposals/"+f.proposalID, nil)
	req.SetPathValue("id", f.proposalID)
	req.Header.Set("Authorization", "Bearer "+string(f.adaToken))
	rec := httptest.NewRecorder()

	// Act
	f.handlers.GetProposalDetailHandler(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var payload proposalDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if payload.CommentCount != 1 {
		t.Fatalf("commentCount = %d, want 1", payload.CommentCount)
	}
}

func TestGET_proposalDetail_malformedID_returns404(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newCommentFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/proposals/not-a-uuid", nil)
	req.SetPathValue("id", "not-a-uuid")
	req.Header.Set("Authorization", "Bearer "+string(f.benToken))
	rec := httptest.NewRecorder()

	// Act
	f.handlers.GetProposalDetailHandler(rec, req)

	// Assert
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
	}
}

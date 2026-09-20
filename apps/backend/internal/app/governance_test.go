package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/dex"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
	"github.com/monaco/monaco/apps/backend/internal/b20"
	"github.com/monaco/monaco/packages/domain"
)

type governanceHarness struct {
	Governance *GovernanceService
	Store      *postgres.Store
	DB         *sql.DB
	Sessions   *SessionService
	Privy      wallets.Client
	Jupiter    dex.Client
	XStocks    b20.Catalog
	Swap       *SwapService
	Symbols    *SymbolResolver
	ISO        *postgres.TestIsolation
}

func integrationGovernanceApp(t *testing.T) governanceHarness {
	t.Helper()

	h := integrationApp(t)
	buy := NewBuyService(h.Jupiter, h.XStocks)
	home := NewHomeService(h.Store, h.Privy, h.Pyth, h.Deposits, h.Symbols)
	governance := NewGovernanceService(h.Store, h.Privy)
	governance.SetBuyService(buy)
	governance.SetHomeService(home)
	governance.SetSwapService(h.Swap)
	return governanceHarness{
		Governance: governance,
		Store:      h.Store,
		DB:         h.DB,
		Sessions:   NewSessionService(h.Store, h.Privy),
		Privy:      h.Privy,
		Jupiter:    h.Jupiter,
		XStocks:    h.XStocks,
		Swap:       h.Swap,
		Symbols:    h.Symbols,
		ISO:        h.ISO,
	}
}

func registerRoutableQuote(t *testing.T, jupiterClient dex.Client, resolver b20.Catalog, symbol string, usdc int64) {
	t.Helper()

	mint := "Mint" + symbol
	b20.RegisterTokenAddress(resolver, symbol, mint)
	jupiter.RegisterQuoteBuy(jupiterClient, mint, usdc, jupiter.BuyQuote{
		Routable:   true,
		OutputToken: mint,
		InAmount:   strconv.FormatInt(usdc, 10),
		OutAmount:  strconv.FormatInt(usdc, 10),
	})
}

func TestPOST_proposals_happyPath_createsOpenProposalWithExpiry(t *testing.T) {
	// Arrange
	h := integrationGovernanceApp(t)
	userID := openTestSession(t, h.ISO, h.Sessions, h.Privy, "proposer", "Proposer")
	token := h.ISO.UniqueToken("proposer")
	created, err := h.Governance.CreateGroupWithRules(context.Background(), token, testGroupName(h.ISO, "vote"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(created.GroupID)
	seedTestTreasuryUSDC(t, h.Privy, created.TreasuryAddress, 10_000_000)
	registerRoutableQuote(t, h.Jupiter, h.XStocks, "AAPLx", 2_000_000)
	fixedNow := time.Unix(1_700_000_000, 0).UTC()
	h.Governance.SetClock(func() time.Time { return fixedNow })

	// Act
	proposal, err := h.Governance.CreateProposal(context.Background(), CreateProposalInput{
		GroupID:    created.GroupID,
		ProposerID: userID.UserID,
		Symbol:     "AAPLx",
		UsdcMicros: 2_000_000,
	})

	// Assert
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}
	if proposal.Status != ProposalOpen {
		t.Fatalf("status = %q, want open", proposal.Status)
	}
	wantExpiry := fixedNow.Add(time.Duration(DefaultGroupRules().VoteExpirySeconds) * time.Second).Unix()
	if proposal.ExpiresAt != wantExpiry {
		t.Fatalf("expiresAt = %d, want %d", proposal.ExpiresAt, wantExpiry)
	}
	if proposal.Symbol != "AAPLx" || proposal.UsdcMicros != 2_000_000 {
		t.Fatalf("unexpected proposal payload: %+v", proposal)
	}
}

func TestCreateProposal_jupiterTakerOrderFails_priceOnlyQuoteCreates(t *testing.T) {
	const (
		usdcMicros  = 2_000_000
		treasuryUSDC = 5_000_000
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("taker") != "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"requestId":"01a0b261-4278-708b-9c6a-710981e01775","error":"Failed to get quotes"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jupiter.FixtureJupiterSuccessResponse("0xb200000000000000000000c2e324d24d7eecd1fb"))
	}))
	t.Cleanup(server.Close)

	h := integrationGovernanceApp(t)
	h.Governance.SetBuyService(NewBuyService(
		jupiter.NewHTTPClientWithBaseURL(server.URL, server.Client()),
		h.XStocks,
	))

	userID := openTestSession(t, h.ISO, h.Sessions, h.Privy, "rfq-propose", "RFQ Proposer")
	token := h.ISO.UniqueToken("rfq-propose")
	created, err := h.Governance.CreateGroupWithRules(context.Background(), token, testGroupName(h.ISO, "rfq-propose"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(created.GroupID)
	seedTestTreasuryUSDC(t, h.Privy, created.TreasuryAddress, treasuryUSDC)
	b20.RegisterTokenAddress(h.XStocks, "AAPLx", "0xb200000000000000000000c2e324d24d7eecd1fb")

	proposal, err := h.Governance.CreateProposal(context.Background(), CreateProposalInput{
		GroupID:    created.GroupID,
		ProposerID: userID.UserID,
		Symbol:     "AAPLx",
		UsdcMicros: usdcMicros,
	})
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}
	if proposal.UsdcMicros != usdcMicros {
		t.Fatalf("usdcMicros = %d, want %d", proposal.UsdcMicros, usdcMicros)
	}
}

func TestCreateProposal_treasurySurplusOnChain_allowsAfterReconcile(t *testing.T) {
	h := integrationGovernanceApp(t)
	userID := openTestSession(t, h.ISO, h.Sessions, h.Privy, "surplus-propose", "Surplus Proposer")
	token := h.ISO.UniqueToken("surplus-propose")
	created, err := h.Governance.CreateGroupWithRules(context.Background(), token, testGroupName(h.ISO, "surplus-propose"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(created.GroupID)
	seedTestTreasuryUSDC(t, h.Privy, created.TreasuryAddress, 5_000_000)
	registerRoutableQuote(t, h.Jupiter, h.XStocks, "AAPLx", 2_000_000)

	proposal, err := h.Governance.CreateProposal(context.Background(), CreateProposalInput{
		GroupID:    created.GroupID,
		ProposerID: userID.UserID,
		Symbol:     "AAPLx",
		UsdcMicros: 2_000_000,
	})
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}
	if proposal.UsdcMicros != 2_000_000 {
		t.Fatalf("usdcMicros = %d, want 2000000", proposal.UsdcMicros)
	}

	totalShares, err := h.Store.SumShareUnitsByGroup(context.Background(), created.GroupID)
	if err != nil {
		t.Fatalf("SumShareUnitsByGroup: %v", err)
	}
	if totalShares != 5_000_000 {
		t.Fatalf("share units = %d, want 5000000 after reconcile", totalShares)
	}
}

func TestCreateProposal_exceedsTreasuryUSDC_rejected(t *testing.T) {
	h := integrationGovernanceApp(t)
	userID := openTestSession(t, h.ISO, h.Sessions, h.Privy, "treasury-cap", "Treasury Cap")
	token := h.ISO.UniqueToken("treasury-cap")
	created, err := h.Governance.CreateGroupWithRules(context.Background(), token, testGroupName(h.ISO, "treasury-cap"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(created.GroupID)
	seedTestTreasuryUSDC(t, h.Privy, created.TreasuryAddress, 1_000_000)
	registerRoutableQuote(t, h.Jupiter, h.XStocks, "AAPLx", 2_000_000)

	_, err = h.Governance.CreateProposal(context.Background(), CreateProposalInput{
		GroupID:    created.GroupID,
		ProposerID: userID.UserID,
		Symbol:     "AAPLx",
		UsdcMicros: 2_000_000,
	})
	if !errors.Is(err, ErrExceedsTreasuryUSDC) {
		t.Fatalf("err = %v, want ErrExceedsTreasuryUSDC", err)
	}
}

func TestCreateProposal_thesisTooLong_rejected(t *testing.T) {
	h := integrationGovernanceApp(t)
	userID := openTestSession(t, h.ISO, h.Sessions, h.Privy, "thesis-cap", "Thesis Cap")
	token := h.ISO.UniqueToken("thesis-cap")
	created, err := h.Governance.CreateGroupWithRules(context.Background(), token, testGroupName(h.ISO, "thesis-cap"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(created.GroupID)

	_, err = h.Governance.CreateProposal(context.Background(), CreateProposalInput{
		GroupID:    created.GroupID,
		ProposerID: userID.UserID,
		Symbol:     "AAPLx",
		UsdcMicros: 2_000_000,
		Thesis:     strings.Repeat("a", MaxProposalThesisLength+1),
	})
	if !errors.Is(err, ErrThesisTooLong) {
		t.Fatalf("err = %v, want ErrThesisTooLong", err)
	}
}

func TestTallyProposal_expiredOpenProposal_failsWithoutSwap(t *testing.T) {
	// Arrange
	h := integrationGovernanceApp(t)
	userID := openTestSession(t, h.ISO, h.Sessions, h.Privy, "expiry", "Expiry")
	token := h.ISO.UniqueToken("expiry")
	rules := DefaultGroupRules()
	rules.VoteExpirySeconds = 60
	created, err := h.Governance.CreateGroupWithRules(context.Background(), token, testGroupName(h.ISO, "expiry"), rules)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(created.GroupID)
	seedTestTreasuryUSDC(t, h.Privy, created.TreasuryAddress, 10_000_000)
	registerRoutableQuote(t, h.Jupiter, h.XStocks, "TSLAx", 1_000_000)
	start := time.Unix(1_700_100_000, 0).UTC()
	h.Governance.SetClock(func() time.Time { return start })
	proposal, err := h.Governance.CreateProposal(context.Background(), CreateProposalInput{
		GroupID:    created.GroupID,
		ProposerID: userID.UserID,
		Symbol:     "TSLAx",
		UsdcMicros: 1_000_000,
	})
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}
	h.Governance.SetClock(func() time.Time { return start.Add(61 * time.Second) })

	// Act
	final, err := h.Governance.FinalizeExpiredProposal(context.Background(), proposal.ID)

	// Assert
	if err != nil {
		t.Fatalf("FinalizeExpiredProposal: %v", err)
	}
	if final.Status != ProposalExpired {
		t.Fatalf("status = %q, want expired", final.Status)
	}
	count, err := h.Store.CountTransactionsForProposal(context.Background(), proposal.ID)
	if err != nil {
		t.Fatalf("count transactions: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no swap transactions, got %d", count)
	}
}

func TestPOST_vote_nonVoterSetMember_returns403(t *testing.T) {
	// Arrange
	h := integrationGovernanceApp(t)
	creator := openTestSession(t, h.ISO, h.Sessions, h.Privy, "creator", "Creator")
	creatorToken := h.ISO.UniqueToken("creator")
	rules := DefaultGroupRules()
	rules.VoterSet = VoterSet{Mode: VoterSetNamed, MemberIDs: []string{creator.UserID}}
	created, err := h.Governance.CreateGroupWithRules(context.Background(), creatorToken, testGroupName(h.ISO, "named-voters"), rules)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(created.GroupID)
	seedTestTreasuryUSDC(t, h.Privy, created.TreasuryAddress, 10_000_000)

	outsiderID := openTestSession(t, h.ISO, h.Sessions, h.Privy, "outsider", "Outsider").UserID
	tx, err := h.Store.BeginTx(context.Background())
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := h.Store.InsertGroupMemberTx(context.Background(), tx, created.GroupID, outsiderID); err != nil {
		t.Fatalf("insert outsider member: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit member: %v", err)
	}

	registerRoutableQuote(t, h.Jupiter, h.XStocks, "NVDAx", 500_000)
	proposal, err := h.Governance.CreateProposal(context.Background(), CreateProposalInput{
		GroupID:    created.GroupID,
		ProposerID: creator.UserID,
		Symbol:     "NVDAx",
		UsdcMicros: 500_000,
	})
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}

	// Act
	_, err = h.Governance.CastVote(context.Background(), CastVoteInput{
		ProposalID: proposal.ID,
		VoterID:    outsiderID,
		Choice:     domain.VoteYes,
	})

	// Assert
	if !errors.Is(err, ErrNotEligibleVoter) {
		t.Fatalf("err = %v, want ErrNotEligibleVoter (maps to HTTP 403)", err)
	}
	if httpStatusForVoteErr(err) != http.StatusForbidden {
		t.Fatalf("http status = %d, want 403", httpStatusForVoteErr(err))
	}
}

func TestPOST_vote_doubleVoteSameMember_isIdempotentOrRejected(t *testing.T) {
	// Arrange
	h := integrationGovernanceApp(t)
	userID := openTestSession(t, h.ISO, h.Sessions, h.Privy, "double", "Double")
	token := h.ISO.UniqueToken("double")
	created, err := h.Governance.CreateGroupWithRules(context.Background(), token, testGroupName(h.ISO, "double-vote"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(created.GroupID)
	seedTestTreasuryUSDC(t, h.Privy, created.TreasuryAddress, 10_000_000)
	registerRoutableQuote(t, h.Jupiter, h.XStocks, "MSFTx", 750_000)
	proposal, err := h.Governance.CreateProposal(context.Background(), CreateProposalInput{
		GroupID:    created.GroupID,
		ProposerID: userID.UserID,
		Symbol:     "MSFTx",
		UsdcMicros: 750_000,
	})
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}

	// Act
	first, err := h.Governance.CastVote(context.Background(), CastVoteInput{
		ProposalID: proposal.ID,
		VoterID:    userID.UserID,
		Choice:     domain.VoteYes,
	})
	if err != nil {
		t.Fatalf("first vote: %v", err)
	}
	second, err := h.Governance.CastVote(context.Background(), CastVoteInput{
		ProposalID: proposal.ID,
		VoterID:    userID.UserID,
		Choice:     domain.VoteYes,
	})

	// Assert
	if err != nil {
		t.Fatalf("second vote should be idempotent: %v", err)
	}
	if second.Status != first.Status {
		t.Fatalf("status after duplicate vote = %q, want %q", second.Status, first.Status)
	}
	votes, err := h.Store.ListVotesForProposal(context.Background(), proposal.ID)
	if err != nil {
		t.Fatalf("list votes: %v", err)
	}
	if len(votes) != 1 {
		t.Fatalf("expected 1 ballot row, got %d", len(votes))
	}
}

func TestPOST_vote_concurrentDoubleVote_recordsOneBallot(t *testing.T) {
	// Arrange
	h := integrationGovernanceApp(t)
	userID := openTestSession(t, h.ISO, h.Sessions, h.Privy, "race", "Race")
	token := h.ISO.UniqueToken("race")
	created, err := h.Governance.CreateGroupWithRules(context.Background(), token, testGroupName(h.ISO, "race-vote"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(created.GroupID)
	seedTestTreasuryUSDC(t, h.Privy, created.TreasuryAddress, 10_000_000)
	registerRoutableQuote(t, h.Jupiter, h.XStocks, "GOOGx", 900_000)
	proposal, err := h.Governance.CreateProposal(context.Background(), CreateProposalInput{
		GroupID:    created.GroupID,
		ProposerID: userID.UserID,
		Symbol:     "GOOGx",
		UsdcMicros: 900_000,
	})
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}

	// Act
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = h.Governance.CastVote(context.Background(), CastVoteInput{
				ProposalID: proposal.ID,
				VoterID:    userID.UserID,
				Choice:     domain.VoteYes,
			})
		}(i)
	}
	wg.Wait()

	// Assert
	for _, err := range errs {
		if err != nil && !errors.Is(err, ErrProposalNotOpen) {
			t.Fatalf("unexpected concurrent vote error: %v", err)
		}
	}
	votes, err := h.Store.ListVotesForProposal(context.Background(), proposal.ID)
	if err != nil {
		t.Fatalf("list votes: %v", err)
	}
	if len(votes) != 1 {
		t.Fatalf("expected 1 ballot row after race, got %d", len(votes))
	}
}

func TestCreateProposal_sellHeldAmount_createsOpenProposal(t *testing.T) {
	h := integrationGovernanceApp(t)
	ctx := context.Background()
	userID := openTestSession(t, h.ISO, h.Sessions, h.Privy, "sell-prop", "Sell Prop")
	token := h.ISO.UniqueToken("sell-prop")
	created, err := h.Governance.CreateGroupWithRules(ctx, token, testGroupName(h.ISO, "sell-prop"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(created.GroupID)

	const held = int64(50_000_000)
	b20.RegisterTokenAddress(h.XStocks, "AAPLx", "0xb200000000000000000000c2e324d24d7eecd1fb")
	_, _, err = h.Store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID:          created.GroupID,
		Amount:           2_000_000,
		InputToken:        evm.USDCAddress,
		OutputToken:       "0xb200000000000000000000c2e324d24d7eecd1fb",
		TxHash:      testTxHash(h.ISO, "sell-prop-buy"),
		ExecuteRequestID: testRequestID(h.ISO, "sell-prop-buy"),
		CostBasisPrice:   2_000_000,
		CostBasisAmount:  held,
	})
	if err != nil {
		t.Fatalf("ConfirmBuyTransaction: %v", err)
	}
	jupiter.RegisterSellQuote(h.Jupiter, "0xb200000000000000000000c2e324d24d7eecd1fb", held, jupiter.SellQuote{
		Routable:   true,
		InputToken:  "0xb200000000000000000000c2e324d24d7eecd1fb",
		OutputToken: evm.USDCAddress,
		InAmount:   "50000000",
		OutAmount:  "1500000",
		RequestID:  "sell-quote-held",
	})

	proposal, err := h.Governance.CreateProposal(ctx, CreateProposalInput{
		GroupID:     created.GroupID,
		ProposerID:  userID.UserID,
		Symbol:      "AAPLx",
		Kind:        domain.ProposalKindSell,
		TokenAmount: held,
	})
	if err != nil {
		t.Fatalf("CreateProposal sell: %v", err)
	}
	if proposal.Kind != domain.ProposalKindSell || proposal.TokenAmount != held {
		t.Fatalf("proposal = %+v", proposal)
	}
	if proposal.Status != ProposalOpen {
		t.Fatalf("status = %q, want open", proposal.Status)
	}
}

func TestCreateProposal_sellExceedsHolding_rejected(t *testing.T) {
	h := integrationGovernanceApp(t)
	ctx := context.Background()
	userID := openTestSession(t, h.ISO, h.Sessions, h.Privy, "sell-over", "Sell Over")
	token := h.ISO.UniqueToken("sell-over")
	created, err := h.Governance.CreateGroupWithRules(ctx, token, testGroupName(h.ISO, "sell-over"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(created.GroupID)
	b20.RegisterTokenAddress(h.XStocks, "AAPLx", "0xb200000000000000000000c2e324d24d7eecd1fb")

	_, err = h.Governance.CreateProposal(ctx, CreateProposalInput{
		GroupID:     created.GroupID,
		ProposerID:  userID.UserID,
		Symbol:      "AAPLx",
		Kind:        domain.ProposalKindSell,
		TokenAmount: 1,
	})
	if !errors.Is(err, ErrExceedsTreasuryHolding) {
		t.Fatalf("CreateProposal sell over holding: %v, want ErrExceedsTreasuryHolding", err)
	}
}

func httpStatusForVoteErr(err error) int {
	switch {
	case errors.Is(err, ErrNotEligibleVoter):
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}

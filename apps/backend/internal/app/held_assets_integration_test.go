package app

import (
	"context"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/auth"
	"github.com/monaco/monaco/apps/backend/internal/chainlink"
	"github.com/monaco/monaco/apps/backend/internal/dex"
	"github.com/monaco/monaco/apps/backend/internal/marks"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
	"github.com/monaco/monaco/packages/domain"
)

const heldAppleToken = "0xb200000000000000000000c2e324d24d7eecd1fb"

// Two members put in $2 each, the cabal buys $1 of AAPLc (0.5 tokens) and the
// mark is $2.40, so the position is worth $1.20. The viewer holds half the share
// units, so their slice of it is $0.60, the same share base Home divides the whole
// pot by. Their Home equity ($2.10 of a $4.20 pot) is that slice plus half the
// $3.00 of cash.
func TestGetHeldAssets_sliceUsesHomesShareBase(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := integrationApp(t)
	home := NewHomeService(h.Store, h.Auth, h.Wallets, h.Pyth, h.Deposits, h.Symbols)
	sessions := NewSessionService(h.Store, h.Auth, h.Wallets)

	viewer := openTestSession(t, h.ISO, sessions, h.Auth, "held-viewer", "Viewer")
	other := openTestSession(t, h.ISO, sessions, h.Auth, "held-other", "Other")
	token := string(auth.AccessToken(h.ISO.UniqueToken("held-viewer")))
	group, err := h.Groups.CreateGroup(ctx, token, testGroupName(h.ISO, "held"))
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)
	for _, userID := range []string{viewer.UserID, other.UserID} {
		if err := insertGroupMember(t, ctx, h.Store, group.GroupID, userID); err != nil {
			t.Fatalf("insertGroupMember: %v", err)
		}
	}
	treasury, found, err := h.Store.GetTreasuryByGroupID(ctx, group.GroupID)
	if err != nil || !found {
		t.Fatalf("GetTreasuryByGroupID: found=%v err=%v", found, err)
	}

	const depositMicros = int64(2_000_000)
	tx, err := h.Store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	for _, userID := range []string{viewer.UserID, other.UserID} {
		if _, err := h.Store.IncrementPositionTx(ctx, tx, userID, group.GroupID, depositMicros, depositMicros); err != nil {
			t.Fatalf("IncrementPositionTx: %v", err)
		}
	}
	if _, err := h.Store.InsertProposalTx(ctx, tx, postgres.InsertProposalParams{
		GroupID:    group.GroupID,
		ProposerID: viewer.UserID,
		Symbol:     "NVDAc",
		Kind:       domain.ProposalKindBuy,
		UsdcMicros: 1_000_000,
		ExpiresAt:  time.Now().UTC().Add(time.Hour),
	}); err != nil {
		t.Fatalf("InsertProposalTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	const swappedUSDC = int64(1_000_000)
	const aaplAtomics = int64(50_000_000)
	if _, _, err := h.Store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID:          group.GroupID,
		Amount:           swappedUSDC,
		InputToken:       dex.USDCAddress(),
		OutputToken:      heldAppleToken,
		TxHash:           testTxHash(h.ISO, "held-buy"),
		ExecuteRequestID: testRequestID(h.ISO, "held-buy"),
		CostBasisPrice:   swappedUSDC,
		CostBasisAmount:  aaplAtomics,
	}); err != nil {
		t.Fatalf("ConfirmBuyTransaction: %v", err)
	}
	remainingUSDC := 2*depositMicros - swappedUSDC
	wallets.SetTreasuryUSDCBalance(h.Privy, treasury.Address, remainingUSDC)
	chainlink.RegisterMarkedPot(h.Pyth, marks.TreasuryRef{GroupID: group.GroupID, Address: treasury.Address}, marks.NavInput{
		TreasuryUsdc: remainingUSDC,
		Holdings: []marks.MarkedHolding{{
			Symbol:    "AAPLc",
			Token:     heldAppleToken,
			Units:     aaplAtomics,
			MarkUsdc:  2_400_000,
			CostBasis: swappedUSDC,
		}},
	})

	result, err := home.GetHeldAssets(ctx, token)
	if err != nil {
		t.Fatalf("GetHeldAssets: %v", err)
	}
	if len(result.Held) != 1 {
		t.Fatalf("held = %+v, want AAPLc only (cash is not a holding)", result.Held)
	}
	apple := result.Held[0]
	if apple.Symbol != "AAPLc" || apple.TotalValueUsd != "1.20" || apple.TotalDollarPnL != "+0.20" {
		t.Fatalf("AAPLc = %+v, want $1.20 worth, +$0.20", apple)
	}
	if apple.MySliceUsd != "0.60" || len(apple.Cabals) != 1 || apple.Cabals[0].MySliceUsd != "0.60" {
		t.Fatalf("slice = %q (cabals %+v), want 0.60: half the share units of a $1.20 position", apple.MySliceUsd, apple.Cabals)
	}

	// Home divides the same pot by the same share base.
	view, err := home.GetGroupView(ctx, token, group.GroupID)
	if err != nil {
		t.Fatalf("GetGroupView: %v", err)
	}
	if view.You.EquityUsd != "2.10" {
		t.Fatalf("Home equity = %q, want 2.10 (0.60 of stock + 1.50 of cash)", view.You.EquityUsd)
	}

	if len(result.UpForVote) != 1 {
		t.Fatalf("upForVote = %+v, want the NVDAc vote", result.UpForVote)
	}
	vote := result.UpForVote[0]
	if vote.Symbol != "NVDAc" || vote.OpenProposals != 1 || len(vote.CabalNames) != 1 {
		t.Fatalf("vote = %+v", vote)
	}
	if vote.SoonestExpiresAt == nil || vote.SoonestExpiresAt.Location() != time.UTC {
		t.Fatalf("soonestExpiresAt = %v, want a UTC time", vote.SoonestExpiresAt)
	}
}

func TestGetHeldAssets_noCabalsIsTwoEmptyLists(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := integrationApp(t)
	home := NewHomeService(h.Store, h.Auth, h.Wallets, h.Pyth, h.Deposits, h.Symbols)
	openTestSession(t, h.ISO, NewSessionService(h.Store, h.Auth, h.Wallets), h.Auth, "held-alone", "Alone")

	result, err := home.GetHeldAssets(ctx, string(auth.AccessToken(h.ISO.UniqueToken("held-alone"))))
	if err != nil {
		t.Fatalf("GetHeldAssets: %v", err)
	}
	if result.Held == nil || result.UpForVote == nil || len(result.Held) != 0 || len(result.UpForVote) != 0 {
		t.Fatalf("result = %+v, want two empty, non-nil lists", result)
	}
}

func TestGetHeldAssets_unknownTokenIsUnauthorized(t *testing.T) {
	t.Parallel()

	h := integrationApp(t)
	home := NewHomeService(h.Store, h.Auth, h.Wallets, h.Pyth, h.Deposits, h.Symbols)
	if _, err := home.GetHeldAssets(context.Background(), "not-a-session"); err == nil {
		t.Fatal("an unknown token read someone's holdings")
	}
}

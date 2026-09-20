package postgres

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/packages/domain"
)

func seedPassedBuyProposal(t *testing.T, store *Store, userID, groupID string) ProposalRow {
	t.Helper()
	ctx := context.Background()
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	proposal, err := store.InsertProposalTx(ctx, tx, InsertProposalParams{
		GroupID:    groupID,
		ProposerID: userID,
		Symbol:     "AAPLx",
		Kind:       domain.ProposalKindBuy,
		UsdcMicros: 2_000_000,
		ExpiresAt:  time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("insert proposal: %v", err)
	}
	if ok, err := store.UpdateProposalStatusTx(ctx, tx, proposal.ID, domain.ProposalOpen, domain.ProposalPassed); err != nil || !ok {
		t.Fatalf("pass proposal: %v ok=%v", err, ok)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return proposal
}

func buyIntentParams(groupID, proposalID, requestID string) InsertSwapIntentParams {
	return InsertSwapIntentParams{
		GroupID:           groupID,
		ProposalID:        proposalID,
		Action:            TransactionActionBuy,
		InputMint:         jupiter.USDCMint,
		OutputMint:        jupiter.AAPLxMint,
		Amount:            2_000_000,
		Provider:          "jupiter",
		ExecuteRequestID:  requestID,
		SignedTxSignature: "sig-" + requestID,
		SignedTxBlockhash: "hash-" + requestID,
	}
}

func TestInsertSwapIntent_oneActiveSwapPerProposal(t *testing.T) {
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	userID, groupID := seedProposalGroup(t, store, iso)
	proposal := seedPassedBuyProposal(t, store, userID, groupID)

	first, err := store.InsertSwapIntent(ctx, buyIntentParams(groupID, proposal.ID, "intent-a-"+proposal.ID))
	if err != nil {
		t.Fatalf("first intent: %v", err)
	}
	if first.Status != TransactionStatusPending {
		t.Fatalf("status = %q, want pending", first.Status)
	}

	// Pending holds the slot.
	if _, err := store.InsertSwapIntent(ctx, buyIntentParams(groupID, proposal.ID, "intent-b-"+proposal.ID)); !errors.Is(err, ErrActiveSwapExists) {
		t.Fatalf("second intent while pending: err = %v, want ErrActiveSwapExists", err)
	}

	// Only a definitive failure frees it.
	if _, ok, err := store.FailPendingSwap(ctx, first.ID, "blockhash expired"); err != nil || !ok {
		t.Fatalf("FailPendingSwap: %v ok=%v", err, ok)
	}
	retry, err := store.InsertSwapIntent(ctx, buyIntentParams(groupID, proposal.ID, "intent-c-"+proposal.ID))
	if err != nil {
		t.Fatalf("intent after failure: %v", err)
	}

	// Confirmed holds it for good.
	if _, created, err := store.ConfirmPendingSwap(ctx, retry.ID, SwapFill{TxSignature: "confirmed-" + proposal.ID, InputAmount: 2_000_000, OutputAmount: 1_000_000}); err != nil || !created {
		t.Fatalf("ConfirmPendingSwap: %v created=%v", err, created)
	}
	if _, err := store.InsertSwapIntent(ctx, buyIntentParams(groupID, proposal.ID, "intent-d-"+proposal.ID)); !errors.Is(err, ErrActiveSwapExists) {
		t.Fatalf("intent after confirm: err = %v, want ErrActiveSwapExists", err)
	}
	count, err := store.CountTransactionsForProposal(ctx, proposal.ID)
	if err != nil {
		t.Fatalf("CountTransactionsForProposal: %v", err)
	}
	if count != 2 {
		t.Fatalf("transactions for proposal = %d, want 2 (one failed, one confirmed)", count)
	}
}

func TestInsertSwapIntent_concurrentExecutorsOnlyOneWins(t *testing.T) {
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	userID, groupID := seedProposalGroup(t, store, iso)
	proposal := seedPassedBuyProposal(t, store, userID, groupID)

	const executors = 8
	var wg sync.WaitGroup
	errs := make([]error, executors)
	start := make(chan struct{})
	for i := 0; i < executors; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = store.InsertSwapIntent(ctx, buyIntentParams(groupID, proposal.ID, fmt.Sprintf("race-%d-%s", i, proposal.ID)))
		}(i)
	}
	close(start)
	wg.Wait()

	won := 0
	for i, err := range errs {
		switch {
		case err == nil:
			won++
		case !errors.Is(err, ErrActiveSwapExists):
			t.Fatalf("executor %d: err = %v, want nil or ErrActiveSwapExists", i, err)
		}
	}
	if won != 1 {
		t.Fatalf("%d executors took the proposal's slot, want exactly 1", won)
	}
}

func TestListPassedProposalsPendingExecute_pendingBuyNotListed(t *testing.T) {
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	userID, groupID := seedProposalGroup(t, store, iso)
	proposal := seedPassedBuyProposal(t, store, userID, groupID)

	if _, err := store.InsertSwapIntent(ctx, buyIntentParams(groupID, proposal.ID, "pending-buy-"+proposal.ID)); err != nil {
		t.Fatalf("InsertSwapIntent: %v", err)
	}

	listed, err := store.ListPassedProposalsPendingExecute(ctx, 500)
	if err != nil {
		t.Fatalf("ListPassedProposalsPendingExecute: %v", err)
	}
	for _, row := range listed {
		if row.ID == proposal.ID {
			t.Fatal("buy with a pending swap was listed for execute: a second buy could be submitted")
		}
	}
}

func TestConfirmPendingSwap_idempotentAndNeverOverwritesFailed(t *testing.T) {
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	_, groupID := seedProposalGroup(t, store, iso)
	suffix := iso.Suffix()

	landed, err := store.InsertSwapIntent(ctx, buyIntentParams(groupID, "", "confirm-idem-"+suffix))
	if err != nil {
		t.Fatalf("InsertSwapIntent: %v", err)
	}
	fill := SwapFill{TxSignature: "landed-" + suffix, InputAmount: 2_000_000, OutputAmount: 1_000_000}
	first, created, err := store.ConfirmPendingSwap(ctx, landed.ID, fill)
	if err != nil || !created {
		t.Fatalf("first confirm: %v created=%v", err, created)
	}
	if first.CostBasisPrice.Int64 != 2_000_000 || first.CostBasisAmount.Int64 != 1_000_000 {
		t.Fatalf("cost basis = %d/%d, want 2000000/1000000", first.CostBasisPrice.Int64, first.CostBasisAmount.Int64)
	}
	second, created, err := store.ConfirmPendingSwap(ctx, landed.ID, fill)
	if err != nil || created || second.ID != first.ID {
		t.Fatalf("second confirm: row=%q created=%v err=%v, want the same row and created=false", second.ID, created, err)
	}
	if _, ok, err := store.FailPendingSwap(ctx, landed.ID, "late"); err != nil || ok {
		t.Fatalf("FailPendingSwap on a confirmed row: ok=%v err=%v, want ok=false", ok, err)
	}

	dead, err := store.InsertSwapIntent(ctx, buyIntentParams(groupID, "", "confirm-dead-"+suffix))
	if err != nil {
		t.Fatalf("InsertSwapIntent: %v", err)
	}
	if _, ok, err := store.FailPendingSwap(ctx, dead.ID, "expired"); err != nil || !ok {
		t.Fatalf("FailPendingSwap: %v ok=%v", err, ok)
	}
	if _, _, err := store.ConfirmPendingSwap(ctx, dead.ID, SwapFill{TxSignature: "zombie-" + suffix, InputAmount: 1, OutputAmount: 1}); err == nil {
		t.Fatal("confirming a failed swap succeeded; want an error so the contradiction surfaces")
	}
}

func TestClaimPassedProposalsForExecute_leaseBackoffAndConcurrentClaimers(t *testing.T) {
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	userID, groupID := seedProposalGroup(t, store, iso)
	proposal := seedPassedBuyProposal(t, store, userID, groupID)

	now := time.Now().UTC()
	const lease = 5 * time.Minute
	claimedBy := func(rows []ClaimedProposalRow) (ClaimedProposalRow, bool) {
		for _, row := range rows {
			if row.Proposal.ID == proposal.ID {
				return row, true
			}
		}
		return ClaimedProposalRow{}, false
	}

	// Two instances tick at the same moment: exactly one may hold the proposal.
	const claimers = 6
	var wg sync.WaitGroup
	wins := make([]bool, claimers)
	errs := make([]error, claimers)
	start := make(chan struct{})
	for i := 0; i < claimers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			rows, err := store.ClaimPassedProposalsForExecute(ctx, now, lease, 500)
			errs[i] = err
			_, wins[i] = claimedBy(rows)
		}(i)
	}
	close(start)
	wg.Wait()
	won := 0
	for i := range wins {
		if errs[i] != nil {
			t.Fatalf("claimer %d: %v", i, errs[i])
		}
		if wins[i] {
			won++
		}
	}
	if won != 1 {
		t.Fatalf("%d claimers hold the proposal, want exactly 1", won)
	}

	// The lease withholds it, including from a restarted instance with no memory.
	rows, err := store.ClaimPassedProposalsForExecute(ctx, now.Add(lease-time.Second), lease, 500)
	if err != nil {
		t.Fatalf("claim inside lease: %v", err)
	}
	if _, ok := claimedBy(rows); ok {
		t.Fatal("proposal was claimed again inside its lease")
	}

	// A recorded failure replaces the lease with the backoff deadline.
	retryAt := now.Add(30 * time.Second)
	if err := store.RecordProposalExecuteFailure(ctx, proposal.ID, retryAt, "jupiter: 503"); err != nil {
		t.Fatalf("RecordProposalExecuteFailure: %v", err)
	}
	rows, err = store.ClaimPassedProposalsForExecute(ctx, retryAt.Add(-time.Second), lease, 500)
	if err != nil {
		t.Fatalf("claim before backoff: %v", err)
	}
	if _, ok := claimedBy(rows); ok {
		t.Fatal("proposal was claimed before its backoff deadline")
	}
	rows, err = store.ClaimPassedProposalsForExecute(ctx, retryAt, lease, 500)
	if err != nil {
		t.Fatalf("claim at backoff: %v", err)
	}
	claimed, ok := claimedBy(rows)
	if !ok {
		t.Fatal("proposal was not claimable at its backoff deadline")
	}
	if claimed.ExecuteAttempts != 2 {
		t.Fatalf("execute attempts = %d, want 2", claimed.ExecuteAttempts)
	}

	// A pending swap takes the proposal out of the claimable set whatever the deadline says.
	if _, err := store.InsertSwapIntent(ctx, buyIntentParams(groupID, proposal.ID, "claim-pending-"+proposal.ID)); err != nil {
		t.Fatalf("InsertSwapIntent: %v", err)
	}
	rows, err = store.ClaimPassedProposalsForExecute(ctx, now.Add(24*time.Hour), lease, 500)
	if err != nil {
		t.Fatalf("claim with pending swap: %v", err)
	}
	if _, ok := claimedBy(rows); ok {
		t.Fatal("proposal with a pending swap was claimed for execute")
	}
}

func TestActiveSwapIndex_exemptsRowsFromBeforeTheMigration(t *testing.T) {
	// Duplicates written by the old executor must not block the migration, and must still be
	// seen by the slot check so the proposal is never executed again.
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	userID, groupID := seedProposalGroup(t, store, iso)
	proposal := seedPassedBuyProposal(t, store, userID, groupID)

	for i := 0; i < 2; i++ {
		_, err := db.ExecContext(ctx, `
INSERT INTO transactions (group_id, proposal_id, amount, action, input_mint, output_mint, status, tx_signature, created_at, confirmed_at)
VALUES ($1, $2, 2000000, 'buy', $3, $4, 'confirmed', $5, '2020-01-01T00:00:00Z', '2020-01-01T00:00:00Z')`,
			groupID, proposal.ID, jupiter.USDCMint, jupiter.AAPLxMint, fmt.Sprintf("legacy-%d-%s", i, proposal.ID))
		if err != nil {
			t.Fatalf("legacy duplicate %d: %v", i, err)
		}
	}

	if _, found, err := store.GetActiveSwapByProposalAndAction(ctx, proposal.ID, TransactionActionBuy); err != nil || !found {
		t.Fatalf("GetActiveSwapByProposalAndAction: found=%v err=%v, want the legacy confirmed buy", found, err)
	}
	listed, err := store.ListPassedProposalsPendingExecute(ctx, 500)
	if err != nil {
		t.Fatalf("ListPassedProposalsPendingExecute: %v", err)
	}
	for _, row := range listed {
		if row.ID == proposal.ID {
			t.Fatal("proposal with legacy confirmed buys is listed for execute")
		}
	}
}

package seeddemo

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/packages/domain"
)

// Options configures a demo seed run.
type Options struct {
	UserID      string
	PrivyUserID string
	IfEmpty     bool
}

// Result summarizes a completed seed run.
type Result struct {
	UserID    string
	GroupIDs  map[string]string
	Skipped   bool
	SkipReason string
}

// Runner seeds local Postgres with demo cabals.
type Runner struct {
	DB    *sql.DB
	Store *postgres.Store
	Privy privy.Client
	Now   func() time.Time
}

// Run seeds demo data for the resolved user.
func (r *Runner) Run(ctx context.Context, opts Options) (Result, error) {
	if r.Now == nil {
		r.Now = time.Now
	}
	user, err := ResolveUser(ctx, r.DB, opts.UserID, opts.PrivyUserID)
	if err != nil {
		return Result{}, err
	}
	slog.Info("seed demo user", "user_id", user.ID, "privy_user_id", user.PrivyUserID)

	if opts.IfEmpty {
		exists, err := demoGroupsExist(ctx, r.DB)
		if err != nil {
			return Result{}, err
		}
		if exists {
			slog.Info("seed demo skipped", "reason", "demo cabals already exist")
			return Result{UserID: user.ID, Skipped: true, SkipReason: "demo cabals already exist (use --if-empty=false after just reset db to re-seed)"}, nil
		}
	}

	alice, err := r.storeDemoUser(ctx, privyAlice, "Alice")
	if err != nil {
		return Result{}, err
	}
	bob, err := r.storeDemoUser(ctx, privyBob, "Bob")
	if err != nil {
		return Result{}, err
	}
	carol, err := r.storeDemoUser(ctx, privyCarol, "Carol")
	if err != nil {
		return Result{}, err
	}

	openRules := app.DefaultGroupRules()
	requestRules := app.GroupRules{
		JoinPolicy:        app.JoinPolicy{Mode: app.JoinModeRequest},
		VoterSet:          app.VoterSet{Mode: app.VoterSetAllMembers},
		Threshold:         app.ThresholdMajority,
		VoteExpirySeconds: 86_400,
	}

	groupIDs := make(map[string]string, len(GroupNames))

	weekend, err := r.createGroup(ctx, user.ID, "Weekend investors", openRules, []string{user.ID, alice.ID, bob.ID})
	if err != nil {
		return Result{}, fmt.Errorf("weekend investors: %w", err)
	}
	groupIDs["Weekend investors"] = weekend.ID
	if err := r.seedWeekendInvestors(ctx, user.ID, alice.ID, bob.ID, weekend); err != nil {
		return Result{}, fmt.Errorf("weekend investors data: %w", err)
	}

	rent, err := r.createGroup(ctx, user.ID, "Rent money", openRules, []string{user.ID, alice.ID})
	if err != nil {
		return Result{}, fmt.Errorf("rent money: %w", err)
	}
	groupIDs["Rent money"] = rent.ID
	if err := r.seedRentMoney(ctx, user.ID, alice.ID, rent); err != nil {
		return Result{}, fmt.Errorf("rent money data: %w", err)
	}

	apple, err := r.createGroup(ctx, user.ID, "Apple heads", openRules, []string{user.ID, bob.ID})
	if err != nil {
		return Result{}, fmt.Errorf("apple heads: %w", err)
	}
	groupIDs["Apple heads"] = apple.ID
	if err := r.seedAppleHeads(ctx, user.ID, bob.ID, apple); err != nil {
		return Result{}, fmt.Errorf("apple heads data: %w", err)
	}

	dorm, err := r.createGroup(ctx, carol.ID, "Dorm 4B fund", openRules, []string{carol.ID, alice.ID})
	if err != nil {
		return Result{}, fmt.Errorf("dorm 4b fund: %w", err)
	}
	groupIDs["Dorm 4B fund"] = dorm.ID
	if err := r.seedUnjoinedPublic(ctx, dorm); err != nil {
		return Result{}, fmt.Errorf("dorm 4b fund data: %w", err)
	}

	tesla, err := r.createGroup(ctx, carol.ID, "Tesla or bust", requestRules, []string{carol.ID})
	if err != nil {
		return Result{}, fmt.Errorf("tesla or bust: %w", err)
	}
	groupIDs["Tesla or bust"] = tesla.ID
	if err := r.seedTeslaOrBust(ctx, carol.ID, tesla); err != nil {
		return Result{}, fmt.Errorf("tesla or bust data: %w", err)
	}

	slog.Info("seed demo complete", "groups", len(groupIDs))
	return Result{UserID: user.ID, GroupIDs: groupIDs}, nil
}

type createdGroup struct {
	ID               string
	Name             string
	TreasuryAddress  string
}

func demoSig(groupID, label string) string {
	return fmt.Sprintf("sig-demo-%s-%s", label, groupID)
}

func demoReq(groupID, label string) string {
	return fmt.Sprintf("req-demo-%s-%s", label, groupID)
}

func demoGroupsExist(ctx context.Context, db *sql.DB) (bool, error) {
	const q = `SELECT EXISTS (SELECT 1 FROM groups WHERE name = ANY($1::text[]))`
	var exists bool
	if err := db.QueryRowContext(ctx, q, GroupNames).Scan(&exists); err != nil {
		return false, fmt.Errorf("check demo groups: %w", err)
	}
	return exists, nil
}

func (r *Runner) storeDemoUser(ctx context.Context, privyID, displayName string) (postgres.User, error) {
	return r.Store.UpsertUser(ctx, privyID, displayName)
}

func (r *Runner) createGroup(ctx context.Context, creatorID, name string, rules app.GroupRules, memberIDs []string) (createdGroup, error) {
	tx, err := r.Store.BeginTx(ctx)
	if err != nil {
		return createdGroup{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	group, err := r.Store.InsertGroupWithRulesTx(ctx, tx, name, creatorID, rules)
	if err != nil {
		return createdGroup{}, err
	}
	for _, memberID := range memberIDs {
		if err := r.Store.InsertGroupMemberTx(ctx, tx, group.ID, memberID); err != nil {
			return createdGroup{}, err
		}
	}
	treasuryRef, err := r.Privy.EnsureTreasury(ctx, privy.GroupID(group.ID))
	if err != nil {
		return createdGroup{}, fmt.Errorf("privy ensure treasury: %w", err)
	}
	if _, err = r.Store.InsertTreasuryTx(ctx, tx, group.ID, treasuryRef.PrivyWalletID, treasuryRef.SolanaAddress); err != nil {
		return createdGroup{}, err
	}
	if err := tx.Commit(); err != nil {
		return createdGroup{}, fmt.Errorf("commit create group: %w", err)
	}
	committed = true
	return createdGroup{ID: group.ID, Name: group.Name, TreasuryAddress: treasuryRef.SolanaAddress}, nil
}

func (r *Runner) creditPositions(ctx context.Context, groupID string, credits []positionCredit) error {
	tx, err := r.Store.BeginTx(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	var totalShares int64
	for _, c := range credits {
		if _, err := r.Store.IncrementPositionTx(ctx, tx, c.UserID, groupID, c.ShareMicros, c.DepositedMicros); err != nil {
			return err
		}
		totalShares += c.ShareMicros
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

type positionCredit struct {
	UserID           string
	ShareMicros      int64
	DepositedMicros  int64
}

func (r *Runner) seedWeekendInvestors(ctx context.Context, userID, aliceID, bobID string, g createdGroup) error {
	const (
		userShares  = int64(80_000_000)
		aliceShares = int64(40_000_000)
		bobShares   = int64(30_000_000)
	)
	netIn := userShares + aliceShares + bobShares
	if err := r.creditPositions(ctx, g.ID, []positionCredit{
		{userID, userShares, userShares},
		{aliceID, aliceShares, aliceShares},
		{bobID, bobShares, bobShares},
	}); err != nil {
		return err
	}

	const buyUSDC = 120_000_000
	const aaplAtomics = 60_000_000
	if _, _, err := r.Store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID: g.ID, Amount: buyUSDC, InputMint: jupiter.USDCMint, OutputMint: jupiter.AAPLxMint,
		TxSignature: demoSig(g.ID, "weekend-buy"), ExecuteRequestID: demoReq(g.ID, "weekend-buy"),
		CostBasisPrice: buyUSDC, CostBasisAmount: aaplAtomics,
	}); err != nil {
		return err
	}

	now := r.Now().UTC()
	if err := r.insertSnapshotAt(ctx, g.ID, now.Add(-14*24*time.Hour), netIn, &netIn); err != nil {
		return err
	}
	gainPot := int64(float64(netIn) * 1.25)
	if err := r.insertSnapshotAt(ctx, g.ID, now.Add(-7*24*time.Hour), gainPot, &netIn); err != nil {
		return err
	}
	if err := r.insertSnapshotAt(ctx, g.ID, now.Add(-24*time.Hour), gainPot, &netIn); err != nil {
		return err
	}

	if _, err := r.insertOpenProposal(ctx, g.ID, userID, domain.ProposalKindBuy, "MSFTx", 5_000_000, 0, r.Now().Add(48*time.Hour)); err != nil {
		return err
	}
	if err := r.seedActiveAgent(ctx, g.ID, userID); err != nil {
		return err
	}
	if _, err := r.Store.InsertFailedTransaction(ctx, g.ID, postgres.TransactionActionBuy, jupiter.USDCMint, jupiter.AAPLxMint, 2_000_000, demoReq(g.ID, "weekend-failed")); err != nil {
		return err
	}
	return nil
}

func (r *Runner) seedRentMoney(ctx context.Context, userID, aliceID string, g createdGroup) error {
	netIn := int64(50_000_000)
	if err := r.creditPositions(ctx, g.ID, []positionCredit{
		{userID, 30_000_000, 30_000_000},
		{aliceID, 20_000_000, 20_000_000},
	}); err != nil {
		return err
	}

	const buyUSDC = 40_000_000
	const tslaAtomics = 20_000_000
	if _, _, err := r.Store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID: g.ID, Amount: buyUSDC, InputMint: jupiter.USDCMint, OutputMint: jupiter.TSLAxMint,
		TxSignature: demoSig(g.ID, "rent-buy"), ExecuteRequestID: demoReq(g.ID, "rent-buy"),
		CostBasisPrice: buyUSDC, CostBasisAmount: tslaAtomics,
	}); err != nil {
		return err
	}
	if _, _, err := r.Store.ConfirmSellTransaction(ctx, postgres.ConfirmSellTransactionParams{
		GroupID: g.ID, Amount: 10_000_000, InputMint: jupiter.TSLAxMint, OutputMint: jupiter.USDCMint,
		TxSignature: demoSig(g.ID, "rent-sell"), ExecuteRequestID: demoReq(g.ID, "rent-sell"),
		ProceedsUSDC: 7_000_000,
	}); err != nil {
		return err
	}

	lossPot := netIn - int64(3_000_000)
	now := r.Now().UTC()
	if err := r.insertSnapshotAt(ctx, g.ID, now.Add(-10*24*time.Hour), netIn, &netIn); err != nil {
		return err
	}
	if err := r.insertSnapshotAt(ctx, g.ID, now.Add(-2*24*time.Hour), lossPot, &netIn); err != nil {
		return err
	}

	failedID, err := r.insertOpenProposal(ctx, g.ID, userID, domain.ProposalKindBuy, "TSLAx", 10_000_000, 0, r.Now().Add(24*time.Hour))
	if err != nil {
		return err
	}
	return r.closeProposal(ctx, failedID, userID, domain.VoteNo, domain.ProposalFailed)
}

func (r *Runner) seedAppleHeads(ctx context.Context, userID, bobID string, g createdGroup) error {
	netIn := int64(25_000_000)
	if err := r.creditPositions(ctx, g.ID, []positionCredit{
		{userID, 15_000_000, 15_000_000},
		{bobID, 10_000_000, 10_000_000},
	}); err != nil {
		return err
	}

	if _, _, err := r.Store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID: g.ID, Amount: 18_000_000, InputMint: jupiter.USDCMint, OutputMint: jupiter.AAPLxMint,
		TxSignature: demoSig(g.ID, "apple-buy"), ExecuteRequestID: demoReq(g.ID, "apple-buy"),
		CostBasisPrice: 18_000_000, CostBasisAmount: 9_000_000,
	}); err != nil {
		return err
	}

	smallGain := int64(float64(netIn) * 1.08)
	now := r.Now().UTC()
	if err := r.insertSnapshotAt(ctx, g.ID, now.Add(-5*24*time.Hour), netIn, &netIn); err != nil {
		return err
	}
	if err := r.insertSnapshotAt(ctx, g.ID, now.Add(-12*time.Hour), smallGain, &netIn); err != nil {
		return err
	}

	passedID, err := r.insertOpenProposal(ctx, g.ID, userID, domain.ProposalKindBuy, "AAPLx", 3_000_000, 0, r.Now().Add(36*time.Hour))
	if err != nil {
		return err
	}
	if err := r.closeProposal(ctx, passedID, userID, domain.VoteYes, domain.ProposalPassed); err != nil {
		return err
	}
	sellID, err := r.insertOpenProposal(ctx, g.ID, userID, domain.ProposalKindSell, "AAPLx", 0, 2_000_000, r.Now().Add(72*time.Hour))
	if err != nil {
		return err
	}
	_ = sellID
	return nil
}

func (r *Runner) seedUnjoinedPublic(ctx context.Context, g createdGroup) error {
	netIn := int64(35_000_000)
	// Carol and Alice already members; fund with stock holding for visible pot.
	carol, _, err := r.Store.GetUserByPrivyUserID(ctx, privyCarol)
	if err != nil {
		return err
	}
	if err := r.creditPositions(ctx, g.ID, []positionCredit{{carol.ID, netIn, netIn}}); err != nil {
		return err
	}
	if _, _, err := r.Store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID: g.ID, Amount: 20_000_000, InputMint: jupiter.USDCMint, OutputMint: jupiter.AAPLxMint,
		TxSignature: demoSig(g.ID, "dorm-buy"), ExecuteRequestID: demoReq(g.ID, "dorm-buy"),
		CostBasisPrice: 20_000_000, CostBasisAmount: 10_000_000,
	}); err != nil {
		return err
	}
	now := r.Now().UTC()
	return r.insertSnapshotAt(ctx, g.ID, now.Add(-3*24*time.Hour), netIn, &netIn)
}

func (r *Runner) seedTeslaOrBust(ctx context.Context, carolID string, g createdGroup) error {
	netIn := int64(15_000_000)
	if err := r.creditPositions(ctx, g.ID, []positionCredit{{carolID, netIn, netIn}}); err != nil {
		return err
	}
	if _, _, err := r.Store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID: g.ID, Amount: 12_000_000, InputMint: jupiter.USDCMint, OutputMint: jupiter.TSLAxMint,
		TxSignature: demoSig(g.ID, "tesla-buy"), ExecuteRequestID: demoReq(g.ID, "tesla-buy"),
		CostBasisPrice: 12_000_000, CostBasisAmount: 6_000_000,
	}); err != nil {
		return err
	}
	expiredID, err := r.insertOpenProposal(ctx, g.ID, carolID, domain.ProposalKindBuy, "TSLAx", 8_000_000, 0, r.Now().Add(-48*time.Hour))
	if err != nil {
		return err
	}
	return r.setProposalStatus(ctx, expiredID, domain.ProposalExpired)
}

func (r *Runner) insertSnapshotAt(ctx context.Context, groupID string, at time.Time, pot int64, net *int64) error {
	_, err := r.DB.ExecContext(ctx, `
INSERT INTO nav_snapshots (group_id, pot_nav_micros, nav_per_share_micros, total_shares, reason, created_at, net_contributed_micros)
VALUES ($1, $2, 1000000, $2, 'transaction_confirm', $3, $4)`,
		groupID, pot, at.UTC(), net)
	if err != nil {
		return fmt.Errorf("insert nav snapshot: %w", err)
	}
	return nil
}

func (r *Runner) insertOpenProposal(ctx context.Context, groupID, proposerID string, kind domain.ProposalKind, symbol string, usdcMicros, tokenAmount int64, expiresAt time.Time) (string, error) {
	tx, err := r.Store.BeginTx(ctx)
	if err != nil {
		return "", err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	row, err := r.Store.InsertProposalTx(ctx, tx, postgres.InsertProposalParams{
		GroupID:     groupID,
		ProposerID:  proposerID,
		Symbol:      symbol,
		Kind:        kind,
		UsdcMicros:  usdcMicros,
		TokenAmount: tokenAmount,
		ExpiresAt:   expiresAt.UTC(),
	})
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	committed = true
	return row.ID, nil
}

func (r *Runner) closeProposal(ctx context.Context, proposalID, voterID string, choice domain.VoteChoice, to domain.ProposalStatus) error {
	tx, err := r.Store.BeginTx(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, _, err := r.Store.InsertVoteTx(ctx, tx, proposalID, voterID, choice); err != nil {
		return err
	}
	ok, err := r.Store.UpdateProposalStatusTx(ctx, tx, proposalID, domain.ProposalOpen, to)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("proposal %s status not updated to %s", proposalID, to)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (r *Runner) setProposalStatus(ctx context.Context, proposalID string, to domain.ProposalStatus) error {
	res, err := r.DB.ExecContext(ctx, `UPDATE proposals SET status = $2 WHERE id = $1`, proposalID, string(to))
	if err != nil {
		return fmt.Errorf("set proposal status: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("proposal %s not found", proposalID)
	}
	return nil
}

func (r *Runner) seedActiveAgent(ctx context.Context, groupID, proposerID string) error {
	tx, err := r.Store.BeginTx(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	expires := r.Now().Add(24 * time.Hour).UTC()
	proposal, err := r.Store.InsertProposalTx(ctx, tx, postgres.InsertProposalParams{
		GroupID:              groupID,
		ProposerID:           proposerID,
		Kind:                 domain.ProposalKindAddAgent,
		AgentDisplayName:     "Weekend Bot",
		AllocationUsdcMicros: 2_000_000,
		ExpiresAt:            expires,
	})
	if err != nil {
		return err
	}
	if _, _, err := r.Store.InsertVoteTx(ctx, tx, proposal.ID, proposerID, domain.VoteYes); err != nil {
		return err
	}
	if _, err := r.Store.UpdateProposalStatusTx(ctx, tx, proposal.ID, domain.ProposalOpen, domain.ProposalPassed); err != nil {
		return err
	}

	plaintext, hash, prefix, err := app.MintAgentAPIKey()
	if err != nil {
		return err
	}
	agentRow := postgres.GroupAgentRow{
		GroupID:              groupID,
		Status:               domain.AgentStatusActive,
		InstallProposalID:    sql.NullString{String: proposal.ID, Valid: true},
		AgentDisplayName:     "Weekend Bot",
		AllocationUsdcMicros: 2_000_000,
		APIKeyHash:           sql.NullString{String: hash, Valid: true},
		APIKeyPrefix:         sql.NullString{String: prefix, Valid: true},
	}
	if _, err := r.Store.InsertGroupAgentTx(ctx, tx, agentRow); err != nil {
		return err
	}
	if err := r.Store.InsertAgentKeyRevealTx(ctx, tx, proposal.ID, plaintext); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	slog.Info("seed demo agent", "group_id", groupID, "agent", "Weekend Bot")
	return nil
}

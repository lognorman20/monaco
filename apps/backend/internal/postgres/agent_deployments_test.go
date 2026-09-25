package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/monaco/monaco/packages/domain"
)

func TestUSDCOnlyPotNavMicros(t *testing.T) {
	cases := []struct {
		name                     string
		treasury, deployed, shrs int64
		want                     int64
	}{
		{"cash only", 10, 0, 10, 10},
		{"uncredited cash clamps to shares", 15, 0, 10, 10},
		{"empty treasury falls back to shares", 0, 0, 10, 10},
		// Funded 10, deployed 6: 4 left in the treasury plus 6 outstanding.
		{"deployed added after clamp", 4, 6, 10, 10},
		// Everything deployed: no share fallback, or the cash would count twice.
		{"all cash deployed", 0, 10, 10, 10},
		{"clamp applies to treasury cash only", 12, 5, 10, 15},
	}
	for _, tc := range cases {
		if got := USDCOnlyPotNavMicros(tc.treasury, tc.deployed, tc.shrs); got != tc.want {
			t.Fatalf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestAgentDeploymentProposals_checkConstraints(t *testing.T) {
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	userID, groupID := seedProposalGroup(t, store, iso)
	expires := time.Now().Add(time.Hour)

	insert := func(kind string, usdc, allocation any, wallet any) error {
		_, err := db.ExecContext(ctx, `
INSERT INTO proposals (group_id, proposer_id, symbol, kind, usdc_micros, allocation_usdc_micros, agent_wallet_address, status, expires_at)
VALUES ($1, $2, 'x', $3, $4, $5, $6, 'open', $7)`, groupID, userID, kind, usdc, allocation, wallet, expires)
		return err
	}
	const wallet = "9WzDXwBbmkg8ZTbNMqUxvQRAyrZzDsGYdLVL9zYtAWWM"
	if err := insert("deploy_agent", 1_000_000, nil, wallet); err != nil {
		t.Fatalf("valid deploy_agent rejected: %v", err)
	}
	if err := insert("recall_agent", nil, nil, wallet); err != nil {
		t.Fatalf("valid recall_agent rejected: %v", err)
	}
	for name, args := range map[string][]any{
		"deploy without wallet":       {"deploy_agent", 1_000_000, nil, "  "},
		"deploy without usdc":         {"deploy_agent", nil, nil, wallet},
		"deploy with allocation":      {"deploy_agent", 1_000_000, 5, wallet},
		"recall with usdc":            {"recall_agent", 1_000_000, nil, wallet},
		"recall without wallet":       {"recall_agent", nil, nil, nil},
		"unknown kind still rejected": {"fund_agent", 1_000_000, nil, wallet},
	} {
		if err := insert(args[0].(string), args[1], args[2], args[3]); err == nil {
			t.Fatalf("%s: constraint accepted it", name)
		}
	}

	// The store round-trips the wallet and falls back to it for symbol.
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	row, err := store.InsertProposalTx(ctx, tx, InsertProposalParams{
		GroupID: groupID, ProposerID: userID, Kind: domain.ProposalKindDeployAgent,
		UsdcMicros: 2_000_000, AgentWalletAddress: " " + wallet + " ", ExpiresAt: expires,
	})
	if err != nil {
		t.Fatalf("InsertProposalTx deploy: %v", err)
	}
	if row.AgentWalletAddress != wallet || row.Symbol != wallet || row.UsdcMicros != 2_000_000 || row.Kind != domain.ProposalKindDeployAgent {
		t.Fatalf("deploy row = %+v", row)
	}
}

func TestAgentDeployment_oneActivePerWalletAndNavReason(t *testing.T) {
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	userID, groupID := seedProposalGroup(t, store, iso)
	const wallet = "9WzDXwBbmkg8ZTbNMqUxvQRAyrZzDsGYdLVL9zYtAWWM"

	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	newProposal := func() string {
		row, err := store.InsertProposalTx(ctx, tx, InsertProposalParams{
			GroupID: groupID, ProposerID: userID, Kind: domain.ProposalKindDeployAgent,
			UsdcMicros: 1_000_000, AgentWalletAddress: wallet, ExpiresAt: time.Now().Add(time.Hour),
		})
		if err != nil {
			t.Fatal(err)
		}
		return row.ID
	}
	if _, err := store.InsertAgentDeploymentTx(ctx, tx, InsertAgentDeploymentParams{
		GroupID: groupID, AgentWalletAddress: wallet, DeployedUsdcMicros: 1_000_000, DeployProposalID: newProposal(),
	}); err != nil {
		t.Fatalf("first deployment: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `SAVEPOINT dup`); err != nil {
		t.Fatal(err)
	}
	_, err = store.InsertAgentDeploymentTx(ctx, tx, InsertAgentDeploymentParams{
		GroupID: groupID, AgentWalletAddress: wallet, DeployedUsdcMicros: 1_000_000, DeployProposalID: newProposal(),
	})
	if !IsUniqueViolation(err) {
		t.Fatalf("second active deployment err = %v, want unique violation", err)
	}
	if _, err := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT dup`); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteNavSnapshotOnAgentDeploymentTx(ctx, tx, groupID, 0); err != nil {
		t.Fatalf("agent_deployment nav reason rejected: %v", err)
	}
}

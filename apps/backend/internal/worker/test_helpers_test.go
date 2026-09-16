package worker

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

type workerTestApp struct {
	DB       *sql.DB
	Store    *postgres.Store
	Privy    privy.Client
	Deposits *app.DepositService
	Now      time.Time
}

func integrationWorkerApp(t *testing.T) *workerTestApp {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is not set")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	_, err = db.ExecContext(ctx, "TRUNCATE users, member_wallets, groups, treasuries, deposits, positions, withdrawals RESTART IDENTITY CASCADE")
	if err != nil {
		t.Fatalf("reset tables: %v", err)
	}

	store := postgres.NewStore(db)
	privyClient := privy.NewFakeClient()
	return &workerTestApp{
		DB:       db,
		Store:    store,
		Privy:    privyClient,
		Deposits: app.NewDepositService(store, privyClient),
		Now:      time.Unix(1_700_000_000, 0).UTC(),
	}
}

func seedPendingDeposit(t *testing.T, store *postgres.Store, privyClient privy.Client) (postgres.DepositRow, string, string) {
	t.Helper()
	ctx := context.Background()
	privyUserID := "did:privy:worker-" + t.Name()
	token := privy.AccessToken("worker-token-" + t.Name())
	privy.RegisterToken(privyClient, token, privy.Identity{PrivyUserID: privyUserID, DisplayName: "Worker"})
	sessions := app.NewSessionService(store, privyClient)
	if _, err := sessions.OpenSession(ctx, string(token)); err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	groups := app.NewGroupService(store, privyClient)
	group, err := groups.CreateGroup(ctx, string(token), "Worker Fund")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	deposits := app.NewDepositService(store, privyClient)
	result, err := deposits.CreateDeposit(ctx, string(token), group.GroupID, 2_000_000)
	if err != nil {
		t.Fatalf("CreateDeposit: %v", err)
	}
	row, found, err := store.GetDepositByID(ctx, result.Deposit.ID)
	if err != nil || !found {
		t.Fatalf("GetDepositByID: found=%v err=%v", found, err)
	}
	treasury, _ := privyClient.EnsureTreasury(ctx, privy.GroupID(group.GroupID))
	return row, result.Deposit.FromAddress, treasury.SolanaAddress
}

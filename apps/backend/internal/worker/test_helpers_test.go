package worker

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
)

type workerTestApp struct {
	DB       *sql.DB
	Store    *postgres.Store
	Privy    wallets.Client
	Deposits *app.DepositService
	ISO      *postgres.TestIsolation
	Now      time.Time
}

func integrationWorkerApp(t *testing.T) *workerTestApp {
	t.Helper()
	db := postgres.OpenTestDB(t)
	iso := postgres.PrepareTestDB(t, db)

	store := postgres.NewStore(db)
	privyClient := wallets.NewFakeClient()
	pythClient := chainlink.NewFakeClient()
	return &workerTestApp{
		DB:       db,
		Store:    store,
		Privy:    privyClient,
		Deposits: app.NewDepositService(store, privyClient, pythClient, app.NewSymbolResolver(nil)),
		ISO:      iso,
		Now:      time.Unix(1_700_000_000, 0).UTC(),
	}
}

func seedPendingDepositWithToken(t *testing.T, testApp *workerTestApp) (postgres.DepositRow, string, string) {
	t.Helper()
	return seedPendingDeposit(t, testApp)
}

func seedPendingDeposit(t *testing.T, testApp *workerTestApp) (postgres.DepositRow, string, string) {
	t.Helper()
	ctx := context.Background()
	privyUserID := testApp.ISO.UniqueDynamicID("member")
	token := auth.AccessToken(testApp.ISO.UniqueToken("member"))
	auth.RegisterToken(testApp.Privy, token, auth.Identity{PrivyUserID: privyUserID, DisplayName: "Worker"})
	sessions := app.NewSessionService(testApp.Store, auth.NewFakeVerifier(), testApp.Privy)
	session, err := sessions.OpenSession(ctx, string(token))
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	testApp.ISO.TrackUser(session.UserID)
	groups := app.NewGroupService(testApp.Store, testApp.Privy)
	group, err := groups.CreateGroup(ctx, string(token), "Worker Fund "+testApp.ISO.Suffix())
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	testApp.ISO.TrackGroup(group.GroupID)
	deposits := app.NewDepositService(testApp.Store, testApp.Privy, chainlink.NewFakeClient(), app.NewSymbolResolver(nil))
	result, err := deposits.CreateDeposit(ctx, string(token), group.GroupID, 2_000_000)
	if err != nil {
		t.Fatalf("CreateDeposit: %v", err)
	}
	row, found, err := testApp.Store.GetDepositByID(ctx, result.Deposit.ID)
	if err != nil || !found {
		t.Fatalf("GetDepositByID: found=%v err=%v", found, err)
	}
	return row, result.Deposit.FromAddress, string(token)
}

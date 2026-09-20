package worker

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/auth"
	"github.com/monaco/monaco/apps/backend/internal/chainlink"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
)

type workerTestApp struct {
	DB       *sql.DB
	Store    *postgres.Store
	Auth     auth.Verifier
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
	walletClient := wallets.NewFakeClient()
	verifier := auth.NewFakeVerifier()
	marksClient := chainlink.NewFakeClient()
	return &workerTestApp{
		DB:       db,
		Store:    store,
		Auth:     verifier,
		Privy:    walletClient,
		Deposits: app.NewDepositService(store, verifier, walletClient, marksClient, app.NewSymbolResolver(nil)),
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
	dynamicUserID := testApp.ISO.UniqueDynamicID("member")
	token := auth.AccessToken(testApp.ISO.UniqueToken("member"))
	auth.RegisterToken(testApp.Auth, token, auth.Identity{DynamicUserID: dynamicUserID, DisplayName: "Worker"})
	sessions := app.NewSessionService(testApp.Store, testApp.Auth, testApp.Privy)
	session, err := sessions.OpenSession(ctx, string(token))
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	testApp.ISO.TrackUser(session.UserID)
	groups := app.NewGroupService(testApp.Store, testApp.Auth, testApp.Privy)
	group, err := groups.CreateGroup(ctx, string(token), "Worker Fund "+testApp.ISO.Suffix())
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	testApp.ISO.TrackGroup(group.GroupID)
	result, err := testApp.Deposits.CreateDeposit(ctx, string(token), group.GroupID, 2_000_000)
	if err != nil {
		t.Fatalf("CreateDeposit: %v", err)
	}
	row, found, err := testApp.Store.GetDepositByID(ctx, result.Deposit.ID)
	if err != nil || !found {
		t.Fatalf("GetDepositByID: found=%v err=%v", found, err)
	}
	return row, result.Deposit.FromAddress, string(token)
}

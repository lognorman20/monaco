package worker

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
)

type workerTestApp struct {
	DB       *sql.DB
	Store    *postgres.Store
	Privy    privy.Client
	Deposits *app.DepositService
	ISO      *postgres.TestIsolation
	Now      time.Time
}

func integrationWorkerApp(t *testing.T) *workerTestApp {
	t.Helper()
	db := postgres.OpenTestDB(t)
	iso := postgres.PrepareTestDB(t, db)

	store := postgres.NewStore(db)
	privyClient := privy.NewFakeClient()
	pythClient := pyth.NewFakeClient()
	return &workerTestApp{
		DB:       db,
		Store:    store,
		Privy:    privyClient,
		Deposits: app.NewDepositService(store, privyClient, pythClient, app.NewSymbolResolver(nil)),
		ISO:      iso,
		Now:      time.Unix(1_700_000_000, 0).UTC(),
	}
}

// markOtherTreasuriesSurplusChecked stamps every treasury outside groupIDs as just checked, so
// rows other tests left in the shared database do not take this tick's reconcile slots.
func markOtherTreasuriesSurplusChecked(t *testing.T, db *sql.DB, now time.Time, groupIDs ...string) {
	t.Helper()
	if groupIDs == nil {
		groupIDs = []string{}
	}
	if _, err := db.ExecContext(context.Background(), `
UPDATE treasuries SET surplus_checked_at = $1 WHERE NOT (group_id = ANY($2::uuid[]))`,
		now.UTC(), groupIDs,
	); err != nil {
		t.Fatalf("mark other treasuries surplus checked: %v", err)
	}
}

// sweepClient narrows the fake Privy client to the sweep poller's view of it.
func sweepClient(t *testing.T, client privy.Client) privy.SweepClient {
	t.Helper()
	sweeps, ok := client.(privy.SweepClient)
	if !ok {
		t.Fatalf("privy client %T does not implement privy.SweepClient", client)
	}
	return sweeps
}

func seedPendingDepositWithToken(t *testing.T, testApp *workerTestApp) (postgres.DepositRow, string, string) {
	t.Helper()
	return seedPendingDeposit(t, testApp)
}

func seedPendingDeposit(t *testing.T, testApp *workerTestApp) (postgres.DepositRow, string, string) {
	t.Helper()
	return seedPendingDepositAs(t, testApp, "member")
}

// seedPendingDepositAs seeds a pending deposit for the member named label in a new group.
// Distinct labels give distinct users and member wallets.
func seedPendingDepositAs(t *testing.T, testApp *workerTestApp, label string) (postgres.DepositRow, string, string) {
	t.Helper()
	ctx := context.Background()
	privyUserID := testApp.ISO.UniquePrivyID(label)
	token := privy.AccessToken(testApp.ISO.UniqueToken(label))
	privy.RegisterToken(testApp.Privy, token, privy.Identity{PrivyUserID: privyUserID, DisplayName: "Worker"})
	sessions := app.NewSessionService(testApp.Store, testApp.Privy)
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
	deposits := app.NewDepositService(testApp.Store, testApp.Privy, pyth.NewFakeClient(), app.NewSymbolResolver(nil))
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

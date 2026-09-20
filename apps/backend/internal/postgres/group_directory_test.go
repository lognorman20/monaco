package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/monaco/monaco/packages/domain"
)

// insertNamedGroup creates a group with an exact name (no suffix appended) and
// makes creatorID its first member.
func insertNamedGroup(t *testing.T, ctx context.Context, store *Store, iso *TestIsolation, creatorID, name string, mode domain.JoinMode) string {
	t.Helper()
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	group, err := store.InsertGroupWithRulesTx(ctx, tx, name, creatorID, domain.GroupRules{
		JoinPolicy:        domain.JoinPolicy{Mode: mode},
		VoterSet:          domain.VoterSet{Mode: domain.VoterSetAllMembers},
		Threshold:         domain.ThresholdMajority,
		VoteExpirySeconds: 86_400,
	})
	if err != nil {
		t.Fatalf("InsertGroupWithRulesTx: %v", err)
	}
	iso.TrackGroup(group.ID)
	if err := store.InsertGroupMemberTx(ctx, tx, group.ID, creatorID); err != nil {
		t.Fatalf("InsertGroupMemberTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return group.ID
}

func seedDirectoryUser(t *testing.T, ctx context.Context, store *Store, iso *TestIsolation, label string) string {
	t.Helper()
	user, err := store.UpsertUser(ctx, iso.UniquePrivyID(label), "Ada "+label)
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	iso.TrackUser(user.ID)
	return user.ID
}

func searchNames(rows []GroupSearchRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Name)
	}
	return out
}

func assertNames(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("names = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("names = %q, want %q", got, want)
		}
	}
}

func TestSearchGroupDirectory_isCaseInsensitiveAndRanksPrefixFirst(t *testing.T) {
	t.Parallel()
	// Arrange
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	userID := seedDirectoryUser(t, ctx, store, iso, "search-case")
	token := "zq" + iso.Suffix()
	insertNamedGroup(t, ctx, store, iso, userID, "The "+token+" crew", domain.JoinModeOpen)
	insertNamedGroup(t, ctx, store, iso, userID, token+" Beta", domain.JoinModeOpen)
	insertNamedGroup(t, ctx, store, iso, userID, "ZQ"+iso.Suffix()+" alpha", domain.JoinModeRequest)

	// Act
	rows, err := store.SearchGroupDirectory(ctx, "Zq"+iso.Suffix(), 10, nil)

	// Assert
	if err != nil {
		t.Fatalf("SearchGroupDirectory: %v", err)
	}
	assertNames(t, searchNames(rows), "ZQ"+iso.Suffix()+" alpha", token+" Beta", "The "+token+" crew")
	if rows[0].MatchRank != 0 || rows[2].MatchRank != 1 {
		t.Fatalf("match ranks = %d, %d; want prefix 0 and contains 1", rows[0].MatchRank, rows[2].MatchRank)
	}
	if rows[0].JoinMode != domain.JoinModeRequest {
		t.Fatalf("join mode = %q, want request", rows[0].JoinMode)
	}
}

func TestSearchGroupDirectory_treatsLikeWildcardsLiterally(t *testing.T) {
	t.Parallel()
	// Arrange
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	userID := seedDirectoryUser(t, ctx, store, iso, "search-wild")
	token := "zw" + iso.Suffix()
	insertNamedGroup(t, ctx, store, iso, userID, token+" plain", domain.JoinModeOpen)
	insertNamedGroup(t, ctx, store, iso, userID, token+"% club", domain.JoinModeOpen)
	insertNamedGroup(t, ctx, store, iso, userID, token+"_x", domain.JoinModeOpen)
	insertNamedGroup(t, ctx, store, iso, userID, token+`\ back`, domain.JoinModeOpen)

	cases := []struct {
		query string
		want  string
	}{
		{token + "%", token + "% club"},
		{token + "_", token + "_x"},
		{token + `\`, token + `\ back`},
	}
	for _, tc := range cases {
		// Act
		rows, err := store.SearchGroupDirectory(ctx, tc.query, 10, nil)

		// Assert
		if err != nil {
			t.Fatalf("SearchGroupDirectory(%q): %v", tc.query, err)
		}
		assertNames(t, searchNames(rows), tc.want)
	}
}

func TestSearchGroupDirectory_keysetPagesCoverEveryMatchOnce(t *testing.T) {
	t.Parallel()
	// Arrange
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	userID := seedDirectoryUser(t, ctx, store, iso, "search-page")
	token := "zp" + iso.Suffix()
	for _, name := range []string{token + " d", token + " a", "x " + token, token + " c", token + " b"} {
		insertNamedGroup(t, ctx, store, iso, userID, name, domain.JoinModeOpen)
	}

	// Act
	var all []string
	var after *GroupSearchCursor
	pages := 0
	for {
		rows, err := store.SearchGroupDirectory(ctx, token, 2, after)
		if err != nil {
			t.Fatalf("SearchGroupDirectory: %v", err)
		}
		pages++
		all = append(all, searchNames(rows)...)
		if len(rows) < 2 {
			break
		}
		last := rows[len(rows)-1]
		after = &GroupSearchCursor{MatchRank: last.MatchRank, SortName: last.SortName, GroupID: last.ID}
	}

	// Assert
	assertNames(t, all, token+" a", token+" b", token+" c", token+" d", "x "+token)
	if pages != 3 {
		t.Fatalf("pages = %d, want 3", pages)
	}
}

func TestSearchGroupDirectory_reportsMemberCountAndNetIn(t *testing.T) {
	t.Parallel()
	// Arrange
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	ada := seedDirectoryUser(t, ctx, store, iso, "count-a")
	ben := seedDirectoryUser(t, ctx, store, iso, "count-b")
	token := "zc" + iso.Suffix()
	groupID := insertNamedGroup(t, ctx, store, iso, ada, token+" pot", domain.JoinModeOpen)
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := store.InsertGroupMemberTx(ctx, tx, groupID, ben); err != nil {
		t.Fatalf("InsertGroupMemberTx: %v", err)
	}
	if _, err := store.IncrementPositionTx(ctx, tx, ada, groupID, 30_000_000, 30_000_000); err != nil {
		t.Fatalf("IncrementPositionTx: %v", err)
	}
	if _, err := store.IncrementPositionTx(ctx, tx, ben, groupID, 20_000_000, 20_000_000); err != nil {
		t.Fatalf("IncrementPositionTx: %v", err)
	}
	if _, err := store.IncrementAmountWithdrawnTx(ctx, tx, ben, groupID, 5_000_000); err != nil {
		t.Fatalf("IncrementAmountWithdrawnTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Act
	rows, err := store.SearchGroupDirectory(ctx, token, 10, nil)

	// Assert
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows = %v, err = %v", rows, err)
	}
	if rows[0].MemberCount != 2 {
		t.Fatalf("member count = %d, want 2", rows[0].MemberCount)
	}
	if rows[0].NetUsdcInMicros != 45_000_000 {
		t.Fatalf("net in = %d, want 45_000_000", rows[0].NetUsdcInMicros)
	}
}

func TestInsertNavSnapshotTx_recordsNetContributedFromSameTransaction(t *testing.T) {
	t.Parallel()
	// Arrange
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	userID := seedDirectoryUser(t, ctx, store, iso, "snap-net")
	groupID := insertOpenGroup(t, ctx, store, iso, userID, "Net cabal")
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := store.IncrementPositionTx(ctx, tx, userID, groupID, 25_000_000, 25_000_000); err != nil {
		t.Fatalf("IncrementPositionTx: %v", err)
	}

	// Act
	row, err := store.InsertNavSnapshotTx(ctx, tx, groupID, NavSnapshotReasonDeposit, NavSnapshotValues{
		PotNavMicros: 25_000_000, NavPerShareMicros: 1_000_000, TotalShares: 25_000_000,
	})
	if err != nil {
		t.Fatalf("InsertNavSnapshotTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Assert
	if row.NetContributedMicros == nil || *row.NetContributedMicros != 25_000_000 {
		t.Fatalf("net contributed = %v, want 25_000_000 (includes this tx's credit)", row.NetContributedMicros)
	}
	if row.CreatedAt.Location() != time.UTC {
		t.Fatalf("created_at location = %v, want UTC", row.CreatedAt.Location())
	}
}

func insertSnapshotAt(t *testing.T, ctx context.Context, db *sql.DB, groupID string, at time.Time, pot int64, net *int64) {
	t.Helper()
	_, err := db.ExecContext(ctx, `
INSERT INTO nav_snapshots (group_id, pot_nav_micros, nav_per_share_micros, total_shares, reason, created_at, net_contributed_micros)
VALUES ($1, $2, 1000000, $2, 'deposit', $3, $4)`, groupID, pot, at.UTC(), net)
	if err != nil {
		t.Fatalf("insert nav snapshot: %v", err)
	}
}

func TestListNavSnapshotsForGroupsWindow_returnsOneBaselineThenWindowRows(t *testing.T) {
	t.Parallel()
	// Arrange
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	userID := seedDirectoryUser(t, ctx, store, iso, "snap-window")
	groupA := insertOpenGroup(t, ctx, store, iso, userID, "Window A")
	groupB := insertOpenGroup(t, ctx, store, iso, userID, "Window B")
	since := time.Now().UTC().Add(-24 * time.Hour)
	net := int64(10_000_000)
	insertSnapshotAt(t, ctx, db, groupA, since.Add(-72*time.Hour), 1, &net)
	insertSnapshotAt(t, ctx, db, groupA, since.Add(-48*time.Hour), 2, nil)
	insertSnapshotAt(t, ctx, db, groupA, since.Add(2*time.Hour), 3, &net)
	insertSnapshotAt(t, ctx, db, groupA, since.Add(1*time.Hour), 4, &net)
	insertSnapshotAt(t, ctx, db, groupB, since.Add(3*time.Hour), 5, &net)

	// Act
	rows, err := store.ListNavSnapshotsForGroupsWindow(ctx, []string{groupA, groupB}, since)

	// Assert
	if err != nil {
		t.Fatalf("ListNavSnapshotsForGroupsWindow: %v", err)
	}
	var potsA, potsB []int64
	var baselineA *NavSnapshotRow
	for i, r := range rows {
		if r.GroupID == groupA {
			if baselineA == nil {
				baselineA = &rows[i]
			}
			potsA = append(potsA, r.PotNavMicros)
		} else {
			potsB = append(potsB, r.PotNavMicros)
		}
	}
	if len(potsA) != 3 || potsA[0] != 2 || potsA[1] != 4 || potsA[2] != 3 {
		t.Fatalf("group A pots = %v, want [2 4 3] (latest pre-window baseline, then window ascending)", potsA)
	}
	if len(potsB) != 1 || potsB[0] != 5 {
		t.Fatalf("group B pots = %v, want [5]", potsB)
	}
	if baselineA.NetContributedMicros != nil {
		t.Fatalf("legacy baseline net contributed = %d, want nil", *baselineA.NetContributedMicros)
	}
}

func TestListContributionEventsAfter_signsDepositsPositiveAndPayoutsNegative(t *testing.T) {
	t.Parallel()
	// Arrange
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	userID := seedDirectoryUser(t, ctx, store, iso, "events")
	groupID := insertOpenGroup(t, ctx, store, iso, userID, "Events")
	before := time.Now().UTC().Add(-time.Minute)

	confirmed, err := store.InsertDeposit(ctx, userID, groupID, 40_000_000, "member-wallet-"+iso.Suffix())
	if err != nil {
		t.Fatalf("InsertDeposit: %v", err)
	}
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, _, err := store.ConfirmDepositTx(ctx, tx, confirmed.ID, "sig-dep-"+iso.Suffix()); err != nil {
		t.Fatalf("ConfirmDepositTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if _, err := store.InsertDeposit(ctx, userID, groupID, 7_000_000, "member-wallet-"+iso.Suffix()); err != nil {
		t.Fatalf("InsertDeposit pending: %v", err)
	}
	withdrawal, err := store.InsertWithdrawal(ctx, userID, groupID, 15_000_000, "payout-"+iso.Suffix())
	if err != nil {
		t.Fatalf("InsertWithdrawal: %v", err)
	}
	tx, err = store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := store.IncrementPositionTx(ctx, tx, userID, groupID, 40_000_000, 40_000_000); err != nil {
		t.Fatalf("IncrementPositionTx: %v", err)
	}
	if _, _, err := store.ConfirmWithdrawalPayoutTx(ctx, tx, withdrawal.ID, "sig-wd-"+iso.Suffix(), NavSnapshotValues{PotNavMicros: 25_000_000, NavPerShareMicros: 625_000, TotalShares: 40_000_000}); err != nil {
		t.Fatalf("ConfirmWithdrawalPayoutTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Act
	events, err := store.ListContributionEventsAfter(ctx, []string{groupID}, before)

	// Assert
	if err != nil {
		t.Fatalf("ListContributionEventsAfter: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %+v, want confirmed deposit and payout only", events)
	}
	if events[0].AmountMicros != 40_000_000 || events[1].AmountMicros != -15_000_000 {
		t.Fatalf("amounts = %d, %d; want +40_000_000 then -15_000_000", events[0].AmountMicros, events[1].AmountMicros)
	}
}

func TestEscapeLikePattern_escapesBackslashFirst(t *testing.T) {
	t.Parallel()
	if got := EscapeLikePattern(`50%_off\`); got != `50\%\_off\\` {
		t.Fatalf("EscapeLikePattern = %q", got)
	}
}

package postgres

import (
	"context"
	"errors"
	"testing"
)

func seedWatchlistUser(t *testing.T, store *Store, iso *TestIsolation, label string) string {
	t.Helper()
	user, err := store.UpsertUser(context.Background(), iso.UniquePrivyID(label), "Watcher "+label)
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	iso.TrackUser(user.ID)
	return user.ID
}

func watchlistSymbols(entries []WatchlistEntry) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Symbol)
	}
	return out
}

func equalSymbols(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestWatchlist_addsAppendInOrderAndListBack(t *testing.T) {
	// Arrange
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	userID := seedWatchlistUser(t, store, iso, "order")

	// Act
	for _, symbol := range []string{"AAPLx", "NVDAx", "tSpaceX"} {
		if _, created, err := store.AddToWatchlist(ctx, userID, symbol, 40); err != nil || !created {
			t.Fatalf("AddToWatchlist(%s) created=%v err=%v", symbol, created, err)
		}
	}
	listed, err := store.ListWatchlist(ctx, userID)

	// Assert
	if err != nil {
		t.Fatalf("ListWatchlist: %v", err)
	}
	if got, want := watchlistSymbols(listed), []string{"AAPLx", "NVDAx", "tSpaceX"}; !equalSymbols(got, want) {
		t.Fatalf("watchlist = %v, want %v", got, want)
	}
	if listed[0].Position != 0 || listed[2].Position != 2 {
		t.Fatalf("positions = %d..%d, want 0..2", listed[0].Position, listed[2].Position)
	}
}

func TestWatchlist_addingAStockTwiceInAnyCasingKeepsOneRowInPlace(t *testing.T) {
	// Arrange
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	userID := seedWatchlistUser(t, store, iso, "dup")
	if _, _, err := store.AddToWatchlist(ctx, userID, "AAPLx", 40); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if _, _, err := store.AddToWatchlist(ctx, userID, "TSLAx", 40); err != nil {
		t.Fatalf("second add: %v", err)
	}

	// Act
	entry, created, err := store.AddToWatchlist(ctx, userID, "aaplx", 40)

	// Assert
	if err != nil {
		t.Fatalf("re-add: %v", err)
	}
	if created || entry.Symbol != "AAPLx" || entry.Position != 0 {
		t.Fatalf("re-add = %+v created=%v, want the existing AAPLx at 0", entry, created)
	}
	listed, _ := store.ListWatchlist(ctx, userID)
	if got := watchlistSymbols(listed); !equalSymbols(got, []string{"AAPLx", "TSLAx"}) {
		t.Fatalf("watchlist = %v", got)
	}
}

func TestWatchlist_fullListRefusesAnotherStock(t *testing.T) {
	// Arrange
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	userID := seedWatchlistUser(t, store, iso, "full")
	for _, symbol := range []string{"AAPLx", "NVDAx"} {
		if _, _, err := store.AddToWatchlist(ctx, userID, symbol, 2); err != nil {
			t.Fatalf("add %s: %v", symbol, err)
		}
	}

	// Act
	_, _, err := store.AddToWatchlist(ctx, userID, "TSLAx", 2)

	// Assert
	if !errors.Is(err, ErrWatchlistFull) {
		t.Fatalf("err = %v, want ErrWatchlistFull", err)
	}
}

func TestWatchlist_removeDropsOneStockAndReportsWhetherItWasThere(t *testing.T) {
	// Arrange
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	userID := seedWatchlistUser(t, store, iso, "remove")
	for _, symbol := range []string{"AAPLx", "NVDAx", "TSLAx"} {
		if _, _, err := store.AddToWatchlist(ctx, userID, symbol, 40); err != nil {
			t.Fatalf("add %s: %v", symbol, err)
		}
	}

	// Act
	removed, err := store.RemoveFromWatchlist(ctx, userID, "nvdax")
	again, againErr := store.RemoveFromWatchlist(ctx, userID, "NVDAx")

	// Assert
	if err != nil || !removed {
		t.Fatalf("remove = %v, %v; want true", removed, err)
	}
	if againErr != nil || again {
		t.Fatalf("second remove = %v, %v; want false", again, againErr)
	}
	listed, _ := store.ListWatchlist(ctx, userID)
	if got := watchlistSymbols(listed); !equalSymbols(got, []string{"AAPLx", "TSLAx"}) {
		t.Fatalf("watchlist = %v", got)
	}
	watching, _ := store.IsWatching(ctx, userID, "NVDAx")
	if watching {
		t.Fatal("IsWatching(NVDAx) = true after remove")
	}
}

func TestWatchlist_reorderRewritesPositions(t *testing.T) {
	// Arrange
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	userID := seedWatchlistUser(t, store, iso, "reorder")
	for _, symbol := range []string{"AAPLx", "NVDAx", "TSLAx"} {
		if _, _, err := store.AddToWatchlist(ctx, userID, symbol, 40); err != nil {
			t.Fatalf("add %s: %v", symbol, err)
		}
	}

	// Act
	reordered, err := store.ReorderWatchlist(ctx, userID, []string{"tslax", "AAPLx", "NVDAx"})

	// Assert
	if err != nil {
		t.Fatalf("ReorderWatchlist: %v", err)
	}
	if got, want := watchlistSymbols(reordered), []string{"TSLAx", "AAPLx", "NVDAx"}; !equalSymbols(got, want) {
		t.Fatalf("reordered = %v, want %v", got, want)
	}
	listed, _ := store.ListWatchlist(ctx, userID)
	if got := watchlistSymbols(listed); !equalSymbols(got, []string{"TSLAx", "AAPLx", "NVDAx"}) {
		t.Fatalf("listed after reorder = %v", got)
	}
}

func TestWatchlist_reorderThatDoesNotNameEveryStockOnceMovesNothing(t *testing.T) {
	cases := []struct {
		name    string
		symbols []string
	}{
		{name: "missing one", symbols: []string{"NVDAx", "AAPLx"}},
		{name: "one it does not hold", symbols: []string{"NVDAx", "AAPLx", "MSFTx"}},
		{name: "a duplicate", symbols: []string{"NVDAx", "AAPLx", "aaplx"}},
		{name: "an extra", symbols: []string{"NVDAx", "AAPLx", "TSLAx", "MSFTx"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			ctx := context.Background()
			db := integrationDB(t)
			iso := prepareIsolation(t, db)
			store := NewStore(db)
			userID := seedWatchlistUser(t, store, iso, "mismatch")
			for _, symbol := range []string{"AAPLx", "NVDAx", "TSLAx"} {
				if _, _, err := store.AddToWatchlist(ctx, userID, symbol, 40); err != nil {
					t.Fatalf("add %s: %v", symbol, err)
				}
			}

			// Act
			_, err := store.ReorderWatchlist(ctx, userID, tc.symbols)

			// Assert
			if !errors.Is(err, ErrWatchlistOrderMismatch) {
				t.Fatalf("err = %v, want ErrWatchlistOrderMismatch", err)
			}
			listed, _ := store.ListWatchlist(ctx, userID)
			if got := watchlistSymbols(listed); !equalSymbols(got, []string{"AAPLx", "NVDAx", "TSLAx"}) {
				t.Fatalf("watchlist moved to %v", got)
			}
		})
	}
}

func TestWatchlist_isPerMember(t *testing.T) {
	// Arrange
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	alex := seedWatchlistUser(t, store, iso, "alex")
	blair := seedWatchlistUser(t, store, iso, "blair")
	if _, _, err := store.AddToWatchlist(ctx, alex, "AAPLx", 40); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Act
	blairs, err := store.ListWatchlist(ctx, blair)
	removed, removeErr := store.RemoveFromWatchlist(ctx, blair, "AAPLx")

	// Assert
	if err != nil || len(blairs) != 0 {
		t.Fatalf("blair's watchlist = %v, %v; want empty", blairs, err)
	}
	if removeErr != nil || removed {
		t.Fatalf("blair removed alex's row: %v, %v", removed, removeErr)
	}
	watching, _ := store.IsWatching(ctx, alex, "aaplx")
	if !watching {
		t.Fatal("alex should still be watching AAPLx")
	}
}

package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestPriceAlerts_insertListAndDeleteRoundTrip(t *testing.T) {
	// Arrange
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	userID := seedWatchlistUser(t, store, iso, "alerts")

	// Act
	created, err := store.InsertPriceAlert(ctx, userID, "GOOGLx", PriceAlertAbove, 360_000_000, 20)
	if err != nil {
		t.Fatalf("InsertPriceAlert: %v", err)
	}
	listed, listErr := store.ListPriceAlerts(ctx, userID, "", 30*24*time.Hour, 50)

	// Assert
	if listErr != nil {
		t.Fatalf("ListPriceAlerts: %v", listErr)
	}
	if len(listed) != 1 {
		t.Fatalf("listed %d alerts, want 1", len(listed))
	}
	got := listed[0]
	if got.ID != created.ID || got.Symbol != "GOOGLx" || got.Direction != PriceAlertAbove ||
		got.PriceUsdcMicros != 360_000_000 || !got.Active || got.TriggeredAt != nil {
		t.Fatalf("listed alert = %+v, want the active GOOGLx above $360", got)
	}

	deleted, err := store.DeletePriceAlert(ctx, userID, created.ID)
	if err != nil || !deleted {
		t.Fatalf("DeletePriceAlert = %v, %v", deleted, err)
	}
	after, _ := store.ListPriceAlerts(ctx, userID, "", 30*24*time.Hour, 50)
	if len(after) != 0 {
		t.Fatalf("alerts after delete = %d, want 0", len(after))
	}
}

func TestPriceAlerts_capOfActiveAlertsPerMember(t *testing.T) {
	// Arrange
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	userID := seedWatchlistUser(t, store, iso, "cap")
	const maxActive = 20
	for i := 0; i < maxActive; i++ {
		if _, err := store.InsertPriceAlert(ctx, userID, "AAPLx", PriceAlertAbove, int64(240+i)*1_000_000, maxActive); err != nil {
			t.Fatalf("alert %d: %v", i+1, err)
		}
	}

	// Act
	_, err := store.InsertPriceAlert(ctx, userID, "AAPLx", PriceAlertBelow, 200_000_000, maxActive)

	// Assert
	if !errors.Is(err, ErrPriceAlertLimit) {
		t.Fatalf("21st alert err = %v, want ErrPriceAlertLimit", err)
	}
}

func TestPriceAlerts_firedAlertsDoNotCountTowardTheCap(t *testing.T) {
	// Arrange
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	userID := seedWatchlistUser(t, store, iso, "capfired")
	symbol := "CAP" + iso.Suffix()
	if _, err := store.InsertPriceAlert(ctx, userID, symbol, PriceAlertAbove, 100_000_000, 1); err != nil {
		t.Fatalf("first alert: %v", err)
	}
	if _, err := store.TriggerPriceAlerts(ctx, symbol, 101_000_000, time.Now()); err != nil {
		t.Fatalf("trigger: %v", err)
	}

	// Act
	_, err := store.InsertPriceAlert(ctx, userID, symbol, PriceAlertAbove, 120_000_000, 1)

	// Assert
	if err != nil {
		t.Fatalf("alert after the first fired: %v, want room under the cap", err)
	}
}

func TestPriceAlerts_capHoldsUnderConcurrentCreates(t *testing.T) {
	// Arrange
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	userID := seedWatchlistUser(t, store, iso, "race")
	const maxActive, attempts = 3, 8

	// Act
	errs := make(chan error, attempts)
	for i := 0; i < attempts; i++ {
		go func(i int) {
			_, err := store.InsertPriceAlert(ctx, userID, "AAPLx", PriceAlertAbove, int64(250+i)*1_000_000, maxActive)
			errs <- err
		}(i)
	}
	accepted := 0
	for i := 0; i < attempts; i++ {
		if err := <-errs; err == nil {
			accepted++
		} else if !errors.Is(err, ErrPriceAlertLimit) {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	// Assert
	if accepted != maxActive {
		t.Fatalf("accepted %d concurrent alerts, want exactly %d", accepted, maxActive)
	}
}

func TestPriceAlerts_listFiltersBySymbolAndPutsActiveBeforeFired(t *testing.T) {
	// Arrange
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	userID := seedWatchlistUser(t, store, iso, "list")
	fired := "FIRE" + iso.Suffix()
	if _, err := store.InsertPriceAlert(ctx, userID, fired, PriceAlertBelow, 50_000_000, 20); err != nil {
		t.Fatalf("fired alert: %v", err)
	}
	if _, err := store.TriggerPriceAlerts(ctx, fired, 49_000_000, time.Now()); err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if _, err := store.InsertPriceAlert(ctx, userID, fired, PriceAlertAbove, 70_000_000, 20); err != nil {
		t.Fatalf("active alert: %v", err)
	}
	if _, err := store.InsertPriceAlert(ctx, userID, "TSLAx", PriceAlertAbove, 450_000_000, 20); err != nil {
		t.Fatalf("other stock: %v", err)
	}

	// Act
	listed, err := store.ListPriceAlerts(ctx, userID, fired, 30*24*time.Hour, 50)

	// Assert
	if err != nil {
		t.Fatalf("ListPriceAlerts: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("listed %d alerts for %s, want 2", len(listed), fired)
	}
	if !listed[0].Active || listed[1].Active {
		t.Fatalf("order = [%v %v], want the active alert first", listed[0].Active, listed[1].Active)
	}
	if listed[1].TriggeredPriceUsdcMicros == nil || *listed[1].TriggeredPriceUsdcMicros != 49_000_000 {
		t.Fatalf("fired alert mark = %v, want 49000000", listed[1].TriggeredPriceUsdcMicros)
	}
}

func TestPriceAlerts_deleteRefusesAnotherMembersAlert(t *testing.T) {
	// Arrange
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	alex := seedWatchlistUser(t, store, iso, "alex")
	blair := seedWatchlistUser(t, store, iso, "blair")
	alert, err := store.InsertPriceAlert(ctx, alex, "AAPLx", PriceAlertAbove, 240_000_000, 20)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Act
	deleted, err := store.DeletePriceAlert(ctx, blair, alert.ID)

	// Assert
	if err != nil || deleted {
		t.Fatalf("blair deleted alex's alert: %v, %v", deleted, err)
	}
	count, _ := store.CountActivePriceAlerts(ctx, alex, "aaplx")
	if count != 1 {
		t.Fatalf("alex's active alerts on AAPLx = %d, want 1", count)
	}
}

func TestPriceAlerts_triggerFiresEachReachedLineExactlyOnce(t *testing.T) {
	// Arrange
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	userID := seedWatchlistUser(t, store, iso, "trigger")
	symbol := "TRG" + iso.Suffix()
	lines := []struct {
		direction string
		price     int64
	}{
		{PriceAlertAbove, 100_000_000}, // reached: mark 105 is above 100
		{PriceAlertAbove, 105_000_000}, // reached: at the line counts
		{PriceAlertAbove, 110_000_000}, // not reached
		{PriceAlertBelow, 106_000_000}, // reached: mark 105 is below 106
		{PriceAlertBelow, 90_000_000},  // not reached
	}
	for _, line := range lines {
		if _, err := store.InsertPriceAlert(ctx, userID, symbol, line.direction, line.price, 20); err != nil {
			t.Fatalf("insert %s %d: %v", line.direction, line.price, err)
		}
	}
	at := time.Date(2026, 9, 25, 14, 30, 0, 0, time.UTC)

	// Act
	first, err := store.TriggerPriceAlerts(ctx, symbol, 105_000_000, at)
	if err != nil {
		t.Fatalf("first trigger: %v", err)
	}
	second, err := store.TriggerPriceAlerts(ctx, symbol, 105_000_000, at.Add(time.Minute))

	// Assert
	if err != nil {
		t.Fatalf("second trigger: %v", err)
	}
	fired := map[string]bool{}
	for _, alert := range first {
		fired[fmt.Sprintf("%s %d", alert.Direction, alert.PriceUsdcMicros)] = true
		if alert.Active || alert.TriggeredAt == nil || !alert.TriggeredAt.Equal(at) {
			t.Fatalf("fired alert = %+v, want inactive and stamped at %s", alert, at)
		}
	}
	want := []string{"above 100000000", "above 105000000", "below 106000000"}
	if len(first) != len(want) {
		t.Fatalf("first pass fired %v, want %v", fired, want)
	}
	for _, key := range want {
		if !fired[key] {
			t.Fatalf("first pass fired %v, missing %s", fired, key)
		}
	}
	if len(second) != 0 {
		t.Fatalf("second pass fired %d alerts again, want 0", len(second))
	}
	symbols, _ := store.ActivePriceAlertSymbols(ctx)
	found := false
	for _, s := range symbols {
		if s == symbol {
			found = true
		}
	}
	if !found {
		t.Fatalf("active symbols %v should still include %s (two lines not reached)", symbols, symbol)
	}
}

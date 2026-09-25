package worker

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
)

// scriptedMarks is a fake price source: the mark per upper-cased symbol, changeable between
// ticks the way a market moves.
type scriptedMarks struct {
	mu    sync.Mutex
	marks map[string]app.AlertMark
	err   error
	calls int
}

func (s *scriptedMarks) set(symbol, name string, micros int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.marks == nil {
		s.marks = map[string]app.AlertMark{}
	}
	s.marks[strings.ToUpper(symbol)] = app.AlertMark{Symbol: symbol, Name: name, PriceUsdcMicros: micros}
}

func (s *scriptedMarks) Marks(_ context.Context, symbols []string) (map[string]app.AlertMark, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	out := map[string]app.AlertMark{}
	for _, symbol := range symbols {
		if mark, ok := s.marks[strings.ToUpper(symbol)]; ok {
			out[strings.ToUpper(symbol)] = mark
		}
	}
	return out, nil
}

// recordingNotifier keeps every event it is handed.
type recordingNotifier struct {
	mu     sync.Mutex
	events []recordedAlert
	err    error
}

type recordedAlert struct {
	userID string
	event  app.AlertEvent
}

func (r *recordingNotifier) Notify(_ context.Context, userID string, event app.AlertEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, recordedAlert{userID: userID, event: event})
	return r.err
}

func (r *recordingNotifier) recorded() []recordedAlert {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recordedAlert(nil), r.events...)
}

// memoryAlertStore applies the store's trigger rule in memory, for the unit tests.
type memoryAlertStore struct {
	mu     sync.Mutex
	alerts []postgres.PriceAlertRow
}

func (m *memoryAlertStore) ActivePriceAlertSymbols(context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := map[string]bool{}
	out := []string{}
	for _, alert := range m.alerts {
		if alert.Active && !seen[alert.Symbol] {
			seen[alert.Symbol] = true
			out = append(out, alert.Symbol)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (m *memoryAlertStore) TriggerPriceAlerts(_ context.Context, symbol string, mark int64, at time.Time) ([]postgres.PriceAlertRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fired := []postgres.PriceAlertRow{}
	for i := range m.alerts {
		alert := &m.alerts[i]
		if !alert.Active || alert.Symbol != symbol {
			continue
		}
		if !app.AlertDirection(alert.Direction).Reached(mark, alert.PriceUsdcMicros) {
			continue
		}
		stamped := at
		price := mark
		alert.Active = false
		alert.TriggeredAt = &stamped
		alert.TriggeredPriceUsdcMicros = &price
		fired = append(fired, *alert)
	}
	return fired, nil
}

var alertPollerNow = time.Date(2026, 9, 25, 14, 30, 0, 0, time.UTC)

func TestAlertPoller_firesOncePerCrossingAndNotAgainOnTheNextTick(t *testing.T) {
	// Arrange
	store := &memoryAlertStore{alerts: []postgres.PriceAlertRow{
		{ID: "alert-1", UserID: "user-1", Symbol: "GOOGLx", Direction: "above", PriceUsdcMicros: 360_000_000, Active: true},
	}}
	marks := &scriptedMarks{}
	marks.set("GOOGLx", "Alphabet", 352_100_000)
	notifier := &recordingNotifier{}
	poller := NewAlertPoller(store, marks, notifier, NewStubClock(alertPollerNow))
	ctx := context.Background()

	// Act: under the line, then over it, then still over it on the next tick.
	firstFired, _ := poller.Tick(ctx)
	marks.set("GOOGLx", "Alphabet", 361_250_000)
	secondFired, _ := poller.Tick(ctx)
	thirdFired, _ := poller.Tick(ctx)

	// Assert
	if firstFired != 0 || secondFired != 1 || thirdFired != 0 {
		t.Fatalf("fired per tick = %d, %d, %d; want 0, 1, 0", firstFired, secondFired, thirdFired)
	}
	events := notifier.recorded()
	if len(events) != 1 {
		t.Fatalf("notified %d times, want exactly once", len(events))
	}
	got := events[0]
	want := app.AlertEvent{
		AlertID:         "alert-1",
		Symbol:          "GOOGLx",
		Name:            "Alphabet",
		Direction:       app.AlertAbove,
		Threshold:       360_000_000,
		PriceUsdcMicros: 361_250_000,
		TriggeredAt:     alertPollerNow,
	}
	if got.userID != "user-1" || got.event != want {
		t.Fatalf("notified %s with %+v, want user-1 with %+v", got.userID, got.event, want)
	}
}

func TestAlertPoller_belowAlertFiresWhenThePriceFalls(t *testing.T) {
	// Arrange
	store := &memoryAlertStore{alerts: []postgres.PriceAlertRow{
		{ID: "alert-2", UserID: "user-1", Symbol: "TSLAx", Direction: "below", PriceUsdcMicros: 400_000_000, Active: true},
		{ID: "alert-3", UserID: "user-2", Symbol: "TSLAx", Direction: "above", PriceUsdcMicros: 450_000_000, Active: true},
	}}
	marks := &scriptedMarks{}
	marks.set("TSLAx", "Tesla", 398_700_000)
	notifier := &recordingNotifier{}
	poller := NewAlertPoller(store, marks, notifier, NewStubClock(alertPollerNow))

	// Act
	fired, err := poller.Tick(context.Background())

	// Assert
	if err != nil || fired != 1 {
		t.Fatalf("Tick = %d, %v; want 1 fired", fired, err)
	}
	if events := notifier.recorded(); len(events) != 1 || events[0].event.AlertID != "alert-2" {
		t.Fatalf("events = %+v, want only the below alert", events)
	}
}

func TestAlertPoller_aSymbolWithNoMarkWaits(t *testing.T) {
	// Arrange
	store := &memoryAlertStore{alerts: []postgres.PriceAlertRow{
		{ID: "alert-4", UserID: "user-1", Symbol: "NEWx", Direction: "below", PriceUsdcMicros: 50_000_000, Active: true},
	}}
	notifier := &recordingNotifier{}
	poller := NewAlertPoller(store, &scriptedMarks{}, notifier, NewStubClock(alertPollerNow))

	// Act
	fired, err := poller.Tick(context.Background())

	// Assert
	if err != nil || fired != 0 || len(notifier.recorded()) != 0 {
		t.Fatalf("Tick = %d, %v, events %d; want nothing fired on a missing price", fired, err, len(notifier.recorded()))
	}
}

func TestAlertPoller_aFailedPriceReadFiresNothing(t *testing.T) {
	// Arrange
	store := &memoryAlertStore{alerts: []postgres.PriceAlertRow{
		{ID: "alert-5", UserID: "user-1", Symbol: "AAPLx", Direction: "above", PriceUsdcMicros: 1, Active: true},
	}}
	marks := &scriptedMarks{err: errors.New("jupiter down")}
	notifier := &recordingNotifier{}
	poller := NewAlertPoller(store, marks, notifier, NewStubClock(alertPollerNow))

	// Act
	fired, err := poller.Tick(context.Background())

	// Assert
	if err == nil || fired != 0 || len(notifier.recorded()) != 0 {
		t.Fatalf("Tick = %d, %v; want the error and nothing fired", fired, err)
	}
	if !store.alerts[0].Active {
		t.Fatal("alert was deactivated on a failed price read")
	}
}

func TestAlertPoller_aFailedNotificationDoesNotStopTheOthers(t *testing.T) {
	// Arrange
	store := &memoryAlertStore{alerts: []postgres.PriceAlertRow{
		{ID: "alert-6", UserID: "user-1", Symbol: "AAPLx", Direction: "above", PriceUsdcMicros: 230_000_000, Active: true},
		{ID: "alert-7", UserID: "user-2", Symbol: "AAPLx", Direction: "above", PriceUsdcMicros: 231_000_000, Active: true},
	}}
	marks := &scriptedMarks{}
	marks.set("AAPLx", "Apple", 232_050_000)
	notifier := &recordingNotifier{err: errors.New("push service down")}
	poller := NewAlertPoller(store, marks, notifier, NewStubClock(alertPollerNow))

	// Act
	fired, err := poller.Tick(context.Background())

	// Assert
	if err != nil || fired != 2 || len(notifier.recorded()) != 2 {
		t.Fatalf("Tick = %d, %v, events %d; want both alerts handed over", fired, err, len(notifier.recorded()))
	}
}

func TestAlertPoller_noActiveAlertsSkipsThePriceRead(t *testing.T) {
	// Arrange
	marks := &scriptedMarks{}
	poller := NewAlertPoller(&memoryAlertStore{}, marks, &recordingNotifier{}, NewStubClock(alertPollerNow))

	// Act
	fired, err := poller.Tick(context.Background())

	// Assert
	if err != nil || fired != 0 || marks.calls != 0 {
		t.Fatalf("Tick = %d, %v, price reads %d; want no read with nothing to watch", fired, err, marks.calls)
	}
}

// Against the real store: the claim that makes "exactly once" true lives in SQL, so the
// poller is run on it twice with the price past the line both times.
func TestAlertPoller_onTheStore_firesExactlyOnceAcrossTicks(t *testing.T) {
	// Arrange
	a := integrationWorkerApp(t)
	ctx := context.Background()
	user, err := a.Store.UpsertUser(ctx, a.ISO.UniquePrivyID("alerts"), "Alert Watcher")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	a.ISO.TrackUser(user.ID)
	symbol := "ALRT" + a.ISO.Suffix()
	alert, err := a.Store.InsertPriceAlert(ctx, user.ID, symbol, postgres.PriceAlertAbove, 360_000_000, 20)
	if err != nil {
		t.Fatalf("InsertPriceAlert: %v", err)
	}
	waiting, err := a.Store.InsertPriceAlert(ctx, user.ID, symbol, postgres.PriceAlertAbove, 400_000_000, 20)
	if err != nil {
		t.Fatalf("InsertPriceAlert: %v", err)
	}
	marks := &scriptedMarks{}
	marks.set(symbol, "Alert Test Co", 361_000_000)
	notifier := &recordingNotifier{}
	clock := NewStubClock(alertPollerNow)
	poller := NewAlertPoller(a.Store, marks, notifier, clock)

	// Act
	if _, err := poller.Tick(ctx); err != nil {
		t.Fatalf("first tick: %v", err)
	}
	clock.Advance(DefaultAlertPollInterval)
	if _, err := poller.Tick(ctx); err != nil {
		t.Fatalf("second tick: %v", err)
	}

	// Assert
	mine := []recordedAlert{}
	for _, rec := range notifier.recorded() {
		if rec.userID == user.ID {
			mine = append(mine, rec)
		}
	}
	if len(mine) != 1 || mine[0].event.AlertID != alert.ID {
		t.Fatalf("notifications = %+v, want exactly one for %s", mine, alert.ID)
	}
	if !mine[0].event.TriggeredAt.Equal(alertPollerNow) || mine[0].event.PriceUsdcMicros != 361_000_000 {
		t.Fatalf("event = %+v, want fired at the first tick's mark", mine[0].event)
	}
	listed, err := a.Store.ListPriceAlerts(ctx, user.ID, symbol, 30*24*time.Hour, 50)
	if err != nil {
		t.Fatalf("ListPriceAlerts: %v", err)
	}
	active := map[string]bool{}
	for _, row := range listed {
		active[row.ID] = row.Active
	}
	if active[alert.ID] || !active[waiting.ID] {
		t.Fatalf("active = %v, want %s fired and %s still waiting", active, alert.ID, waiting.ID)
	}
}

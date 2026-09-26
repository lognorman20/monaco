package app

import (
	"context"
	"errors"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

// fixedMarks is an AlertMarkSource that answers from a map, or fails.
type fixedMarks struct {
	marks map[string]AlertMark
	err   error
}

func (f fixedMarks) Marks(context.Context, []string) (map[string]AlertMark, error) {
	return f.marks, f.err
}

type watchlistHarness struct {
	service *WatchlistService
	store   *postgres.Store
	catalog xstocks.CatalogSearcher
	token   string
	userID  string
}

func newWatchlistHarness(t *testing.T, marks AlertMarkSource) watchlistHarness {
	t.Helper()
	db, iso := integrationDB(t)
	store := postgres.NewStore(db)
	privyClient := privy.NewFakeClient()
	catalog := xstocks.NewFakeCatalogSearcher()
	for _, asset := range []xstocks.CatalogAsset{
		{Symbol: "AAPLx", Name: "Apple", SolanaMint: jupiter.AAPLxMint, Routable: true},
		{Symbol: "GOOGLx", Name: "Alphabet", SolanaMint: "XsCPL9dNWBMvFtTmwcCA5v3xWPSMEBCszbQdiLLq6aN", Routable: true},
	} {
		xstocks.RegisterCatalogAsset(catalog, asset)
	}
	sessions := NewSessionService(store, privyClient)
	session := openTestSession(t, iso, sessions, privyClient, "watcher", "Watcher")
	return watchlistHarness{
		service: NewWatchlistService(store, privyClient, catalog, marks),
		store:   store,
		catalog: catalog,
		token:   iso.UniqueToken("watcher"),
		userID:  session.UserID,
	}
}

func TestAlertDirection_reached(t *testing.T) {
	cases := []struct {
		name      string
		direction AlertDirection
		mark      int64
		line      int64
		want      bool
	}{
		{"above, under the line", AlertAbove, 359_990_000, 360_000_000, false},
		{"above, on the line", AlertAbove, 360_000_000, 360_000_000, true},
		{"above, over the line", AlertAbove, 361_200_000, 360_000_000, true},
		{"below, over the line", AlertBelow, 330_010_000, 330_000_000, false},
		{"below, on the line", AlertBelow, 330_000_000, 330_000_000, true},
		{"below, under the line", AlertBelow, 329_000_000, 330_000_000, true},
		{"unknown direction never fires", AlertDirection("sideways"), 1, 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			got := tc.direction.Reached(tc.mark, tc.line)

			// Assert
			if got != tc.want {
				t.Fatalf("Reached(%d, %d) = %v, want %v", tc.mark, tc.line, got, tc.want)
			}
		})
	}
}

func TestParseAlertDirection(t *testing.T) {
	cases := []struct {
		raw  string
		want AlertDirection
		ok   bool
	}{
		{"above", AlertAbove, true},
		{" Below ", AlertBelow, true},
		{"ABOVE", AlertAbove, true},
		{"", "", false},
		{"over", "", false},
	}
	for _, tc := range cases {
		// Act
		got, ok := ParseAlertDirection(tc.raw)

		// Assert
		if got != tc.want || ok != tc.ok {
			t.Fatalf("ParseAlertDirection(%q) = %q, %v; want %q, %v", tc.raw, got, ok, tc.want, tc.ok)
		}
	}
}

func TestAlertDollars(t *testing.T) {
	cases := map[int64]string{
		360_000_000: "$360.00",
		232_054_999: "$232.05",
		232_055_000: "$232.06",
		999:         "$0.00",
		-1_500_000:  "-$1.50",
	}
	for micros, want := range cases {
		if got := alertDollars(micros); got != want {
			t.Fatalf("alertDollars(%d) = %q, want %q", micros, got, want)
		}
	}
}

func TestCatalogMarkSource_pricesKnownSymbolsAndSkipsTheRest(t *testing.T) {
	// Arrange
	catalog := xstocks.NewFakeCatalogSearcher()
	xstocks.RegisterCatalogAsset(catalog, xstocks.CatalogAsset{Symbol: "AAPLx", Name: "Apple", SolanaMint: jupiter.AAPLxMint})
	xstocks.RegisterCatalogAsset(catalog, xstocks.CatalogAsset{Symbol: "NOPRICEx", Name: "Unpriced", SolanaMint: "NoPrice1111111111111111111111111111111111111"})
	prices := jupiter.NewFakePriceClient()
	jupiter.RegisterPrice(prices, jupiter.AAPLxMint, jupiter.TokenPrice{PriceUsdcMicros: 232_050_000})
	source := &CatalogMarkSource{Catalog: catalog, Price: prices}

	// Act
	marks, err := source.Marks(context.Background(), []string{"aaplx", "NOPRICEx", "UNLISTEDx", "AAPLx"})

	// Assert
	if err != nil {
		t.Fatalf("Marks: %v", err)
	}
	if len(marks) != 1 {
		t.Fatalf("marks = %v, want only AAPLx", marks)
	}
	apple := marks["AAPLX"]
	if apple.Symbol != "AAPLx" || apple.Name != "Apple" || apple.PriceUsdcMicros != 232_050_000 {
		t.Fatalf("AAPLx mark = %+v", apple)
	}
	if calls := jupiter.PriceCallCount(prices); calls != 1 {
		t.Fatalf("price calls = %d, want one batched call", calls)
	}
}

func TestCatalogMarkSource_failsWhenThePriceReadFails(t *testing.T) {
	// Arrange
	catalog := xstocks.NewFakeCatalogSearcher()
	xstocks.RegisterCatalogAsset(catalog, xstocks.CatalogAsset{Symbol: "AAPLx", Name: "Apple", SolanaMint: jupiter.AAPLxMint})
	prices := jupiter.NewFakePriceClient()
	jupiter.RegisterPriceError(prices, errors.New("jupiter down"))
	source := &CatalogMarkSource{Catalog: catalog, Price: prices}

	// Act
	_, err := source.Marks(context.Background(), []string{"AAPLx"})

	// Assert
	if err == nil {
		t.Fatal("Marks succeeded with the price read failing; want the error")
	}
}

func TestWatchlistService_addUsesTheCatalogueSpelling(t *testing.T) {
	// Arrange
	h := newWatchlistHarness(t, nil)

	// Act
	entry, created, err := h.service.AddToWatchlist(context.Background(), h.token, "aaplx")

	// Assert
	if err != nil || !created {
		t.Fatalf("AddToWatchlist = %v, %v", created, err)
	}
	if entry.Symbol != "AAPLx" {
		t.Fatalf("stored symbol = %q, want the catalogue's AAPLx", entry.Symbol)
	}
}

func TestWatchlistService_addRefusesWhatTheCatalogueDoesNotList(t *testing.T) {
	cases := []struct {
		name   string
		symbol string
	}{
		{"unlisted", "ZZZZx"},
		{"blank", "   "},
		{"absurdly long", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			h := newWatchlistHarness(t, nil)

			// Act
			_, _, err := h.service.AddToWatchlist(context.Background(), h.token, tc.symbol)

			// Assert
			if !errors.Is(err, ErrWatchlistUnknownSymbol) {
				t.Fatalf("err = %v, want ErrWatchlistUnknownSymbol", err)
			}
		})
	}
}

func TestWatchlistService_addWhileTheCatalogueIsDownWritesNothing(t *testing.T) {
	// Arrange
	h := newWatchlistHarness(t, nil)
	xstocks.RegisterCatalogSearchError(h.catalog, errors.New("catalogue down"))

	// Act
	_, _, err := h.service.AddToWatchlist(context.Background(), h.token, "AAPLx")

	// Assert
	if !errors.Is(err, ErrWatchlistCatalogUnavailable) {
		t.Fatalf("err = %v, want ErrWatchlistCatalogUnavailable", err)
	}
	listed, _ := h.store.ListWatchlist(context.Background(), h.userID)
	if len(listed) != 0 {
		t.Fatalf("watchlist = %v, want nothing written", listed)
	}
}

func TestWatchlistService_fullWatchlistIsRefused(t *testing.T) {
	// Arrange
	h := newWatchlistHarness(t, nil)
	for i := 0; i < WatchlistMaxSymbols; i++ {
		if _, _, err := h.store.AddToWatchlist(context.Background(), h.userID, "FILL"+string(rune('A'+i%26))+string(rune('A'+i/26)), WatchlistMaxSymbols); err != nil {
			t.Fatalf("fill %d: %v", i, err)
		}
	}

	// Act
	_, _, err := h.service.AddToWatchlist(context.Background(), h.token, "AAPLx")

	// Assert
	if !errors.Is(err, ErrWatchlistFull) {
		t.Fatalf("err = %v, want ErrWatchlistFull", err)
	}
}

func TestWatchlistService_reorderFromAStaleCopyIsRefused(t *testing.T) {
	// Arrange
	h := newWatchlistHarness(t, nil)
	ctx := context.Background()
	for _, symbol := range []string{"AAPLx", "GOOGLx"} {
		if _, _, err := h.service.AddToWatchlist(ctx, h.token, symbol); err != nil {
			t.Fatalf("add %s: %v", symbol, err)
		}
	}

	// Act
	_, staleErr := h.service.ReorderWatchlist(ctx, h.token, []string{"GOOGLx"})
	_, blankErr := h.service.ReorderWatchlist(ctx, h.token, []string{"GOOGLx", " "})
	reordered, err := h.service.ReorderWatchlist(ctx, h.token, []string{"GOOGLx", "AAPLx"})

	// Assert
	if !errors.Is(staleErr, ErrWatchlistOrderStale) {
		t.Fatalf("stale reorder err = %v, want ErrWatchlistOrderStale", staleErr)
	}
	if !errors.Is(blankErr, ErrWatchlistOrderInvalid) {
		t.Fatalf("blank reorder err = %v, want ErrWatchlistOrderInvalid", blankErr)
	}
	if err != nil || len(reordered) != 2 || reordered[0].Symbol != "GOOGLx" {
		t.Fatalf("reorder = %+v, %v; want GOOGLx first", reordered, err)
	}
}

func TestWatchlistService_createAlertValidatesTheLine(t *testing.T) {
	cases := []struct {
		name  string
		input CreatePriceAlertInput
		want  error
	}{
		{"sideways", CreatePriceAlertInput{Symbol: "AAPLx", Direction: "sideways", PriceUsdcMicros: 240_000_000}, ErrAlertDirectionInvalid},
		{"zero", CreatePriceAlertInput{Symbol: "AAPLx", Direction: "above", PriceUsdcMicros: 0}, ErrAlertPriceInvalid},
		{"negative", CreatePriceAlertInput{Symbol: "AAPLx", Direction: "below", PriceUsdcMicros: -1}, ErrAlertPriceInvalid},
		{"a million and a cent", CreatePriceAlertInput{Symbol: "AAPLx", Direction: "above", PriceUsdcMicros: maxAlertPriceUsdcMicros + 10_000}, ErrAlertPriceInvalid},
		{"unlisted", CreatePriceAlertInput{Symbol: "ZZZZx", Direction: "above", PriceUsdcMicros: 240_000_000}, ErrWatchlistUnknownSymbol},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			h := newWatchlistHarness(t, nil)

			// Act
			_, err := h.service.CreatePriceAlert(context.Background(), h.token, tc.input)

			// Assert
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestWatchlistService_createAlertRefusesALineAlreadyReached(t *testing.T) {
	// Arrange
	marks := fixedMarks{marks: map[string]AlertMark{"AAPLX": {Symbol: "AAPLx", Name: "Apple", PriceUsdcMicros: 232_050_000}}}
	h := newWatchlistHarness(t, marks)

	// Act
	_, err := h.service.CreatePriceAlert(context.Background(), h.token, CreatePriceAlertInput{
		Symbol: "AAPLx", Direction: "above", PriceUsdcMicros: 230_000_000,
	})

	// Assert
	var met *AlertAlreadyMetError
	if !errors.As(err, &met) {
		t.Fatalf("err = %v, want AlertAlreadyMetError", err)
	}
	if met.MarkUsdcMicros != 232_050_000 || met.Direction != AlertAbove {
		t.Fatalf("error = %+v", met)
	}
}

func TestWatchlistService_createAlertStillWorksWithoutAMark(t *testing.T) {
	// Arrange
	h := newWatchlistHarness(t, fixedMarks{err: errors.New("price vendor down")})

	// Act
	alert, err := h.service.CreatePriceAlert(context.Background(), h.token, CreatePriceAlertInput{
		Symbol: "googlx", Direction: "Below", PriceUsdcMicros: 330_000_000,
	})

	// Assert
	if err != nil {
		t.Fatalf("CreatePriceAlert: %v", err)
	}
	if alert.Symbol != "GOOGLx" || alert.Direction != AlertBelow || !alert.Active || alert.TriggeredAt != nil {
		t.Fatalf("alert = %+v, want an active GOOGLx below alert", alert)
	}
}

func TestWatchlistService_createAlertPastTheCapIsRefused(t *testing.T) {
	// Arrange
	h := newWatchlistHarness(t, nil)
	ctx := context.Background()
	for i := 0; i < MaxActivePriceAlerts; i++ {
		if _, err := h.service.CreatePriceAlert(ctx, h.token, CreatePriceAlertInput{
			Symbol: "AAPLx", Direction: "above", PriceUsdcMicros: int64(240+i) * 1_000_000,
		}); err != nil {
			t.Fatalf("alert %d: %v", i+1, err)
		}
	}

	// Act
	_, err := h.service.CreatePriceAlert(ctx, h.token, CreatePriceAlertInput{
		Symbol: "AAPLx", Direction: "above", PriceUsdcMicros: 300_000_000,
	})

	// Assert
	if !errors.Is(err, ErrAlertLimitReached) {
		t.Fatalf("err = %v, want ErrAlertLimitReached", err)
	}
}

func TestWatchlistService_deleteAnUnknownAlertIsNotFound(t *testing.T) {
	cases := []string{"not-a-uuid", "7b0d6c86-21a4-4d0f-9d77-6b1c2b3a4f5e"}
	for _, id := range cases {
		t.Run(id, func(t *testing.T) {
			// Arrange
			h := newWatchlistHarness(t, nil)

			// Act
			err := h.service.DeletePriceAlert(context.Background(), h.token, id)

			// Assert
			if !errors.Is(err, ErrAlertNotFound) {
				t.Fatalf("err = %v, want ErrAlertNotFound", err)
			}
		})
	}
}

func TestWatchlistService_assetWatchStateCountsOnlyActiveAlertsOnThatStock(t *testing.T) {
	// Arrange
	h := newWatchlistHarness(t, nil)
	ctx := context.Background()
	if _, _, err := h.service.AddToWatchlist(ctx, h.token, "AAPLx"); err != nil {
		t.Fatalf("add: %v", err)
	}
	for _, in := range []CreatePriceAlertInput{
		{Symbol: "AAPLx", Direction: "above", PriceUsdcMicros: 240_000_000},
		{Symbol: "AAPLx", Direction: "below", PriceUsdcMicros: 220_000_000},
		{Symbol: "GOOGLx", Direction: "above", PriceUsdcMicros: 360_000_000},
	} {
		if _, err := h.service.CreatePriceAlert(ctx, h.token, in); err != nil {
			t.Fatalf("create %+v: %v", in, err)
		}
	}

	// Act
	watching, alerts, err := h.service.AssetWatchState(ctx, h.userID, "aaplx")
	googleWatching, googleAlerts, googleErr := h.service.AssetWatchState(ctx, h.userID, "GOOGLx")

	// Assert
	if err != nil || !watching || alerts != 2 {
		t.Fatalf("AAPLx state = %v, %d, %v; want watching with 2 alerts", watching, alerts, err)
	}
	if googleErr != nil || googleWatching || googleAlerts != 1 {
		t.Fatalf("GOOGLx state = %v, %d, %v; want not watching with 1 alert", googleWatching, googleAlerts, googleErr)
	}
}

func TestWatchlistService_rejectsAnUnknownToken(t *testing.T) {
	// Arrange
	h := newWatchlistHarness(t, nil)

	// Act
	_, err := h.service.Watchlist(context.Background(), "not-a-token")

	// Assert
	if !errors.Is(err, privy.ErrInvalidToken) {
		t.Fatalf("err = %v, want privy.ErrInvalidToken", err)
	}
}

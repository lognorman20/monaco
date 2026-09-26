package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

// Watchlist and price alert limits.
const (
	// WatchlistMaxSymbols is how many stocks one watchlist holds. It matches how many
	// symbols one market-row response decorates, so every row on a full watchlist still
	// carries its price and sparkline.
	WatchlistMaxSymbols = 40
	// MaxActivePriceAlerts is how many alerts one member may have waiting at once.
	MaxActivePriceAlerts = 20
	// maxAlertPriceUsdcMicros bounds a line at $1,000,000 a share, far past any listed stock,
	// so a fat-fingered line is refused rather than stored forever.
	maxAlertPriceUsdcMicros = 1_000_000 * 1_000_000
	// firedAlertsWindow and firedAlertsLimit bound the history GET /v1/me/alerts returns
	// beside the active alerts.
	firedAlertsWindow = 30 * 24 * time.Hour
	firedAlertsLimit  = 50
	// maxWatchlistSymbolRunes bounds a path symbol before it reaches the catalogue.
	maxWatchlistSymbolRunes = 32
)

var (
	// ErrWatchlistUnknownSymbol means the catalogue lists no stock by that symbol.
	ErrWatchlistUnknownSymbol = errors.New("unknown symbol")
	// ErrWatchlistCatalogUnavailable means the catalogue could not be asked, so the symbol
	// could not be checked. Nothing was written.
	ErrWatchlistCatalogUnavailable = errors.New("catalog unavailable")
	// ErrWatchlistFull means the member already follows WatchlistMaxSymbols stocks.
	ErrWatchlistFull = errors.New("watchlist is full")
	// ErrWatchlistOrderInvalid means a reorder body was empty-handed or malformed.
	ErrWatchlistOrderInvalid = errors.New("invalid watchlist order")
	// ErrWatchlistOrderStale means a reorder named a different set of stocks than the
	// watchlist holds, so the client is working from an old copy.
	ErrWatchlistOrderStale = errors.New("watchlist changed")
	// ErrAlertDirectionInvalid means the direction was neither above nor below.
	ErrAlertDirectionInvalid = errors.New("direction must be above or below")
	// ErrAlertPriceInvalid means the line was zero, negative or implausibly large.
	ErrAlertPriceInvalid = errors.New("invalid alert price")
	// ErrAlertLimitReached means the member already has MaxActivePriceAlerts waiting.
	ErrAlertLimitReached = errors.New("too many active alerts")
	// ErrAlertNotFound means no alert of the caller's has that id.
	ErrAlertNotFound = errors.New("alert not found")
)

// AlertAlreadyMetError refuses an alert whose line the stock has already reached: it would
// fire on the next tick and tell the member nothing they did not know when they set it.
type AlertAlreadyMetError struct {
	Symbol         string
	Direction      AlertDirection
	MarkUsdcMicros int64
}

func (e *AlertAlreadyMetError) Error() string {
	return fmt.Sprintf("%s is already %s that price (now %s)", e.Symbol, e.Direction, alertDollars(e.MarkUsdcMicros))
}

// WatchlistService serves a member's watchlist and price alerts.
type WatchlistService struct {
	store   *postgres.Store
	privy   privy.Client
	catalog xstocks.SymbolCatalog
	// marks prices a new alert's stock so an alert is never created already met. Nil skips
	// that check.
	marks AlertMarkSource
}

// NewWatchlistService wires the watchlist. catalog validates symbols; marks may be nil.
func NewWatchlistService(store *postgres.Store, privyClient privy.Client, catalog xstocks.SymbolCatalog, marks AlertMarkSource) *WatchlistService {
	return &WatchlistService{store: store, privy: privyClient, catalog: catalog, marks: marks}
}

// WatchlistEntry is one stock on a watchlist.
type WatchlistEntry struct {
	Symbol    string
	Position  int
	CreatedAt time.Time
}

// PriceAlert is one alert as its owner sees it.
type PriceAlert struct {
	ID              string
	Symbol          string
	Direction       AlertDirection
	PriceUsdcMicros int64
	CreatedAt       time.Time
	// TriggeredAt and TriggeredPriceUsdcMicros are set once the alert fired.
	TriggeredAt              *time.Time
	TriggeredPriceUsdcMicros *int64
	Active                   bool
}

// CreatePriceAlertInput is POST /v1/me/alerts.
type CreatePriceAlertInput struct {
	Symbol          string
	Direction       string
	PriceUsdcMicros int64
}

// Watchlist returns the caller's watchlist in position order.
func (s *WatchlistService) Watchlist(ctx context.Context, accessToken string) ([]WatchlistEntry, error) {
	userID, err := s.authorize(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.ListWatchlist(ctx, userID)
	if err != nil {
		return nil, err
	}
	return watchlistEntries(rows), nil
}

// AddToWatchlist puts a catalogue stock at the end of the caller's watchlist. created is
// false when it was already there, in which case it keeps its place.
func (s *WatchlistService) AddToWatchlist(ctx context.Context, accessToken, symbol string) (WatchlistEntry, bool, error) {
	userID, err := s.authorize(ctx, accessToken)
	if err != nil {
		return WatchlistEntry{}, false, err
	}
	canonical, err := s.resolveSymbol(ctx, symbol)
	if err != nil {
		return WatchlistEntry{}, false, err
	}
	row, created, err := s.store.AddToWatchlist(ctx, userID, canonical, WatchlistMaxSymbols)
	if errors.Is(err, postgres.ErrWatchlistFull) {
		return WatchlistEntry{}, false, ErrWatchlistFull
	}
	if err != nil {
		return WatchlistEntry{}, false, err
	}
	return WatchlistEntry(row), created, nil
}

// RemoveFromWatchlist takes a stock off the caller's watchlist. Removing one that is not
// there is not an error: the member's intent, "not on my list", already holds.
func (s *WatchlistService) RemoveFromWatchlist(ctx context.Context, accessToken, symbol string) (bool, error) {
	userID, err := s.authorize(ctx, accessToken)
	if err != nil {
		return false, err
	}
	symbol = strings.TrimSpace(symbol)
	if symbol == "" || len([]rune(symbol)) > maxWatchlistSymbolRunes {
		return false, nil
	}
	return s.store.RemoveFromWatchlist(ctx, userID, symbol)
}

// ReorderWatchlist sets the caller's order. symbols must name every stock on the watchlist
// exactly once.
func (s *WatchlistService) ReorderWatchlist(ctx context.Context, accessToken string, symbols []string) ([]WatchlistEntry, error) {
	if len(symbols) > WatchlistMaxSymbols {
		return nil, ErrWatchlistOrderInvalid
	}
	for _, symbol := range symbols {
		trimmed := strings.TrimSpace(symbol)
		if trimmed == "" || len([]rune(trimmed)) > maxWatchlistSymbolRunes {
			return nil, ErrWatchlistOrderInvalid
		}
	}
	userID, err := s.authorize(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.ReorderWatchlist(ctx, userID, symbols)
	if errors.Is(err, postgres.ErrWatchlistOrderMismatch) {
		return nil, ErrWatchlistOrderStale
	}
	if err != nil {
		return nil, err
	}
	return watchlistEntries(rows), nil
}

// PriceAlerts returns the caller's active alerts, newest first, then the ones that fired in
// the last 30 days. symbol, when set, narrows the list to that stock.
func (s *WatchlistService) PriceAlerts(ctx context.Context, accessToken, symbol string) ([]PriceAlert, error) {
	userID, err := s.authorize(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	symbol = strings.TrimSpace(symbol)
	if len([]rune(symbol)) > maxWatchlistSymbolRunes {
		return []PriceAlert{}, nil
	}
	rows, err := s.store.ListPriceAlerts(ctx, userID, symbol, firedAlertsWindow, firedAlertsLimit)
	if err != nil {
		return nil, err
	}
	out := make([]PriceAlert, 0, len(rows))
	for _, row := range rows {
		out = append(out, priceAlertFromRow(row))
	}
	return out, nil
}

// CreatePriceAlert sets a new alert for the caller on a catalogue stock.
func (s *WatchlistService) CreatePriceAlert(ctx context.Context, accessToken string, in CreatePriceAlertInput) (PriceAlert, error) {
	direction, ok := ParseAlertDirection(in.Direction)
	if !ok {
		return PriceAlert{}, ErrAlertDirectionInvalid
	}
	if in.PriceUsdcMicros <= 0 || in.PriceUsdcMicros > maxAlertPriceUsdcMicros {
		return PriceAlert{}, ErrAlertPriceInvalid
	}
	userID, err := s.authorize(ctx, accessToken)
	if err != nil {
		return PriceAlert{}, err
	}
	canonical, err := s.resolveSymbol(ctx, in.Symbol)
	if err != nil {
		return PriceAlert{}, err
	}
	if err := s.refuseAlreadyMet(ctx, canonical, direction, in.PriceUsdcMicros); err != nil {
		return PriceAlert{}, err
	}
	row, err := s.store.InsertPriceAlert(ctx, userID, canonical, string(direction), in.PriceUsdcMicros, MaxActivePriceAlerts)
	if errors.Is(err, postgres.ErrPriceAlertLimit) {
		return PriceAlert{}, ErrAlertLimitReached
	}
	if err != nil {
		return PriceAlert{}, err
	}
	return priceAlertFromRow(row), nil
}

// DeletePriceAlert removes one of the caller's alerts, active or fired.
func (s *WatchlistService) DeletePriceAlert(ctx context.Context, accessToken, alertID string) error {
	userID, err := s.authorize(ctx, accessToken)
	if err != nil {
		return err
	}
	alertID = strings.TrimSpace(alertID)
	if !isUUID(alertID) {
		return ErrAlertNotFound
	}
	deleted, err := s.store.DeletePriceAlert(ctx, userID, alertID)
	if err != nil {
		return err
	}
	if !deleted {
		return ErrAlertNotFound
	}
	return nil
}

// AssetWatchState answers the stock screen's two questions in one read: is this stock on the
// member's watchlist, and how many alerts are waiting on it. userID is already verified.
func (s *WatchlistService) AssetWatchState(ctx context.Context, userID, symbol string) (bool, int, error) {
	watching, err := s.store.IsWatching(ctx, userID, symbol)
	if err != nil {
		return false, 0, err
	}
	alerts, err := s.store.CountActivePriceAlerts(ctx, userID, symbol)
	if err != nil {
		return false, 0, err
	}
	return watching, alerts, nil
}

// refuseAlreadyMet prices the stock and refuses a line it has already reached. A mark that
// cannot be read lets the alert through: the check exists to spare the member a pointless
// alert, not to make setting one depend on a price vendor.
func (s *WatchlistService) refuseAlreadyMet(ctx context.Context, symbol string, direction AlertDirection, line int64) error {
	if s.marks == nil {
		return nil
	}
	marks, err := s.marks.Marks(ctx, []string{symbol})
	if err != nil {
		slog.WarnContext(ctx, "price alert mark check skipped", "symbol", symbol, "err", err)
		return nil
	}
	mark, ok := marks[strings.ToUpper(symbol)]
	if !ok || mark.PriceUsdcMicros <= 0 {
		return nil
	}
	if direction.Reached(mark.PriceUsdcMicros, line) {
		return &AlertAlreadyMetError{Symbol: symbol, Direction: direction, MarkUsdcMicros: mark.PriceUsdcMicros}
	}
	return nil
}

// resolveSymbol checks a symbol against the catalogue and returns the catalogue's spelling.
func (s *WatchlistService) resolveSymbol(ctx context.Context, raw string) (string, error) {
	symbol := strings.TrimSpace(raw)
	if symbol == "" || len([]rune(symbol)) > maxWatchlistSymbolRunes {
		return "", ErrWatchlistUnknownSymbol
	}
	if s.catalog == nil {
		return "", ErrWatchlistCatalogUnavailable
	}
	asset, found, err := s.catalog.LookupBySymbol(ctx, symbol)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrWatchlistCatalogUnavailable, err)
	}
	if !found {
		return "", ErrWatchlistUnknownSymbol
	}
	canonical := strings.TrimSpace(asset.Normalize().Symbol)
	if canonical == "" {
		return "", ErrWatchlistUnknownSymbol
	}
	return canonical, nil
}

func (s *WatchlistService) authorize(ctx context.Context, accessToken string) (string, error) {
	identity, err := s.privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			return "", privy.ErrInvalidToken
		}
		return "", fmt.Errorf("verify session: %w", err)
	}
	user, found, err := s.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		return "", err
	}
	if !found {
		return "", ErrUserNotFound
	}
	return user.ID, nil
}

func watchlistEntries(rows []postgres.WatchlistEntry) []WatchlistEntry {
	out := make([]WatchlistEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, WatchlistEntry(row))
	}
	return out
}

func priceAlertFromRow(row postgres.PriceAlertRow) PriceAlert {
	return PriceAlert{
		ID:                       row.ID,
		Symbol:                   row.Symbol,
		Direction:                AlertDirection(row.Direction),
		PriceUsdcMicros:          row.PriceUsdcMicros,
		CreatedAt:                row.CreatedAt.UTC(),
		TriggeredAt:              row.TriggeredAt,
		TriggeredPriceUsdcMicros: row.TriggeredPriceUsdcMicros,
		Active:                   row.Active,
	}
}

// alertDollars writes micros as dollars for log lines and error text: "$360.00".
func alertDollars(micros int64) string {
	sign := ""
	if micros < 0 {
		sign = "-"
		micros = -micros
	}
	cents := (micros + 5_000) / 10_000
	return fmt.Sprintf("%s$%d.%02d", sign, cents/100, cents%100)
}

package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/packages/domain"
)

// ErrInvalidHistoryQuery is a history request with a filter, cursor or limit the API does not
// accept. The message says which.
var ErrInvalidHistoryQuery = errors.New("invalid history query")

// HistoryFilter is the `type` query of GET /v1/me/transactions.
type HistoryFilter string

const (
	HistoryFilterAll     HistoryFilter = "all"
	HistoryFilterMoneyIn HistoryFilter = "money_in"
	HistoryFilterCashOut HistoryFilter = "cash_out"
	HistoryFilterBuy     HistoryFilter = "buy"
	HistoryFilterSell    HistoryFilter = "sell"
)

// Page sizes for history. The app asks for a screenful; the export walks every page.
const (
	HistoryDefaultLimit = 30
	HistoryMaxLimit     = 100
	// HistoryExportMaxRows bounds one export. A member past it gets the newest rows and the
	// response says it was cut (see the handler).
	HistoryExportMaxRows = 10_000
)

// ParseHistoryFilter reads the `type` query. Empty is all.
func ParseHistoryFilter(raw string) (HistoryFilter, error) {
	switch HistoryFilter(strings.ToLower(strings.TrimSpace(raw))) {
	case "", HistoryFilterAll:
		return HistoryFilterAll, nil
	case HistoryFilterMoneyIn:
		return HistoryFilterMoneyIn, nil
	case HistoryFilterCashOut:
		return HistoryFilterCashOut, nil
	case HistoryFilterBuy:
		return HistoryFilterBuy, nil
	case HistoryFilterSell:
		return HistoryFilterSell, nil
	default:
		return "", fmt.Errorf("%w: type must be all, money_in, cash_out, buy or sell", ErrInvalidHistoryQuery)
	}
}

// kinds is the set of row kinds the filter keeps; nil keeps all. A bot's trade is a buy or a
// sell like any other, so it stays under its side.
func (f HistoryFilter) kinds() []string {
	switch f {
	case HistoryFilterMoneyIn:
		return []string{postgres.MemberHistoryDeposit, postgres.MemberHistoryFund}
	case HistoryFilterCashOut:
		return []string{postgres.MemberHistoryCashOut, postgres.MemberHistoryWithdrawal}
	case HistoryFilterBuy:
		return []string{postgres.MemberHistoryBuy, postgres.MemberHistoryBotBuy}
	case HistoryFilterSell:
		return []string{postgres.MemberHistorySell, postgres.MemberHistoryBotSell}
	default:
		return nil
	}
}

// ParseHistoryLimit reads the `limit` query: empty is the default, anything past the maximum
// is the maximum, and anything that is not a positive number is refused.
func ParseHistoryLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return HistoryDefaultLimit, nil
	}
	var limit int
	if _, err := fmt.Sscanf(raw, "%d", &limit); err != nil || fmt.Sprint(limit) != raw || limit <= 0 {
		return 0, fmt.Errorf("%w: limit must be a positive whole number", ErrInvalidHistoryQuery)
	}
	if limit > HistoryMaxLimit {
		limit = HistoryMaxLimit
	}
	return limit, nil
}

// Row statuses, in the member's terms.
const (
	HistoryStatusPending = "pending"
	HistoryStatusDone    = "done"
	HistoryStatusFailed  = "failed"
)

// HistoryItem is one row of GET /v1/me/transactions.
type HistoryItem struct {
	ID        string
	Kind      string
	Status    string
	GroupID   string
	GroupName string
	Symbol    string
	Name      string
	// AssetKind is "stock" or "pre_ipo" on a trade, so a client names the stock the way it
	// names it everywhere else. Empty on money rows.
	AssetKind string
	// AmountMicros is the member's dollars: the whole amount of their own money moves, and
	// their slice of a cabal's trade. Nil when the ledger has no dollar figure yet (a sell
	// that has not filled).
	AmountMicros *int64
	// Quantity is the member's slice of a trade's shares or tokens, as a decimal string.
	// Empty on money rows and on a buy that has not filled.
	Quantity      string
	At            time.Time
	TransactionID string
}

// HistoryPage is one page of history plus the cursor for the next, empty on the last page.
type HistoryPage struct {
	Items      []HistoryItem
	NextCursor string
}

// ListHistory returns one page of the member's money history, newest first.
//
// Buys and sells are the cabal's trades attributed to the member by their slice of that
// cabal. The ledger keeps each member's share units as they are now, not as they were at the
// time of the trade, so the slice applied is the current one: a member who has since added
// money sees a larger share of an old trade than they held then. Trades from before the
// member's first deposit into a cabal are left out, and a cabal the member has fully cashed
// out of has no slice left, so its trades are too.
func (p *PortfolioService) ListHistory(ctx context.Context, accessToken string, filter HistoryFilter, cursor string, limit int) (HistoryPage, error) {
	after, err := DecodeHistoryCursor(cursor)
	if err != nil {
		return HistoryPage{}, err
	}
	if limit <= 0 || limit > HistoryMaxLimit {
		return HistoryPage{}, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidHistoryQuery, HistoryMaxLimit)
	}
	user, _, err := p.home.authenticateHomeUser(ctx, accessToken)
	if err != nil {
		return HistoryPage{}, err
	}
	page, err := p.historyPage(ctx, user.ID, filter, after, limit, newHistoryAttribution())
	if err != nil {
		return HistoryPage{}, err
	}
	slog.Info("history read", "user_id", user.ID, "filter", string(filter), "items", len(page.Items), "has_more", page.NextCursor != "")
	return page, nil
}

// ExportHistory returns every row the filter keeps, newest first, up to
// HistoryExportMaxRows. truncated is true when there were more.
func (p *PortfolioService) ExportHistory(ctx context.Context, accessToken string, filter HistoryFilter) (items []HistoryItem, truncated bool, err error) {
	user, _, err := p.home.authenticateHomeUser(ctx, accessToken)
	if err != nil {
		return nil, false, err
	}
	attribution := newHistoryAttribution()
	var after *postgres.MemberHistoryCursor
	for {
		page, err := p.historyPage(ctx, user.ID, filter, after, HistoryMaxLimit, attribution)
		if err != nil {
			return nil, false, err
		}
		items = append(items, page.Items...)
		if page.NextCursor == "" {
			break
		}
		if len(items) >= HistoryExportMaxRows {
			slog.Warn("history export cut at the row cap", "user_id", user.ID, "rows", len(items))
			return items[:HistoryExportMaxRows], true, nil
		}
		last := page.Items[len(page.Items)-1]
		after = &postgres.MemberHistoryCursor{At: last.At, ID: last.ID}
	}
	slog.Info("history export", "user_id", user.ID, "filter", string(filter), "rows", len(items))
	return items, false, nil
}

// historyAttribution memoises, for one request, each cabal's slice and each mint's name so a
// page (or a whole export) reads them once.
type historyAttribution struct {
	slices map[string]memberSlice
	assets map[string]assetMetaRow
	scales map[string]mintScale
}

type memberSlice struct {
	units int64
	base  int64
}

func newHistoryAttribution() *historyAttribution {
	return &historyAttribution{
		slices: make(map[string]memberSlice),
		assets: make(map[string]assetMetaRow),
		scales: make(map[string]mintScale),
	}
}

func (p *PortfolioService) historyPage(
	ctx context.Context,
	userID string,
	filter HistoryFilter,
	after *postgres.MemberHistoryCursor,
	limit int,
	attribution *historyAttribution,
) (HistoryPage, error) {
	h := p.home
	rows, err := h.store.ListMemberHistory(ctx, postgres.MemberHistoryQuery{
		UserID: userID,
		Kinds:  filter.kinds(),
		After:  after,
		Limit:  limit + 1,
	})
	if err != nil {
		return HistoryPage{}, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	if err := p.loadSlices(ctx, userID, rows, attribution); err != nil {
		return HistoryPage{}, err
	}

	page := HistoryPage{Items: make([]HistoryItem, 0, len(rows))}
	for _, row := range rows {
		item, err := p.historyItem(ctx, row, attribution)
		if err != nil {
			return HistoryPage{}, err
		}
		page.Items = append(page.Items, item)
	}
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		page.NextCursor = EncodeHistoryCursor(postgres.MemberHistoryCursor{At: last.At, ID: last.ID})
	}
	return page, nil
}

// loadSlices reads the member's current slice of every cabal a trade on this page belongs to,
// in two set-based queries.
func (p *PortfolioService) loadSlices(ctx context.Context, userID string, rows []postgres.MemberHistoryRow, attribution *historyAttribution) error {
	missing := make([]string, 0)
	seen := make(map[string]struct{})
	for _, row := range rows {
		if !isTradeKind(row.Kind) || row.GroupID == "" {
			continue
		}
		if _, known := attribution.slices[row.GroupID]; known {
			continue
		}
		if _, dup := seen[row.GroupID]; dup {
			continue
		}
		seen[row.GroupID] = struct{}{}
		missing = append(missing, row.GroupID)
	}
	if len(missing) == 0 {
		return nil
	}
	positions, err := p.home.store.ListPositionsForUserInGroups(ctx, userID, missing)
	if err != nil {
		return err
	}
	bases, err := p.home.store.ListShareBaseForGroups(ctx, missing)
	if err != nil {
		return err
	}
	for _, groupID := range missing {
		attribution.slices[groupID] = memberSlice{units: positions[groupID].ShareUnits, base: bases[groupID]}
	}
	return nil
}

func (p *PortfolioService) historyItem(ctx context.Context, row postgres.MemberHistoryRow, attribution *historyAttribution) (HistoryItem, error) {
	item := HistoryItem{
		ID:            row.ID,
		Kind:          row.Kind,
		Status:        historyStatus(row.RawStatus),
		GroupID:       row.GroupID,
		GroupName:     row.GroupName,
		At:            row.At,
		TransactionID: row.TransactionID,
	}
	if !isTradeKind(row.Kind) {
		if row.UsdcMicros.Valid {
			amount := row.UsdcMicros.Int64
			item.AmountMicros = &amount
		}
		return item, nil
	}

	meta, ok := attribution.assets[row.Mint]
	if !ok {
		meta = p.assetMeta(ctx, row.Mint, "", "")
		attribution.assets[row.Mint] = meta
	}
	item.Symbol = meta.symbol
	item.Name = meta.name
	item.AssetKind = meta.kind

	slice := attribution.slices[row.GroupID]
	if row.UsdcMicros.Valid {
		amount, err := sliceOf(row.UsdcMicros.Int64, slice)
		if err != nil {
			return HistoryItem{}, err
		}
		item.AmountMicros = &amount
	}
	if row.TokenAtomics.Valid {
		atomics, err := sliceOf(row.TokenAtomics.Int64, slice)
		if err != nil {
			return HistoryItem{}, err
		}
		scale, ok := attribution.scales[row.Mint]
		if !ok {
			decimals, kind, mult := holdingScale(ctx, p.home.symbols, row.Mint, row.TokenDecimals)
			scale = mintScale{decimals: decimals, kind: kind, mult: mult}
			attribution.scales[row.Mint] = scale
		}
		if quantity, err := pyth.TokenAtomicsToScaledDecimalUnits(atomics, scale.decimals, scale.mult, scale.kind); err == nil {
			item.Quantity = string(quantity)
		} else {
			// No scale for the mint means no honest share count; the dollars still stand.
			slog.Warn("history: trade quantity could not be scaled", "mint", row.Mint, "err", err)
		}
	}
	return item, nil
}

// sliceOf is the member's share of a cabal-wide amount: amount × units / base, rounded down.
func sliceOf(amount int64, slice memberSlice) (int64, error) {
	if amount <= 0 || slice.units <= 0 || slice.base <= 0 {
		return 0, nil
	}
	return domain.MulDivFloor(amount, slice.units, slice.base)
}

func isTradeKind(kind string) bool {
	switch kind {
	case postgres.MemberHistoryBuy, postgres.MemberHistorySell, postgres.MemberHistoryBotBuy, postgres.MemberHistoryBotSell:
		return true
	default:
		return false
	}
}

// historyStatus folds every table's status words into pending, done or failed.
func historyStatus(raw string) string {
	status := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case status == "confirmed" || status == "settled":
		return HistoryStatusDone
	case strings.HasPrefix(status, "failed") || status == "dropped":
		return HistoryStatusFailed
	default:
		return HistoryStatusPending
	}
}

// historyCursorIDPattern is a row id: every history source keys its rows by uuid.
var historyCursorIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// EncodeHistoryCursor makes an opaque page cursor from the last row of a page.
func EncodeHistoryCursor(cursor postgres.MemberHistoryCursor) string {
	raw := cursor.At.UTC().Format(time.RFC3339Nano) + "|" + cursor.ID
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeHistoryCursor reads a cursor made by EncodeHistoryCursor. Empty is the first page.
func DecodeHistoryCursor(raw string) (*postgres.MemberHistoryCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	invalid := fmt.Errorf("%w: cursor is not one this API issued", ErrInvalidHistoryQuery)
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, invalid
	}
	at, id, found := strings.Cut(string(decoded), "|")
	if !found {
		return nil, invalid
	}
	when, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return nil, invalid
	}
	if !historyCursorIDPattern.MatchString(id) {
		return nil, invalid
	}
	return &postgres.MemberHistoryCursor{At: when.UTC(), ID: id}, nil
}

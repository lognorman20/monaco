package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Price alert directions, as stored.
const (
	PriceAlertAbove = "above"
	PriceAlertBelow = "below"
)

// ErrPriceAlertLimit means the member already has the most active alerts allowed.
var ErrPriceAlertLimit = errors.New("too many active price alerts")

// PriceAlertRow is one row in price_alerts. TriggeredAt and TriggeredPriceUsdcMicros are
// set together, when the poller fires the alert.
type PriceAlertRow struct {
	ID                       string
	UserID                   string
	Symbol                   string
	Direction                string
	PriceUsdcMicros          int64
	CreatedAt                time.Time
	TriggeredAt              *time.Time
	TriggeredPriceUsdcMicros *int64
	Active                   bool
}

const priceAlertColumns = `id, user_id, symbol, direction, price_usdc_micros, created_at,
  triggered_at, triggered_price_usdc_micros, active`

type priceAlertScanner interface {
	Scan(dest ...any) error
}

func scanPriceAlert(row priceAlertScanner) (PriceAlertRow, error) {
	var (
		alert          PriceAlertRow
		triggeredAt    sql.NullTime
		triggeredPrice sql.NullInt64
	)
	if err := row.Scan(
		&alert.ID,
		&alert.UserID,
		&alert.Symbol,
		&alert.Direction,
		&alert.PriceUsdcMicros,
		&alert.CreatedAt,
		&triggeredAt,
		&triggeredPrice,
		&alert.Active,
	); err != nil {
		return PriceAlertRow{}, err
	}
	if triggeredAt.Valid {
		at := triggeredAt.Time.UTC()
		alert.TriggeredAt = &at
	}
	if triggeredPrice.Valid {
		price := triggeredPrice.Int64
		alert.TriggeredPriceUsdcMicros = &price
	}
	alert.CreatedAt = alert.CreatedAt.UTC()
	return alert, nil
}

// InsertPriceAlert stores a new active alert for userID. With maxActive already active it is
// refused with ErrPriceAlertLimit. The member's users row is locked across the count and the
// insert, so two creates racing from two devices cannot both slip under the cap.
func (s *Store) InsertPriceAlert(ctx context.Context, userID, symbol, direction string, priceUsdcMicros int64, maxActive int) (PriceAlertRow, error) {
	symbol = strings.TrimSpace(symbol)
	if userID == "" || symbol == "" {
		return PriceAlertRow{}, fmt.Errorf("user_id and symbol are required")
	}
	if direction != PriceAlertAbove && direction != PriceAlertBelow {
		return PriceAlertRow{}, fmt.Errorf("direction %q is not above or below", direction)
	}
	if priceUsdcMicros <= 0 {
		return PriceAlertRow{}, fmt.Errorf("price must be positive")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PriceAlertRow{}, fmt.Errorf("begin insert price alert: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := lockWatchlistOwner(ctx, tx, userID); err != nil {
		return PriceAlertRow{}, err
	}
	var active int
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM price_alerts WHERE user_id = $1 AND active`, userID).Scan(&active); err != nil {
		return PriceAlertRow{}, fmt.Errorf("count active price alerts: %w", err)
	}
	if maxActive > 0 && active >= maxActive {
		return PriceAlertRow{}, ErrPriceAlertLimit
	}

	alert, err := scanPriceAlert(tx.QueryRowContext(ctx, `
INSERT INTO price_alerts (user_id, symbol, direction, price_usdc_micros)
VALUES ($1, $2, $3, $4)
RETURNING `+priceAlertColumns, userID, symbol, direction, priceUsdcMicros))
	if err != nil {
		return PriceAlertRow{}, fmt.Errorf("insert price alert: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return PriceAlertRow{}, fmt.Errorf("commit price alert: %w", err)
	}
	return alert, nil
}

// ListPriceAlerts returns userID's active alerts, newest first, then up to firedLimit alerts
// that fired within firedWithin, most recently fired first. symbol, when set, narrows both to
// one stock (any casing).
func (s *Store) ListPriceAlerts(ctx context.Context, userID, symbol string, firedWithin time.Duration, firedLimit int) ([]PriceAlertRow, error) {
	if userID == "" {
		return nil, fmt.Errorf("user_id is required")
	}
	if firedLimit < 0 {
		firedLimit = 0
	}
	symbol = strings.TrimSpace(symbol)

	const selectSQL = `
(SELECT ` + priceAlertColumns + `, 0 AS bucket, created_at AS sort_at
 FROM price_alerts
 WHERE user_id = $1 AND active AND ($2 = '' OR lower(symbol) = lower($2)))
UNION ALL
(SELECT ` + priceAlertColumns + `, 1 AS bucket, triggered_at AS sort_at
 FROM price_alerts
 WHERE user_id = $1 AND triggered_at IS NOT NULL
   AND triggered_at >= now() - make_interval(secs => $3)
   AND ($2 = '' OR lower(symbol) = lower($2))
 ORDER BY triggered_at DESC, id
 LIMIT $4)
ORDER BY bucket ASC, sort_at DESC, id`

	rows, err := s.db.QueryContext(ctx, selectSQL, userID, symbol, firedWithin.Seconds(), firedLimit)
	if err != nil {
		return nil, fmt.Errorf("list price alerts: %w", err)
	}
	defer rows.Close()

	out := []PriceAlertRow{}
	for rows.Next() {
		var (
			bucket int
			sortAt time.Time
		)
		alert, err := scanPriceAlert(scanWithTail{rows: rows, tail: []any{&bucket, &sortAt}})
		if err != nil {
			return nil, fmt.Errorf("scan price alert: %w", err)
		}
		out = append(out, alert)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate price alerts: %w", err)
	}
	return out, nil
}

// scanWithTail scans a row whose leading columns are priceAlertColumns and whose trailing
// columns exist only to order the query.
type scanWithTail struct {
	rows *sql.Rows
	tail []any
}

func (s scanWithTail) Scan(dest ...any) error {
	return s.rows.Scan(append(dest, s.tail...)...)
}

// DeletePriceAlert removes one of userID's alerts, active or fired, and reports whether it
// existed. Another member's alert id is the same answer as an unknown one.
func (s *Store) DeletePriceAlert(ctx context.Context, userID, alertID string) (bool, error) {
	if userID == "" || alertID == "" {
		return false, fmt.Errorf("user_id and alert id are required")
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM price_alerts WHERE id = $1 AND user_id = $2`, alertID, userID)
	if err != nil {
		return false, fmt.Errorf("delete price alert: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("delete price alert rows: %w", err)
	}
	return n > 0, nil
}

// CountActivePriceAlerts counts userID's active alerts on symbol (any casing).
func (s *Store) CountActivePriceAlerts(ctx context.Context, userID, symbol string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM price_alerts
WHERE user_id = $1 AND active AND lower(symbol) = lower($2)`, userID, strings.TrimSpace(symbol)).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count price alerts: %w", err)
	}
	return count, nil
}

// ActivePriceAlertSymbols lists every symbol with at least one active alert: the set the
// poller has to price.
func (s *Store) ActivePriceAlertSymbols(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT symbol FROM price_alerts WHERE active ORDER BY symbol`)
	if err != nil {
		return nil, fmt.Errorf("list alert symbols: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var symbol string
		if err := rows.Scan(&symbol); err != nil {
			return nil, fmt.Errorf("scan alert symbol: %w", err)
		}
		out = append(out, symbol)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate alert symbols: %w", err)
	}
	return out, nil
}

// TriggerPriceAlerts fires every active alert on symbol whose line the mark has reached:
// 'above' alerts at or under the mark, 'below' alerts at or over it. Each one is stamped with
// at and the mark and made inactive in the same statement that selects it, so an alert is
// returned by exactly one call however many pollers race: the next call, from this poller or
// another, no longer sees it as active.
func (s *Store) TriggerPriceAlerts(ctx context.Context, symbol string, markUsdcMicros int64, at time.Time) ([]PriceAlertRow, error) {
	if strings.TrimSpace(symbol) == "" || markUsdcMicros <= 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
UPDATE price_alerts
SET active = false, triggered_at = $3, triggered_price_usdc_micros = $2
WHERE active AND symbol = $1
  AND ((direction = 'above' AND price_usdc_micros <= $2)
    OR (direction = 'below' AND price_usdc_micros >= $2))
RETURNING `+priceAlertColumns, symbol, markUsdcMicros, at.UTC())
	if err != nil {
		return nil, fmt.Errorf("trigger price alerts: %w", err)
	}
	defer rows.Close()
	out := []PriceAlertRow{}
	for rows.Next() {
		alert, err := scanPriceAlert(rows)
		if err != nil {
			return nil, fmt.Errorf("scan triggered price alert: %w", err)
		}
		out = append(out, alert)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate triggered price alerts: %w", err)
	}
	return out, nil
}

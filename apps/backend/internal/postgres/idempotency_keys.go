package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Idempotency key lifecycle states.
const (
	IdempotencyStateInProgress = "in_progress"
	IdempotencyStateCompleted  = "completed"
)

// IdempotencyKeyRow is a row in idempotency_keys.
type IdempotencyKeyRow struct {
	UserID              string
	Key                 string
	Route               string
	RequestHash         string
	State               string
	ResponseStatus      int
	ResponseContentType string
	ResponseBody        []byte
	CreatedAt           time.Time
}

// ClaimIdempotencyKeyParams claims (user, key) for one request.
type ClaimIdempotencyKeyParams struct {
	UserID      string
	Key         string
	Route       string
	RequestHash string
	// Now is the claim time. Rows created before ExpiredBefore are purged; an in_progress
	// row for this key created before AbandonedBefore is a request that never finished
	// (process died mid-flight) and is handed to the new caller.
	Now             time.Time
	ExpiredBefore   time.Time
	AbandonedBefore time.Time
}

// claimIdempotencyKeyAttempts bounds the insert/select loop. Another pass is only needed
// when the existing row is released between the two statements.
const claimIdempotencyKeyAttempts = 3

// ClaimIdempotencyKey inserts an in_progress row for (user, key). claimed is true when this
// caller owns the key and must run the request; otherwise row is the existing claim.
// Expired rows are purged on the way in, so the table needs no separate sweeper.
func (s *Store) ClaimIdempotencyKey(ctx context.Context, params ClaimIdempotencyKeyParams) (row IdempotencyKeyRow, claimed bool, err error) {
	if params.UserID == "" || params.Key == "" || params.Route == "" || params.RequestHash == "" {
		return IdempotencyKeyRow{}, false, fmt.Errorf("user_id, key, route and request_hash are required")
	}
	// created_at doubles as the claim token for complete/release; keep it at Postgres precision.
	now := params.Now.UTC().Truncate(time.Microsecond)

	const purgeSQL = `
DELETE FROM idempotency_keys
WHERE created_at < $1
   OR (user_id = $2 AND key = $3 AND state = 'in_progress' AND created_at < $4)`
	if _, err := s.db.ExecContext(ctx, purgeSQL, params.ExpiredBefore.UTC(), params.UserID, params.Key, params.AbandonedBefore.UTC()); err != nil {
		return IdempotencyKeyRow{}, false, fmt.Errorf("purge idempotency keys: %w", err)
	}

	const insertSQL = `
INSERT INTO idempotency_keys (user_id, key, route, request_hash, state, created_at)
VALUES ($1, $2, $3, $4, 'in_progress', $5)
ON CONFLICT (user_id, key) DO NOTHING`

	const selectSQL = `
SELECT user_id, key, route, request_hash, state, response_status, response_content_type, response_body, created_at
FROM idempotency_keys
WHERE user_id = $1 AND key = $2`

	for attempt := 0; attempt < claimIdempotencyKeyAttempts; attempt++ {
		result, err := s.db.ExecContext(ctx, insertSQL, params.UserID, params.Key, params.Route, params.RequestHash, now)
		if err != nil {
			return IdempotencyKeyRow{}, false, fmt.Errorf("insert idempotency key: %w", err)
		}
		inserted, err := result.RowsAffected()
		if err != nil {
			return IdempotencyKeyRow{}, false, fmt.Errorf("insert idempotency key rows affected: %w", err)
		}
		if inserted == 1 {
			return IdempotencyKeyRow{
				UserID:      params.UserID,
				Key:         params.Key,
				Route:       params.Route,
				RequestHash: params.RequestHash,
				State:       IdempotencyStateInProgress,
				CreatedAt:   now,
			}, true, nil
		}

		var (
			existing    IdempotencyKeyRow
			status      sql.NullInt64
			contentType sql.NullString
		)
		err = s.db.QueryRowContext(ctx, selectSQL, params.UserID, params.Key).Scan(
			&existing.UserID,
			&existing.Key,
			&existing.Route,
			&existing.RequestHash,
			&existing.State,
			&status,
			&contentType,
			&existing.ResponseBody,
			&existing.CreatedAt,
		)
		if errors.Is(err, sql.ErrNoRows) {
			// The owner released the key between our insert and select; claim again.
			continue
		}
		if err != nil {
			return IdempotencyKeyRow{}, false, fmt.Errorf("select idempotency key: %w", err)
		}
		existing.ResponseStatus = int(status.Int64)
		existing.ResponseContentType = contentType.String
		existing.CreatedAt = existing.CreatedAt.UTC()
		return existing, false, nil
	}
	return IdempotencyKeyRow{}, false, fmt.Errorf("claim idempotency key: row kept disappearing")
}

// CompleteIdempotencyKey stores the response for the claim made at claimedAt so retries
// replay it. A claim that was since handed to another caller is left alone.
func (s *Store) CompleteIdempotencyKey(ctx context.Context, userID, key string, claimedAt time.Time, status int, contentType string, body []byte) error {
	const updateSQL = `
UPDATE idempotency_keys
SET state = 'completed', response_status = $4, response_content_type = $5, response_body = $6
WHERE user_id = $1 AND key = $2 AND created_at = $3 AND state = 'in_progress'`
	if body == nil {
		body = []byte{}
	}
	if _, err := s.db.ExecContext(ctx, updateSQL, userID, key, claimedAt.UTC(), status, contentType, body); err != nil {
		return fmt.Errorf("complete idempotency key: %w", err)
	}
	return nil
}

// ReleaseIdempotencyKey drops the in_progress claim made at claimedAt when its request
// produced nothing worth replaying (5xx, panic), so a retry with the same key runs again.
func (s *Store) ReleaseIdempotencyKey(ctx context.Context, userID, key string, claimedAt time.Time) error {
	const deleteSQL = `
DELETE FROM idempotency_keys
WHERE user_id = $1 AND key = $2 AND created_at = $3 AND state = 'in_progress'`
	if _, err := s.db.ExecContext(ctx, deleteSQL, userID, key, claimedAt.UTC()); err != nil {
		return fmt.Errorf("release idempotency key: %w", err)
	}
	return nil
}

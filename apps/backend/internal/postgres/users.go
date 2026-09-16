package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Store is the Postgres persistence layer for M1 tables.
type Store struct {
	db *sql.DB
}

// NewStore returns a Store backed by db.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// User is a row in users.
type User struct {
	ID          string
	PrivyUserID string
	DisplayName sql.NullString
	CreatedAt   time.Time
}

// UpsertUser inserts a user keyed by privy_user_id or returns the existing row.
func (s *Store) UpsertUser(ctx context.Context, privyUserID string, displayName string) (User, error) {
	if privyUserID == "" {
		return User{}, fmt.Errorf("privy_user_id is required")
	}

	var displayNameArg sql.NullString
	if displayName != "" {
		displayNameArg = sql.NullString{String: displayName, Valid: true}
	}

	const upsertSQL = `
INSERT INTO users (privy_user_id, display_name)
VALUES ($1, $2)
ON CONFLICT (privy_user_id) DO UPDATE
  SET privy_user_id = users.privy_user_id
RETURNING id, privy_user_id, display_name, created_at`

	var user User
	err := s.db.QueryRowContext(ctx, upsertSQL, privyUserID, displayNameArg).Scan(
		&user.ID,
		&user.PrivyUserID,
		&user.DisplayName,
		&user.CreatedAt,
	)
	if err != nil {
		return User{}, fmt.Errorf("upsert user: %w", err)
	}

	return user, nil
}

// GetUserByPrivyUserID returns the user for privyUserID, or false if none exists.
func (s *Store) GetUserByPrivyUserID(ctx context.Context, privyUserID string) (User, bool, error) {
	if privyUserID == "" {
		return User{}, false, fmt.Errorf("privy_user_id is required")
	}

	const selectSQL = `
SELECT id, privy_user_id, display_name, created_at
FROM users
WHERE privy_user_id = $1`

	var user User
	err := s.db.QueryRowContext(ctx, selectSQL, privyUserID).Scan(
		&user.ID,
		&user.PrivyUserID,
		&user.DisplayName,
		&user.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, fmt.Errorf("get user by privy_user_id: %w", err)
	}

	return user, true, nil
}

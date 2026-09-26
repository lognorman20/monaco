package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Store is the Postgres persistence layer for M1 tables.
type Store struct {
	db                          *sql.DB
	platformWithdrawalTestHooks platformWithdrawalTestHooks
}

// NewStore returns a Store backed by db.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// User is a row in users.
type User struct {
	ID              string
	PrivyUserID     string
	DisplayName     sql.NullString
	ProfilePhotoURL sql.NullString
	CreatedAt       time.Time
}

// UpsertUser inserts a user keyed by privy_user_id or returns the existing row. A row whose
// member deleted the account is not handed back: it returns ErrUserDeleted.
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
  WHERE users.deleted_at IS NULL
RETURNING id, privy_user_id, display_name, profile_photo_url, created_at`

	var user User
	err := s.db.QueryRowContext(ctx, upsertSQL, privyUserID, displayNameArg).Scan(
		&user.ID,
		&user.PrivyUserID,
		&user.DisplayName,
		&user.ProfilePhotoURL,
		&user.CreatedAt,
	)
	// lane: settings. The conflict's WHERE skipped a deleted row, so nothing came back.
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUserDeleted
	}
	if err != nil {
		return User{}, fmt.Errorf("upsert user: %w", err)
	}

	return user, nil
}

// GetUserByPrivyUserID returns the user for privyUserID, or false if none exists. A deleted
// account reads as none, so no route acts as a member who deleted it.
func (s *Store) GetUserByPrivyUserID(ctx context.Context, privyUserID string) (User, bool, error) {
	if privyUserID == "" {
		return User{}, false, fmt.Errorf("privy_user_id is required")
	}

	// lane: settings
	const selectSQL = `
SELECT id, privy_user_id, display_name, profile_photo_url, created_at
FROM users
WHERE privy_user_id = $1 AND deleted_at IS NULL`

	var user User
	err := s.db.QueryRowContext(ctx, selectSQL, privyUserID).Scan(
		&user.ID,
		&user.PrivyUserID,
		&user.DisplayName,
		&user.ProfilePhotoURL,
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

// ListUserDisplayNamesByIDs returns display names keyed by user id.
func (s *Store) ListUserDisplayNamesByIDs(ctx context.Context, userIDs []string) (map[string]string, error) {
	if len(userIDs) == 0 {
		return map[string]string{}, nil
	}

	const selectSQL = `
SELECT id, display_name
FROM users
WHERE id = ANY($1::uuid[])`

	rows, err := s.db.QueryContext(ctx, selectSQL, userIDs)
	if err != nil {
		return nil, fmt.Errorf("list user display names: %w", err)
	}
	defer rows.Close()

	names := make(map[string]string, len(userIDs))
	for rows.Next() {
		var id string
		var displayName sql.NullString
		if err := rows.Scan(&id, &displayName); err != nil {
			return nil, fmt.Errorf("scan user display name: %w", err)
		}
		if displayName.Valid {
			names[id] = displayName.String
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user display names: %w", err)
	}
	return names, nil
}

// UpdateUserProfilePhotoURL persists the canonical avatar URL for userID.
func (s *Store) UpdateUserProfilePhotoURL(ctx context.Context, userID, profilePhotoURL string) (User, error) {
	if userID == "" {
		return User{}, fmt.Errorf("user_id is required")
	}
	trimmed := strings.TrimSpace(profilePhotoURL)
	if trimmed == "" {
		return User{}, fmt.Errorf("profile_photo_url is required")
	}

	const updateSQL = `
UPDATE users
SET profile_photo_url = $2
WHERE id = $1
RETURNING id, privy_user_id, display_name, profile_photo_url, created_at`

	var user User
	err := s.db.QueryRowContext(ctx, updateSQL, userID, trimmed).Scan(
		&user.ID,
		&user.PrivyUserID,
		&user.DisplayName,
		&user.ProfilePhotoURL,
		&user.CreatedAt,
	)
	if err != nil {
		return User{}, fmt.Errorf("update user profile photo url: %w", err)
	}
	return user, nil
}

// UpdateUserDisplayName sets display_name for userID and returns the updated row.
// found is false when no user has that id. displayName must already be validated.
func (s *Store) UpdateUserDisplayName(ctx context.Context, userID, displayName string) (User, bool, error) {
	if userID == "" {
		return User{}, false, fmt.Errorf("user_id is required")
	}
	if displayName == "" {
		return User{}, false, fmt.Errorf("display_name is required")
	}

	const updateSQL = `
UPDATE users
SET display_name = $2
WHERE id = $1
RETURNING id, privy_user_id, display_name, profile_photo_url, created_at`

	var user User
	err := s.db.QueryRowContext(ctx, updateSQL, userID, displayName).Scan(
		&user.ID,
		&user.PrivyUserID,
		&user.DisplayName,
		&user.ProfilePhotoURL,
		&user.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, fmt.Errorf("update user display name: %w", err)
	}
	return user, true, nil
}

// UserProfileSummary is the public identity shown next to a user on boards.
type UserProfileSummary struct {
	DisplayName     string
	ProfilePhotoURL string
}

// ListUserProfilesByIDs returns display name and avatar URL keyed by user id in one
// query. Users without a row are absent; unset columns are empty strings.
func (s *Store) ListUserProfilesByIDs(ctx context.Context, userIDs []string) (map[string]UserProfileSummary, error) {
	if len(userIDs) == 0 {
		return map[string]UserProfileSummary{}, nil
	}

	const selectSQL = `
SELECT id, display_name, profile_photo_url
FROM users
WHERE id = ANY($1::uuid[])`

	rows, err := s.db.QueryContext(ctx, selectSQL, userIDs)
	if err != nil {
		return nil, fmt.Errorf("list user profiles: %w", err)
	}
	defer rows.Close()

	profiles := make(map[string]UserProfileSummary, len(userIDs))
	for rows.Next() {
		var id string
		var displayName, profilePhotoURL sql.NullString
		if err := rows.Scan(&id, &displayName, &profilePhotoURL); err != nil {
			return nil, fmt.Errorf("scan user profile: %w", err)
		}
		profiles[id] = UserProfileSummary{
			DisplayName:     strings.TrimSpace(displayName.String),
			ProfilePhotoURL: strings.TrimSpace(profilePhotoURL.String),
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user profiles: %w", err)
	}
	return profiles, nil
}

package seeddemo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
)

// ErrSignInFirst means no Monaco user exists yet (complete OTP login in the app first).
var ErrSignInFirst = errors.New("sign in first: no users in database — launch the app and complete OTP login before running just seed demo")

// ResolveUser picks the seed target user from flags or the most recently created row.
func ResolveUser(ctx context.Context, db *sql.DB, userID, privyUserID string) (postgres.User, error) {
	if userID != "" {
		return getUserByID(ctx, db, userID)
	}
	if privyUserID != "" {
		store := postgres.NewStore(db)
		user, found, err := store.GetUserByPrivyUserID(ctx, privyUserID)
		if err != nil {
			return postgres.User{}, err
		}
		if !found {
			return postgres.User{}, fmt.Errorf("user not found for privy_user_id %q", privyUserID)
		}
		return user, nil
	}
	user, err := mostRecentUser(ctx, db)
	if err != nil {
		return postgres.User{}, err
	}
	if user.ID == "" {
		return postgres.User{}, ErrSignInFirst
	}
	return user, nil
}

func getUserByID(ctx context.Context, db *sql.DB, userID string) (postgres.User, error) {
	const q = `
SELECT id, privy_user_id, display_name, profile_photo_url, created_at
FROM users
WHERE id = $1`
	var user postgres.User
	err := db.QueryRowContext(ctx, q, userID).Scan(
		&user.ID,
		&user.PrivyUserID,
		&user.DisplayName,
		&user.ProfilePhotoURL,
		&user.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return postgres.User{}, fmt.Errorf("user not found for user_id %q", userID)
	}
	if err != nil {
		return postgres.User{}, fmt.Errorf("get user by id: %w", err)
	}
	return user, nil
}

func mostRecentUser(ctx context.Context, db *sql.DB) (postgres.User, error) {
	const q = `
SELECT id, privy_user_id, display_name, profile_photo_url, created_at
FROM users
ORDER BY created_at DESC
LIMIT 1`
	var user postgres.User
	err := db.QueryRowContext(ctx, q).Scan(
		&user.ID,
		&user.PrivyUserID,
		&user.DisplayName,
		&user.ProfilePhotoURL,
		&user.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return postgres.User{}, nil
	}
	if err != nil {
		return postgres.User{}, fmt.Errorf("most recent user: %w", err)
	}
	return user, nil
}

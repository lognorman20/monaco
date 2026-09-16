package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/monaco/monaco/packages/domain"
)

type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Group is a row in groups.
type Group struct {
	ID            string
	Name          string
	CreatorUserID string
	CreatedAt     time.Time
}

// InsertGroup persists a new group row.
func (s *Store) InsertGroup(ctx context.Context, name string, creatorUserID string) (Group, error) {
	return insertGroup(ctx, s.db, name, creatorUserID)
}

// InsertGroupTx persists a new group row within tx.
func (s *Store) InsertGroupTx(ctx context.Context, tx *sql.Tx, name string, creatorUserID string) (Group, error) {
	return insertGroup(ctx, tx, name, creatorUserID)
}

func insertGroup(ctx context.Context, q queryRower, name string, creatorUserID string) (Group, error) {
	if name == "" {
		return Group{}, fmt.Errorf("name is required")
	}
	if creatorUserID == "" {
		return Group{}, fmt.Errorf("creator_user_id is required")
	}

	const insertSQL = `
INSERT INTO groups (name, creator_user_id)
VALUES ($1, $2)
RETURNING id, name, creator_user_id, created_at`

	var group Group
	err := q.QueryRowContext(ctx, insertSQL, name, creatorUserID).Scan(
		&group.ID,
		&group.Name,
		&group.CreatorUserID,
		&group.CreatedAt,
	)
	if err != nil {
		return Group{}, fmt.Errorf("insert group: %w", err)
	}

	return group, nil
}

// GetGroupByID returns the group for id, or false if none exists.
func (s *Store) GetGroupByID(ctx context.Context, id string) (Group, bool, error) {
	if id == "" {
		return Group{}, false, fmt.Errorf("id is required")
	}

	const selectSQL = `
SELECT id, name, creator_user_id, created_at
FROM groups
WHERE id = $1`

	var group Group
	err := s.db.QueryRowContext(ctx, selectSQL, id).Scan(
		&group.ID,
		&group.Name,
		&group.CreatorUserID,
		&group.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Group{}, false, nil
	}
	if err != nil {
		return Group{}, false, fmt.Errorf("get group by id: %w", err)
	}

	return group, true, nil
}

func (s *Store) InsertGroupWithRulesTx(ctx context.Context, tx *sql.Tx, name, creatorUserID string, rules domain.GroupRules, passwordHash string) (Group, error) {
	if name == "" || creatorUserID == "" {
		return Group{}, fmt.Errorf("name and creator_user_id are required")
	}
	const insertSQL = `INSERT INTO groups (name, creator_user_id, join_mode, join_password_hash, voter_set_mode, threshold, vote_expiry_seconds) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id, name, creator_user_id, created_at`
	var hash any
	if passwordHash != "" {
		hash = passwordHash
	}
	var group Group
	err := tx.QueryRowContext(ctx, insertSQL, name, creatorUserID, string(rules.JoinPolicy.Mode), hash, string(rules.VoterSet.Mode), string(rules.Threshold), int64(rules.VoteExpirySeconds)).Scan(&group.ID, &group.Name, &group.CreatorUserID, &group.CreatedAt)
	if err != nil {
		return Group{}, fmt.Errorf("insert group with rules: %w", err)
	}
	return group, nil
}

func (s *Store) GetGroupRules(ctx context.Context, groupID string) (domain.GroupRules, bool, error) {
	if groupID == "" {
		return domain.GroupRules{}, false, fmt.Errorf("group_id is required")
	}
	const selectSQL = `SELECT join_mode, join_password_hash, voter_set_mode, threshold, vote_expiry_seconds FROM groups WHERE id = $1`
	var joinMode, voterSetMode, threshold string
	var passwordHash sql.NullString
	var voteExpirySeconds int64
	err := s.db.QueryRowContext(ctx, selectSQL, groupID).Scan(&joinMode, &passwordHash, &voterSetMode, &threshold, &voteExpirySeconds)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.GroupRules{}, false, nil
	}
	if err != nil {
		return domain.GroupRules{}, false, fmt.Errorf("get group rules: %w", err)
	}
	parsedJoinMode, err := domain.ParseJoinMode(joinMode)
	if err != nil {
		return domain.GroupRules{}, false, err
	}
	parsedVoterSetMode, err := domain.ParseVoterSetMode(voterSetMode)
	if err != nil {
		return domain.GroupRules{}, false, err
	}
	parsedThreshold, err := domain.ParseVoteThreshold(threshold)
	if err != nil {
		return domain.GroupRules{}, false, err
	}
	rules := domain.GroupRules{JoinPolicy: domain.JoinPolicy{Mode: parsedJoinMode}, VoterSet: domain.VoterSet{Mode: parsedVoterSetMode}, Threshold: parsedThreshold, VoteExpirySeconds: domain.VoteExpirySeconds(voteExpirySeconds)}
	if passwordHash.Valid {
		rules.JoinPolicy.PasswordHash = passwordHash.String
	}
	return rules, true, nil
}

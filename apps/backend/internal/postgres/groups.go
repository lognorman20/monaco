package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
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

// GetGroupByIDForUpdateTx returns the group row locked for update within tx.
func (s *Store) GetGroupByIDForUpdateTx(ctx context.Context, tx *sql.Tx, id string) (Group, bool, error) {
	if id == "" {
		return Group{}, false, fmt.Errorf("id is required")
	}

	const selectSQL = `
SELECT id, name, creator_user_id, created_at
FROM groups
WHERE id = $1
FOR UPDATE`

	var group Group
	err := tx.QueryRowContext(ctx, selectSQL, id).Scan(
		&group.ID,
		&group.Name,
		&group.CreatorUserID,
		&group.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Group{}, false, nil
	}
	if err != nil {
		return Group{}, false, fmt.Errorf("get group by id for update: %w", err)
	}

	return group, true, nil
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

// SearchGroupsByName returns groups whose name matches query (case-insensitive), paginated.
func (s *Store) SearchGroupsByName(ctx context.Context, query string, limit, offset int) ([]Group, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []Group{}, nil
	}
	if limit <= 0 {
		limit = 25
	}
	if limit > 50 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	const selectSQL = `
SELECT id, name, creator_user_id, created_at
FROM groups
WHERE name ILIKE '%' || $1 || '%'
ORDER BY name ASC, created_at ASC
LIMIT $2 OFFSET $3`

	rows, err := s.db.QueryContext(ctx, selectSQL, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("search groups by name: %w", err)
	}
	defer rows.Close()

	var groups []Group
	for rows.Next() {
		var group Group
		if err := rows.Scan(&group.ID, &group.Name, &group.CreatorUserID, &group.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan searched group: %w", err)
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate searched groups: %w", err)
	}
	return groups, nil
}

// ListGroupIDs returns every group id.
func (s *Store) ListGroupIDs(ctx context.Context) ([]string, error) {
	const selectSQL = `SELECT id FROM groups ORDER BY created_at ASC`

	rows, err := s.db.QueryContext(ctx, selectSQL)
	if err != nil {
		return nil, fmt.Errorf("list group ids: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan group id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate group ids: %w", err)
	}
	return ids, nil
}

// ListGroupsCreatedByUserID returns group ids created by userID.
func (s *Store) ListGroupsCreatedByUserID(ctx context.Context, userID string) ([]string, error) {
	if userID == "" {
		return nil, fmt.Errorf("user_id is required")
	}

	const selectSQL = `
SELECT id
FROM groups
WHERE creator_user_id = $1
ORDER BY created_at ASC`

	rows, err := s.db.QueryContext(ctx, selectSQL, userID)
	if err != nil {
		return nil, fmt.Errorf("list groups created by user: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan group id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate groups created by user: %w", err)
	}
	return ids, nil
}

func (s *Store) InsertGroupWithRulesTx(ctx context.Context, tx *sql.Tx, name, creatorUserID string, rules domain.GroupRules) (Group, error) {
	if name == "" || creatorUserID == "" {
		return Group{}, fmt.Errorf("name and creator_user_id are required")
	}
	const insertSQL = `INSERT INTO groups (name, creator_user_id, join_mode, voter_set_mode, threshold, vote_expiry_seconds) VALUES ($1,$2,$3,$4,$5,$6) RETURNING id, name, creator_user_id, created_at`
	var group Group
	err := tx.QueryRowContext(ctx, insertSQL, name, creatorUserID, string(rules.JoinPolicy.Mode), string(rules.VoterSet.Mode), string(rules.Threshold), int64(rules.VoteExpirySeconds)).Scan(&group.ID, &group.Name, &group.CreatorUserID, &group.CreatedAt)
	if err != nil {
		return Group{}, fmt.Errorf("insert group with rules: %w", err)
	}
	return group, nil
}

func parseGroupRules(joinMode, voterSetMode, threshold string, voteExpirySeconds int64) (domain.GroupRules, error) {
	parsedJoinMode, err := domain.ParseJoinMode(joinMode)
	if err != nil {
		return domain.GroupRules{}, err
	}
	parsedVoterSetMode, err := domain.ParseVoterSetMode(voterSetMode)
	if err != nil {
		return domain.GroupRules{}, err
	}
	parsedThreshold, err := domain.ParseVoteThreshold(threshold)
	if err != nil {
		return domain.GroupRules{}, err
	}
	return domain.GroupRules{
		JoinPolicy:        domain.JoinPolicy{Mode: parsedJoinMode},
		VoterSet:          domain.VoterSet{Mode: parsedVoterSetMode},
		Threshold:         parsedThreshold,
		VoteExpirySeconds: domain.VoteExpirySeconds(voteExpirySeconds),
	}, nil
}

func (s *Store) GetGroupRules(ctx context.Context, groupID string) (domain.GroupRules, bool, error) {
	if groupID == "" {
		return domain.GroupRules{}, false, fmt.Errorf("group_id is required")
	}
	const selectSQL = `SELECT join_mode, voter_set_mode, threshold, vote_expiry_seconds FROM groups WHERE id = $1`
	var joinMode, voterSetMode, threshold string
	var voteExpirySeconds int64
	err := s.db.QueryRowContext(ctx, selectSQL, groupID).Scan(&joinMode, &voterSetMode, &threshold, &voteExpirySeconds)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.GroupRules{}, false, nil
	}
	if err != nil {
		return domain.GroupRules{}, false, fmt.Errorf("get group rules: %w", err)
	}
	rules, err := parseGroupRules(joinMode, voterSetMode, threshold, voteExpirySeconds)
	if err != nil {
		return domain.GroupRules{}, false, err
	}
	return rules, true, nil
}

// GetGroupRulesTx returns group rules within tx.
func (s *Store) GetGroupRulesTx(ctx context.Context, tx *sql.Tx, groupID string) (domain.GroupRules, bool, error) {
	if groupID == "" {
		return domain.GroupRules{}, false, fmt.Errorf("group_id is required")
	}
	const selectSQL = `SELECT join_mode, voter_set_mode, threshold, vote_expiry_seconds FROM groups WHERE id = $1`
	var joinMode, voterSetMode, threshold string
	var voteExpirySeconds int64
	err := tx.QueryRowContext(ctx, selectSQL, groupID).Scan(&joinMode, &voterSetMode, &threshold, &voteExpirySeconds)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.GroupRules{}, false, nil
	}
	if err != nil {
		return domain.GroupRules{}, false, fmt.Errorf("get group rules tx: %w", err)
	}
	rules, err := parseGroupRules(joinMode, voterSetMode, threshold, voteExpirySeconds)
	if err != nil {
		return domain.GroupRules{}, false, err
	}
	return rules, true, nil
}

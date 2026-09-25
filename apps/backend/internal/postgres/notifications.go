package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/monaco/monaco/packages/domain"
)

// MaxDeviceTokensPerUser bounds push fan-out: the newest tokens a member registered win.
const MaxDeviceTokensPerUser = 10

// NotificationRow is one inbox row with the cabal it points at.
type NotificationRow struct {
	ID              string
	UserID          string
	Kind            string
	Title           string
	Body            string
	GroupID         sql.NullString
	GroupName       sql.NullString
	GroupPictureURL sql.NullString
	ProposalID      sql.NullString
	TransactionID   sql.NullString
	Symbol          sql.NullString
	ReadAt          sql.NullTime
	CreatedAt       time.Time
}

// NotificationCursor is the (created_at, id) keyset of the last row on a page.
type NotificationCursor struct {
	CreatedAt time.Time
	ID        string
}

// InsertNotificationsParams writes one row per recipient who wants this category.
type InsertNotificationsParams struct {
	UserIDs []string
	// Category is the preferences key (`preferences.notifications.<category>`). A member who
	// set it to false gets no row. Empty means the notification cannot be switched off.
	Category      string
	Kind          string
	Title         string
	Body          string
	GroupID       string
	ProposalID    string
	TransactionID string
	Symbol        string
	// ThrottleSince, when set, skips a member who already got a notification of this kind for
	// this cabal at or after that time (chat: one per cabal per member per window).
	ThrottleSince *time.Time
	// Once skips a member who already has this kind for the same proposal and transaction, so an
	// event reported twice (a retried fill, a proposal finalized from two places) lands once.
	Once      bool
	CreatedAt time.Time
}

// InsertedNotification is one row InsertNotifications wrote.
type InsertedNotification struct {
	ID        string
	UserID    string
	CreatedAt time.Time
}

// InsertNotifications writes the rows and returns who got one. Seeded ghost users never do.
func (s *Store) InsertNotifications(ctx context.Context, p InsertNotificationsParams) ([]InsertedNotification, error) {
	if len(p.UserIDs) == 0 {
		return nil, nil
	}
	if p.Kind == "" || p.Title == "" {
		return nil, fmt.Errorf("notification kind and title are required")
	}
	createdAt := p.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	var throttle sql.NullTime
	if p.ThrottleSince != nil {
		throttle = sql.NullTime{Time: p.ThrottleSince.UTC(), Valid: true}
	}

	const insertSQL = `
INSERT INTO notifications (user_id, kind, title, body, group_id, proposal_id, transaction_id, symbol, created_at)
SELECT u.id, $2, $3, $4, $5::uuid, $6::uuid, $7::uuid, $8, $9
FROM users u
WHERE u.id = ANY($1::uuid[])
  AND NOT u.is_faker
  AND ($10 = '' OR COALESCE(u.preferences->'notifications'->>$10, 'true') <> 'false')
  AND (
    $11::timestamptz IS NULL OR NOT EXISTS (
      SELECT 1 FROM notifications n
      WHERE n.user_id = u.id
        AND n.kind = $2
        AND n.group_id IS NOT DISTINCT FROM $5::uuid
        AND n.created_at >= $11::timestamptz
    )
  )
  AND (
    NOT $12::boolean OR NOT EXISTS (
      SELECT 1 FROM notifications n
      WHERE n.user_id = u.id
        AND n.kind = $2
        AND n.proposal_id IS NOT DISTINCT FROM $6::uuid
        AND n.transaction_id IS NOT DISTINCT FROM $7::uuid
    )
  )
RETURNING id, user_id, created_at`

	rows, err := s.db.QueryContext(ctx, insertSQL,
		dedupeIDs(p.UserIDs), p.Kind, p.Title, p.Body,
		nullableUUID(p.GroupID), nullableUUID(p.ProposalID), nullableUUID(p.TransactionID),
		nullableText(p.Symbol), createdAt.UTC(), p.Category, throttle, p.Once,
	)
	if err != nil {
		return nil, fmt.Errorf("insert notifications: %w", err)
	}
	defer rows.Close()
	var out []InsertedNotification
	for rows.Next() {
		var row InsertedNotification
		if err := rows.Scan(&row.ID, &row.UserID, &row.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan inserted notification: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate inserted notifications: %w", err)
	}
	return out, nil
}

// ListNotifications returns up to limit rows older than before (newest when nil), newest first.
func (s *Store) ListNotifications(ctx context.Context, userID string, before *NotificationCursor, limit int) ([]NotificationRow, error) {
	if userID == "" {
		return nil, fmt.Errorf("user_id is required")
	}
	const selectSQL = `
SELECT n.id, n.user_id, n.kind, n.title, n.body, n.group_id, g.name, g.picture_url,
  n.proposal_id, n.transaction_id, n.symbol, n.read_at, n.created_at
FROM notifications n
LEFT JOIN groups g ON g.id = n.group_id
WHERE n.user_id = $1
  AND ($2::timestamptz IS NULL OR (n.created_at, n.id) < ($2::timestamptz, $3::uuid))
ORDER BY n.created_at DESC, n.id DESC
LIMIT $4`

	var beforeAt sql.NullTime
	var beforeID sql.NullString
	if before != nil {
		beforeAt = sql.NullTime{Time: before.CreatedAt.UTC(), Valid: true}
		beforeID = sql.NullString{String: before.ID, Valid: true}
	}
	rows, err := s.db.QueryContext(ctx, selectSQL, userID, beforeAt, beforeID, limit)
	if err != nil {
		return nil, fmt.Errorf("list notifications: %w", err)
	}
	defer rows.Close()
	var out []NotificationRow
	for rows.Next() {
		var row NotificationRow
		if err := rows.Scan(
			&row.ID, &row.UserID, &row.Kind, &row.Title, &row.Body,
			&row.GroupID, &row.GroupName, &row.GroupPictureURL,
			&row.ProposalID, &row.TransactionID, &row.Symbol, &row.ReadAt, &row.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan notification: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate notifications: %w", err)
	}
	return out, nil
}

// CountUnreadNotifications is the badge for one member.
func (s *Store) CountUnreadNotifications(ctx context.Context, userID string) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL`, userID,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count unread notifications: %w", err)
	}
	return count, nil
}

// CountUnreadNotificationsByUser is the badge for each of userIDs; members with none are absent.
func (s *Store) CountUnreadNotificationsByUser(ctx context.Context, userIDs []string) (map[string]int, error) {
	out := make(map[string]int, len(userIDs))
	if len(userIDs) == 0 {
		return out, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT user_id, count(*)
FROM notifications
WHERE user_id = ANY($1::uuid[]) AND read_at IS NULL
GROUP BY user_id`, dedupeIDs(userIDs))
	if err != nil {
		return nil, fmt.Errorf("count unread notifications by user: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var userID string
		var count int
		if err := rows.Scan(&userID, &count); err != nil {
			return nil, fmt.Errorf("scan unread count: %w", err)
		}
		out[userID] = count
	}
	return out, rows.Err()
}

// MarkNotificationsRead stamps the caller's own unread rows among ids. Other members' ids are ignored.
func (s *Store) MarkNotificationsRead(ctx context.Context, userID string, ids []string, at time.Time) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE notifications SET read_at = $3
WHERE user_id = $1 AND id = ANY($2::uuid[]) AND read_at IS NULL`, userID, dedupeIDs(ids), at.UTC())
	if err != nil {
		return 0, fmt.Errorf("mark notifications read: %w", err)
	}
	return res.RowsAffected()
}

// MarkAllNotificationsRead stamps every unread row the caller has.
func (s *Store) MarkAllNotificationsRead(ctx context.Context, userID string, at time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
UPDATE notifications SET read_at = $2
WHERE user_id = $1 AND read_at IS NULL`, userID, at.UTC())
	if err != nil {
		return 0, fmt.Errorf("mark all notifications read: %w", err)
	}
	return res.RowsAffected()
}

// DeviceTokenRow is one registered install.
type DeviceTokenRow struct {
	UserID    string
	Token     string
	Platform  string
	AppEnv    string
	UpdatedAt time.Time
}

// UpsertDeviceToken registers token for userID, moving it from whoever had it, and keeps only
// the member's newest MaxDeviceTokensPerUser tokens.
func (s *Store) UpsertDeviceToken(ctx context.Context, userID, token, platform, appEnv string, at time.Time) error {
	tx, err := s.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO device_tokens (user_id, token, platform, app_env, updated_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (token) DO UPDATE
SET user_id = EXCLUDED.user_id, platform = EXCLUDED.platform, app_env = EXCLUDED.app_env, updated_at = EXCLUDED.updated_at`,
		userID, token, platform, appEnv, at.UTC()); err != nil {
		return fmt.Errorf("upsert device token: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
DELETE FROM device_tokens
WHERE user_id = $1 AND token NOT IN (
  SELECT token FROM device_tokens WHERE user_id = $1 ORDER BY updated_at DESC, token LIMIT $2
)`, userID, MaxDeviceTokensPerUser); err != nil {
		return fmt.Errorf("prune device tokens: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit device token: %w", err)
	}
	return nil
}

// DeleteDeviceTokenForUser removes token when it belongs to userID.
func (s *Store) DeleteDeviceTokenForUser(ctx context.Context, userID, token string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM device_tokens WHERE user_id = $1 AND token = $2`, userID, token)
	if err != nil {
		return false, fmt.Errorf("delete device token: %w", err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// DeleteDeviceToken removes token whoever owns it (Apple said the install is gone).
func (s *Store) DeleteDeviceToken(ctx context.Context, token string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM device_tokens WHERE token = $1`, token); err != nil {
		return fmt.Errorf("delete device token: %w", err)
	}
	return nil
}

// ListDeviceTokensForUsers returns every install registered to userIDs.
func (s *Store) ListDeviceTokensForUsers(ctx context.Context, userIDs []string) ([]DeviceTokenRow, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT user_id, token, platform, app_env, updated_at
FROM device_tokens
WHERE user_id = ANY($1::uuid[])
ORDER BY user_id, updated_at DESC`, dedupeIDs(userIDs))
	if err != nil {
		return nil, fmt.Errorf("list device tokens: %w", err)
	}
	defer rows.Close()
	var out []DeviceTokenRow
	for rows.Next() {
		var row DeviceTokenRow
		if err := rows.Scan(&row.UserID, &row.Token, &row.Platform, &row.AppEnv, &row.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan device token: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// GetProposalForUpdateTx locks the proposal row so two reminders cannot race past the limit.
func (s *Store) GetProposalForUpdateTx(ctx context.Context, tx *sql.Tx, proposalID string) (ProposalRow, bool, error) {
	row, err := scanProposalRow(tx.QueryRowContext(ctx,
		`SELECT `+proposalSelectColumns+` FROM proposals WHERE id = $1 FOR UPDATE`, proposalID))
	if errors.Is(err, sql.ErrNoRows) {
		return ProposalRow{}, false, nil
	}
	if err != nil {
		return ProposalRow{}, false, fmt.Errorf("lock proposal: %w", err)
	}
	return row, true, nil
}

// LatestNudgeAtTx is when anyone last reminded voters about proposalID.
func (s *Store) LatestNudgeAtTx(ctx context.Context, tx *sql.Tx, proposalID string) (time.Time, bool, error) {
	var at sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT max(sent_at) FROM nudges WHERE proposal_id = $1`, proposalID).Scan(&at); err != nil {
		return time.Time{}, false, fmt.Errorf("latest nudge: %w", err)
	}
	return at.Time, at.Valid, nil
}

// InsertNudgeTx records that sentBy reminded voters about proposalID at at.
func (s *Store) InsertNudgeTx(ctx context.Context, tx *sql.Tx, proposalID, sentBy string, at time.Time) error {
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO nudges (proposal_id, sent_by, sent_at) VALUES ($1, $2, $3)`, proposalID, sentBy, at.UTC()); err != nil {
		return fmt.Errorf("insert nudge: %w", err)
	}
	return nil
}

// ListProposalsClosingSoon returns open, real proposals closing in (now, horizon] in cabals
// whose vote window is at least minWindow long, and whose closing reminder has not gone out.
func (s *Store) ListProposalsClosingSoon(ctx context.Context, now, horizon time.Time, minWindow time.Duration, limit int) ([]ProposalRow, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT `+prefixedProposalColumns("p")+`
FROM proposals p
JOIN groups g ON g.id = p.group_id
JOIN users u ON u.id = p.proposer_id
WHERE p.status = 'open'
  AND p.expires_at > $1 AND p.expires_at <= $2
  AND g.vote_expiry_seconds >= $3
  AND NOT g.is_faker AND NOT u.is_faker
  AND NOT EXISTS (SELECT 1 FROM proposal_reminders r WHERE r.proposal_id = p.id)
ORDER BY p.expires_at
LIMIT $4`, now.UTC(), horizon.UTC(), int64(minWindow.Seconds()), limit)
	if err != nil {
		return nil, fmt.Errorf("list proposals closing soon: %w", err)
	}
	defer rows.Close()
	return collectProposalRows(rows, "scan proposal closing soon")
}

// ClaimProposalReminder records the closing reminder for proposalID. False means another
// process already sent it.
func (s *Store) ClaimProposalReminder(ctx context.Context, proposalID string, at time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
INSERT INTO proposal_reminders (proposal_id, sent_at) VALUES ($1, $2)
ON CONFLICT (proposal_id) DO NOTHING`, proposalID, at.UTC())
	if err != nil {
		return false, fmt.Errorf("claim proposal reminder: %w", err)
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// ListExpiredOpenProposalIDs returns real proposals still marked open past their deadline.
func (s *Store) ListExpiredOpenProposalIDs(ctx context.Context, now time.Time, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT p.id
FROM proposals p
JOIN groups g ON g.id = p.group_id
JOIN users u ON u.id = p.proposer_id
WHERE p.status = 'open' AND p.expires_at <= $1
  AND NOT g.is_faker AND NOT u.is_faker
ORDER BY p.expires_at
LIMIT $2`, now.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("list expired open proposals: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan expired proposal id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// MemberMoneyFlows is what Monaco itself moved in and out of one member wallet.
type MemberMoneyFlows struct {
	// SweepsOut is USDC confirmed swept from the wallet into cabals.
	SweepsOut int64
	// WithdrawalsOut is USDC confirmed sent from the wallet to an outside address.
	WithdrawalsOut int64
	// PayoutsIn is cabal cash-outs paid to the wallet, pending ones included: a pending
	// payout may already have landed, and counting it early only delays a notification.
	PayoutsIn int64
}

// GetMemberMoneyFlows sums the Monaco-initiated movements for the member wallet at address.
func (s *Store) GetMemberMoneyFlows(ctx context.Context, userID, address string) (MemberMoneyFlows, error) {
	var flows MemberMoneyFlows
	err := s.db.QueryRowContext(ctx, `
SELECT
  COALESCE((SELECT sum(amount) FROM deposits WHERE user_id = $1 AND from_address = $2 AND status = 'confirmed'), 0),
  COALESCE((SELECT sum(amount) FROM platform_withdrawals WHERE user_id = $1 AND status = 'confirmed'), 0),
  COALESCE((SELECT sum(amount) FROM redeem_payouts WHERE user_id = $1 AND to_address = $2 AND status IN ('pending', 'confirmed')), 0)`,
		userID, address,
	).Scan(&flows.SweepsOut, &flows.WithdrawalsOut, &flows.PayoutsIn)
	if err != nil {
		return MemberMoneyFlows{}, fmt.Errorf("member money flows: %w", err)
	}
	return flows, nil
}

// GetMemberBalanceMark returns the outside-inflow total last seen for userID.
func (s *Store) GetMemberBalanceMark(ctx context.Context, userID string) (int64, bool, error) {
	var inflow int64
	err := s.db.QueryRowContext(ctx, `SELECT inflow_micros FROM member_balance_marks WHERE user_id = $1`, userID).Scan(&inflow)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("get member balance mark: %w", err)
	}
	return inflow, true, nil
}

// InsertMemberBalanceMark records the first sighting. False when another reader got there first.
func (s *Store) InsertMemberBalanceMark(ctx context.Context, userID string, inflow int64, at time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
INSERT INTO member_balance_marks (user_id, inflow_micros, observed_at) VALUES ($1, $2, $3)
ON CONFLICT (user_id) DO NOTHING`, userID, inflow, at.UTC())
	if err != nil {
		return false, fmt.Errorf("insert member balance mark: %w", err)
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// AdvanceMemberBalanceMark moves the mark from `from` to `to`. False when another reader moved
// it first, so exactly one of them reports the arrival.
func (s *Store) AdvanceMemberBalanceMark(ctx context.Context, userID string, from, to int64, at time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
UPDATE member_balance_marks SET inflow_micros = $3, observed_at = $4
WHERE user_id = $1 AND inflow_micros = $2`, userID, from, to, at.UTC())
	if err != nil {
		return false, fmt.Errorf("advance member balance mark: %w", err)
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// TouchMemberBalanceMark records that the balance was read, so the watcher rotates past it.
func (s *Store) TouchMemberBalanceMark(ctx context.Context, userID string, at time.Time) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE member_balance_marks SET observed_at = $2 WHERE user_id = $1`, userID, at.UTC()); err != nil {
		return fmt.Errorf("touch member balance mark: %w", err)
	}
	return nil
}

// BalanceWatchCandidate is a member whose wallet the watcher should read next.
type BalanceWatchCandidate struct {
	UserID        string
	WalletAddress string
}

// ListBalanceWatchCandidates returns members with a push device registered since activeSince
// whose balance was last read before staleBefore (or never), oldest read first.
func (s *Store) ListBalanceWatchCandidates(ctx context.Context, activeSince, staleBefore time.Time, limit int) ([]BalanceWatchCandidate, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT mw.user_id, mw.solana_address
FROM member_wallets mw
JOIN users u ON u.id = mw.user_id
LEFT JOIN member_balance_marks m ON m.user_id = mw.user_id
WHERE NOT u.is_faker
  AND EXISTS (SELECT 1 FROM device_tokens d WHERE d.user_id = mw.user_id AND d.updated_at >= $1)
  AND (m.observed_at IS NULL OR m.observed_at < $2)
ORDER BY m.observed_at NULLS FIRST, mw.user_id
LIMIT $3`, activeSince.UTC(), staleBefore.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("list balance watch candidates: %w", err)
	}
	defer rows.Close()
	var out []BalanceWatchCandidate
	for rows.Next() {
		var c BalanceWatchCandidate
		if err := rows.Scan(&c.UserID, &c.WalletAddress); err != nil {
			return nil, fmt.Errorf("scan balance watch candidate: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CountGroupMembers is how many members a cabal has, ghosts excluded in a real cabal.
func (s *Store) CountGroupMembers(ctx context.Context, groupID string) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `
SELECT count(*)
FROM group_members gm
JOIN users u ON u.id = gm.user_id
JOIN groups g ON g.id = gm.group_id
WHERE gm.group_id = $1 AND (g.is_faker OR NOT u.is_faker)`, groupID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count group members: %w", err)
	}
	return n, nil
}

// ListVoterIDsWhoVoted returns who has a ballot on proposalID.
func (s *Store) ListVoterIDsWhoVoted(ctx context.Context, proposalID string) (map[string]domain.VoteChoice, error) {
	votes, err := s.ListVotesForProposal(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]domain.VoteChoice, len(votes))
	for _, v := range votes {
		out[v.VoterID] = v.Choice
	}
	return out, nil
}

func prefixedProposalColumns(alias string) string {
	return alias + `.id, ` + alias + `.group_id, ` + alias + `.proposer_id, ` + alias + `.symbol, ` + alias + `.kind, ` +
		alias + `.usdc_micros, ` + alias + `.token_amount, ` + alias + `.token_decimals, ` + alias + `.premium_bps, ` +
		alias + `.agent_display_name, ` + alias + `.allocation_usdc_micros, ` + alias + `.thesis, ` + alias + `.status, ` +
		alias + `.expires_at, ` + alias + `.created_at`
}

func dedupeIDs(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func nullableUUID(id string) sql.NullString {
	return sql.NullString{String: id, Valid: id != ""}
}

func nullableText(v string) sql.NullString {
	return sql.NullString{String: v, Valid: v != ""}
}

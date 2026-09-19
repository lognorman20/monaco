package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

var privyLabelPattern = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// TestIsolation gives each test its own data lane on shared monaco_test.
// Seed with UniquePrivyID; call TrackUser and TrackGroup after inserts; t.Cleanup
// deletes only tracked rows (no global TRUNCATE or advisory lock per test).
type TestIsolation struct {
	t        *testing.T
	db       *sql.DB
	suffix   string
	userIDs  []string
	groupIDs []string
}

// PrepareTestDB registers per-test cleanup on db. Use the returned TestIsolation
// for unique privy IDs and to track rows this test creates.
func PrepareTestDB(t *testing.T, db *sql.DB) *TestIsolation {
	t.Helper()

	iso := &TestIsolation{
		t:      t,
		db:     db,
		suffix: newTestLaneSuffix(),
	}
	t.Cleanup(func() {
		if err := iso.cleanup(context.Background()); err != nil {
			t.Errorf("TestIsolation cleanup: %v", err)
		}
	})
	return iso
}

// PrepareIntegrationDB is an alias for PrepareTestDB. Parallel-safe: no global lock or TRUNCATE.
func PrepareIntegrationDB(t *testing.T, db *sql.DB) *TestIsolation {
	t.Helper()
	return PrepareTestDB(t, db)
}

// Suffix returns the unique lane id for this test (for debugging).
func (iso *TestIsolation) Suffix() string {
	return iso.suffix
}

// UniquePrivyID returns a privy user id unique to this test lane.
func (iso *TestIsolation) UniquePrivyID(label string) string {
	return fmt.Sprintf("did:privy:test-%s-%s", iso.suffix, sanitizeTestLabel(label))
}

// UniqueToken returns an access token string unique to this test lane.
func (iso *TestIsolation) UniqueToken(label string) string {
	return fmt.Sprintf("token-%s-%s", iso.suffix, sanitizeTestLabel(label))
}

// TrackUser records a user root row for cleanup.
func (iso *TestIsolation) TrackUser(userID string) {
	iso.t.Helper()
	if userID == "" {
		return
	}
	for _, existing := range iso.userIDs {
		if existing == userID {
			return
		}
	}
	iso.userIDs = append(iso.userIDs, userID)
}

// TrackGroup records a group root row for cleanup.
func (iso *TestIsolation) TrackGroup(groupID string) {
	iso.t.Helper()
	if groupID == "" {
		return
	}
	for _, existing := range iso.groupIDs {
		if existing == groupID {
			return
		}
	}
	iso.groupIDs = append(iso.groupIDs, groupID)
}

func (iso *TestIsolation) cleanup(ctx context.Context) error {
	if len(iso.groupIDs) > 0 {
		if err := deleteTrackedGroups(ctx, iso.db, iso.groupIDs); err != nil {
			return err
		}
	}
	if len(iso.userIDs) > 0 {
		if err := deleteTrackedUsers(ctx, iso.db, iso.userIDs); err != nil {
			return err
		}
	}
	return nil
}

func newTestLaneSuffix() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("test lane suffix: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

func sanitizeTestLabel(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return "x"
	}
	label = privyLabelPattern.ReplaceAllString(label, "-")
	label = strings.Trim(label, "-")
	if label == "" {
		return "x"
	}
	if len(label) > 48 {
		label = label[len(label)-48:]
	}
	return label
}

func deleteTrackedGroups(ctx context.Context, db *sql.DB, groupIDs []string) error {
	steps := []string{
		`DELETE FROM proposal_comments WHERE proposal_id IN (SELECT id FROM proposals WHERE group_id = ANY($1::uuid[]))`,
		`DELETE FROM votes WHERE proposal_id IN (SELECT id FROM proposals WHERE group_id = ANY($1::uuid[]))`,
		`DELETE FROM agent_intents WHERE group_id = ANY($1::uuid[])`,
		`DELETE FROM group_agent_key_reveals WHERE proposal_id IN (SELECT id FROM proposals WHERE group_id = ANY($1::uuid[]))`,
		`DELETE FROM group_agents WHERE group_id = ANY($1::uuid[])`,
		`DELETE FROM transactions WHERE group_id = ANY($1::uuid[])`,
		`DELETE FROM proposals WHERE group_id = ANY($1::uuid[])`,
		`DELETE FROM redeem_jobs WHERE group_id = ANY($1::uuid[])`,
		`DELETE FROM payout_proofs WHERE group_id = ANY($1::uuid[])`,
		`DELETE FROM nav_snapshots WHERE group_id = ANY($1::uuid[])`,
		`DELETE FROM group_voters WHERE group_id = ANY($1::uuid[])`,
		`DELETE FROM group_members WHERE group_id = ANY($1::uuid[])`,
		`DELETE FROM positions WHERE group_id = ANY($1::uuid[])`,
		`DELETE FROM deposits WHERE group_id = ANY($1::uuid[])`,
		`DELETE FROM withdrawals WHERE group_id = ANY($1::uuid[])`,
		`DELETE FROM treasuries WHERE group_id = ANY($1::uuid[])`,
		`DELETE FROM groups WHERE id = ANY($1::uuid[])`,
	}
	for _, stmt := range steps {
		if _, err := db.ExecContext(ctx, stmt, groupIDs); err != nil {
			return fmt.Errorf("delete tracked groups: %w", err)
		}
	}
	return nil
}

func deleteTrackedUsers(ctx context.Context, db *sql.DB, userIDs []string) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM platform_withdrawals WHERE user_id = ANY($1::uuid[])`, userIDs); err != nil {
		return fmt.Errorf("delete tracked users (platform_withdrawals): %w", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM member_wallets WHERE user_id = ANY($1::uuid[])`, userIDs); err != nil {
		return fmt.Errorf("delete tracked users (member_wallets): %w", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM users WHERE id = ANY($1::uuid[])`, userIDs); err != nil {
		return fmt.Errorf("delete tracked users: %w", err)
	}
	return nil
}

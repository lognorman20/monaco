package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

// fixedSliceValuer prices every slice at one figure, or fails for the groups listed.
type fixedSliceValuer struct {
	micros int64
	fail   map[string]bool
}

func (v fixedSliceValuer) MemberSliceMicros(_ context.Context, _ string, groupID string) (int64, error) {
	if v.fail[groupID] {
		return 0, errors.New("pot unpriced")
	}
	return v.micros, nil
}

type accountHarness struct {
	integrationHarness
	Accounts   *AccountService
	Sessions   *SessionService
	Governance *GovernanceService
}

func newAccountHarness(t *testing.T, valuer SliceValuer) accountHarness {
	t.Helper()
	h := integrationApp(t)
	return accountHarness{
		integrationHarness: h,
		Accounts:           NewAccountService(h.Store, h.Privy, valuer),
		Sessions:           NewSessionService(h.Store, h.Privy),
		Governance:         NewGovernanceService(h.Store, h.Privy),
	}
}

// member opens a session for a new member and returns its token and user id.
func (h accountHarness) member(t *testing.T, label, name string) (string, SessionResult) {
	t.Helper()
	session := openTestSession(t, h.ISO, h.Sessions, h.Privy, label, name)
	return h.ISO.UniqueToken(label), session
}

func (h accountHarness) cabal(t *testing.T, token, label string) CreateGroupResult {
	t.Helper()
	group, err := h.Governance.CreateGroupWithRules(context.Background(), token, testGroupName(h.ISO, label), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create cabal: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)
	return group
}

func (h accountHarness) giveShareUnits(t *testing.T, userID, groupID string, units int64) {
	t.Helper()
	ctx := context.Background()
	tx, err := h.Store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := h.Store.IncrementPositionTx(ctx, tx, userID, groupID, units, units); err != nil {
		t.Fatalf("IncrementPositionTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit position: %v", err)
	}
}

func (h accountHarness) memberAddress(t *testing.T, userID string) string {
	t.Helper()
	wallet, found, err := h.Store.GetMemberWalletByUserID(context.Background(), userID)
	if err != nil || !found {
		t.Fatalf("member wallet: found=%v err=%v", found, err)
	}
	return wallet.SolanaAddress
}

func TestValidatePreferencesPatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		body      string
		want      string
		wantError string
	}{
		{name: "one switch", body: `{"notifications":{"chat":false}}`, want: `{"notifications":{"chat":false}}`},
		{name: "every switch", body: `{"notifications":{"money":true,"proposals":false,"results":true,"chat":false}}`, want: `{"notifications":{"chat":false,"money":true,"proposals":false,"results":true}}`},
		{name: "empty patch changes nothing", body: `{}`, want: `{}`},
		{name: "empty notifications", body: `{"notifications":{}}`, want: `{"notifications":{}}`},
		{name: "unknown top-level key", body: `{"theme":"dark"}`, wantError: `unknown preference "theme"`},
		{name: "unknown notification", body: `{"notifications":{"marketing":false}}`, wantError: `unknown preference "notifications.marketing"`},
		{name: "string instead of boolean", body: `{"notifications":{"chat":"false"}}`, wantError: "notifications.chat must be true or false"},
		{name: "null instead of boolean", body: `{"notifications":{"chat":null}}`, wantError: "notifications.chat must be true or false"},
		{name: "number instead of boolean", body: `{"notifications":{"money":0}}`, wantError: "notifications.money must be true or false"},
		{name: "notifications not an object", body: `{"notifications":true}`, wantError: "notifications must be a JSON object"},
		{name: "body is null", body: `null`, wantError: "the body must be a JSON object"},
		{name: "body is an array", body: `[{"notifications":{}}]`, wantError: "the body must be a JSON object"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Act
			got, err := ValidatePreferencesPatch([]byte(tt.body))

			// Assert
			if tt.wantError != "" {
				var prefsErr *PreferencesError
				if !errors.As(err, &prefsErr) || prefsErr.Message != tt.wantError {
					t.Fatalf("err = %v, want PreferencesError %q", err, tt.wantError)
				}
				if !errors.Is(err, ErrInvalidPreferences) {
					t.Fatalf("err = %v, want errors.Is ErrInvalidPreferences", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidatePreferencesPatch: %v", err)
			}
			if string(got) != tt.want {
				t.Fatalf("canonical = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestPreferencesFromStored(t *testing.T) {
	t.Parallel()

	allOn := DefaultPreferences()
	chatOff := DefaultPreferences()
	chatOff.Notifications.Chat = false

	tests := []struct {
		name   string
		stored string
		want   Preferences
	}{
		{name: "nothing stored is everything on", stored: `{}`, want: allOn},
		{name: "a stored false wins", stored: `{"notifications":{"chat":false}}`, want: chatOff},
		{name: "a damaged value keeps the default", stored: `{"notifications":{"chat":"no","money":null}}`, want: allOn},
		{name: "unreadable row keeps defaults", stored: `not json`, want: allOn},
		{name: "keys the app does not know are ignored", stored: `{"notifications":{"chat":false,"later":false},"theme":"dark"}`, want: chatOff},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Act
			got := PreferencesFromStored([]byte(tt.stored))

			// Assert
			if got != tt.want {
				t.Fatalf("PreferencesFromStored(%s) = %+v, want %+v", tt.stored, got, tt.want)
			}
		})
	}
}

func TestNotificationPreferences_allowsUnknownCategories(t *testing.T) {
	t.Parallel()

	// Arrange
	prefs := NotificationPreferences{}

	// Act
	allowed := prefs.Allows("streaks")

	// Assert
	if !allowed || prefs.Allows(NotifyChat) {
		t.Fatalf("Allows: unknown=%v chat=%v, want unknown on and chat off", allowed, prefs.Allows(NotifyChat))
	}
}

func TestUpdatePreferences_mergesPatchesAndKeepsOtherSwitches(t *testing.T) {
	t.Parallel()

	// Arrange
	h := newAccountHarness(t, nil)
	ctx := context.Background()
	token, _ := h.member(t, "prefs-merge", "Nadia Park")

	// Act
	if _, err := h.Accounts.UpdatePreferences(ctx, token, []byte(`{"notifications":{"chat":false}}`)); err != nil {
		t.Fatalf("first patch: %v", err)
	}
	got, err := h.Accounts.UpdatePreferences(ctx, token, []byte(`{"notifications":{"money":false}}`))
	if err != nil {
		t.Fatalf("second patch: %v", err)
	}

	// Assert
	want := NotificationPreferences{Proposals: true, Results: true, Chat: false, Money: false}
	if got.Notifications != want {
		t.Fatalf("merged = %+v, want %+v", got.Notifications, want)
	}
	read, err := h.Accounts.GetPreferences(ctx, token)
	if err != nil {
		t.Fatalf("GetPreferences: %v", err)
	}
	if read != got {
		t.Fatalf("GET after PATCH = %+v, want %+v", read, got)
	}
}

func TestGetPreferences_newMemberHasEverythingOn(t *testing.T) {
	t.Parallel()

	// Arrange
	h := newAccountHarness(t, nil)
	token, _ := h.member(t, "prefs-default", "Omar Haddad")

	// Act
	got, err := h.Accounts.GetPreferences(context.Background(), token)

	// Assert
	if err != nil {
		t.Fatalf("GetPreferences: %v", err)
	}
	if got != DefaultPreferences() {
		t.Fatalf("preferences = %+v, want defaults", got)
	}
}

func TestUpdatePreferences_invalidPatchStoresNothing(t *testing.T) {
	t.Parallel()

	// Arrange
	h := newAccountHarness(t, nil)
	ctx := context.Background()
	token, _ := h.member(t, "prefs-invalid", "Priya Raman")

	// Act
	_, err := h.Accounts.UpdatePreferences(ctx, token, []byte(`{"notifications":{"chat":false,"marketing":false}}`))

	// Assert
	if !errors.Is(err, ErrInvalidPreferences) {
		t.Fatalf("err = %v, want ErrInvalidPreferences", err)
	}
	read, err := h.Accounts.GetPreferences(ctx, token)
	if err != nil {
		t.Fatalf("GetPreferences: %v", err)
	}
	if read != DefaultPreferences() {
		t.Fatalf("preferences after refused patch = %+v, want defaults", read)
	}
}

func TestUpdatePreferences_rateLimitedPerMember(t *testing.T) {
	t.Parallel()

	// Arrange
	h := newAccountHarness(t, nil)
	h.Accounts.WithPreferencesLimiter(NewPreferencesUpdateLimiter())
	ctx := context.Background()
	token, _ := h.member(t, "prefs-limit", "Quinn Ellis")
	patch := []byte(`{"notifications":{"chat":false}}`)
	for i := 0; i < preferencesUpdateBurst; i++ {
		if _, err := h.Accounts.UpdatePreferences(ctx, token, patch); err != nil {
			t.Fatalf("patch %d: %v", i, err)
		}
	}

	// Act
	_, err := h.Accounts.UpdatePreferences(ctx, token, patch)

	// Assert
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
}

func TestDeletionCheck_emptyAccountCanDelete(t *testing.T) {
	t.Parallel()

	// Arrange
	h := newAccountHarness(t, fixedSliceValuer{micros: 1})
	token, session := h.member(t, "check-empty", "Rosa Lindqvist")
	h.cabal(t, token, "check-empty") // a membership with no slice does not block

	// Act
	check, err := h.Accounts.DeletionCheck(context.Background(), token)

	// Assert
	if err != nil {
		t.Fatalf("DeletionCheck: %v", err)
	}
	if !check.CanDelete() || len(check.Blockers) != 0 {
		t.Fatalf("check for %s = %+v, want no blockers", session.UserID, check)
	}
}

func TestDeletionCheck_listsSlicesTransfersAndBalanceInOrder(t *testing.T) {
	t.Parallel()

	// Arrange
	h := newAccountHarness(t, fixedSliceValuer{micros: 245_120_000})
	ctx := context.Background()
	token, session := h.member(t, "check-blocked", "Sam Okafor")
	group := h.cabal(t, token, "check-blocked")
	h.giveShareUnits(t, session.UserID, group.GroupID, 200_000_000)
	address := h.memberAddress(t, session.UserID)
	privy.SetMemberUSDCBalance(h.Privy, address, 30_000_000)
	if _, err := h.Store.InsertDeposit(ctx, session.UserID, group.GroupID, 12_500_000, address); err != nil {
		t.Fatalf("InsertDeposit: %v", err)
	}

	// Act
	check, err := h.Accounts.DeletionCheck(ctx, token)

	// Assert
	if err != nil {
		t.Fatalf("DeletionCheck: %v", err)
	}
	want := []DeletionBlocker{
		{Kind: DeletionBlockerCabalSlice, GroupID: group.GroupID, GroupName: group.Name, ValueMicros: 245_120_000, ValueKnown: true},
		{Kind: DeletionBlockerTransferPending, ValueMicros: 12_500_000, ValueKnown: true},
		// The balance is what GET /v1/me/balance shows: chain minus the fund in flight.
		{Kind: DeletionBlockerAccountBalance, ValueMicros: 17_500_000, ValueKnown: true},
	}
	if check.CanDelete() {
		t.Fatal("CanDelete = true with money in the account")
	}
	if len(check.Blockers) != len(want) {
		t.Fatalf("blockers = %+v, want %+v", check.Blockers, want)
	}
	for i := range want {
		if check.Blockers[i] != want[i] {
			t.Fatalf("blocker %d = %+v, want %+v", i, check.Blockers[i], want[i])
		}
	}
}

func TestDeletionCheck_unpricedSliceStillBlocksWithUnknownValue(t *testing.T) {
	t.Parallel()

	// Arrange
	h := newAccountHarness(t, nil)
	token, session := h.member(t, "check-unpriced", "Tomas Weber")
	group := h.cabal(t, token, "check-unpriced")
	h.Accounts.slices = fixedSliceValuer{fail: map[string]bool{group.GroupID: true}}
	h.giveShareUnits(t, session.UserID, group.GroupID, 5_000_000)

	// Act
	check, err := h.Accounts.DeletionCheck(context.Background(), token)

	// Assert
	if err != nil {
		t.Fatalf("DeletionCheck: %v", err)
	}
	if len(check.Blockers) != 1 || check.Blockers[0].Kind != DeletionBlockerCabalSlice || check.Blockers[0].ValueKnown {
		t.Fatalf("blockers = %+v, want one slice with an unknown value", check.Blockers)
	}
}

func TestDeletionCheck_pendingCashOutBlocks(t *testing.T) {
	t.Parallel()

	// Arrange
	h := newAccountHarness(t, fixedSliceValuer{})
	ctx := context.Background()
	token, session := h.member(t, "check-cashout", "Uma Castillo")
	group := h.cabal(t, token, "check-cashout")
	h.giveShareUnits(t, session.UserID, group.GroupID, 4_000_000)
	tx, err := h.Store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := h.Store.DebitPositionShareUnitsTx(ctx, tx, session.UserID, group.GroupID, 4_000_000); err != nil {
		t.Fatalf("DebitPositionShareUnitsTx: %v", err)
	}
	if _, err := h.Store.InsertRedeemJobTx(ctx, tx, session.UserID, group.GroupID, 4_000_000, 4_100_000, h.memberAddress(t, session.UserID)); err != nil {
		t.Fatalf("InsertRedeemJobTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Act
	check, err := h.Accounts.DeletionCheck(ctx, token)

	// Assert
	if err != nil {
		t.Fatalf("DeletionCheck: %v", err)
	}
	want := DeletionBlocker{Kind: DeletionBlockerCashOutPending, GroupID: group.GroupID, GroupName: group.Name, ValueMicros: 4_100_000, ValueKnown: true}
	if len(check.Blockers) != 1 || check.Blockers[0] != want {
		t.Fatalf("blockers = %+v, want [%+v]", check.Blockers, want)
	}
}

func TestDeleteAccount_anonymisesAndLeavesHistory(t *testing.T) {
	t.Parallel()

	// Arrange
	h := newAccountHarness(t, fixedSliceValuer{})
	ctx := context.Background()
	creatorToken, creator := h.member(t, "delete-creator", "Vera Novak")
	token, session := h.member(t, "delete-leaver", "Wes Tanaka")
	group := h.cabal(t, creatorToken, "delete-happy")
	if _, err := h.Governance.JoinGroup(ctx, token, group.GroupID); err != nil {
		t.Fatalf("JoinGroup: %v", err)
	}
	if _, err := h.Store.UpdateUserProfilePhotoURL(ctx, session.UserID, "https://example.test/wes.png"); err != nil {
		t.Fatalf("UpdateUserProfilePhotoURL: %v", err)
	}
	if _, err := h.Accounts.UpdatePreferences(ctx, token, []byte(`{"notifications":{"chat":false}}`)); err != nil {
		t.Fatalf("UpdatePreferences: %v", err)
	}
	chat := NewGroupChatService(h.Store, h.Privy)
	if _, err := chat.PostMessage(ctx, token, group.GroupID, "NVDA into earnings, who's in?"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	deletedAt := time.Date(2026, 9, 25, 14, 30, 0, 0, time.UTC)
	h.Accounts.SetClock(func() time.Time { return deletedAt })

	// Act
	result, err := h.Accounts.DeleteAccount(ctx, token)

	// Assert
	if err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}
	if result.UserID != session.UserID || !result.DeletedAt.Equal(deletedAt) {
		t.Fatalf("result = %+v, want user %s at %s", result, session.UserID, deletedAt)
	}
	profiles, err := h.Store.ListUserProfilesByIDs(ctx, []string{session.UserID, creator.UserID})
	if err != nil {
		t.Fatalf("ListUserProfilesByIDs: %v", err)
	}
	if got := profiles[session.UserID]; got.DisplayName != postgres.DeletedMemberDisplayName || got.ProfilePhotoURL != "" {
		t.Fatalf("deleted profile = %+v, want %q with no photo", got, postgres.DeletedMemberDisplayName)
	}
	if got := profiles[creator.UserID].DisplayName; got != "Vera Novak" {
		t.Fatalf("creator renamed to %q", got)
	}
	members, err := h.Store.ListGroupMemberIDs(ctx, group.GroupID)
	if err != nil {
		t.Fatalf("ListGroupMemberIDs: %v", err)
	}
	if len(members) != 1 || members[0] != creator.UserID {
		t.Fatalf("members = %v, want only the creator", members)
	}
	account, found, err := h.Store.GetAccountByPrivyUserID(ctx, h.ISO.UniquePrivyID("delete-leaver"))
	if err != nil || !found {
		t.Fatalf("GetAccountByPrivyUserID: found=%v err=%v", found, err)
	}
	if !account.DeletedAt.Valid || !account.DeletedAt.Time.Equal(deletedAt) {
		t.Fatalf("deleted_at = %+v, want %s", account.DeletedAt, deletedAt)
	}
	if _, found, _ := h.Store.GetUserPreferences(ctx, session.UserID); found {
		t.Fatal("preferences still readable for a deleted account")
	}
	page, err := chat.ListMessages(ctx, creatorToken, group.GroupID, "", 10)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(page.Messages) != 1 || page.Messages[0].AuthorName != postgres.DeletedMemberDisplayName || page.Messages[0].Body != "NVDA into earnings, who's in?" {
		t.Fatalf("chat after delete = %+v, want the message kept under %q", page.Messages, postgres.DeletedMemberDisplayName)
	}
}

func TestDeleteAccount_isIdempotent(t *testing.T) {
	t.Parallel()

	// Arrange
	h := newAccountHarness(t, fixedSliceValuer{})
	ctx := context.Background()
	token, _ := h.member(t, "delete-twice", "Xia Chen")
	first, err := h.Accounts.DeleteAccount(ctx, token)
	if err != nil {
		t.Fatalf("first DeleteAccount: %v", err)
	}
	h.Accounts.SetClock(func() time.Time { return first.DeletedAt.Add(time.Hour) })

	// Act
	second, err := h.Accounts.DeleteAccount(ctx, token)

	// Assert
	if err != nil {
		t.Fatalf("second DeleteAccount: %v", err)
	}
	if second != first {
		t.Fatalf("second delete = %+v, want the first answer %+v", second, first)
	}
}

func TestDeleteAccount_blockedByASliceChangesNothing(t *testing.T) {
	t.Parallel()

	// Arrange
	h := newAccountHarness(t, fixedSliceValuer{micros: 9_990_000})
	ctx := context.Background()
	token, session := h.member(t, "delete-blocked", "Yusuf Demir")
	group := h.cabal(t, token, "delete-blocked")
	h.giveShareUnits(t, session.UserID, group.GroupID, 10_000_000)

	// Act
	_, err := h.Accounts.DeleteAccount(ctx, token)

	// Assert
	var blocked *AccountDeletionBlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("err = %v, want AccountDeletionBlockedError", err)
	}
	if len(blocked.Check.Blockers) != 1 || blocked.Check.Blockers[0].ValueMicros != 9_990_000 {
		t.Fatalf("blockers = %+v, want the one slice", blocked.Check.Blockers)
	}
	me, err := h.Sessions.GetMe(ctx, token)
	if err != nil {
		t.Fatalf("GetMe after refused delete: %v", err)
	}
	if me.DisplayName != "Yusuf Demir" {
		t.Fatalf("display name = %q, want unchanged", me.DisplayName)
	}
}

func TestDeleteAccount_blockedByAccountBalance(t *testing.T) {
	t.Parallel()

	// Arrange
	h := newAccountHarness(t, fixedSliceValuer{})
	token, session := h.member(t, "delete-balance", "Zara Ahmed")
	privy.SetMemberUSDCBalance(h.Privy, h.memberAddress(t, session.UserID), 1)

	// Act
	_, err := h.Accounts.DeleteAccount(context.Background(), token)

	// Assert
	var blocked *AccountDeletionBlockedError
	if !errors.As(err, &blocked) || len(blocked.Check.Blockers) != 1 || blocked.Check.Blockers[0].Kind != DeletionBlockerAccountBalance {
		t.Fatalf("err = %v, want one account_balance blocker", err)
	}
}

func TestOpenSession_deletedAccountIsRefused(t *testing.T) {
	t.Parallel()

	// Arrange
	h := newAccountHarness(t, fixedSliceValuer{})
	ctx := context.Background()
	token, _ := h.member(t, "delete-reopen", "Ana Ruiz")
	if _, err := h.Accounts.DeleteAccount(ctx, token); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}

	// Act
	_, err := h.Sessions.OpenSession(ctx, token)

	// Assert
	if !errors.Is(err, ErrAccountDeleted) {
		t.Fatalf("OpenSession err = %v, want ErrAccountDeleted", err)
	}
	if _, err := h.Sessions.GetMe(ctx, token); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("GetMe err = %v, want ErrUserNotFound for a deleted account", err)
	}
	if _, err := h.Accounts.GetPreferences(ctx, token); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("GetPreferences err = %v, want ErrUserNotFound for a deleted account", err)
	}
}

func TestDeleteAccount_unknownUserIsNotFound(t *testing.T) {
	t.Parallel()

	// Arrange
	h := newAccountHarness(t, nil)
	token := h.ISO.UniqueToken("delete-unknown")
	privy.RegisterToken(h.Privy, privy.AccessToken(token), privy.Identity{PrivyUserID: h.ISO.UniquePrivyID("delete-unknown")})

	// Act
	_, err := h.Accounts.DeleteAccount(context.Background(), token)

	// Assert
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("err = %v, want ErrUserNotFound", err)
	}
}

func TestMemberSliceMicros_pricesTheSliceLikeHome(t *testing.T) {
	t.Parallel()

	// Arrange
	h := newAccountHarness(t, nil)
	token, session := h.member(t, "slice-price", "Ben Carter")
	group := h.cabal(t, token, "slice-price")
	h.giveShareUnits(t, session.UserID, group.GroupID, 50_000_000)
	seedTestTreasuryUSDC(t, h.Privy, group.TreasuryAddress, 50_000_000)
	home := NewHomeService(h.Store, h.Privy, h.Pyth, h.Deposits, h.Symbols)

	// Act
	value, err := home.MemberSliceMicros(context.Background(), session.UserID, group.GroupID)

	// Assert
	if err != nil {
		t.Fatalf("MemberSliceMicros: %v", err)
	}
	if value != 50_000_000 {
		t.Fatalf("slice = %d, want 50000000 (sole holder of a $50 pot)", value)
	}
}

func TestDeletedMemberDisplayName_isAPrintableName(t *testing.T) {
	t.Parallel()

	// The placeholder is a plain name so every existing join prints it without a special case.
	if _, err := NormalizeDisplayName(postgres.DeletedMemberDisplayName); err != nil || strings.TrimSpace(postgres.DeletedMemberDisplayName) == "" {
		t.Fatalf("placeholder %q must be a printable display name: %v", postgres.DeletedMemberDisplayName, err)
	}
}

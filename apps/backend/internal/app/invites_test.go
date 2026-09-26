package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

var inviteCodeShape = regexp.MustCompile(`^[2-9A-HJ-NP-Z]{8}$`)

func TestNewInviteCode_everyByteLandsInTheAlphabet(t *testing.T) {
	// Arrange: all 256 byte values, eight at a time.
	all := make([]byte, 256)
	for i := range all {
		all[i] = byte(i)
	}
	reader := bytes.NewReader(all)

	for chunk := 0; chunk < 256/InviteCodeLength; chunk++ {
		// Act
		code, err := NewInviteCode(reader)

		// Assert
		if err != nil {
			t.Fatalf("chunk %d: %v", chunk, err)
		}
		if !inviteCodeShape.MatchString(code) {
			t.Fatalf("chunk %d: %q is not eight alphabet symbols", chunk, code)
		}
	}
}

func TestNewInviteCode_neverUsesAmbiguousSymbols(t *testing.T) {
	for i := 0; i < 2000; i++ {
		// Act
		code, err := NewInviteCode(rand.Reader)

		// Assert
		if err != nil {
			t.Fatalf("draw %d: %v", i, err)
		}
		if strings.ContainsAny(code, "0O1I") || !inviteCodeShape.MatchString(code) {
			t.Fatalf("draw %d: %q uses a symbol a member could misread", i, code)
		}
	}
}

func TestNewInviteCode_shortRandomSourceIsAnError(t *testing.T) {
	// Act
	_, err := NewInviteCode(bytes.NewReader([]byte{1, 2, 3}))

	// Assert
	if err == nil {
		t.Fatal("want an error when the random source runs dry")
	}
}

func TestNormalizeInviteCode(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
		ok   bool
	}{
		{"canonical", "K7QM4XPD", "K7QM4XPD", true},
		{"lowercase", "k7qm4xpd", "K7QM4XPD", true},
		{"grouped with a space", "K7QM 4XPD", "K7QM4XPD", true},
		{"grouped with a dash", "k7qm-4xpd", "K7QM4XPD", true},
		{"surrounding whitespace", "  K7QM4XPD\n", "K7QM4XPD", true},
		{"seven symbols", "K7QM4XP", "", false},
		{"nine symbols", "K7QM4XPDA", "", false},
		{"a zero", "K7QM4XP0", "", false},
		{"a letter O", "K7QM4XPO", "", false},
		{"a one", "AB12CD34", "", false},
		{"a letter I", "K7QM4XPI", "", false},
		{"empty", "", "", false},
		{"punctuation", "K7QM_4XPD", "", false},
		{"cyrillic lookalike", "К7QM4XPD", "", false},
		{"a full link is not a code", "https://trymonaco.xyz/join/K7QM4XPD", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			got, ok := NormalizeInviteCode(tc.raw)

			// Assert
			if got != tc.want || ok != tc.ok {
				t.Fatalf("NormalizeInviteCode(%q) = (%q, %v), want (%q, %v)", tc.raw, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestCabalTintName_matchesTheAppsHash(t *testing.T) {
	// The app pins "" → plum and "a" → ochre (DesignPrimitivesTests.swift); the ids are
	// checked against the same FNV-1a 64 mod 5.
	cases := []struct{ id, want string }{
		{"", "plum"},
		{"a", "ochre"},
		{"3f5b2c9e-8d1a-4e7f-9b6c-2a1d0e4f7c88", "ochre"},
		{"3F5B2C9E-8D1A-4E7F-9B6C-2A1D0E4F7C88", "ochre"},
		{" 5b1f0c9e-0005-4c55-9a51-000000000005\n", "indigo"},
	}
	for _, tc := range cases {
		if got := CabalTintName(tc.id); got != tc.want {
			t.Errorf("CabalTintName(%q) = %q, want %q", tc.id, got, tc.want)
		}
	}
}

func TestInviteURL_isTheLandingPageJoinRoute(t *testing.T) {
	if got := InviteURL("K7QM4XPD"); got != "https://trymonaco.xyz/join/K7QM4XPD" {
		t.Fatalf("InviteURL = %q", got)
	}
}

// --- database ---

type stubPotValuer struct {
	micros int64
	err    error
}

func (s stubPotValuer) PotValueMicros(context.Context, string) (int64, error) {
	return s.micros, s.err
}

type inviteFixture struct {
	t          *testing.T
	store      *postgres.Store
	privy      privy.Client
	iso        *postgres.TestIsolation
	sessions   *SessionService
	governance *GovernanceService
	invites    *InviteService
}

func newInviteFixture(t *testing.T) inviteFixture {
	t.Helper()
	db, iso := integrationDB(t)
	store := postgres.NewStore(db)
	privyClient := privy.NewFakeClient()
	governance := NewGovernanceService(store, privyClient)
	return inviteFixture{
		t:          t,
		store:      store,
		privy:      privyClient,
		iso:        iso,
		sessions:   NewSessionService(store, privyClient),
		governance: governance,
		invites:    NewInviteService(store, privyClient, governance, stubPotValuer{micros: 1_240_500_000}),
	}
}

// member signs a user in and returns their token.
func (f inviteFixture) member(label string) (SessionResult, string) {
	f.t.Helper()
	session := openTestSession(f.t, f.iso, f.sessions, f.privy, label, label)
	return session, f.iso.UniqueToken(label)
}

func (f inviteFixture) cabal(token, label string, mode JoinMode) string {
	f.t.Helper()
	rules := DefaultGroupRules()
	rules.JoinPolicy = JoinPolicy{Mode: mode}
	created, err := f.governance.CreateGroupWithRules(context.Background(), token, testGroupName(f.iso, label), rules)
	if err != nil {
		f.t.Fatalf("create cabal: %v", err)
	}
	f.iso.TrackGroup(created.GroupID)
	return created.GroupID
}

// codes returns a generator that hands out the given codes in order.
func codes(list ...string) func() (string, error) {
	next := 0
	return func() (string, error) {
		if next >= len(list) {
			return "", errors.New("test generator ran out of codes")
		}
		code := list[next]
		next++
		return code, nil
	}
}

// uniqueCode is a code unlikely to collide with anything a parallel test holds.
func uniqueCode(t *testing.T) string {
	t.Helper()
	code, err := NewInviteCode(rand.Reader)
	if err != nil {
		t.Fatalf("draw code: %v", err)
	}
	return code
}

func TestCurrentInvite_createsOnceThenReturnsTheSameCode(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newInviteFixture(t)
	_, token := f.member("creator")
	groupID := f.cabal(token, "current", JoinModeOpen)
	ctx := context.Background()

	// Act
	first, err := f.invites.CurrentInvite(ctx, token, groupID)
	if err != nil {
		t.Fatalf("first CurrentInvite: %v", err)
	}
	second, err := f.invites.CurrentInvite(ctx, token, groupID)
	if err != nil {
		t.Fatalf("second CurrentInvite: %v", err)
	}

	// Assert
	if !inviteCodeShape.MatchString(first.Code) {
		t.Fatalf("code %q is not eight alphabet symbols", first.Code)
	}
	if second.Code != first.Code {
		t.Fatalf("second call = %q, want the same live code %q", second.Code, first.Code)
	}
	if first.URL != "https://trymonaco.xyz/join/"+first.Code {
		t.Fatalf("url = %q", first.URL)
	}
}

func TestNewInvite_revokesTheOldCode(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newInviteFixture(t)
	_, token := f.member("creator")
	groupID := f.cabal(token, "rotate", JoinModeOpen)
	ctx := context.Background()
	old, err := f.invites.CurrentInvite(ctx, token, groupID)
	if err != nil {
		t.Fatalf("CurrentInvite: %v", err)
	}

	// Act
	fresh, err := f.invites.NewInvite(ctx, token, groupID)
	if err != nil {
		t.Fatalf("NewInvite: %v", err)
	}

	// Assert
	if fresh.Code == old.Code {
		t.Fatalf("NewInvite returned the old code %q", old.Code)
	}
	if _, err := f.invites.Preview(ctx, old.Code); !errors.Is(err, ErrInviteNotFound) {
		t.Fatalf("old code preview err = %v, want ErrInviteNotFound", err)
	}
	if current, err := f.invites.CurrentInvite(ctx, token, groupID); err != nil || current.Code != fresh.Code {
		t.Fatalf("CurrentInvite after NewInvite = (%q, %v), want %q", current.Code, err, fresh.Code)
	}
}

func TestNewInvite_drawsAgainWhenTheCodeIsTaken(t *testing.T) {
	t.Parallel()
	// Arrange: another cabal already holds `taken`.
	f := newInviteFixture(t)
	_, token := f.member("creator")
	other := f.cabal(token, "holder", JoinModeOpen)
	groupID := f.cabal(token, "retry", JoinModeOpen)
	ctx := context.Background()
	taken, fresh := uniqueCode(t), uniqueCode(t)
	if _, err := f.invites.WithCodeSource(codes(taken)).CurrentInvite(ctx, token, other); err != nil {
		t.Fatalf("seed taken code: %v", err)
	}

	// Act
	invite, err := f.invites.WithCodeSource(codes(taken, fresh)).NewInvite(ctx, token, groupID)

	// Assert
	if err != nil {
		t.Fatalf("NewInvite: %v", err)
	}
	if invite.Code != fresh {
		t.Fatalf("code = %q, want the second draw %q", invite.Code, fresh)
	}
	holder, err := f.invites.Preview(ctx, taken)
	if err != nil || holder.GroupID != other {
		t.Fatalf("taken code now previews (%q, %v), want it still on %q", holder.GroupID, err, other)
	}
}

func TestNewInvite_givesUpAfterEveryDrawCollides(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newInviteFixture(t)
	_, token := f.member("creator")
	other := f.cabal(token, "holder", JoinModeOpen)
	groupID := f.cabal(token, "exhausted", JoinModeOpen)
	ctx := context.Background()
	taken := uniqueCode(t)
	if _, err := f.invites.WithCodeSource(codes(taken)).CurrentInvite(ctx, token, other); err != nil {
		t.Fatalf("seed taken code: %v", err)
	}
	always := func() (string, error) { return taken, nil }

	// Act
	_, err := f.invites.WithCodeSource(always).NewInvite(ctx, token, groupID)

	// Assert
	if !errors.Is(err, ErrInviteCodeExhausted) {
		t.Fatalf("err = %v, want ErrInviteCodeExhausted", err)
	}
}

func TestRevokeInvite_leavesNoLiveCodeUntilAsked(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newInviteFixture(t)
	_, token := f.member("creator")
	groupID := f.cabal(token, "revoke", JoinModeOpen)
	ctx := context.Background()
	old, err := f.invites.CurrentInvite(ctx, token, groupID)
	if err != nil {
		t.Fatalf("CurrentInvite: %v", err)
	}

	// Act
	if err := f.invites.RevokeInvite(ctx, token, groupID); err != nil {
		t.Fatalf("RevokeInvite: %v", err)
	}
	again := f.invites.RevokeInvite(ctx, token, groupID)
	next, err := f.invites.CurrentInvite(ctx, token, groupID)

	// Assert
	if again != nil {
		t.Fatalf("revoking with no live code = %v, want nil", again)
	}
	if _, previewErr := f.invites.Preview(ctx, old.Code); !errors.Is(previewErr, ErrInviteNotFound) {
		t.Fatalf("revoked code preview err = %v, want ErrInviteNotFound", previewErr)
	}
	if err != nil || next.Code == old.Code {
		t.Fatalf("CurrentInvite after revoke = (%q, %v), want a new code", next.Code, err)
	}
}

func TestInviteManagement_isForMembersOnly(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newInviteFixture(t)
	_, creator := f.member("creator")
	_, stranger := f.member("stranger")
	groupID := f.cabal(creator, "members", JoinModeOpen)
	ctx := context.Background()

	calls := map[string]func(token, id string) error{
		"current": func(token, id string) error { _, err := f.invites.CurrentInvite(ctx, token, id); return err },
		"new":     func(token, id string) error { _, err := f.invites.NewInvite(ctx, token, id); return err },
		"revoke":  func(token, id string) error { return f.invites.RevokeInvite(ctx, token, id) },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			// Act / Assert
			if err := call(stranger, groupID); !errors.Is(err, ErrGroupNotFound) {
				t.Errorf("non-member err = %v, want ErrGroupNotFound", err)
			}
			if err := call(creator, "not-a-uuid"); !errors.Is(err, ErrGroupNotFound) {
				t.Errorf("malformed id err = %v, want ErrGroupNotFound", err)
			}
			if err := call("no-such-token", groupID); !errors.Is(err, privy.ErrInvalidToken) {
				t.Errorf("bad token err = %v, want ErrInvalidToken", err)
			}
		})
	}
}

func TestPreview_describesTheCabalBehindALiveCode(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newInviteFixture(t)
	_, token := f.member("creator")
	groupID := f.cabal(token, "preview", JoinModeRequest)
	ctx := context.Background()
	invite, err := f.invites.CurrentInvite(ctx, token, groupID)
	if err != nil {
		t.Fatalf("CurrentInvite: %v", err)
	}

	// Act: lowercase and grouped, the way someone might type it.
	preview, err := f.invites.Preview(ctx, strings.ToLower(invite.Code[:4]+" "+invite.Code[4:]))

	// Assert
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	want := InvitePreview{
		Code:        invite.Code,
		GroupID:     groupID,
		Name:        testGroupName(f.iso, "preview"),
		MemberCount: 1,
		Tint:        CabalTintName(groupID),
		JoinPolicy:  JoinModeRequest,
		PotValueUsd: "1240.50",
	}
	if preview != want {
		t.Fatalf("preview = %+v, want %+v", preview, want)
	}
}

func TestPreview_unknownAndMalformedCodesAreNotFound(t *testing.T) {
	t.Parallel()
	f := newInviteFixture(t)
	for _, raw := range []string{uniqueCode(t), "", "nope", "AB12CD34", "K7QM4XPD9"} {
		if _, err := f.invites.Preview(context.Background(), raw); !errors.Is(err, ErrInviteNotFound) {
			t.Errorf("Preview(%q) err = %v, want ErrInviteNotFound", raw, err)
		}
	}
}

func TestPreview_aValuationFailureIsAnError(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newInviteFixture(t)
	_, token := f.member("creator")
	groupID := f.cabal(token, "unpriced", JoinModeOpen)
	ctx := context.Background()
	invite, err := f.invites.CurrentInvite(ctx, token, groupID)
	if err != nil {
		t.Fatalf("CurrentInvite: %v", err)
	}
	broken := NewInviteService(f.store, f.privy, f.governance, stubPotValuer{err: errors.New("rpc down")})

	// Act
	_, err = broken.Preview(ctx, invite.Code)

	// Assert
	if err == nil || errors.Is(err, ErrInviteNotFound) {
		t.Fatalf("err = %v, want the valuation failure, not a 404", err)
	}
}

func TestJoinByCode_followsTheCabalsJoinPolicy(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		mode    JoinMode
		outcome JoinGroupOutcome
		member  bool
	}{
		{"open cabal takes the member in", JoinModeOpen, JoinOutcomeJoined, true},
		{"request cabal files a request", JoinModeRequest, JoinOutcomePending, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Arrange
			f := newInviteFixture(t)
			_, creator := f.member("creator")
			joiner, joinerToken := f.member("joiner")
			groupID := f.cabal(creator, "join", tc.mode)
			ctx := context.Background()
			invite, err := f.invites.CurrentInvite(ctx, creator, groupID)
			if err != nil {
				t.Fatalf("CurrentInvite: %v", err)
			}

			// Act
			gotGroup, outcome, err := f.invites.JoinByCode(ctx, joinerToken, strings.ToLower(invite.Code))

			// Assert
			if err != nil {
				t.Fatalf("JoinByCode: %v", err)
			}
			if gotGroup != groupID || outcome != tc.outcome {
				t.Fatalf("JoinByCode = (%q, %q), want (%q, %q)", gotGroup, outcome, groupID, tc.outcome)
			}
			isMember, err := f.store.IsGroupMember(ctx, groupID, joiner.UserID)
			if err != nil || isMember != tc.member {
				t.Fatalf("member after join = (%v, %v), want %v", isMember, err, tc.member)
			}
		})
	}
}

func TestJoinByCode_aMemberJoiningAgainIsAlreadyIn(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newInviteFixture(t)
	_, creator := f.member("creator")
	groupID := f.cabal(creator, "again", JoinModeOpen)
	ctx := context.Background()
	invite, err := f.invites.CurrentInvite(ctx, creator, groupID)
	if err != nil {
		t.Fatalf("CurrentInvite: %v", err)
	}

	// Act
	_, outcome, err := f.invites.JoinByCode(ctx, creator, invite.Code)

	// Assert
	if err != nil || outcome != JoinOutcomeAlreadyMember {
		t.Fatalf("JoinByCode = (%q, %v), want already_member", outcome, err)
	}
}

func TestJoinByCode_refusesDeadCodesAndMissingSessions(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newInviteFixture(t)
	_, creator := f.member("creator")
	_, joiner := f.member("joiner")
	groupID := f.cabal(creator, "dead", JoinModeOpen)
	ctx := context.Background()
	revoked, err := f.invites.CurrentInvite(ctx, creator, groupID)
	if err != nil {
		t.Fatalf("CurrentInvite: %v", err)
	}
	if _, err := f.invites.NewInvite(ctx, creator, groupID); err != nil {
		t.Fatalf("NewInvite: %v", err)
	}

	cases := []struct {
		name  string
		token string
		code  string
		want  error
	}{
		{"unknown code", joiner, uniqueCode(t), ErrInviteNotFound},
		{"revoked code", joiner, revoked.Code, ErrInviteNotFound},
		{"malformed code", joiner, "AB12CD34", ErrInviteNotFound},
		{"no session, live code", "no-such-token", revoked.Code, privy.ErrInvalidToken},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			_, _, err := f.invites.JoinByCode(ctx, tc.token, tc.code)

			// Assert
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/packages/domain"
)

// Invite codes: a cabal's short, shareable way in.
//
// A code is eight characters from an alphabet without 0, O, 1 or I, so it survives being
// read aloud or copied off a screenshot. 32 symbols to the eighth is about 10^12 codes, which
// is what keeps the public preview safe to leave unauthenticated: a guesser at the per-IP
// limit would need millennia to find one live code.
//
// Each cabal has at most one live code. Asking for the current code creates one when there
// is none; asking for a new one revokes the old in the same transaction. Revoked codes are
// kept so they never come back as someone else's.
const (
	InviteCodeAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	InviteCodeLength   = 8
	// InviteLinkBase is what a code is appended to for the link members share. The app's
	// associated domain and the landing page's /join route are both on this host.
	InviteLinkBase = "https://trymonaco.xyz/join/"

	// inviteCodeAttempts bounds the redraws when a generated code already exists. With
	// 10^12 codes a single collision is already unlikely; five in a row means the
	// generator is broken, and the request fails rather than looping.
	inviteCodeAttempts = 5
)

var (
	// ErrInviteNotFound means the code is malformed, unknown, or revoked. The three are
	// one answer on purpose: a revoked code must not confirm that it once existed.
	ErrInviteNotFound = errors.New("invite not found")
	// ErrInviteCodeExhausted means every drawn code was already taken.
	ErrInviteCodeExhausted = errors.New("could not draw an unused invite code")
)

// NewInviteCode draws a code from random. 256 is a multiple of the alphabet's 32 symbols,
// so taking each byte mod 32 is uniform.
func NewInviteCode(random io.Reader) (string, error) {
	raw := make([]byte, InviteCodeLength)
	if _, err := io.ReadFull(random, raw); err != nil {
		return "", fmt.Errorf("read random invite code: %w", err)
	}
	code := make([]byte, InviteCodeLength)
	for i, b := range raw {
		code[i] = InviteCodeAlphabet[int(b)%len(InviteCodeAlphabet)]
	}
	return string(code), nil
}

// NormalizeInviteCode uppercases raw and drops spaces and dashes, so "k7qm-4xpd" and
// "K7QM 4XPD" name the same code. It reports false for anything that is not eight symbols
// of the alphabet.
func NormalizeInviteCode(raw string) (string, bool) {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(raw)) {
		switch {
		case r == ' ' || r == '-':
			continue
		case r > 127 || !strings.ContainsRune(InviteCodeAlphabet, r):
			return "", false
		}
		b.WriteRune(r)
	}
	code := b.String()
	if len(code) != InviteCodeLength {
		return "", false
	}
	return code, true
}

// InviteURL is the link a member shares for code.
func InviteURL(code string) string {
	return InviteLinkBase + code
}

// cabalTints are the app's CabalTint cases in their identity order (MonacoTheme.swift).
var cabalTints = []string{"pine", "ochre", "plum", "indigo", "moss"}

// CabalTintName is the cabal's identity tint, computed exactly as the app's
// `CabalTint.forGroupId` does: FNV-1a 64 over the trimmed, lowercased id, mod 5. The
// preview carries it so the landing page paints the same mark the app does.
func CabalTintName(groupID string) string {
	key := strings.ToLower(strings.TrimSpace(groupID))
	var hash uint64 = 0xCBF29CE484222325
	for i := 0; i < len(key); i++ {
		hash ^= uint64(key[i])
		hash *= 0x100000001B3
	}
	return cabalTints[hash%uint64(len(cabalTints))]
}

// Invite is a cabal's live code and its link.
type Invite struct {
	Code    string
	URL     string
	GroupID string
}

// InvitePreview is what anyone holding a live code may see about its cabal: the fields the
// Groups tab already publishes for every cabal, plus the tint the mark is drawn in.
type InvitePreview struct {
	Code        string
	GroupID     string
	Name        string
	MemberCount int
	Tint        string
	// PictureURL is blank when the cabal has no picture.
	PictureURL  string
	JoinPolicy  domain.JoinMode
	PotValueUsd string
}

// invitePotValuer prices a cabal's pot for the preview.
type invitePotValuer interface {
	PotValueMicros(ctx context.Context, groupID string) (int64, error)
}

// inviteJoiner is the join a code leads to: the same one POST /v1/groups/{id}/join runs.
type inviteJoiner interface {
	JoinGroup(ctx context.Context, accessToken, groupID string) (JoinGroupOutcome, error)
}

// InviteService issues, revokes, previews and redeems invite codes.
type InviteService struct {
	store   *postgres.Store
	privy   privy.Client
	joiner  inviteJoiner
	pots    invitePotValuer
	newCode func() (string, error)
}

// NewInviteService wires invites. joiner is normally the GovernanceService and pots the
// GroupsTabService, whose 30-second valuation cache keeps a shared link from pricing the
// pot on every open.
func NewInviteService(store *postgres.Store, privyClient privy.Client, joiner inviteJoiner, pots invitePotValuer) *InviteService {
	return &InviteService{
		store:   store,
		privy:   privyClient,
		joiner:  joiner,
		pots:    pots,
		newCode: func() (string, error) { return NewInviteCode(rand.Reader) },
	}
}

// WithCodeSource replaces the code generator. Intended for tests.
func (s *InviteService) WithCodeSource(next func() (string, error)) *InviteService {
	s.newCode = next
	return s
}

// CurrentInvite returns the cabal's live code, creating one when it has none. Members only;
// anyone else gets ErrGroupNotFound, as they do for the cabal itself.
func (s *InviteService) CurrentInvite(ctx context.Context, accessToken, groupID string) (Invite, error) {
	user, err := s.authenticatedMember(ctx, accessToken, groupID)
	if err != nil {
		return Invite{}, err
	}
	return s.inGroupLock(ctx, groupID, func(tx *sql.Tx) (postgres.InviteCode, error) {
		live, found, err := s.store.GetLiveInviteCodeTx(ctx, tx, groupID)
		if err != nil || found {
			return live, err
		}
		return s.insertFreshCode(ctx, tx, groupID, user.ID)
	})
}

// NewInvite revokes the cabal's live code, if any, and returns a fresh one. Members only.
func (s *InviteService) NewInvite(ctx context.Context, accessToken, groupID string) (Invite, error) {
	user, err := s.authenticatedMember(ctx, accessToken, groupID)
	if err != nil {
		return Invite{}, err
	}
	invite, err := s.inGroupLock(ctx, groupID, func(tx *sql.Tx) (postgres.InviteCode, error) {
		if _, err := s.store.RevokeLiveInviteCodesTx(ctx, tx, groupID); err != nil {
			return postgres.InviteCode{}, err
		}
		return s.insertFreshCode(ctx, tx, groupID, user.ID)
	})
	if err == nil {
		slog.InfoContext(ctx, "invite code replaced", "group_id", groupID, "user_id", user.ID)
	}
	return invite, err
}

// RevokeInvite revokes the cabal's live code, leaving it with none until a member asks for
// one again. Members only. Revoking a cabal with no live code is not an error.
func (s *InviteService) RevokeInvite(ctx context.Context, accessToken, groupID string) error {
	user, err := s.authenticatedMember(ctx, accessToken, groupID)
	if err != nil {
		return err
	}
	tx, err := s.store.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if found, err := s.store.LockGroupForInviteTx(ctx, tx, groupID); err != nil {
		return err
	} else if !found {
		return ErrGroupNotFound
	}
	revoked, err := s.store.RevokeLiveInviteCodesTx(ctx, tx, groupID)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit revoke invite: %w", err)
	}
	slog.InfoContext(ctx, "invite code revoked", "group_id", groupID, "user_id", user.ID, "revoked", revoked)
	return nil
}

// Preview answers GET /v1/invites/{code} without a session. Anything but a live code is
// ErrInviteNotFound.
func (s *InviteService) Preview(ctx context.Context, rawCode string) (InvitePreview, error) {
	invite, err := s.liveInvite(ctx, rawCode)
	if err != nil {
		return InvitePreview{}, err
	}
	dir, found, err := s.store.GetGroupDirectoryRow(ctx, invite.GroupID)
	if err != nil {
		return InvitePreview{}, err
	}
	if !found {
		return InvitePreview{}, ErrInviteNotFound
	}
	potMicros, err := s.pots.PotValueMicros(ctx, invite.GroupID)
	if err != nil {
		return InvitePreview{}, err
	}
	return InvitePreview{
		Code:        invite.Code,
		GroupID:     dir.ID,
		Name:        dir.Name,
		MemberCount: dir.MemberCount,
		Tint:        CabalTintName(dir.ID),
		PictureURL:  nullStringValue(dir.PictureURL),
		JoinPolicy:  dir.JoinMode,
		PotValueUsd: formatMicrosAsUsdDecimal(potMicros),
	}, nil
}

// JoinByCode resolves a live code and runs the cabal's own join: an open cabal takes the
// member in, one that approves members gets a join request. It returns the cabal's id so
// the caller can open it.
func (s *InviteService) JoinByCode(ctx context.Context, accessToken, rawCode string) (string, JoinGroupOutcome, error) {
	// The session first, so a caller without one learns nothing about the code.
	if _, err := s.privy.VerifySession(ctx, privy.AccessToken(accessToken)); err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			return "", "", privy.ErrInvalidToken
		}
		return "", "", fmt.Errorf("verify session: %w", err)
	}
	invite, err := s.liveInvite(ctx, rawCode)
	if err != nil {
		return "", "", err
	}
	outcome, err := s.joiner.JoinGroup(ctx, accessToken, invite.GroupID)
	if err != nil {
		return invite.GroupID, "", err
	}
	return invite.GroupID, outcome, nil
}

func (s *InviteService) liveInvite(ctx context.Context, rawCode string) (postgres.InviteCode, error) {
	code, ok := NormalizeInviteCode(rawCode)
	if !ok {
		return postgres.InviteCode{}, ErrInviteNotFound
	}
	invite, found, err := s.store.GetInviteCode(ctx, code)
	if err != nil {
		return postgres.InviteCode{}, err
	}
	if !found || !invite.Live() {
		return postgres.InviteCode{}, ErrInviteNotFound
	}
	return invite, nil
}

// authenticatedMember resolves the caller and checks they belong to groupID. A malformed
// id, an unknown cabal and someone else's cabal are all ErrGroupNotFound.
func (s *InviteService) authenticatedMember(ctx context.Context, accessToken, groupID string) (postgres.User, error) {
	identity, err := s.privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			return postgres.User{}, privy.ErrInvalidToken
		}
		return postgres.User{}, fmt.Errorf("verify session: %w", err)
	}
	user, found, err := s.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		return postgres.User{}, err
	}
	if !found {
		return postgres.User{}, ErrUserNotFound
	}
	if !isUUID(groupID) {
		return postgres.User{}, ErrGroupNotFound
	}
	member, err := s.store.IsGroupMember(ctx, groupID, user.ID)
	if err != nil {
		return postgres.User{}, err
	}
	if !member {
		return postgres.User{}, ErrGroupNotFound
	}
	return user, nil
}

// inGroupLock runs write inside one transaction that holds the cabal's row lock, so two
// members asking at once see one code rather than racing to insert two.
func (s *InviteService) inGroupLock(ctx context.Context, groupID string, write func(*sql.Tx) (postgres.InviteCode, error)) (Invite, error) {
	tx, err := s.store.BeginTx(ctx)
	if err != nil {
		return Invite{}, err
	}
	defer func() { _ = tx.Rollback() }()
	found, err := s.store.LockGroupForInviteTx(ctx, tx, groupID)
	if err != nil {
		return Invite{}, err
	}
	if !found {
		return Invite{}, ErrGroupNotFound
	}
	code, err := write(tx)
	if err != nil {
		return Invite{}, err
	}
	if err := tx.Commit(); err != nil {
		return Invite{}, fmt.Errorf("commit invite: %w", err)
	}
	return Invite{Code: code.Code, URL: InviteURL(code.Code), GroupID: code.GroupID}, nil
}

// insertFreshCode draws codes until one is unused. The insert does not raise on a taken
// code, so the transaction survives a collision.
func (s *InviteService) insertFreshCode(ctx context.Context, tx *sql.Tx, groupID, userID string) (postgres.InviteCode, error) {
	for attempt := 1; attempt <= inviteCodeAttempts; attempt++ {
		code, err := s.newCode()
		if err != nil {
			return postgres.InviteCode{}, err
		}
		if normalized, ok := NormalizeInviteCode(code); !ok || normalized != code {
			return postgres.InviteCode{}, fmt.Errorf("invite code generator produced %q, outside the alphabet", code)
		}
		invite, err := s.store.InsertInviteCodeTx(ctx, tx, code, groupID, userID)
		if errors.Is(err, postgres.ErrInviteCodeTaken) {
			slog.WarnContext(ctx, "invite code collision, drawing again", "group_id", groupID, "attempt", attempt)
			continue
		}
		return invite, err
	}
	return postgres.InviteCode{}, ErrInviteCodeExhausted
}

// PotValueMicros is the cabal's pot as the Groups tab values it, through the same
// 30-second cache, so an invite preview and a leaderboard row agree.
func (s *GroupsTabService) PotValueMicros(ctx context.Context, groupID string) (int64, error) {
	valuations, err := s.valueGroups(ctx, []string{groupID}, nil)
	if err != nil {
		return 0, err
	}
	return valuations[groupID].PotNavMicros, nil
}

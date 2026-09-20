package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
)

// Faker (#153) guards shared by app services.
//
// Two kinds of seeded demo rows exist:
//   - faker scale clubs (groups.is_faker): wholly fake, dummy treasury, spectator-readable by any
//     authenticated user, never mutable, never touched by Privy/RPC/Jupiter.
//   - ghost members (users.is_faker) inside a real group: display-only positions, excluded from
//     pot math, surplus credit, and the live voter set.
//
// These guards stay active regardless of FAKER_ENABLED so leftover seed rows remain inert.

// ErrFakerGroupReadOnly means a mutation targeted a faker scale club or a faker-owned proposal.
var ErrFakerGroupReadOnly = errors.New("faker group is read-only")

// authorizeGroupReader verifies the session and allows members, plus any authenticated user
// for faker scale clubs. Returns the viewer's user id.
func authorizeGroupReader(ctx context.Context, store *postgres.Store, privyClient wallets.Client, accessToken, groupID string) (string, error) {
	identity, err := privyClient.VerifySession(ctx, auth.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			return "", auth.ErrUnauthorized
		}
		return "", fmt.Errorf("verify session: %w", err)
	}

	user, found, err := store.GetUserByDynamicUserID(ctx, identity.DynamicUserID)
	if err != nil {
		return "", err
	}
	if !found {
		return "", ErrUserNotFound
	}

	ok, err := store.CanReadGroup(ctx, groupID, user.ID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrGroupNotFound
	}
	return user.ID, nil
}

// rejectFakerGroup returns ErrFakerGroupReadOnly when groupID is a faker scale club.
func rejectFakerGroup(ctx context.Context, store *postgres.Store, groupID string) error {
	isFaker, err := store.IsFakerGroup(ctx, groupID)
	if err != nil {
		return err
	}
	if isFaker {
		return ErrFakerGroupReadOnly
	}
	return nil
}

// boardShareBasis returns the (total shares, pot NAV) pair member rows are valued against.
//
// potTotalShares is the pot-backed share sum (ghosts excluded). Real members and ghosts are
// both valued at the real NAV per share, so ghosts never dilute real equity. When the real pot
// has no shares yet, ghosts are valued at NAV 1.0 so their seeded P&L still renders.
func boardShareBasis(potTotalShares, potNav int64, positions []postgres.PositionRow) (int64, int64) {
	if potTotalShares > 0 {
		return potTotalShares, potNav
	}
	var ghostShares int64
	for _, position := range positions {
		if position.Ghost && position.ShareUnits > 0 {
			ghostShares += position.ShareUnits
		}
	}
	if ghostShares > 0 {
		return ghostShares, ghostShares
	}
	return potTotalShares, potNav
}

// potNetUsdcIn sums net deposits backed by the pot (ghost positions excluded).
func potNetUsdcIn(positions []postgres.PositionRow) int64 {
	var net int64
	for _, position := range positions {
		if position.Ghost {
			continue
		}
		net += position.AmountDeposited - position.AmountWithdrawn
	}
	return net
}

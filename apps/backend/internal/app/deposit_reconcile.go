package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/packages/domain"
)

// CreditUncreditedTreasuryUSDC credits treasury USDC that the group's ledger cannot explain:
// USDC that reached the treasury without a deposit row (a transfer straight to the treasury
// address). Only that is an uncredited deposit. Realized trading gains, sell proceeds and
// USDC reserved for an in-flight redeem payout are all on the ledger already, so they stay
// P&L and stay owed to whoever they belong to.
//
// The credit is priced like any other deposit: shares are minted at pre-credit NAV from the
// shared pot valuation and split across holders by their share of the pot. When the pot
// cannot be priced from live marks the credit is deferred to a later pass.
// Faker scale clubs (#153) are skipped: their treasury is a dummy row with no chain balance.
// Ghost (faker) positions in real groups are excluded from the share sum and from credits.
func (d *DepositService) CreditUncreditedTreasuryUSDC(ctx context.Context, groupID string) (bool, error) {
	if groupID == "" {
		return false, fmt.Errorf("group id is required")
	}

	isFaker, err := d.store.IsFakerGroup(ctx, groupID)
	if err != nil {
		return false, err
	}
	if isFaker {
		return false, nil
	}

	// A pending sweep or swap may already have moved treasury USDC that the ledger only
	// records once it confirms; until then the difference is not a surplus.
	hasPending, err := d.store.HasPendingDepositsForGroup(ctx, groupID)
	if err != nil {
		return false, err
	}
	if hasPending {
		return false, nil
	}
	hasPendingSwap, err := d.store.HasPendingTransactionsForGroup(ctx, groupID)
	if err != nil {
		return false, err
	}
	if hasPendingSwap {
		return false, nil
	}

	treasury, found, err := d.store.GetTreasuryByGroupID(ctx, groupID)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}

	treasuryUSDC, err := d.privy.TreasuryUSDCBalance(ctx, treasury.SolanaAddress)
	if err != nil {
		return false, fmt.Errorf("treasury usdc balance: %w", err)
	}

	ledgerUSDC, err := d.store.PotLedgerUSDC(ctx, groupID)
	if err != nil {
		return false, err
	}
	if ledgerUSDC < 0 {
		ledgerUSDC = 0
	}
	surplus := treasuryUSDC - ledgerUSDC
	if surplus <= 0 {
		return false, nil
	}

	// The valuation caps cash at the ledger, so this is the pot before the surplus counts.
	valuation, err := valuePot(ctx, d.store, d.pyth, d.symbols, nil, groupID, treasury.SolanaAddress, treasuryUSDC, potMarksLiveOnly)
	if err != nil {
		if errors.Is(err, ErrPotMarkUnavailable) {
			logPotMarkUnavailable(groupID, "treasury_surplus_credit", err)
			return false, nil
		}
		return false, err
	}
	if valuation.ShareBaseMicros > 0 && valuation.PotNavMicros <= 0 {
		slog.Warn("treasury surplus credit deferred: pot with shares outstanding has no value to price against",
			"group_id", groupID,
			"surplus_micros", surplus,
			"share_base_micros", valuation.ShareBaseMicros,
		)
		return false, nil
	}

	minted, err := domain.ShareUnitsMicrosForDeposit(domain.USDCMicros(surplus), valuation.ShareBaseMicros, domain.USDCMicros(valuation.PotNavMicros))
	if err != nil {
		// Only a surplus too small to mint a single share unit gets here; it waits for more.
		slog.Warn("treasury surplus credit deferred: surplus cannot be priced",
			"group_id", groupID,
			"surplus_micros", surplus,
			"err", err,
		)
		return false, nil
	}

	credits, err := d.surplusCredits(ctx, groupID, surplus, minted, valuation.ShareBaseMicros)
	if err != nil {
		return false, err
	}
	if len(credits) == 0 {
		return false, nil
	}

	navAfterCredit, err := navSnapshotValuesFor(valuation.PotNavMicros+surplus, valuation.ShareBaseMicros+minted)
	if err != nil {
		return false, err
	}

	tx, err := d.store.BeginTx(ctx)
	if err != nil {
		return false, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	for userID, credit := range credits {
		if _, err := d.store.IncrementPositionTx(ctx, tx, userID, groupID, credit.shareUnits, credit.usdc); err != nil {
			return false, err
		}
	}

	if err := d.store.WriteNavSnapshotOnDepositConfirmTx(ctx, tx, groupID, navAfterCredit); err != nil {
		return false, err
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit treasury reconcile: %w", err)
	}
	committed = true

	slog.Info("treasury surplus credited",
		"group_id", groupID,
		"surplus_micros", surplus,
		"share_units_minted", minted,
		"pre_credit_nav_micros", valuation.PotNavMicros,
		"recipients", len(credits),
	)
	return true, nil
}

// surplusCredit is one holder's part of a credited treasury surplus.
type surplusCredit struct {
	shareUnits int64
	usdc       int64
}

// surplusCredits splits the surplus USDC and the share units minted for it across holders by
// share units held, so nobody's share of the pot moves. With no shares outstanding the
// group creator receives it all.
func (d *DepositService) surplusCredits(
	ctx context.Context,
	groupID string,
	surplus, minted, shareBaseMicro int64,
) (map[string]surplusCredit, error) {
	if surplus <= 0 {
		return nil, nil
	}

	if shareBaseMicro == 0 {
		group, found, err := d.store.GetGroupByID(ctx, groupID)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("group not found")
		}
		return map[string]surplusCredit{group.CreatorUserID: {shareUnits: minted, usdc: surplus}}, nil
	}

	positions, err := d.store.ListPositionsByGroup(ctx, groupID)
	if err != nil {
		return nil, err
	}

	holders := make([]postgres.PositionRow, 0, len(positions))
	var heldMicro int64
	for _, position := range positions {
		if position.Ghost || position.ShareUnits <= 0 {
			continue
		}
		holders = append(holders, position)
		heldMicro += position.ShareUnits
	}
	if len(holders) == 0 {
		// Every outstanding unit belongs to an in-flight redeem job; wait for it to settle.
		return nil, nil
	}

	credits := make(map[string]surplusCredit, len(holders))
	var allocatedUnits, allocatedUSDC int64
	firstCredited := ""
	for _, position := range holders {
		units, err := domain.MulDivFloor(minted, position.ShareUnits, heldMicro)
		if err != nil {
			return nil, fmt.Errorf("surplus share credit: %w", err)
		}
		usdc, err := domain.MulDivFloor(surplus, position.ShareUnits, heldMicro)
		if err != nil {
			return nil, fmt.Errorf("surplus credit: %w", err)
		}
		if units <= 0 || usdc <= 0 {
			// A holder too small to receive a whole micro of either leaves it to the remainder.
			continue
		}
		credits[position.UserID] = surplusCredit{shareUnits: units, usdc: usdc}
		allocatedUnits += units
		allocatedUSDC += usdc
		if firstCredited == "" {
			firstCredited = position.UserID
		}
	}
	if firstCredited == "" {
		return nil, nil
	}

	// Flooring leaves a few micros unallocated; the first holder takes them so the ledger
	// matches the treasury exactly.
	first := credits[firstCredited]
	first.shareUnits += minted - allocatedUnits
	first.usdc += surplus - allocatedUSDC
	credits[firstCredited] = first
	return credits, nil
}

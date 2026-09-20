package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/monaco/monaco/packages/domain"
)

// CreditUncreditedTreasuryUSDC mints share_units and amount_deposited for USDC-only pots
// when treasury USDC exceeds attributed share units (1:1 M2 invariant).
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

	hasPending, err := d.store.HasPendingDepositsForGroup(ctx, groupID)
	if err != nil {
		return false, err
	}
	if hasPending {
		return false, nil
	}

	holdings, err := d.store.ListNetTokenHoldingsByGroup(ctx, groupID)
	if err != nil {
		return false, err
	}
	if len(holdings) > 0 {
		return false, nil
	}

	treasury, found, err := d.store.GetTreasuryByGroupID(ctx, groupID)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}

	treasuryUSDC, err := d.wallets.TreasuryUSDCBalance(ctx, treasury.Address)
	if err != nil {
		return false, fmt.Errorf("treasury usdc balance: %w", err)
	}

	totalSharesMicro, err := d.store.SumShareUnitsByGroup(ctx, groupID)
	if err != nil {
		return false, err
	}

	surplus := treasuryUSDC - totalSharesMicro
	if surplus <= 0 {
		return false, nil
	}

	credits, err := d.surplusCredits(ctx, groupID, surplus, totalSharesMicro)
	if err != nil {
		return false, err
	}
	if len(credits) == 0 {
		return false, nil
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
		if credit <= 0 {
			continue
		}
		if _, err := d.store.IncrementPositionTx(ctx, tx, userID, groupID, credit, credit); err != nil {
			return false, err
		}
	}

	if err := d.store.WriteNavSnapshotOnDepositConfirmTx(ctx, tx, groupID, treasuryUSDC); err != nil {
		return false, err
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit treasury reconcile: %w", err)
	}
	committed = true

	slog.Info("treasury surplus credited",
		"group_id", groupID,
		"surplus_micros", surplus,
		"recipients", len(credits),
	)
	return true, nil
}

func (d *DepositService) surplusCredits(
	ctx context.Context,
	groupID string,
	surplus, totalSharesMicro int64,
) (map[string]int64, error) {
	if surplus <= 0 {
		return nil, nil
	}

	if totalSharesMicro == 0 {
		group, found, err := d.store.GetGroupByID(ctx, groupID)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("group not found")
		}
		return map[string]int64{group.CreatorUserID: surplus}, nil
	}

	positions, err := d.store.ListPositionsByGroup(ctx, groupID)
	if err != nil {
		return nil, err
	}

	credits := make(map[string]int64)
	var allocated int64
	for _, position := range positions {
		if position.Ghost || position.ShareUnits <= 0 {
			continue
		}
		credit, err := domain.MulDivFloor(surplus, position.ShareUnits, totalSharesMicro)
		if err != nil {
			return nil, fmt.Errorf("surplus credit: %w", err)
		}
		if credit <= 0 {
			continue
		}
		credits[position.UserID] = credit
		allocated += credit
	}

	remainder := surplus - allocated
	if remainder > 0 {
		for _, position := range positions {
			if !position.Ghost && position.ShareUnits > 0 {
				credits[position.UserID] += remainder
				break
			}
		}
	}

	if len(credits) == 0 {
		return nil, fmt.Errorf("no share holders to credit treasury surplus")
	}
	return credits, nil
}

package app

import (
	"context"
	"fmt"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/packages/domain"
)

// ComputePotNAV delegates pot valuation to packages/domain.
// Callers assemble NavInput from treasury balances, transaction cost basis, and Pyth marks.
func ComputePotNAV(in domain.NavInput) (domain.PotNAV, error) {
	return domain.ComputePotNAV(in)
}

// BuildInGroupMemberBoard ranks members in one group by percent return.
func BuildInGroupMemberBoard(members []domain.MemberPosition, totalShares domain.ShareUnits, potNav domain.PotNAV) ([]domain.MemberPnL, error) {
	return domain.BuildInGroupBoard(members, totalShares, potNav.TotalUsdc)
}

// BuildInGroupViewMemberBoard ranks every group member for GET /v1/groups/{id}/view.
func BuildInGroupViewMemberBoard(members []domain.MemberPosition, totalShares domain.ShareUnits, potNav domain.PotNAV) ([]domain.MemberPnL, error) {
	return domain.BuildInGroupViewBoard(members, totalShares, potNav.TotalUsdc)
}

// BuildAppGroupBoard ranks groups by pot percent return.
func BuildAppGroupBoard(groups []domain.GroupBoardInput) []domain.GroupBoardRow {
	return domain.BuildGroupBoard(groups)
}

// BuildAppPeopleBoard ranks people by summed cross-group percent return.
func BuildAppPeopleBoard(people []domain.PersonBoardInput) []domain.PersonBoardRow {
	return domain.BuildPeopleBoard(people)
}

// AggregateCrossGroupPerson aggregates member slices across groups for the people board.
func AggregateCrossGroupPerson(entries []domain.MemberPnL) domain.PersonBoardInput {
	return domain.AggregatePersonPnL(entries)
}

// TreasuryHoldingsView is net confirmed token balances held by a group treasury.
type TreasuryHoldingsView struct {
	Holdings []postgres.TokenHoldingRow
}

// TreasuryHoldingsForGroup loads net confirmed treasury token balances from transactions.
func TreasuryHoldingsForGroup(ctx context.Context, store *postgres.Store, groupID string) (TreasuryHoldingsView, error) {
	if groupID == "" {
		return TreasuryHoldingsView{}, fmt.Errorf("group_id is required")
	}
	holdings, err := store.ListNetTokenHoldingsByGroup(ctx, groupID)
	if err != nil {
		return TreasuryHoldingsView{}, err
	}
	return TreasuryHoldingsView{Holdings: holdings}, nil
}

// RecordConfirmedBuyHoldings verifies treasury holdings after a confirmed buy and writes NAV snapshot.
func RecordConfirmedBuyHoldings(
	ctx context.Context,
	store *postgres.Store,
	groupID string,
	tx postgres.TransactionRow,
	treasuryUSDC int64,
) error {
	if _, err := TreasuryHoldingsForGroup(ctx, store, groupID); err != nil {
		return err
	}
	return store.RecordTreasuryHoldingsAndNavSnapshotOnConfirm(ctx, groupID, tx, treasuryUSDC)
}

// BuildMemberBoardAfterWithdrawal re-ranks the in-group board after a withdrawal payout (M4-T35).
func BuildMemberBoardAfterWithdrawal(positions []Position, totalSharesMicro, potNavMicro int64) ([]domain.MemberPnL, error) {
	members := make([]domain.MemberPosition, 0, len(positions))
	for _, pos := range positions {
		shareUnits, err := domain.ShareUnitsMicrosToDomain(pos.ShareUnits)
		if err != nil {
			return nil, err
		}
		members = append(members, domain.MemberPosition{
			UserID:          pos.UserID,
			ShareUnits:      shareUnits,
			AmountDeposited: domain.USDCMicros(pos.AmountDeposited),
			AmountWithdrawn: domain.USDCMicros(pos.AmountWithdrawn),
		})
	}

	totalShares, err := domain.ShareUnitsMicrosToDomain(totalSharesMicro)
	if err != nil {
		return nil, err
	}

	return BuildInGroupMemberBoard(members, totalShares, domain.PotNAV{
		TotalUsdc: domain.USDCMicros(potNavMicro),
	})
}

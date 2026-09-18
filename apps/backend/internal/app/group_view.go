package app

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/packages/domain"
)

// GroupViewPotRow is one line in the group pot section.
type GroupViewPotRow struct {
	Symbol     string
	Units      string
	MarkUsd    string
	ValueUsd   string
	DollarPnL  string
	AfterHours *bool
}

// GroupViewMemberSlice is the authenticated viewer's slice in a group.
type GroupViewMemberSlice struct {
	ShareUnits    string
	EquityUsd     string
	SlicePercent  string
	DollarPnL     string
	PercentReturn *string
}

// GroupViewMemberRow is one ranked member on the in-group board.
type GroupViewMemberRow struct {
	Rank          int
	UserID        string
	DisplayName   string
	PercentReturn *string
	DollarPnL     string
}

// GroupViewResult is GET /v1/groups/{id}/view.
type GroupViewResult struct {
	ID              string
	Name            string
	TreasuryAddress string
	PotTotalUsd     string
	Pot             []GroupViewPotRow
	You             GroupViewMemberSlice
	Members         []GroupViewMemberRow
}

// GetGroupView returns pot, viewer slice, and member board for one club.
func (h *HomeService) GetGroupView(ctx context.Context, accessToken, groupID string) (GroupViewResult, error) {
	if strings.TrimSpace(groupID) == "" {
		return GroupViewResult{}, fmt.Errorf("group id is required")
	}

	identity, err := h.privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			return GroupViewResult{}, privy.ErrInvalidToken
		}
		return GroupViewResult{}, fmt.Errorf("verify session: %w", err)
	}

	user, found, err := h.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		return GroupViewResult{}, err
	}
	if !found {
		return GroupViewResult{}, ErrUserNotFound
	}

	member, err := h.store.IsGroupMember(ctx, groupID, user.ID)
	if err != nil {
		return GroupViewResult{}, err
	}
	if !member {
		return GroupViewResult{}, ErrGroupNotFound
	}

	if err := h.creditUncreditedForGroups(ctx, []string{groupID}); err != nil {
		return GroupViewResult{}, err
	}

	group, groupFound, err := h.store.GetGroupByID(ctx, groupID)
	if err != nil {
		return GroupViewResult{}, err
	}
	if !groupFound {
		return GroupViewResult{}, ErrGroupNotFound
	}

	treasury, treasuryFound, err := h.store.GetTreasuryByGroupID(ctx, groupID)
	if err != nil {
		return GroupViewResult{}, err
	}
	if !treasuryFound {
		return GroupViewResult{}, ErrGroupNotFound
	}

	netUsdcIn, err := h.groupNetUsdcIn(ctx, groupID)
	if err != nil {
		return GroupViewResult{}, err
	}

	treasuryUSDC, err := h.groupTreasuryUSDC(ctx, groupID, netUsdcIn)
	if err != nil {
		return GroupViewResult{}, err
	}

	potView, err := computeGroupPotView(ctx, h.store, h.pyth, h.symbols, groupID, treasury.SolanaAddress, treasuryUSDC)
	if err != nil {
		return GroupViewResult{}, err
	}
	potNavMicros := potView.PotNavMicros
	potRows := potView.Rows

	totalSharesMicro, err := h.store.SumShareUnitsByGroup(ctx, groupID)
	if err != nil {
		return GroupViewResult{}, err
	}

	totalShares, err := domain.ShareUnitsMicrosToDomain(totalSharesMicro)
	if err != nil {
		return GroupViewResult{}, err
	}

	you, err := h.buildGroupViewYouSlice(ctx, user.ID, groupID, totalShares, domain.USDCMicros(potNavMicros))
	if err != nil {
		return GroupViewResult{}, err
	}

	members, err := h.buildGroupViewMemberRows(ctx, groupID, potNavMicros, totalSharesMicro)
	if err != nil {
		return GroupViewResult{}, err
	}

	return GroupViewResult{
		ID:              group.ID,
		Name:            group.Name,
		TreasuryAddress: treasury.SolanaAddress,
		PotTotalUsd:     formatMicrosAsUsdDecimal(potNavMicros),
		Pot:             potRows,
		You:             you,
		Members:         members,
	}, nil
}

func (h *HomeService) buildGroupViewYouSlice(
	ctx context.Context,
	userID, groupID string,
	totalShares domain.ShareUnits,
	potNav domain.USDCMicros,
) (GroupViewMemberSlice, error) {
	positionRow, hasPosition, err := h.store.GetPosition(ctx, userID, groupID)
	if err != nil {
		return GroupViewMemberSlice{}, err
	}

	shareUnitsMicro := int64(0)
	deposited := int64(0)
	withdrawn := int64(0)
	if hasPosition {
		shareUnitsMicro = positionRow.ShareUnits
		deposited = positionRow.AmountDeposited
		withdrawn = positionRow.AmountWithdrawn
	}

	memberShares, err := domain.ShareUnitsMicrosToDomain(shareUnitsMicro)
	if err != nil {
		return GroupViewMemberSlice{}, err
	}

	equityMicros := int64(0)
	if !totalShares.IsZero() && !memberShares.IsZero() {
		equity, err := domain.MemberEquity(memberShares, totalShares, potNav)
		if err != nil {
			return GroupViewMemberSlice{}, err
		}
		equityMicros = int64(equity)
	}

	netIn := deposited - withdrawn
	dollarPnL := formatSignedDollarPnL(equityMicros - netIn)

	var percentReturn *string
	if netIn > 0 {
		pnl, err := domain.ComputeMemberPnL(domain.MemberPosition{
			UserID:          userID,
			ShareUnits:      memberShares,
			AmountDeposited: domain.USDCMicros(deposited),
			AmountWithdrawn: domain.USDCMicros(withdrawn),
		}, totalShares, potNav)
		if err != nil {
			return GroupViewMemberSlice{}, err
		}
		if pnl.PercentReturn != nil {
			percentReturn = formatPercentReturnDecimal(*pnl.PercentReturn)
		}
	}

	return GroupViewMemberSlice{
		ShareUnits:    strconv.FormatInt(shareUnitsMicro, 10),
		EquityUsd:     formatMicrosAsUsdDecimal(equityMicros),
		SlicePercent:  formatShareFractionDecimal(memberShares, totalShares),
		DollarPnL:     dollarPnL,
		PercentReturn: percentReturn,
	}, nil
}

func (h *HomeService) buildGroupViewMemberRows(
	ctx context.Context,
	groupID string,
	potNavMicros int64,
	totalSharesMicro int64,
) ([]GroupViewMemberRow, error) {
	memberIDs, err := h.store.ListGroupMemberIDs(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if len(memberIDs) == 0 {
		return []GroupViewMemberRow{}, nil
	}

	positions, err := h.store.ListPositionsByGroup(ctx, groupID)
	if err != nil {
		return nil, err
	}
	positionByUser := make(map[string]postgres.PositionRow, len(positions))
	for _, position := range positions {
		positionByUser[position.UserID] = position
	}

	totalShares, err := domain.ShareUnitsMicrosToDomain(totalSharesMicro)
	if err != nil {
		return nil, err
	}
	if totalShares.IsZero() {
		totalSharesMicro, err = h.store.SumShareUnitsByGroup(ctx, groupID)
		if err != nil {
			return nil, err
		}
		totalShares, err = domain.ShareUnitsMicrosToDomain(totalSharesMicro)
		if err != nil {
			return nil, err
		}
	}

	members := make([]domain.MemberPosition, 0, len(memberIDs))
	for _, userID := range memberIDs {
		position := positionByUser[userID]
		shareUnits, err := domain.ShareUnitsMicrosToDomain(position.ShareUnits)
		if err != nil {
			return nil, err
		}
		members = append(members, domain.MemberPosition{
			UserID:          userID,
			ShareUnits:      shareUnits,
			AmountDeposited: domain.USDCMicros(position.AmountDeposited),
			AmountWithdrawn: domain.USDCMicros(position.AmountWithdrawn),
		})
	}

	board, err := BuildInGroupViewMemberBoard(members, totalShares, domain.PotNAV{TotalUsdc: domain.USDCMicros(potNavMicros)})
	if err != nil {
		return nil, err
	}

	userIDs := make([]string, 0, len(board))
	for _, row := range board {
		userIDs = append(userIDs, row.UserID)
	}
	displayNames, err := h.store.ListUserDisplayNamesByIDs(ctx, userIDs)
	if err != nil {
		return nil, err
	}

	rows := make([]GroupViewMemberRow, 0, len(board))
	for rank, row := range board {
		displayName := displayNames[row.UserID]
		if displayName == "" {
			displayName = "Member"
		}
		dollarPnL := formatSignedDollarPnL(int64(row.Equity - row.NetUsdcIn))
		var percentReturn *string
		if row.PercentReturn != nil {
			percentReturn = formatPercentReturnDecimal(*row.PercentReturn)
		}
		rows = append(rows, GroupViewMemberRow{
			Rank:          rank + 1,
			UserID:        row.UserID,
			DisplayName:   displayName,
			PercentReturn: percentReturn,
			DollarPnL:     dollarPnL,
		})
	}
	return rows, nil
}

func formatMicrosAsUsdDecimal(micros int64) string {
	return fmt.Sprintf("%.2f", float64(micros)/1_000_000.0)
}

func formatShareFractionDecimal(memberShares, totalShares domain.ShareUnits) string {
	if memberShares.IsZero() || totalShares.IsZero() {
		return "0"
	}
	member, ok := new(big.Rat).SetString(string(memberShares))
	if !ok {
		return "0"
	}
	total, ok := new(big.Rat).SetString(string(totalShares))
	if !ok || total.Sign() == 0 {
		return "0"
	}
	fraction := new(big.Rat).Quo(member, total)
	return strings.TrimRight(strings.TrimRight(fraction.FloatString(6), "0"), ".")
}


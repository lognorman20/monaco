package app

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/monaco/monaco/packages/domain"
)

// GroupViewPotRow is one line in the group pot section.
type GroupViewPotRow struct {
	Symbol      string
	Units       string
	MarkUsd     string
	ValueUsd    string
	DollarPnL   string
	AfterHours          *bool
	TokenAmount         string
	UiAmountMultiplier  string
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
	Rank            int
	UserID          string
	DisplayName     string
	ProfilePhotoURL string
	PercentReturn   *string
	DollarPnL       string
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

	// Members read their club; any authed user may spectate a faker scale club (#153).
	viewerID, err := authorizeGroupReader(ctx, h.store, h.privy, accessToken, groupID)
	if err != nil {
		return GroupViewResult{}, err
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

	totalSharesMicro := potView.ShareBaseMicros

	totalShares, err := domain.ShareUnitsMicrosToDomain(totalSharesMicro)
	if err != nil {
		return GroupViewResult{}, err
	}

	you, err := h.buildGroupViewYouSlice(ctx, viewerID, groupID, totalShares, domain.USDCMicros(potNavMicros))
	if err != nil {
		return GroupViewResult{}, err
	}

	members, err := h.buildGroupViewMemberRows(ctx, groupID, potNavMicros, totalSharesMicro)
	if err != nil {
		return GroupViewResult{}, err
	}

	treasuryAddress := treasury.SolanaAddress
	if group.IsFaker {
		// Dummy treasury (#153): never surface an address anyone could send real USDC to.
		treasuryAddress = ""
	}

	return GroupViewResult{
		ID:              group.ID,
		Name:            group.Name,
		TreasuryAddress: treasuryAddress,
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

	members, totalShares, boardPot, err := h.memberBoardInputs(ctx, groupID, memberIDs, potNavMicros, totalSharesMicro)
	if err != nil {
		return nil, err
	}

	board, err := BuildInGroupViewMemberBoard(members, totalShares, domain.PotNAV{TotalUsdc: domain.USDCMicros(boardPot)})
	if err != nil {
		return nil, err
	}

	userIDs := make([]string, 0, len(board))
	for _, row := range board {
		userIDs = append(userIDs, row.UserID)
	}
	profiles, err := h.store.ListUserProfilesByIDs(ctx, userIDs)
	if err != nil {
		return nil, err
	}

	rows := make([]GroupViewMemberRow, 0, len(board))
	for rank, row := range board {
		displayName, profilePhotoURL := boardIdentity(profiles, row.UserID)
		dollarPnL := formatSignedDollarPnL(int64(row.Equity - row.NetUsdcIn))
		var percentReturn *string
		if row.PercentReturn != nil {
			percentReturn = formatPercentReturnDecimal(*row.PercentReturn)
		}
		rows = append(rows, GroupViewMemberRow{
			Rank:            rank + 1,
			UserID:          row.UserID,
			DisplayName:     displayName,
			ProfilePhotoURL: profilePhotoURL,
			PercentReturn:   percentReturn,
			DollarPnL:       dollarPnL,
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


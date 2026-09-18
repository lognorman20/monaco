package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/packages/domain"
)

// HomeService builds app-home board projections.
type HomeService struct {
	store    *postgres.Store
	privy    privy.Client
	pyth     pyth.Client
	deposits *DepositService
	symbols  *SymbolResolver
}

// NewHomeService wires home dependencies.
func NewHomeService(store *postgres.Store, privyClient privy.Client, pythClient pyth.Client, deposits *DepositService, symbols *SymbolResolver) *HomeService {
	return &HomeService{
		store:    store,
		privy:    privyClient,
		pyth:     pythClient,
		deposits: deposits,
		symbols:  symbols,
	}
}

// HomeGroupRow is one ranked group on the app-home group board.
type HomeGroupRow struct {
	GroupID       string
	Name          string
	PotValueUsd   string
	PercentReturn *string
	DollarPnL     string
	IsJoined      bool
}

// HomePeopleRow is one ranked person on the app-home people board.
type HomePeopleRow struct {
	UserID        string
	DisplayName   string
	PercentReturn *string
	DollarPnL     string
}

// HomeResult is the authenticated app-home projection for GET /v1/home.
type HomeResult struct {
	Groups []HomeGroupRow
	People []HomePeopleRow
}

// GetHome returns all groups on the group board plus a people board from the viewer's clubs.
func (h *HomeService) GetHome(ctx context.Context, accessToken string) (HomeResult, error) {
	logHomeGetStart()

	identity, err := h.privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			slog.Warn("home get rejected", "reason", "invalid token")
			return HomeResult{}, privy.ErrInvalidToken
		}
		logHomeBranchError("home get verify session failed", err)
		return HomeResult{}, fmt.Errorf("verify session: %w", err)
	}

	user, found, err := h.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		logHomeBranchError("home get lookup user failed", err)
		return HomeResult{}, err
	}
	if !found {
		slog.Warn("home get rejected", "reason", "user not found")
		return HomeResult{}, ErrUserNotFound
	}

	joinedGroupIDs, err := h.store.ListUserGroupIDs(ctx, user.ID)
	if err != nil {
		return HomeResult{}, err
	}
	joinedGroups := make(map[string]struct{}, len(joinedGroupIDs))
	for _, groupID := range joinedGroupIDs {
		joinedGroups[groupID] = struct{}{}
	}

	if err := h.creditUncreditedForGroups(ctx, joinedGroupIDs); err != nil {
		return HomeResult{}, err
	}

	allGroupIDs, err := h.store.ListGroupIDs(ctx)
	if err != nil {
		return HomeResult{}, err
	}

	groupInputs := make([]domain.GroupBoardInput, 0, len(allGroupIDs))
	memberPnLByUser := make(map[string][]domain.MemberPnL)

	for _, groupID := range allGroupIDs {
		group, groupFound, err := h.store.GetGroupByID(ctx, groupID)
		if err != nil {
			return HomeResult{}, err
		}
		if !groupFound {
			continue
		}

		netUsdcIn, err := h.groupNetUsdcIn(ctx, groupID)
		if err != nil {
			return HomeResult{}, err
		}

		potNav, totalSharesMicro, err := h.groupPotNavAndShares(ctx, groupID, netUsdcIn)
		if err != nil {
			if _, isJoined := joinedGroups[group.ID]; isJoined {
				return HomeResult{}, err
			}
			slog.Warn("home get group pot failed; using net usdc in for discovery row", "group_id", groupID, "err", err)
			potNav = netUsdcIn
			totalSharesMicro = 0
		}

		groupInputs = append(groupInputs, domain.GroupBoardInput{
			GroupID:   group.ID,
			GroupName: group.Name,
			PotNav:    domain.USDCMicros(potNav),
			NetUsdcIn: domain.USDCMicros(netUsdcIn),
		})

		if _, isJoined := joinedGroups[group.ID]; !isJoined {
			continue
		}
		if err := h.collectGroupMemberPnL(ctx, groupID, potNav, totalSharesMicro, memberPnLByUser); err != nil {
			return HomeResult{}, err
		}
	}

	groupBoard := BuildAppGroupBoard(groupInputs)
	peopleInputs := make([]domain.PersonBoardInput, 0, len(memberPnLByUser))
	for _, entries := range memberPnLByUser {
		peopleInputs = append(peopleInputs, AggregateCrossGroupPerson(entries))
	}
	peopleBoard := BuildAppPeopleBoard(peopleInputs)

	displayNames, err := h.displayNamesForUsers(ctx, peopleBoard)
	if err != nil {
		return HomeResult{}, err
	}

	onGroupBoard := make(map[string]struct{}, len(groupBoard))
	result := HomeResult{
		Groups: make([]HomeGroupRow, 0, len(groupInputs)),
		People: make([]HomePeopleRow, 0, len(peopleBoard)),
	}
	for _, row := range groupBoard {
		onGroupBoard[row.GroupID] = struct{}{}
		_, isJoined := joinedGroups[row.GroupID]
		result.Groups = append(result.Groups, HomeGroupRow{
			GroupID:       row.GroupID,
			Name:          row.GroupName,
			PotValueUsd:   formatMicrosAsUsdDecimal(int64(row.PotNav)),
			PercentReturn: formatPercentReturnDecimal(row.PercentReturn),
			DollarPnL:     formatSignedDollarPnL(int64(row.DollarPnL)),
			IsJoined:      isJoined,
		})
	}
	for _, input := range groupInputs {
		if _, onBoard := onGroupBoard[input.GroupID]; onBoard {
			continue
		}
		_, isJoined := joinedGroups[input.GroupID]
		result.Groups = append(result.Groups, HomeGroupRow{
			GroupID:       input.GroupID,
			Name:          input.GroupName,
			PotValueUsd:   formatMicrosAsUsdDecimal(int64(input.PotNav)),
			PercentReturn: nil,
			DollarPnL:     formatSignedDollarPnL(int64(input.PotNav - input.NetUsdcIn)),
			IsJoined:      isJoined,
		})
	}
	for _, row := range peopleBoard {
		displayName := displayNames[row.UserID]
		if displayName == "" {
			displayName = "Member"
		}
		var percentReturn *string
		if row.PercentReturn != nil {
			percentReturn = formatPercentReturnDecimal(*row.PercentReturn)
		}
		result.People = append(result.People, HomePeopleRow{
			UserID:        row.UserID,
			DisplayName:   displayName,
			PercentReturn: percentReturn,
			DollarPnL:     formatSignedDollarPnL(int64(row.DollarPnL)),
		})
	}
	logHomeGetSuccess(user.ID, len(result.Groups), len(result.People))
	return result, nil
}

func (h *HomeService) groupNetUsdcIn(ctx context.Context, groupID string) (int64, error) {
	positions, err := h.store.ListPositionsByGroup(ctx, groupID)
	if err != nil {
		return 0, err
	}
	var net int64
	for _, position := range positions {
		net += position.AmountDeposited - position.AmountWithdrawn
	}
	return net, nil
}

// GroupTreasuryTotalMicros returns marked pot NAV for a group after reconciling
// uncredited on-chain USDC, matching GET /v1/groups/{id}/view pot total.
func (h *HomeService) GroupTreasuryTotalMicros(ctx context.Context, groupID string) (int64, error) {
	if err := h.creditUncreditedForGroups(ctx, []string{groupID}); err != nil {
		return 0, err
	}
	netUsdcIn, err := h.groupNetUsdcIn(ctx, groupID)
	if err != nil {
		return 0, err
	}
	potNav, _, err := h.groupPotNavAndShares(ctx, groupID, netUsdcIn)
	if err != nil {
		return 0, err
	}
	return potNav, nil
}

func (h *HomeService) groupPotNavAndShares(ctx context.Context, groupID string, netUsdcIn int64) (int64, int64, error) {
	treasuryUSDC, err := h.groupTreasuryUSDC(ctx, groupID, netUsdcIn)
	if err != nil {
		return 0, 0, err
	}

	treasuryAddress := ""
	treasury, found, err := h.store.GetTreasuryByGroupID(ctx, groupID)
	if err != nil {
		return 0, 0, err
	}
	if found {
		treasuryAddress = treasury.SolanaAddress
	}

	potView, err := computeGroupPotView(ctx, h.store, h.pyth, h.symbols, groupID, treasuryAddress, treasuryUSDC)
	if err != nil {
		return 0, 0, err
	}

	totalSharesMicro, err := h.store.SumShareUnitsByGroup(ctx, groupID)
	if err != nil {
		return 0, 0, err
	}
	return potView.PotNavMicros, totalSharesMicro, nil
}

func (h *HomeService) groupTreasuryUSDC(ctx context.Context, groupID string, netUsdcIn int64) (int64, error) {
	treasury, found, err := h.store.GetTreasuryByGroupID(ctx, groupID)
	if err != nil {
		return 0, err
	}
	if found {
		balance, err := h.privy.TreasuryUSDCBalance(ctx, treasury.SolanaAddress)
		if err != nil {
			return 0, fmt.Errorf("treasury usdc balance: %w", err)
		}
		return balance, nil
	}
	return netUsdcIn, nil
}

func (h *HomeService) creditUncreditedForGroups(ctx context.Context, groupIDs []string) error {
	if h.deposits == nil {
		return nil
	}
	for _, groupID := range groupIDs {
		if _, err := h.deposits.CreditUncreditedTreasuryUSDC(ctx, groupID); err != nil {
			return fmt.Errorf("credit uncredited treasury usdc for group %s: %w", groupID, err)
		}
	}
	return nil
}

func (h *HomeService) collectGroupMemberPnL(
	ctx context.Context,
	groupID string,
	potNav int64,
	totalSharesMicro int64,
	memberPnLByUser map[string][]domain.MemberPnL,
) error {
	memberIDs, err := h.store.ListGroupMemberIDs(ctx, groupID)
	if err != nil {
		return err
	}
	if len(memberIDs) == 0 {
		return nil
	}

	positions, err := h.store.ListPositionsByGroup(ctx, groupID)
	if err != nil {
		return err
	}
	positionByUser := make(map[string]postgres.PositionRow, len(positions))
	for _, position := range positions {
		positionByUser[position.UserID] = position
	}

	totalShares, err := domain.ShareUnitsMicrosToDomain(totalSharesMicro)
	if err != nil {
		return err
	}
	if totalShares.IsZero() {
		totalSharesMicro, err = h.store.SumShareUnitsByGroup(ctx, groupID)
		if err != nil {
			return err
		}
		totalShares, err = domain.ShareUnitsMicrosToDomain(totalSharesMicro)
		if err != nil {
			return err
		}
	}

	members := make([]domain.MemberPosition, 0, len(memberIDs))
	for _, userID := range memberIDs {
		position := positionByUser[userID]
		shareUnits, err := domain.ShareUnitsMicrosToDomain(position.ShareUnits)
		if err != nil {
			return err
		}
		members = append(members, domain.MemberPosition{
			UserID:          userID,
			ShareUnits:      shareUnits,
			AmountDeposited: domain.USDCMicros(position.AmountDeposited),
			AmountWithdrawn: domain.USDCMicros(position.AmountWithdrawn),
		})
	}

	board, err := BuildInGroupViewMemberBoard(members, totalShares, domain.PotNAV{TotalUsdc: domain.USDCMicros(potNav)})
	if err != nil {
		return err
	}
	for _, row := range board {
		memberPnLByUser[row.UserID] = append(memberPnLByUser[row.UserID], row)
	}
	return nil
}

// GetUserSharedGroups returns clubs shared between the viewer and target user.
func (h *HomeService) GetUserSharedGroups(ctx context.Context, accessToken, targetUserID string) ([]HomeGroupRow, error) {
	if strings.TrimSpace(targetUserID) == "" {
		return nil, fmt.Errorf("user id is required")
	}

	identity, err := h.privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			return nil, privy.ErrInvalidToken
		}
		return nil, fmt.Errorf("verify session: %w", err)
	}

	viewer, found, err := h.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrUserNotFound
	}

	sharedGroupIDs, err := h.store.ListSharedGroupIDsBetweenUsers(ctx, viewer.ID, targetUserID)
	if err != nil {
		return nil, err
	}
	if len(sharedGroupIDs) == 0 {
		return []HomeGroupRow{}, nil
	}

	rows := make([]HomeGroupRow, 0, len(sharedGroupIDs))
	for _, groupID := range sharedGroupIDs {
		group, groupFound, err := h.store.GetGroupByID(ctx, groupID)
		if err != nil {
			return nil, err
		}
		if !groupFound {
			continue
		}

		netUsdcIn, err := h.groupNetUsdcIn(ctx, groupID)
		if err != nil {
			return nil, err
		}

		potNav, _, err := h.groupPotNavAndShares(ctx, groupID, netUsdcIn)
		if err != nil {
			return nil, err
		}

		var percentReturn *string
		if netUsdcIn > 0 {
			if pct := domain.PercentReturn(domain.USDCMicros(potNav), domain.USDCMicros(netUsdcIn)); pct != nil {
				percentReturn = formatPercentReturnDecimal(*pct)
			}
		}

		rows = append(rows, HomeGroupRow{
			GroupID:       group.ID,
			Name:          group.Name,
			PotValueUsd:   formatMicrosAsUsdDecimal(potNav),
			PercentReturn: percentReturn,
			DollarPnL:     formatSignedDollarPnL(potNav - netUsdcIn),
			IsJoined:      true,
		})
	}
	return rows, nil
}

func (h *HomeService) displayNamesForUsers(ctx context.Context, people []domain.PersonBoardRow) (map[string]string, error) {
	if len(people) == 0 {
		return map[string]string{}, nil
	}
	userIDs := make([]string, 0, len(people))
	for _, row := range people {
		userIDs = append(userIDs, row.UserID)
	}
	return h.store.ListUserDisplayNamesByIDs(ctx, userIDs)
}

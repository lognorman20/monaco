package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/packages/domain"
)

// HomeService builds app-home board projections.
type HomeService struct {
	store    *postgres.Store
	privy    privy.Client
	deposits *DepositService
}

// NewHomeService wires home dependencies.
func NewHomeService(store *postgres.Store, privyClient privy.Client, deposits *DepositService) *HomeService {
	return &HomeService{
		store:    store,
		privy:    privyClient,
		deposits: deposits,
	}
}

// HomeGroupRow is one ranked group on the app-home group board.
type HomeGroupRow struct {
	GroupID       string
	Name          string
	PotValueUsd   string
	PercentReturn *string
	DollarPnL     string
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

// GetHome returns group and people boards for the authenticated viewer's clubs.
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

	groupIDs, err := h.store.ListUserGroupIDs(ctx, user.ID)
	if err != nil {
		return HomeResult{}, err
	}

	groupInputs := make([]domain.GroupBoardInput, 0, len(groupIDs))
	memberPnLByUser := make(map[string][]domain.MemberPnL)

	for _, groupID := range groupIDs {
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
			return HomeResult{}, err
		}

		groupInputs = append(groupInputs, domain.GroupBoardInput{
			GroupID:   group.ID,
			GroupName: group.Name,
			PotNav:    domain.USDCMicros(potNav),
			NetUsdcIn: domain.USDCMicros(netUsdcIn),
		})

		if err := h.collectMemberPnL(ctx, groupID, potNav, totalSharesMicro, memberPnLByUser); err != nil {
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
		result.Groups = append(result.Groups, HomeGroupRow{
			GroupID:       row.GroupID,
			Name:          row.GroupName,
			PotValueUsd:   formatMicrosAsUsdDecimal(int64(row.PotNav)),
			PercentReturn: formatPercentReturnDecimal(row.PercentReturn),
			DollarPnL:     formatSignedDollarPnL(int64(row.DollarPnL)),
		})
	}
	for _, input := range groupInputs {
		if _, onBoard := onGroupBoard[input.GroupID]; onBoard {
			continue
		}
		result.Groups = append(result.Groups, HomeGroupRow{
			GroupID:       input.GroupID,
			Name:          input.GroupName,
			PotValueUsd:   formatMicrosAsUsdDecimal(int64(input.PotNav)),
			PercentReturn: nil,
			DollarPnL:     formatSignedDollarPnL(int64(input.PotNav - input.NetUsdcIn)),
		})
	}
	for _, row := range peopleBoard {
		displayName := displayNames[row.UserID]
		if displayName == "" {
			displayName = "Member"
		}
		result.People = append(result.People, HomePeopleRow{
			UserID:        row.UserID,
			DisplayName:   displayName,
			PercentReturn: formatPercentReturnDecimal(row.PercentReturn),
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

func (h *HomeService) groupPotNavAndShares(ctx context.Context, groupID string, netUsdcIn int64) (int64, int64, error) {
	treasuryUSDC, err := h.groupTreasuryUSDC(ctx, groupID, netUsdcIn)
	if err != nil {
		return 0, 0, err
	}

	vals, err := h.store.ComputeNavSnapshotValues(ctx, groupID, treasuryUSDC)
	if err != nil {
		return treasuryUSDC, 0, nil
	}
	return vals.PotNavMicros, vals.TotalShares, nil
}

func (h *HomeService) groupTreasuryUSDC(ctx context.Context, groupID string, netUsdcIn int64) (int64, error) {
	treasury, found, err := h.store.GetTreasuryByGroupID(ctx, groupID)
	if err != nil {
		return 0, err
	}
	if found {
		balance, err := h.privy.TreasuryUSDCBalance(ctx, treasury.SolanaAddress)
		if err == nil && balance > 0 {
			return balance, nil
		}
	}
	return netUsdcIn, nil
}

func (h *HomeService) collectMemberPnL(
	ctx context.Context,
	groupID string,
	potNav int64,
	totalSharesMicro int64,
	memberPnLByUser map[string][]domain.MemberPnL,
) error {
	positions, err := h.store.ListPositionsByGroup(ctx, groupID)
	if err != nil {
		return err
	}
	if len(positions) == 0 {
		return nil
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

	members := make([]domain.MemberPosition, 0, len(positions))
	for _, position := range positions {
		shareUnits, err := domain.ShareUnitsMicrosToDomain(position.ShareUnits)
		if err != nil {
			return err
		}
		members = append(members, domain.MemberPosition{
			UserID:          position.UserID,
			ShareUnits:      shareUnits,
			AmountDeposited: domain.USDCMicros(position.AmountDeposited),
			AmountWithdrawn: domain.USDCMicros(position.AmountWithdrawn),
		})
	}

	board, err := BuildInGroupMemberBoard(members, totalShares, domain.PotNAV{TotalUsdc: domain.USDCMicros(potNav)})
	if err != nil {
		return err
	}
	for _, row := range board {
		memberPnLByUser[row.UserID] = append(memberPnLByUser[row.UserID], row)
	}
	return nil
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

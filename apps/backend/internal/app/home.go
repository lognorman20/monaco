package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/monaco/monaco/apps/backend/internal/auth"
	"github.com/monaco/monaco/apps/backend/internal/marks"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	soltreasury "github.com/monaco/monaco/apps/backend/internal/solana/treasury"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
	"github.com/monaco/monaco/packages/domain"
)

// HomeService builds app-home board projections.
type HomeService struct {
	store    *postgres.Store
	auth     auth.Verifier
	wallets  wallets.Client
	pyth     marks.Client
	deposits *DepositService
	symbols  *SymbolResolver
	solana   soltreasury.Client
}

// NewHomeService wires home dependencies.
func NewHomeService(store *postgres.Store, verifier auth.Verifier, walletClient wallets.Client, marksClient marks.Client, deposits *DepositService, symbols *SymbolResolver) *HomeService {
	return &HomeService{
		store:    store,
		auth:     verifier,
		wallets:  walletClient,
		pyth:     marksClient,
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
	UserID          string
	DisplayName     string
	ProfilePhotoURL string
	PercentReturn   *string
	DollarPnL       string
}

// HomeResult is the authenticated app-home projection for GET /v1/home.
type HomeResult struct {
	Groups []HomeGroupRow
	People []HomePeopleRow
}

// GetHome returns all groups on the group board plus a people board from the viewer's clubs.
func (h *HomeService) GetHome(ctx context.Context, accessToken string) (HomeResult, error) {
	ctx = HomeContextWithPotNavCache(ctx)
	logHomeGetStart()

	identity, err := h.auth.VerifySession(ctx, auth.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			slog.Warn("home get rejected", "reason", "invalid token")
			return HomeResult{}, auth.ErrUnauthorized
		}
		logHomeBranchError("home get verify session failed", err)
		return HomeResult{}, fmt.Errorf("verify session: %w", err)
	}

	user, found, err := h.store.GetUserByDynamicUserID(ctx, identity.DynamicUserID)
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

	// Faker scale clubs (#153) join the people board for every viewer as spectator clubs.
	// The viewer is never inserted into their group_members, so IsJoined stays false.
	peopleBoardGroups, err := h.peopleBoardGroupSet(ctx, joinedGroupIDs)
	if err != nil {
		return HomeResult{}, err
	}

	directory, err := h.store.ListGroupDirectory(ctx)
	if err != nil {
		return HomeResult{}, err
	}

	groupInputs := make([]domain.GroupBoardInput, 0, len(directory))
	memberPnLByUser := make(map[string][]domain.MemberPnL)

	for _, dir := range directory {
		groupID := dir.ID
		netUsdcIn := dir.NetUsdcInMicros
		_, isJoined := joinedGroups[groupID]

		var potNav, totalSharesMicro int64
		if homeDiscoveryNeedsMarkedPot(isJoined, netUsdcIn) {
			var err error
			potNav, totalSharesMicro, err = h.groupPotNavAndShares(ctx, groupID, netUsdcIn)
			if err != nil {
				if isJoined {
					return HomeResult{}, err
				}
				slog.Warn("home get group pot failed; using net usdc in for discovery row", "group_id", groupID, "err", err)
				potNav = netUsdcIn
				totalSharesMicro = 0
			}
		}

		groupInputs = append(groupInputs, domain.GroupBoardInput{
			GroupID:   groupID,
			GroupName: dir.Name,
			PotNav:    domain.USDCMicros(potNav),
			NetUsdcIn: domain.USDCMicros(netUsdcIn),
		})

		if _, onPeopleBoard := peopleBoardGroups[groupID]; !onPeopleBoard {
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

	profiles, err := h.profilesForUsers(ctx, peopleBoard)
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
		displayName, profilePhotoURL := boardIdentity(profiles, row.UserID)
		var percentReturn *string
		if row.PercentReturn != nil {
			percentReturn = formatPercentReturnDecimal(*row.PercentReturn)
		}
		result.People = append(result.People, HomePeopleRow{
			UserID:          row.UserID,
			DisplayName:     displayName,
			ProfilePhotoURL: profilePhotoURL,
			PercentReturn:   percentReturn,
			DollarPnL:       formatSignedDollarPnL(int64(row.DollarPnL)),
		})
	}
	logHomeGetSuccess(user.ID, len(result.Groups), len(result.People))
	return result, nil
}

func homeDiscoveryNeedsMarkedPot(isJoined bool, netUsdcIn int64) bool {
	return isJoined || domain.IncludeOnBoard(domain.USDCMicros(netUsdcIn))
}

// peopleBoardGroupSet returns joined groups plus faker scale clubs (#153).
func (h *HomeService) peopleBoardGroupSet(ctx context.Context, joinedGroupIDs []string) (map[string]struct{}, error) {
	ids, err := h.peopleBoardGroupIDs(ctx, joinedGroupIDs)
	if err != nil {
		return nil, err
	}
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return set, nil
}

// peopleBoardGroupIDs unions joined group ids with faker scale club ids, joined first.
func (h *HomeService) peopleBoardGroupIDs(ctx context.Context, joinedGroupIDs []string) ([]string, error) {
	fakerGroupIDs, err := h.store.ListFakerGroupIDs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(joinedGroupIDs)+len(fakerGroupIDs))
	seen := make(map[string]struct{}, len(joinedGroupIDs)+len(fakerGroupIDs))
	for _, ids := range [][]string{joinedGroupIDs, fakerGroupIDs} {
		for _, id := range ids {
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out, nil
}

// groupNetUsdcIn returns net deposits backed by the pot. Ghost (faker) positions in real
// groups are excluded so they never distort the real club's P&L.
func (h *HomeService) groupNetUsdcIn(ctx context.Context, groupID string) (int64, error) {
	positions, err := h.store.ListPositionsByGroup(ctx, groupID)
	if err != nil {
		return 0, err
	}
	return potNetUsdcIn(positions), nil
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
	cache := homePotNavCacheFrom(ctx)
	if cache == nil {
		return h.computeGroupPotNavAndShares(ctx, groupID, netUsdcIn)
	}

	cache.mu.Lock()
	if entry, ok := cache.entries[groupID]; ok {
		cache.mu.Unlock()
		return entry.potNav, entry.totalShares, entry.err
	}
	if wg, ok := cache.inflight[groupID]; ok {
		cache.mu.Unlock()
		wg.Wait()
		cache.mu.Lock()
		entry := cache.entries[groupID]
		cache.mu.Unlock()
		return entry.potNav, entry.totalShares, entry.err
	}
	wg := &sync.WaitGroup{}
	wg.Add(1)
	if cache.inflight == nil {
		cache.inflight = make(map[string]*sync.WaitGroup)
	}
	cache.inflight[groupID] = wg
	cache.mu.Unlock()

	potNav, totalShares, err := h.computeGroupPotNavAndShares(ctx, groupID, netUsdcIn)

	cache.mu.Lock()
	cache.entries[groupID] = homePotNavCacheEntry{
		potNav:      potNav,
		totalShares: totalShares,
		err:         err,
	}
	cache.computes++
	delete(cache.inflight, groupID)
	cache.mu.Unlock()
	wg.Done()
	return potNav, totalShares, err
}

func (h *HomeService) computeGroupPotNavAndShares(ctx context.Context, groupID string, netUsdcIn int64) (int64, int64, error) {
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
		treasuryAddress = treasury.Address
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

// SetSolanaTreasury makes pot treasury USDC include the group's Solana treasury SPL USDC, so
// USDC deployed to an agent and later returned there keeps the pot whole.
func (h *HomeService) SetSolanaTreasury(client soltreasury.Client) {
	h.solana = client
}

// PotTreasuryUSDCMicros is the treasury USDC pot NAV values for groupID, as the group view reads it.
func (h *HomeService) PotTreasuryUSDCMicros(ctx context.Context, groupID string) (int64, error) {
	netUsdcIn, err := h.groupNetUsdcIn(ctx, groupID)
	if err != nil {
		return 0, err
	}
	return h.groupTreasuryUSDC(ctx, groupID, netUsdcIn)
}

func (h *HomeService) groupTreasuryUSDC(ctx context.Context, groupID string, netUsdcIn int64) (int64, error) {
	cash, err := h.groupBaseTreasuryUSDC(ctx, groupID, netUsdcIn)
	if err != nil {
		return 0, err
	}
	return cash + h.groupSolanaTreasuryUSDC(ctx, groupID), nil
}

// groupSolanaTreasuryUSDC is SPL USDC held by the group's Solana treasury, 0 when the group has
// none. A failed read is logged and valued at 0 rather than failing the pot.
func (h *HomeService) groupSolanaTreasuryUSDC(ctx context.Context, groupID string) int64 {
	if h.solana == nil {
		return 0
	}
	treasury, found, err := h.store.GetGroupSolanaTreasury(ctx, groupID)
	if err != nil || !found {
		if err != nil {
			slog.Warn("solana treasury lookup failed", "group_id", groupID, "err", err)
		}
		return 0
	}
	balance, err := h.solana.TreasuryUSDCBalance(ctx, treasury.SolanaAddress)
	if err != nil {
		slog.Warn("solana treasury usdc balance failed", "group_id", groupID, "err", err)
		return 0
	}
	return balance
}

func (h *HomeService) groupBaseTreasuryUSDC(ctx context.Context, groupID string, netUsdcIn int64) (int64, error) {
	isFaker, err := h.store.IsFakerGroup(ctx, groupID)
	if err != nil {
		return 0, err
	}
	if isFaker {
		// Faker scale club (#153): dummy treasury, display NAV from the seeded ledger. No Dynamic wallet.
		return h.store.FakerLedgerUSDC(ctx, groupID)
	}

	treasury, found, err := h.store.GetTreasuryByGroupID(ctx, groupID)
	if err != nil {
		return 0, err
	}
	if found {
		balance, err := h.wallets.TreasuryUSDCBalance(ctx, treasury.Address)
		if err != nil {
			slog.Warn("treasury usdc balance failed; using net usdc in", "group_id", groupID, "err", err)
			return netUsdcIn, nil
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
			slog.Warn("credit uncredited treasury skipped", "group_id", groupID, "err", err)
			continue
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

	members, totalShares, boardPot, err := h.memberBoardInputs(ctx, groupID, memberIDs, potNav, totalSharesMicro)
	if err != nil {
		return err
	}

	board, err := BuildInGroupViewMemberBoard(members, totalShares, domain.PotNAV{TotalUsdc: domain.USDCMicros(boardPot)})
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

	identity, err := h.auth.VerifySession(ctx, auth.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			return nil, auth.ErrUnauthorized
		}
		return nil, fmt.Errorf("verify session: %w", err)
	}

	viewer, found, err := h.store.GetUserByDynamicUserID(ctx, identity.DynamicUserID)
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

func (h *HomeService) profilesForUsers(ctx context.Context, people []domain.PersonBoardRow) (map[string]postgres.UserProfileSummary, error) {
	if len(people) == 0 {
		return map[string]postgres.UserProfileSummary{}, nil
	}
	userIDs := make([]string, 0, len(people))
	for _, row := range people {
		userIDs = append(userIDs, row.UserID)
	}
	return h.store.ListUserProfilesByIDs(ctx, userIDs)
}

// boardIdentity returns the name and avatar URL shown for userID on a board.
// Users who never set a name show as "Member".
func boardIdentity(profiles map[string]postgres.UserProfileSummary, userID string) (displayName, profilePhotoURL string) {
	profile := profiles[userID]
	displayName = profile.DisplayName
	if displayName == "" {
		displayName = "Member"
	}
	return displayName, profile.ProfilePhotoURL
}

// memberBoardInputs loads positions for memberIDs and returns domain positions plus the
// (total shares, pot) basis to value them against. Ghost (faker) members in a real group are
// valued at the real NAV per share without diluting real members (#153).
func (h *HomeService) memberBoardInputs(
	ctx context.Context,
	groupID string,
	memberIDs []string,
	potNav int64,
	totalSharesMicro int64,
) ([]domain.MemberPosition, domain.ShareUnits, int64, error) {
	positions, err := h.store.ListPositionsByGroup(ctx, groupID)
	if err != nil {
		return nil, "", 0, err
	}
	positionByUser := make(map[string]postgres.PositionRow, len(positions))
	for _, position := range positions {
		positionByUser[position.UserID] = position
	}

	if totalSharesMicro == 0 {
		totalSharesMicro, err = h.store.SumShareUnitsByGroup(ctx, groupID)
		if err != nil {
			return nil, "", 0, err
		}
	}
	basisShares, basisPot := boardShareBasis(totalSharesMicro, potNav, positions)
	totalShares, err := domain.ShareUnitsMicrosToDomain(basisShares)
	if err != nil {
		return nil, "", 0, err
	}

	members := make([]domain.MemberPosition, 0, len(memberIDs))
	for _, userID := range memberIDs {
		position := positionByUser[userID]
		shareUnits, err := domain.ShareUnitsMicrosToDomain(position.ShareUnits)
		if err != nil {
			return nil, "", 0, err
		}
		members = append(members, domain.MemberPosition{
			UserID:          userID,
			ShareUnits:      shareUnits,
			AmountDeposited: domain.USDCMicros(position.AmountDeposited),
			AmountWithdrawn: domain.USDCMicros(position.AmountWithdrawn),
		})
	}
	return members, totalShares, basisPot, nil
}

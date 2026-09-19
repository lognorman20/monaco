package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/packages/domain"
)

const (
	defaultGroupsTabLimit = 25
	maxGroupsTabLimit     = 50
	defaultPnLHistoryDays = 90
	maxPnLHistoryDays     = 365
)

// GroupDiscoveryRow is one group returned from search.
type GroupDiscoveryRow struct {
	GroupID       string
	Name          string
	PotValueUsd   string
	PercentReturn *string
	DollarPnL     string
	IsJoined      bool
	JoinMode      string
}

// GroupLeaderboardRow is one ranked group on the platform leaderboard.
type GroupLeaderboardRow struct {
	Rank          int
	GroupID       string
	Name          string
	PotValueUsd   string
	PercentReturn *string
	DollarPnL     string
	IsJoined      bool
}

// GroupPnLHistoryPoint is one NAV snapshot in a P&L time series.
type GroupPnLHistoryPoint struct {
	At          time.Time
	PotValueUsd string
	DollarPnL   string
}

// GroupPnLHistory is NAV history for one group.
type GroupPnLHistory struct {
	GroupID string
	Name    string
	Points  []GroupPnLHistoryPoint
}

// SearchGroups finds cabals by name for the authenticated viewer.
func (h *HomeService) SearchGroups(ctx context.Context, accessToken, query string, limit, offset int) ([]GroupDiscoveryRow, error) {
	joinedGroups, err := h.viewerJoinedGroups(ctx, accessToken)
	if err != nil {
		return nil, err
	}

	query = strings.TrimSpace(query)
	if query == "" {
		return []GroupDiscoveryRow{}, nil
	}
	if limit <= 0 {
		limit = defaultGroupsTabLimit
	}
	if limit > maxGroupsTabLimit {
		limit = maxGroupsTabLimit
	}
	if offset < 0 {
		offset = 0
	}

	matches, err := h.store.SearchGroupsByName(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}

	rows := make([]GroupDiscoveryRow, 0, len(matches))
	for _, group := range matches {
		row, err := h.groupDiscoveryRow(ctx, group, joinedGroups)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// GetGroupLeaderboard returns platform-wide cabals ranked by percent return.
func (h *HomeService) GetGroupLeaderboard(ctx context.Context, accessToken string, limit, offset int) ([]GroupLeaderboardRow, error) {
	joinedGroups, err := h.viewerJoinedGroups(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = defaultGroupsTabLimit
	}
	if limit > maxGroupsTabLimit {
		limit = maxGroupsTabLimit
	}
	if offset < 0 {
		offset = 0
	}

	groupInputs, err := h.buildAllGroupBoardInputs(ctx, joinedGroups)
	if err != nil {
		return nil, err
	}
	board := BuildAppGroupBoard(groupInputs)
	if offset >= len(board) {
		return []GroupLeaderboardRow{}, nil
	}
	end := offset + limit
	if end > len(board) {
		end = len(board)
	}
	slice := board[offset:end]

	rows := make([]GroupLeaderboardRow, 0, len(slice))
	for i, row := range slice {
		_, isJoined := joinedGroups[row.GroupID]
		rows = append(rows, GroupLeaderboardRow{
			Rank:          offset + i + 1,
			GroupID:       row.GroupID,
			Name:          row.GroupName,
			PotValueUsd:   formatMicrosAsUsdDecimal(int64(row.PotNav)),
			PercentReturn: formatPercentReturnDecimal(row.PercentReturn),
			DollarPnL:     formatSignedDollarPnL(int64(row.DollarPnL)),
			IsJoined:      isJoined,
		})
	}
	return rows, nil
}

// GetGroupPnLHistory returns pot NAV snapshots for charting group P&L over time.
func (h *HomeService) GetGroupPnLHistory(ctx context.Context, accessToken, groupID string, days int) (GroupPnLHistory, error) {
	if strings.TrimSpace(groupID) == "" {
		return GroupPnLHistory{}, ErrGroupNotFound
	}
	if _, err := h.viewerJoinedGroups(ctx, accessToken); err != nil {
		return GroupPnLHistory{}, err
	}
	if days <= 0 {
		days = defaultPnLHistoryDays
	}
	if days > maxPnLHistoryDays {
		days = maxPnLHistoryDays
	}

	group, found, err := h.store.GetGroupByID(ctx, groupID)
	if err != nil {
		return GroupPnLHistory{}, err
	}
	if !found {
		return GroupPnLHistory{}, ErrGroupNotFound
	}

	since := time.Now().UTC().AddDate(0, 0, -days)
	snapshots, err := h.store.ListNavSnapshotsByGroupSince(ctx, groupID, since)
	if err != nil {
		return GroupPnLHistory{}, err
	}

	points := make([]GroupPnLHistoryPoint, 0, len(snapshots))
	var baselineMicros int64
	for i, snapshot := range snapshots {
		if i == 0 {
			baselineMicros = snapshot.PotNavMicros
		}
		points = append(points, GroupPnLHistoryPoint{
			At:          snapshot.CreatedAt.UTC(),
			PotValueUsd: formatMicrosAsUsdDecimal(snapshot.PotNavMicros),
			DollarPnL:   formatSignedDollarPnL(snapshot.PotNavMicros - baselineMicros),
		})
	}

	return GroupPnLHistory{
		GroupID: group.ID,
		Name:    group.Name,
		Points:  points,
	}, nil
}

func (h *HomeService) viewerJoinedGroups(ctx context.Context, accessToken string) (map[string]struct{}, error) {
	identity, err := h.privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			return nil, privy.ErrInvalidToken
		}
		return nil, fmt.Errorf("verify session: %w", err)
	}

	user, found, err := h.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrUserNotFound
	}

	joinedGroupIDs, err := h.store.ListUserGroupIDs(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	joinedGroups := make(map[string]struct{}, len(joinedGroupIDs))
	for _, groupID := range joinedGroupIDs {
		joinedGroups[groupID] = struct{}{}
	}
	return joinedGroups, nil
}

func (h *HomeService) buildAllGroupBoardInputs(ctx context.Context, joinedGroups map[string]struct{}) ([]domain.GroupBoardInput, error) {
	joinedGroupIDs := make([]string, 0, len(joinedGroups))
	for groupID := range joinedGroups {
		joinedGroupIDs = append(joinedGroupIDs, groupID)
	}
	if err := h.creditUncreditedForGroups(ctx, joinedGroupIDs); err != nil {
		return nil, err
	}

	allGroupIDs, err := h.store.ListGroupIDs(ctx)
	if err != nil {
		return nil, err
	}

	groupInputs := make([]domain.GroupBoardInput, 0, len(allGroupIDs))
	for _, groupID := range allGroupIDs {
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
			if _, isJoined := joinedGroups[group.ID]; isJoined {
				return nil, err
			}
			potNav = netUsdcIn
		}

		groupInputs = append(groupInputs, domain.GroupBoardInput{
			GroupID:   group.ID,
			GroupName: group.Name,
			PotNav:    domain.USDCMicros(potNav),
			NetUsdcIn: domain.USDCMicros(netUsdcIn),
		})
	}
	return groupInputs, nil
}

func (h *HomeService) groupDiscoveryRow(ctx context.Context, group postgres.Group, joinedGroups map[string]struct{}) (GroupDiscoveryRow, error) {
	_, isJoined := joinedGroups[group.ID]

	netUsdcIn, err := h.groupNetUsdcIn(ctx, group.ID)
	if err != nil {
		return GroupDiscoveryRow{}, err
	}

	potNav, _, err := h.groupPotNavAndShares(ctx, group.ID, netUsdcIn)
	if err != nil {
		if isJoined {
			return GroupDiscoveryRow{}, err
		}
		potNav = netUsdcIn
	}

	var percentReturn *string
	if netUsdcIn > 0 {
		if pct := domain.PercentReturn(domain.USDCMicros(potNav), domain.USDCMicros(netUsdcIn)); pct != nil {
			percentReturn = formatPercentReturnDecimal(*pct)
		}
	}

	joinMode := string(domain.JoinModeOpen)
	if rules, rulesFound, err := h.store.GetGroupRules(ctx, group.ID); err != nil {
		return GroupDiscoveryRow{}, err
	} else if rulesFound {
		joinMode = string(rules.JoinPolicy.Mode)
	}

	return GroupDiscoveryRow{
		GroupID:       group.ID,
		Name:          group.Name,
		PotValueUsd:   formatMicrosAsUsdDecimal(potNav),
		PercentReturn: percentReturn,
		DollarPnL:     formatSignedDollarPnL(potNav - netUsdcIn),
		IsJoined:      isJoined,
		JoinMode:      joinMode,
	}, nil
}

package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/auth"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/packages/domain"
)

// HomeLeaderboardRange selects the people-board time window.
type HomeLeaderboardRange string

const (
	HomeLeaderboardRange1H  HomeLeaderboardRange = "1H"
	HomeLeaderboardRange1D  HomeLeaderboardRange = "1D"
	HomeLeaderboardRange1W  HomeLeaderboardRange = "1W"
	HomeLeaderboardRange1M  HomeLeaderboardRange = "1M"
	HomeLeaderboardRangeALL HomeLeaderboardRange = "ALL"
)

// HomeMyGroupRow is one joined group with viewer position on the home dashboard.
type HomeMyGroupRow struct {
	GroupID       string
	Name          string
	EquityUsd     string
	SlicePercent  string
	DollarPnL     string
	PercentReturn *string
	// PictureURL is blank when the cabal has no picture.
	PictureURL string
}

// HomePnLSeriesPoint is one aggregate viewer P&L sample for charts.
type HomePnLSeriesPoint struct {
	TS        time.Time
	EquityUsd string
	DollarPnL string
}

// HomeLeaderboardSection is the ranged cross-group people board.
type HomeLeaderboardSection struct {
	Range  HomeLeaderboardRange
	People []HomePeopleRow
}

// HomeMissedProposalRow is an open proposal the viewer has not voted on.
type HomeMissedProposalRow struct {
	GroupID    string
	GroupName  string
	ProposalID string
	Symbol     string
	Status     string
	CreatedAt  time.Time
	ExpiresAt  time.Time
}

// HomeDashboardResult is GET /v1/home/dashboard.
type HomeDashboardResult struct {
	NetWorthUsd           string
	NetWorthDollarPnL     string
	NetWorthPercentReturn *string
	MyGroups              []HomeMyGroupRow
	PnlSeries1H           []HomePnLSeriesPoint
	Leaderboard           HomeLeaderboardSection
	MissedProposals       []HomeMissedProposalRow
}

type viewerGroupPosition struct {
	GroupID          string
	Name             string
	PictureURL       string
	ShareUnitsMicro  int64
	NetUsdcInMicro   int64
	EquityMicro      int64
	TotalSharesMicro int64
}

// GetHomeDashboard returns the authenticated home dashboard projection.
func (h *HomeService) GetHomeDashboard(ctx context.Context, accessToken string, leaderboardRange HomeLeaderboardRange) (HomeDashboardResult, error) {
	ctx = HomeContextWithPotNavCache(ctx)
	user, joinedGroupIDs, err := h.authenticateHomeUser(ctx, accessToken)
	if err != nil {
		return HomeDashboardResult{}, err
	}

	if err := h.creditUncreditedForGroups(ctx, joinedGroupIDs); err != nil {
		return HomeDashboardResult{}, err
	}

	positions, err := h.viewerGroupPositions(ctx, user.ID, joinedGroupIDs)
	if err != nil {
		return HomeDashboardResult{}, err
	}

	myGroups, netEquity, netDeposits := formatMyGroups(positions)
	netPnL := netEquity - netDeposits
	var netPercent *string
	if netDeposits > 0 {
		if pct := domain.PercentReturn(domain.USDCMicros(netEquity), domain.USDCMicros(netDeposits)); pct != nil {
			netPercent = formatPercentReturnDecimal(*pct)
		}
	}

	// People leaderboard spans joined clubs plus faker scale clubs (#153); MyGroups,
	// P&L series, and missed proposals stay limited to clubs the viewer actually joined.
	leaderboardGroupIDs, err := h.peopleBoardGroupIDs(ctx, joinedGroupIDs)
	if err != nil {
		return HomeDashboardResult{}, err
	}

	var (
		leaderboard HomeLeaderboardSection
		missed      []HomeMissedProposalRow
		lbErr       error
		missedErr   error
		tail        sync.WaitGroup
	)
	tail.Add(2)
	go func() {
		defer tail.Done()
		leaderboard, lbErr = h.buildRangedLeaderboard(ctx, leaderboardGroupIDs, leaderboardRange)
	}()
	go func() {
		defer tail.Done()
		missed, missedErr = h.listMissedProposals(ctx, user.ID, joinedGroupIDs)
	}()
	tail.Wait()
	if lbErr != nil {
		return HomeDashboardResult{}, lbErr
	}
	if missedErr != nil {
		return HomeDashboardResult{}, missedErr
	}

	return HomeDashboardResult{
		NetWorthUsd:           formatMicrosAsUsdDecimal(netEquity),
		NetWorthDollarPnL:     formatSignedDollarPnL(netPnL),
		NetWorthPercentReturn: netPercent,
		MyGroups:              myGroups,
		PnlSeries1H:           []HomePnLSeriesPoint{},
		Leaderboard:           leaderboard,
		MissedProposals:       missed,
	}, nil
}

// GetHomePnLSeries returns aggregate viewer P&L points for a time range.
func (h *HomeService) GetHomePnLSeries(ctx context.Context, accessToken string, seriesRange HomeLeaderboardRange) ([]HomePnLSeriesPoint, error) {
	ctx = HomeContextWithPotNavCache(ctx)
	user, joinedGroupIDs, err := h.authenticateHomeUser(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	if err := h.creditUncreditedForGroups(ctx, joinedGroupIDs); err != nil {
		return nil, err
	}
	positions, err := h.viewerGroupPositions(ctx, user.ID, joinedGroupIDs)
	if err != nil {
		return nil, err
	}
	since, err := leaderboardRangeStart(seriesRange, time.Now())
	if err != nil {
		return nil, err
	}
	return h.buildViewerPnLSeries(ctx, positions, since)
}

// GetHomeMissedProposals returns open proposals the viewer has not voted on.
func (h *HomeService) GetHomeMissedProposals(ctx context.Context, accessToken string) ([]HomeMissedProposalRow, error) {
	user, joinedGroupIDs, err := h.authenticateHomeUser(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	return h.listMissedProposals(ctx, user.ID, joinedGroupIDs)
}

func (h *HomeService) authenticateHomeUser(ctx context.Context, accessToken string) (postgres.User, []string, error) {
	identity, err := h.auth.VerifySession(ctx, auth.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			return postgres.User{}, nil, auth.ErrUnauthorized
		}
		return postgres.User{}, nil, fmt.Errorf("verify session: %w", err)
	}

	user, found, err := h.store.GetUserByDynamicUserID(ctx, identity.DynamicUserID)
	if err != nil {
		return postgres.User{}, nil, err
	}
	if !found {
		return postgres.User{}, nil, ErrUserNotFound
	}

	joinedGroupIDs, err := h.store.ListUserGroupIDs(ctx, user.ID)
	if err != nil {
		return postgres.User{}, nil, err
	}
	return user, joinedGroupIDs, nil
}

func (h *HomeService) viewerGroupPositions(ctx context.Context, userID string, joinedGroupIDs []string) ([]viewerGroupPosition, error) {
	if len(joinedGroupIDs) == 0 {
		return nil, nil
	}

	type slot struct {
		row viewerGroupPosition
		ok  bool
	}
	slots := make([]slot, len(joinedGroupIDs))
	errCh := make(chan error, len(joinedGroupIDs))
	var wg sync.WaitGroup
	for i, groupID := range joinedGroupIDs {
		wg.Add(1)
		go func(i int, groupID string) {
			defer wg.Done()
			row, ok, err := h.viewerGroupPosition(ctx, userID, groupID)
			if err != nil {
				errCh <- err
				return
			}
			if ok {
				slots[i] = slot{row: row, ok: true}
			}
		}(i, groupID)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			return nil, err
		}
	}

	rows := make([]viewerGroupPosition, 0, len(joinedGroupIDs))
	for _, slot := range slots {
		if slot.ok {
			rows = append(rows, slot.row)
		}
	}
	return rows, nil
}

func (h *HomeService) viewerGroupPosition(ctx context.Context, userID, groupID string) (viewerGroupPosition, bool, error) {
	group, groupFound, err := h.store.GetGroupByID(ctx, groupID)
	if err != nil {
		return viewerGroupPosition{}, false, err
	}
	if !groupFound {
		return viewerGroupPosition{}, false, nil
	}

	netUsdcIn, err := h.groupNetUsdcIn(ctx, groupID)
	if err != nil {
		return viewerGroupPosition{}, false, err
	}
	potNav, totalSharesMicro, err := h.groupPotNavAndShares(ctx, groupID, netUsdcIn)
	if err != nil {
		slog.Warn("home dashboard pot nav failed; using net usdc in", "group_id", groupID, "err", err)
		potNav = netUsdcIn
		totalSharesMicro = 0
	}

	positionRow, hasPosition, err := h.store.GetPosition(ctx, userID, groupID)
	if err != nil {
		return viewerGroupPosition{}, false, err
	}
	shareUnitsMicro := int64(0)
	deposited := int64(0)
	withdrawn := int64(0)
	if hasPosition {
		shareUnitsMicro = positionRow.ShareUnits
		deposited = positionRow.AmountDeposited
		withdrawn = positionRow.AmountWithdrawn
	}

	equityMicro, err := shareOfPotMicros(shareUnitsMicro, potNav, totalSharesMicro)
	if err != nil {
		return viewerGroupPosition{}, false, err
	}

	return viewerGroupPosition{
		GroupID:          groupID,
		Name:             group.Name,
		PictureURL:       nullStringValue(group.PictureURL),
		ShareUnitsMicro:  shareUnitsMicro,
		NetUsdcInMicro:   deposited - withdrawn,
		EquityMicro:      equityMicro,
		TotalSharesMicro: totalSharesMicro,
	}, true, nil
}

func formatMyGroups(positions []viewerGroupPosition) ([]HomeMyGroupRow, int64, int64) {
	myGroups := make([]HomeMyGroupRow, 0, len(positions))
	var netEquity int64
	var netDeposits int64
	for _, pos := range positions {
		netEquity += pos.EquityMicro
		netDeposits += pos.NetUsdcInMicro

		memberShares, _ := domain.ShareUnitsMicrosToDomain(pos.ShareUnitsMicro)
		totalShares, _ := domain.ShareUnitsMicrosToDomain(pos.TotalSharesMicro)

		var percentReturn *string
		if pos.NetUsdcInMicro > 0 {
			if pct := domain.PercentReturn(domain.USDCMicros(pos.EquityMicro), domain.USDCMicros(pos.NetUsdcInMicro)); pct != nil {
				percentReturn = formatPercentReturnDecimal(*pct)
			}
		}

		myGroups = append(myGroups, HomeMyGroupRow{
			GroupID:       pos.GroupID,
			Name:          pos.Name,
			EquityUsd:     formatMicrosAsUsdDecimal(pos.EquityMicro),
			SlicePercent:  formatShareFractionDecimal(memberShares, totalShares),
			DollarPnL:     formatSignedDollarPnL(pos.EquityMicro - pos.NetUsdcInMicro),
			PercentReturn: percentReturn,
			PictureURL:    pos.PictureURL,
		})
	}
	return myGroups, netEquity, netDeposits
}

func (h *HomeService) buildViewerPnLSeries(ctx context.Context, positions []viewerGroupPosition, since time.Time) ([]HomePnLSeriesPoint, error) {
	if len(positions) == 0 {
		return []HomePnLSeriesPoint{}, nil
	}

	groupIDs := make([]string, 0, len(positions))
	positionByGroup := make(map[string]viewerGroupPosition, len(positions))
	var netDeposits int64
	for _, pos := range positions {
		groupIDs = append(groupIDs, pos.GroupID)
		positionByGroup[pos.GroupID] = pos
		netDeposits += pos.NetUsdcInMicro
	}

	since = since.UTC()
	snapshots, err := h.store.ListNavSnapshotsForGroupsSince(ctx, groupIDs, since)
	if err != nil {
		return nil, err
	}

	snapshotsByGroup := make(map[string][]postgres.NavSnapshotRow)
	for _, snap := range snapshots {
		snapshotsByGroup[snap.GroupID] = append(snapshotsByGroup[snap.GroupID], snap)
	}

	hasHistory := len(snapshots) > 0
	hasBaselineBeforeWindow := false
	for _, groupID := range groupIDs {
		base, found, err := h.store.GetNavSnapshotAtOrBefore(ctx, groupID, since)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		hasHistory = true
		if base.CreatedAt.Before(since) {
			hasBaselineBeforeWindow = true
			snapshotsByGroup[groupID] = append([]postgres.NavSnapshotRow{base}, snapshotsByGroup[groupID]...)
		}
	}
	if !hasHistory {
		return []HomePnLSeriesPoint{}, nil
	}

	timestamps := make([]time.Time, 0, len(snapshots)+2)
	seen := make(map[int64]struct{})
	addTS := func(ts time.Time) {
		key := ts.UTC().Unix()
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		timestamps = append(timestamps, ts.UTC())
	}
	if hasBaselineBeforeWindow {
		addTS(since)
	}
	for _, snap := range snapshots {
		addTS(snap.CreatedAt)
	}
	now := time.Now().UTC()
	addTS(now)
	sort.Slice(timestamps, func(i, j int) bool { return timestamps[i].Before(timestamps[j]) })

	points := make([]HomePnLSeriesPoint, 0, len(timestamps))
	for _, ts := range timestamps {
		equity := int64(0)
		for groupID, pos := range positionByGroup {
			if ts.Equal(now) {
				equity += pos.EquityMicro
				continue
			}
			snap, ok := latestSnapshotAtOrBefore(snapshotsByGroup[groupID], ts)
			if !ok {
				continue
			}
			snapEquity, err := memberEquityAtSnapshot(pos.ShareUnitsMicro, snap)
			if err != nil {
				return nil, err
			}
			equity += snapEquity
		}
		points = append(points, HomePnLSeriesPoint{
			TS:        ts,
			EquityUsd: formatMicrosAsUsdDecimal(equity),
			DollarPnL: formatSignedDollarPnL(equity - netDeposits),
		})
	}

	return points, nil
}

// buildRangedLeaderboard ranks people by percent return over the window.
// Window math uses current share units against the NAV snapshot at or before
// window start — not a historical share ledger or daily rollup.
func (h *HomeService) buildRangedLeaderboard(ctx context.Context, boardGroupIDs []string, leaderboardRange HomeLeaderboardRange) (HomeLeaderboardSection, error) {
	if len(boardGroupIDs) == 0 {
		return HomeLeaderboardSection{Range: leaderboardRange, People: []HomePeopleRow{}}, nil
	}

	if leaderboardRange == "" || leaderboardRange == HomeLeaderboardRangeALL {
		people, err := h.buildLifetimePeopleBoard(ctx, boardGroupIDs)
		if err != nil {
			return HomeLeaderboardSection{}, err
		}
		return HomeLeaderboardSection{Range: HomeLeaderboardRangeALL, People: people}, nil
	}

	now := time.Now().UTC()
	since, err := leaderboardRangeStart(leaderboardRange, now)
	if err != nil {
		return HomeLeaderboardSection{}, err
	}

	type rangedPerson struct {
		userID       string
		startEquity  int64
		endEquity    int64
		endNetUsdcIn int64
	}

	ranged := make(map[string]*rangedPerson)
	for _, groupID := range boardGroupIDs {
		startSnap, found, err := h.store.GetNavSnapshotAtOrBefore(ctx, groupID, since)
		if err != nil {
			return HomeLeaderboardSection{}, err
		}
		if !found {
			continue
		}

		netUsdcIn, err := h.groupNetUsdcIn(ctx, groupID)
		if err != nil {
			return HomeLeaderboardSection{}, err
		}
		potNav, totalSharesMicro, err := h.groupPotNavAndShares(ctx, groupID, netUsdcIn)
		if err != nil {
			slog.Warn("home leaderboard pot nav failed; using net usdc in", "group_id", groupID, "err", err)
			potNav = netUsdcIn
			totalSharesMicro = 0
		}

		memberIDs, err := h.store.ListGroupMemberIDs(ctx, groupID)
		if err != nil {
			return HomeLeaderboardSection{}, err
		}
		positions, err := h.store.ListPositionsByGroup(ctx, groupID)
		if err != nil {
			return HomeLeaderboardSection{}, err
		}
		positionByUser := make(map[string]postgres.PositionRow, len(positions))
		for _, position := range positions {
			positionByUser[position.UserID] = position
		}
		basisShares, basisPot := boardShareBasis(totalSharesMicro, potNav, positions)

		for _, userID := range memberIDs {
			position := positionByUser[userID]
			netIn := position.AmountDeposited - position.AmountWithdrawn
			if !domain.IncludeOnBoard(domain.USDCMicros(netIn)) {
				continue
			}

			endEquity, err := shareOfPotMicros(position.ShareUnits, basisPot, basisShares)
			if err != nil {
				return HomeLeaderboardSection{}, err
			}
			startEquity, err := memberEquityAtSnapshot(position.ShareUnits, startSnap)
			if err != nil {
				return HomeLeaderboardSection{}, err
			}

			person, ok := ranged[userID]
			if !ok {
				person = &rangedPerson{userID: userID}
				ranged[userID] = person
			}
			person.startEquity += startEquity
			person.endEquity += endEquity
			person.endNetUsdcIn += netIn
		}
	}

	rows := make([]domain.PersonBoardRow, 0, len(ranged))
	for userID, person := range ranged {
		if person.startEquity <= 0 {
			continue
		}
		var pct *float64
		if ratio := domain.PercentReturn(domain.USDCMicros(person.endEquity), domain.USDCMicros(person.startEquity)); ratio != nil {
			pct = ratio
		}
		rows = append(rows, domain.PersonBoardRow{
			UserID:         userID,
			TotalEquity:    domain.USDCMicros(person.endEquity),
			TotalNetUsdcIn: domain.USDCMicros(person.endNetUsdcIn),
			PercentReturn:  pct,
			DollarPnL:      domain.USDCMicros(person.endEquity - person.startEquity),
		})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		iHas := rows[i].PercentReturn != nil
		jHas := rows[j].PercentReturn != nil
		if iHas != jHas {
			return iHas
		}
		if iHas {
			pi := *rows[i].PercentReturn
			pj := *rows[j].PercentReturn
			if pi != pj {
				return pi > pj
			}
		}
		return rows[i].UserID < rows[j].UserID
	})

	profiles, err := h.profilesForUsers(ctx, rows)
	if err != nil {
		return HomeLeaderboardSection{}, err
	}

	people := make([]HomePeopleRow, 0, len(rows))
	for _, row := range rows {
		displayName, profilePhotoURL := boardIdentity(profiles, row.UserID)
		var percentReturn *string
		if row.PercentReturn != nil {
			percentReturn = formatPercentReturnDecimal(*row.PercentReturn)
		}
		people = append(people, HomePeopleRow{
			UserID:          row.UserID,
			DisplayName:     displayName,
			ProfilePhotoURL: profilePhotoURL,
			PercentReturn:   percentReturn,
			DollarPnL:       formatSignedDollarPnL(int64(row.DollarPnL)),
		})
	}

	return HomeLeaderboardSection{Range: leaderboardRange, People: people}, nil
}

func (h *HomeService) buildLifetimePeopleBoard(ctx context.Context, boardGroupIDs []string) ([]HomePeopleRow, error) {
	memberPnLByUser, err := h.collectBoardGroupMemberPnL(ctx, boardGroupIDs)
	if err != nil {
		return nil, err
	}
	peopleInputs := make([]domain.PersonBoardInput, 0, len(memberPnLByUser))
	for _, entries := range memberPnLByUser {
		peopleInputs = append(peopleInputs, AggregateCrossGroupPerson(entries))
	}
	peopleBoard := BuildAppPeopleBoard(peopleInputs)
	profiles, err := h.profilesForUsers(ctx, peopleBoard)
	if err != nil {
		return nil, err
	}

	people := make([]HomePeopleRow, 0, len(peopleBoard))
	for _, row := range peopleBoard {
		displayName, profilePhotoURL := boardIdentity(profiles, row.UserID)
		var percentReturn *string
		if row.PercentReturn != nil {
			percentReturn = formatPercentReturnDecimal(*row.PercentReturn)
		}
		people = append(people, HomePeopleRow{
			UserID:          row.UserID,
			DisplayName:     displayName,
			ProfilePhotoURL: profilePhotoURL,
			PercentReturn:   percentReturn,
			DollarPnL:       formatSignedDollarPnL(int64(row.DollarPnL)),
		})
	}
	return people, nil
}

func (h *HomeService) collectBoardGroupMemberPnL(ctx context.Context, boardGroupIDs []string) (map[string][]domain.MemberPnL, error) {
	memberPnLByUser := make(map[string][]domain.MemberPnL)
	for _, groupID := range boardGroupIDs {
		netUsdcIn, err := h.groupNetUsdcIn(ctx, groupID)
		if err != nil {
			return nil, err
		}
		potNav, totalSharesMicro, err := h.groupPotNavAndShares(ctx, groupID, netUsdcIn)
		if err != nil {
			slog.Warn("home people board pot nav failed; using net usdc in", "group_id", groupID, "err", err)
			potNav = netUsdcIn
			totalSharesMicro = 0
		}
		if err := h.collectGroupMemberPnL(ctx, groupID, potNav, totalSharesMicro, memberPnLByUser); err != nil {
			return nil, err
		}
	}
	return memberPnLByUser, nil
}

func (h *HomeService) listMissedProposals(ctx context.Context, userID string, joinedGroupIDs []string) ([]HomeMissedProposalRow, error) {
	rows, err := h.store.ListMissedOpenProposalsForUser(ctx, userID, joinedGroupIDs, 20)
	if err != nil {
		return nil, err
	}
	out := make([]HomeMissedProposalRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, HomeMissedProposalRow{
			GroupID:    row.GroupID,
			GroupName:  row.GroupName,
			ProposalID: row.ProposalID,
			Symbol:     row.Symbol,
			Status:     string(row.Status),
			CreatedAt:  row.CreatedAt,
			ExpiresAt:  row.ExpiresAt,
		})
	}
	return out, nil
}

func ParseHomeLeaderboardRange(raw string) (HomeLeaderboardRange, error) {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "", "ALL":
		return HomeLeaderboardRangeALL, nil
	case "1H":
		return HomeLeaderboardRange1H, nil
	case "1D":
		return HomeLeaderboardRange1D, nil
	case "1W":
		return HomeLeaderboardRange1W, nil
	case "1M":
		return HomeLeaderboardRange1M, nil
	default:
		return "", fmt.Errorf("invalid range")
	}
}

func leaderboardRangeStart(r HomeLeaderboardRange, now time.Time) (time.Time, error) {
	switch r {
	case HomeLeaderboardRange1H:
		return now.Add(-1 * time.Hour), nil
	case HomeLeaderboardRange1D:
		return now.Add(-24 * time.Hour), nil
	case HomeLeaderboardRange1W:
		return now.Add(-7 * 24 * time.Hour), nil
	case HomeLeaderboardRange1M:
		return now.Add(-30 * 24 * time.Hour), nil
	case HomeLeaderboardRangeALL:
		return time.Time{}, fmt.Errorf("ALL range has no start")
	default:
		return time.Time{}, fmt.Errorf("invalid range")
	}
}

func memberEquityAtSnapshot(shareUnitsMicro int64, snap postgres.NavSnapshotRow) (int64, error) {
	return shareOfPotMicros(shareUnitsMicro, snap.PotNavMicros, snap.TotalShares)
}

// shareOfPotMicros is floor(shares × pot / totalShares) without int64 wraparound:
// share micros × pot micros overflows int64 once a pot passes roughly $3,000.
func shareOfPotMicros(shareUnitsMicro, potNavMicros, totalSharesMicro int64) (int64, error) {
	if shareUnitsMicro <= 0 || totalSharesMicro <= 0 || potNavMicros <= 0 {
		return 0, nil
	}
	equity, err := domain.MulDivFloor(shareUnitsMicro, potNavMicros, totalSharesMicro)
	if err != nil {
		return 0, fmt.Errorf("member share of pot: %w", err)
	}
	return equity, nil
}

func latestSnapshotAtOrBefore(snapshots []postgres.NavSnapshotRow, at time.Time) (postgres.NavSnapshotRow, bool) {
	var latest postgres.NavSnapshotRow
	found := false
	for _, snap := range snapshots {
		if snap.CreatedAt.After(at) {
			continue
		}
		if !found || snap.CreatedAt.After(latest.CreatedAt) {
			latest = snap
			found = true
		}
	}
	return latest, found
}

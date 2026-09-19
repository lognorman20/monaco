package app

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
)

// Group P&L history
//
// Definition. For a group at time t:
//
//	P&L(t) = pot value(t) - net contributed(t)
//	net contributed(t) = USDC members funded into the treasury (credited
//	                     deposits) - USDC paid back out (confirmed withdrawal
//	                     payouts), both up to t
//
// Where the numbers come from:
//   - Historical points are nav_snapshots rows. A snapshot is written in the
//     same transaction as every change to net contributed (deposit credit,
//     treasury reconcile, payout) and after each confirmed trade. Its pot value
//     marks stock at fill cost basis (history does not store market prices), so
//     unrealized price moves between events show up only in the live point.
//     Realized gains (a sale or a payout) are in the snapshot's USDC.
//   - net contributed at a snapshot is nav_snapshots.net_contributed_micros,
//     recorded in that same transaction (exact). Rows written before migration
//     000012 lack it; for those we estimate by walking back from the next known
//     value (a later exact snapshot, or the live total) and undoing confirmed
//     deposits/withdrawals created in between. Two known gaps in that estimate:
//     the ledger has created_at but no confirmation time, and treasury
//     reconcile credits have no ledger row, so an estimated point can be off by
//     a deposit that was still pending at that instant or by a reconcile credit.
//   - The last point is live: the same pot valuation and net USDC in that
//     GET /v1/home and the platform leaderboard use (Pyth-marked holdings).
//
// Window. range picks the lookback (max 90 days). When a snapshot exists before
// the window start, its values are carried forward to a point at the window
// start so the line spans the whole window. All times are UTC.

// GroupPnLRange is a P&L history lookback window.
type GroupPnLRange string

const (
	GroupPnLRange1D GroupPnLRange = "1D"
	GroupPnLRange1W GroupPnLRange = "1W"
	GroupPnLRange1M GroupPnLRange = "1M"
	GroupPnLRange3M GroupPnLRange = "3M"

	// GroupPnLMaxWindow caps every range.
	GroupPnLMaxWindow = 90 * 24 * time.Hour
)

// ErrInvalidGroupPnLRange is returned for an unknown range value.
var ErrInvalidGroupPnLRange = errors.New("invalid range")

// ParseGroupPnLRange parses a range query value; empty means 1M.
func ParseGroupPnLRange(raw string) (GroupPnLRange, error) {
	switch GroupPnLRange(strings.ToUpper(strings.TrimSpace(raw))) {
	case "":
		return GroupPnLRange1M, nil
	case GroupPnLRange1D:
		return GroupPnLRange1D, nil
	case GroupPnLRange1W:
		return GroupPnLRange1W, nil
	case GroupPnLRange1M:
		return GroupPnLRange1M, nil
	case GroupPnLRange3M:
		return GroupPnLRange3M, nil
	default:
		return "", ErrInvalidGroupPnLRange
	}
}

// Window returns the lookback duration, never above GroupPnLMaxWindow.
func (r GroupPnLRange) Window() time.Duration {
	var d time.Duration
	switch r {
	case GroupPnLRange1D:
		d = 24 * time.Hour
	case GroupPnLRange1W:
		d = 7 * 24 * time.Hour
	case GroupPnLRange3M:
		d = 90 * 24 * time.Hour
	default:
		d = 30 * 24 * time.Hour
	}
	if d > GroupPnLMaxWindow {
		d = GroupPnLMaxWindow
	}
	return d
}

// GroupPnLPoint is one sample of a group's P&L.
type GroupPnLPoint struct {
	At              time.Time
	PotNavMicros    int64
	NetInMicros     int64
	DollarPnLMicros int64
}

// PotValueUsd formats the pot as a USD decimal string.
func (p GroupPnLPoint) PotValueUsd() string { return formatMicrosAsUsdDecimal(p.PotNavMicros) }

// NetInUsd formats net contributed as a USD decimal string (may be negative).
func (p GroupPnLPoint) NetInUsd() string { return formatMicrosAsUsdDecimal(p.NetInMicros) }

// DollarPnL formats P&L with an explicit sign, matching other board rows.
func (p GroupPnLPoint) DollarPnL() string { return formatSignedDollarPnL(p.DollarPnLMicros) }

// GroupPnLSeries is one group's P&L over a window, oldest point first.
type GroupPnLSeries struct {
	GroupID string
	Name    string
	Range   GroupPnLRange
	Points  []GroupPnLPoint
}

// GroupPnLHistory returns one group's P&L series. Any signed-in user may read
// it (see the authorization note in groups_tab.go).
func (s *GroupsTabService) GroupPnLHistory(ctx context.Context, accessToken, groupID string, rng GroupPnLRange) (GroupPnLSeries, error) {
	if _, _, err := s.home.authenticateHomeUser(ctx, accessToken); err != nil {
		return GroupPnLSeries{}, err
	}
	groupID = strings.TrimSpace(groupID)
	if !IsWellFormedGroupID(groupID) {
		return GroupPnLSeries{}, ErrGroupNotFound
	}
	dir, found, err := s.store.GetGroupDirectoryRow(ctx, groupID)
	if err != nil {
		return GroupPnLSeries{}, err
	}
	if !found {
		return GroupPnLSeries{}, ErrGroupNotFound
	}
	series, err := s.buildPnLSeries(ctx, []postgres.GroupDirectoryRow{dir}, rng)
	if err != nil {
		return GroupPnLSeries{}, err
	}
	return series[0], nil
}

// MyGroupsPnLHistory returns one series per group the viewer belongs to, in
// join order, from a fixed number of queries regardless of group count.
func (s *GroupsTabService) MyGroupsPnLHistory(ctx context.Context, accessToken string, rng GroupPnLRange) ([]GroupPnLSeries, error) {
	_, joinedIDs, err := s.home.authenticateHomeUser(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	if len(joinedIDs) == 0 {
		return []GroupPnLSeries{}, nil
	}
	dirs, err := s.store.ListGroupDirectoryByIDs(ctx, joinedIDs)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]postgres.GroupDirectoryRow, len(dirs))
	for _, d := range dirs {
		byID[d.ID] = d
	}
	ordered := make([]postgres.GroupDirectoryRow, 0, len(dirs))
	for _, id := range joinedIDs {
		if d, ok := byID[id]; ok {
			ordered = append(ordered, d)
		}
	}
	return s.buildPnLSeries(ctx, ordered, rng)
}

func (s *GroupsTabService) buildPnLSeries(ctx context.Context, groups []postgres.GroupDirectoryRow, rng GroupPnLRange) ([]GroupPnLSeries, error) {
	if len(groups) == 0 {
		return []GroupPnLSeries{}, nil
	}
	now := s.now().UTC()
	since := now.Add(-rng.Window())

	ids := make([]string, 0, len(groups))
	for _, g := range groups {
		ids = append(ids, g.ID)
	}

	snapshots, err := s.store.ListNavSnapshotsForGroupsWindow(ctx, ids, since)
	if err != nil {
		return nil, err
	}
	snapsByGroup := make(map[string][]postgres.NavSnapshotRow, len(groups))
	notBefore := make(map[string]time.Time, len(groups))
	var legacyGroups []string
	var earliestLegacy time.Time
	legacySeen := make(map[string]bool)
	for _, snap := range snapshots {
		snapsByGroup[snap.GroupID] = append(snapsByGroup[snap.GroupID], snap)
		if snap.CreatedAt.After(notBefore[snap.GroupID]) {
			notBefore[snap.GroupID] = snap.CreatedAt
		}
		if snap.NetContributedMicros == nil {
			if !legacySeen[snap.GroupID] {
				legacySeen[snap.GroupID] = true
				legacyGroups = append(legacyGroups, snap.GroupID)
			}
			if earliestLegacy.IsZero() || snap.CreatedAt.Before(earliestLegacy) {
				earliestLegacy = snap.CreatedAt
			}
		}
	}

	eventsByGroup := make(map[string][]postgres.ContributionEvent)
	if len(legacyGroups) > 0 {
		events, err := s.store.ListContributionEventsAfter(ctx, legacyGroups, earliestLegacy)
		if err != nil {
			return nil, err
		}
		for _, ev := range events {
			eventsByGroup[ev.GroupID] = append(eventsByGroup[ev.GroupID], ev)
		}
	}

	valuations, err := s.valueGroups(ctx, ids, notBefore)
	if err != nil {
		return nil, err
	}

	out := make([]GroupPnLSeries, 0, len(groups))
	for _, g := range groups {
		points := assembleGroupPnLPoints(pnlSeriesInput{
			Since:     since,
			Snapshots: snapsByGroup[g.ID],
			Events:    eventsByGroup[g.ID],
			Live:      valuations[g.ID],
		})
		out = append(out, GroupPnLSeries{GroupID: g.ID, Name: g.Name, Range: rng, Points: points})
	}
	return out, nil
}

// pnlSeriesInput is everything assembleGroupPnLPoints needs for one group.
type pnlSeriesInput struct {
	// Since is the window start (UTC).
	Since time.Time
	// Snapshots holds at most one row before Since (the baseline) followed by
	// every row at or after Since, oldest first.
	Snapshots []postgres.NavSnapshotRow
	// Events are confirmed deposits (+) and payouts (-) after the earliest
	// snapshot lacking a recorded net contributed, oldest first.
	Events []postgres.ContributionEvent
	// Live is the current valuation, taken after the latest snapshot.
	Live groupValuation
}

// assembleGroupPnLPoints turns snapshots plus the live valuation into an
// ordered P&L series. It is pure so the math can be tested without a database.
func assembleGroupPnLPoints(in pnlSeriesInput) []GroupPnLPoint {
	snaps := append([]postgres.NavSnapshotRow(nil), in.Snapshots...)
	sort.SliceStable(snaps, func(i, j int) bool {
		if !snaps[i].CreatedAt.Equal(snaps[j].CreatedAt) {
			return snaps[i].CreatedAt.Before(snaps[j].CreatedAt)
		}
		return snaps[i].ID < snaps[j].ID
	})
	events := append([]postgres.ContributionEvent(nil), in.Events...)
	sort.SliceStable(events, func(i, j int) bool { return events[i].At.Before(events[j].At) })

	live := in.Live
	if len(snaps) == 0 && (live.Degraded || (live.PotNavMicros == 0 && live.NetUsdcInMicros == 0)) {
		// Never funded (or unpriceable with no history): nothing to chart.
		return []GroupPnLPoint{}
	}
	liveAt := live.ValuedAt.UTC()
	if len(snaps) > 0 && liveAt.Before(snaps[len(snaps)-1].CreatedAt) {
		// Snapshot times come from the database clock and valuation times from
		// this server; never let clock skew put the live point out of order.
		liveAt = snaps[len(snaps)-1].CreatedAt.UTC()
	}

	// Walk newest to oldest, carrying net contributed backwards from the next
	// known value and undoing ledger events that happened after each snapshot.
	netAt := make([]int64, len(snaps))
	running := live.NetUsdcInMicros
	e := len(events) - 1
	// Events after the live valuation are not in live.NetUsdcInMicros yet.
	for e >= 0 && events[e].At.After(live.ValuedAt) {
		e--
	}
	for i := len(snaps) - 1; i >= 0; i-- {
		snap := snaps[i]
		for e >= 0 && events[e].At.After(snap.CreatedAt) {
			running -= events[e].AmountMicros
			e--
		}
		if snap.NetContributedMicros != nil {
			running = *snap.NetContributedMicros
		}
		netAt[i] = running
	}

	points := make([]GroupPnLPoint, 0, len(snaps)+2)
	since := in.Since.UTC()
	for i, snap := range snaps {
		at := snap.CreatedAt.UTC()
		if at.Before(since) {
			// Baseline carried forward to the window start.
			at = since
		}
		points = append(points, GroupPnLPoint{
			At:              at,
			PotNavMicros:    snap.PotNavMicros,
			NetInMicros:     netAt[i],
			DollarPnLMicros: snap.PotNavMicros - netAt[i],
		})
	}
	if live.Degraded {
		// Pot could not be marked; a fallback live point would fake a flat P&L.
		return points
	}
	points = append(points, GroupPnLPoint{
		At:              liveAt,
		PotNavMicros:    live.PotNavMicros,
		NetInMicros:     live.NetUsdcInMicros,
		DollarPnLMicros: live.DollarPnLMicros(),
	})
	return points
}

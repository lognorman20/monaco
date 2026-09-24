package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/packages/domain"
)

// Groups tab discovery: platform leaderboard, name search, and group P&L
// history.
//
// Authorization: every route requires a signed-in user. Rows expose only
// public, group-level fields (name, member count, pot value, P&L, join mode).
// Treasury addresses and member identities never leave these endpoints. Group
// P&L history is readable by any signed-in user, not only members: the
// platform leaderboard already publishes each group's pot and P&L, and product
// rule is that a join policy hides entry, not the score.

const (
	GroupSearchMinQueryRunes = 2
	GroupSearchMaxQueryRunes = 64
	GroupsTabDefaultLimit    = 20
	GroupsTabMaxLimit        = 50
)

var (
	// ErrInvalidGroupSearchQuery is returned for queries shorter than
	// GroupSearchMinQueryRunes or longer than GroupSearchMaxQueryRunes.
	ErrInvalidGroupSearchQuery = errors.New("invalid search query")
	// ErrInvalidGroupSearchCursor is returned for cursors this server did not issue.
	ErrInvalidGroupSearchCursor = errors.New("invalid search cursor")
)

var groupIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// IsWellFormedGroupID reports whether id could name a group (UUID text form).
func IsWellFormedGroupID(id string) bool {
	return groupIDPattern.MatchString(id)
}

// GroupsTabService serves the Groups tab read APIs.
type GroupsTabService struct {
	home       *HomeService
	store      *postgres.Store
	valuations *groupValuationCache
	now        func() time.Time
}

// NewGroupsTabService wires Groups tab reads on top of the home valuation path.
func NewGroupsTabService(home *HomeService, store *postgres.Store) *GroupsTabService {
	return &GroupsTabService{
		home:       home,
		store:      store,
		valuations: newGroupValuationCache(groupValuationTTL),
		now:        time.Now,
	}
}

// GroupDiscoveryRow is one public group row (search result or leaderboard row).
type GroupDiscoveryRow struct {
	GroupID     string
	Name        string
	MemberCount int
	PotValueUsd string
	// PercentReturn is nil unless net USDC in is positive (product: no fake 0% rows).
	PercentReturn *string
	DollarPnL     string
	IsJoined      bool
	JoinMode      domain.JoinMode
	// PictureURL is blank when the cabal has no picture.
	PictureURL string
}

// GroupLeaderboardRow is a ranked row on the platform-wide group board.
type GroupLeaderboardRow struct {
	Rank int
	GroupDiscoveryRow
}

// GroupSearchResult is one page of search results.
type GroupSearchResult struct {
	Groups     []GroupDiscoveryRow
	NextCursor *string
}

// ClampGroupsTabLimit applies the default for 0 and caps at GroupsTabMaxLimit.
// Callers reject negative values before calling.
func ClampGroupsTabLimit(limit int) int {
	if limit <= 0 {
		return GroupsTabDefaultLimit
	}
	if limit > GroupsTabMaxLimit {
		return GroupsTabMaxLimit
	}
	return limit
}

// Leaderboard ranks every funded group on the platform by percent return.
// Ranking is domain.BuildGroupBoard over the same pot/net-in valuation
// GET /v1/home uses, so both surfaces agree on order.
func (s *GroupsTabService) Leaderboard(ctx context.Context, accessToken string, limit int) ([]GroupLeaderboardRow, error) {
	_, joinedIDs, err := s.home.authenticateHomeUser(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	limit = ClampGroupsTabLimit(limit)

	directory, err := s.store.ListGroupDirectory(ctx)
	if err != nil {
		return nil, err
	}
	// Only groups with money in can rank; skip valuing the rest.
	candidates := make([]postgres.GroupDirectoryRow, 0, len(directory))
	ids := make([]string, 0, len(directory))
	for _, row := range directory {
		if domain.IncludeOnBoard(domain.USDCMicros(row.NetUsdcInMicros)) {
			candidates = append(candidates, row)
			ids = append(ids, row.ID)
		}
	}
	valuations, err := s.valueGroups(ctx, ids, nil)
	if err != nil {
		return nil, err
	}

	byID := make(map[string]postgres.GroupDirectoryRow, len(candidates))
	inputs := make([]domain.GroupBoardInput, 0, len(candidates))
	for _, row := range candidates {
		byID[row.ID] = row
		v := valuations[row.ID]
		inputs = append(inputs, domain.GroupBoardInput{
			GroupID:   row.ID,
			GroupName: row.Name,
			PotNav:    domain.USDCMicros(v.PotNavMicros),
			NetUsdcIn: domain.USDCMicros(v.NetUsdcInMicros),
		})
	}
	board := BuildAppGroupBoard(inputs)
	if len(board) > limit {
		board = board[:limit]
	}

	joined := toSet(joinedIDs)
	out := make([]GroupLeaderboardRow, 0, len(board))
	for i, ranked := range board {
		dir := byID[ranked.GroupID]
		out = append(out, GroupLeaderboardRow{
			Rank: i + 1,
			GroupDiscoveryRow: GroupDiscoveryRow{
				GroupID:       ranked.GroupID,
				Name:          ranked.GroupName,
				MemberCount:   dir.MemberCount,
				PotValueUsd:   formatMicrosAsUsdDecimal(int64(ranked.PotNav)),
				PercentReturn: formatPercentReturnDecimal(ranked.PercentReturn),
				DollarPnL:     formatSignedDollarPnL(int64(ranked.DollarPnL)),
				IsJoined:      joined[ranked.GroupID],
				JoinMode:      dir.JoinMode,
				PictureURL:    nullStringValue(dir.PictureURL),
			},
		})
	}
	return out, nil
}

// SearchGroups finds groups by case-insensitive name match. query is trimmed
// and must be GroupSearchMinQueryRunes..GroupSearchMaxQueryRunes runes; LIKE
// wildcards in it match literally. cursor is the opaque nextCursor of the
// previous page, or empty for the first page.
func (s *GroupsTabService) SearchGroups(ctx context.Context, accessToken, query string, limit int, cursor string) (GroupSearchResult, error) {
	_, joinedIDs, err := s.home.authenticateHomeUser(ctx, accessToken)
	if err != nil {
		return GroupSearchResult{}, err
	}

	query = strings.TrimSpace(query)
	runes := utf8.RuneCountInString(query)
	if runes < GroupSearchMinQueryRunes || runes > GroupSearchMaxQueryRunes {
		return GroupSearchResult{}, ErrInvalidGroupSearchQuery
	}
	limit = ClampGroupsTabLimit(limit)

	var after *postgres.GroupSearchCursor
	if cursor != "" {
		decoded, err := decodeGroupSearchCursor(cursor)
		if err != nil {
			return GroupSearchResult{}, err
		}
		after = &decoded
	}

	matches, err := s.store.SearchGroupDirectory(ctx, query, limit+1, after)
	if err != nil {
		return GroupSearchResult{}, err
	}
	var next *string
	if len(matches) > limit {
		matches = matches[:limit]
		last := matches[len(matches)-1]
		encoded := encodeGroupSearchCursor(postgres.GroupSearchCursor{
			MatchRank: last.MatchRank,
			SortName:  last.SortName,
			GroupID:   last.ID,
		})
		next = &encoded
	}

	ids := make([]string, 0, len(matches))
	for _, m := range matches {
		ids = append(ids, m.ID)
	}
	valuations, err := s.valueGroups(ctx, ids, nil)
	if err != nil {
		return GroupSearchResult{}, err
	}

	joined := toSet(joinedIDs)
	rows := make([]GroupDiscoveryRow, 0, len(matches))
	for _, m := range matches {
		rows = append(rows, discoveryRow(m.GroupDirectoryRow, valuations[m.ID], joined[m.ID]))
	}
	return GroupSearchResult{Groups: rows, NextCursor: next}, nil
}

func discoveryRow(dir postgres.GroupDirectoryRow, v groupValuation, isJoined bool) GroupDiscoveryRow {
	var percent *string
	if domain.IncludeOnBoard(domain.USDCMicros(v.NetUsdcInMicros)) {
		if pct := domain.PercentReturn(domain.USDCMicros(v.PotNavMicros), domain.USDCMicros(v.NetUsdcInMicros)); pct != nil {
			percent = formatPercentReturnDecimal(*pct)
		}
	}
	return GroupDiscoveryRow{
		GroupID:       dir.ID,
		Name:          dir.Name,
		MemberCount:   dir.MemberCount,
		PotValueUsd:   formatMicrosAsUsdDecimal(v.PotNavMicros),
		PercentReturn: percent,
		DollarPnL:     formatSignedDollarPnL(v.DollarPnLMicros()),
		IsJoined:      isJoined,
		JoinMode:      dir.JoinMode,
		PictureURL:    nullStringValue(dir.PictureURL),
	}
}

type groupSearchCursorWire struct {
	R int    `json:"r"`
	N string `json:"n"`
	I string `json:"i"`
}

func encodeGroupSearchCursor(c postgres.GroupSearchCursor) string {
	raw, _ := json.Marshal(groupSearchCursorWire{R: c.MatchRank, N: c.SortName, I: c.GroupID})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeGroupSearchCursor(cursor string) (postgres.GroupSearchCursor, error) {
	if len(cursor) > 512 {
		return postgres.GroupSearchCursor{}, ErrInvalidGroupSearchCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return postgres.GroupSearchCursor{}, ErrInvalidGroupSearchCursor
	}
	var wire groupSearchCursorWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return postgres.GroupSearchCursor{}, ErrInvalidGroupSearchCursor
	}
	if (wire.R != 0 && wire.R != 1) || !IsWellFormedGroupID(wire.I) {
		return postgres.GroupSearchCursor{}, ErrInvalidGroupSearchCursor
	}
	return postgres.GroupSearchCursor{MatchRank: wire.R, SortName: wire.N, GroupID: wire.I}, nil
}

func toSet(ids []string) map[string]bool {
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

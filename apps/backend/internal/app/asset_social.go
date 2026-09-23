package app

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/b20"
	"github.com/monaco/monaco/apps/backend/internal/marks"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/packages/domain"
)

// AssetSocialHolding is one cabal's position in a single symbol.
//
// Every money figure is a decimal string in the same shape the group screens
// already use, so the app formats one kind of number everywhere.
type AssetSocialHolding struct {
	GroupID string
	Name    string
	// Units is the token amount in whole tokens ("12.5").
	Units string
	// TokenAmount is the same figure in atomic units, for callers that do math.
	TokenAmount string
	MarkUsd     string
	ValueUsd    string
	// CostBasisUsd is what the cabal paid for the units it still holds.
	CostBasisUsd string
	DollarPnL    string
	// PercentReturn is nil when the cabal has no cost basis to measure against —
	// an airdropped or migrated position, which has a value but no return.
	PercentReturn *string
	// MySliceUsd is the viewer's own share of ValueUsd, by share units. This is the
	// figure that makes the card personal: "the cabal holds $2,900 of it, $412 is
	// yours".
	MySliceUsd     string
	MySlicePercent string
	// AfterHours is true when this holding was marked with a round older than the
	// feed's heartbeat — the Chainlink mark holding its last print.
	AfterHours bool
}

// AssetSocialVoter is one ballot on an open proposal, with the voter's identity.
type AssetSocialVoter struct {
	UserID          string
	DisplayName     string
	ProfilePhotoURL string
	Choice          string
}

// AssetSocialProposal is one open vote about this symbol in one of the viewer's cabals.
type AssetSocialProposal struct {
	ID          string
	GroupID     string
	GroupName   string
	Kind        string
	Status      string
	UsdcMicros  int64
	TokenAmount int64
	Thesis      string
	Yes         int
	No          int
	// MemberCount is how many members could vote, so the app can say "3 of 5".
	MemberCount int
	// MyVote is "yes", "no", or "" when the viewer has not voted — the difference
	// between "2 open votes" and "2 open votes waiting on you".
	MyVote    string
	Voters    []AssetSocialVoter
	ExpiresAt time.Time
	CreatedAt time.Time
}

// Activity kinds name what happened, in the app's vocabulary.
const (
	AssetActivityProposed = "proposed"
	AssetActivityPassed   = "passed"
	AssetActivityFailed   = "failed"
	AssetActivityExpired  = "expired"
	AssetActivityFilled   = "filled"
)

// AssetSocialActivity is one thing that happened to this symbol in one of the
// viewer's cabals: a proposal opened, a vote landed, or a swap filled.
type AssetSocialActivity struct {
	ID          string
	GroupID     string
	GroupName   string
	Kind        string
	Action      string
	Status      string
	UsdcMicros  int64
	TokenAmount int64
	ActorName   string
	TxHash      string
	CreatedAt   time.Time
}

// AssetSocialResult is GET /v1/assets/{symbol}/social.
type AssetSocialResult struct {
	Symbol        string
	Holdings      []AssetSocialHolding
	OpenProposals []AssetSocialProposal
	Activity      []AssetSocialActivity
	// HolderCount is how many of the viewer's cabals hold the symbol.
	HolderCount int
	// UnvaluedGroups is how many of the viewer's cabals could not be valued on this
	// pass. The card says so rather than claiming the missing cabals hold nothing —
	// telling a member no cabal holds a stock on a partial answer is a lie about
	// their money.
	UnvaluedGroups int
}

// assetSocialActivityLimit bounds each of the two activity reads. The card shows a
// handful; the rest is in the cabal's own activity screen.
const assetSocialActivityLimit = 20

// GetAssetSocial answers "what are my cabals doing with this stock".
//
// Three reads, all scoped to the caller's own memberships: the pot of every cabal
// they belong to (for holdings), the proposals about this symbol in those cabals
// (open ones for the vote card, every status for the activity list), and the swaps
// those proposals produced. Nothing here widens beyond the membership list, so a
// non-member never appears in another cabal's answer.
//
// A cabal whose pot cannot be valued is counted, not dropped: the card shows the
// cabals that answered and says how many it could not check.
func (h *HomeService) GetAssetSocial(ctx context.Context, accessToken, symbol string) (AssetSocialResult, error) {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return AssetSocialResult{}, fmt.Errorf("symbol is required")
	}

	user, joinedGroupIDs, err := h.authenticateHomeUser(ctx, accessToken)
	if err != nil {
		return AssetSocialResult{}, err
	}

	result := AssetSocialResult{
		Symbol:        symbol,
		Holdings:      []AssetSocialHolding{},
		OpenProposals: []AssetSocialProposal{},
		Activity:      []AssetSocialActivity{},
	}
	if len(joinedGroupIDs) == 0 {
		return result, nil
	}

	if err := h.creditUncreditedForGroups(ctx, joinedGroupIDs); err != nil {
		return AssetSocialResult{}, err
	}

	holdings, unvalued := h.assetHoldings(ctx, user.ID, joinedGroupIDs, symbol)
	result.Holdings = holdings
	result.HolderCount = len(holdings)
	result.UnvaluedGroups = unvalued

	proposals, err := h.store.ListProposalsForSymbol(
		ctx,
		joinedGroupIDs,
		symbol,
		user.ID,
		[]domain.ProposalStatus{domain.ProposalOpen, domain.ProposalPassed, domain.ProposalFailed, domain.ProposalExpired},
		assetSocialActivityLimit,
	)
	if err != nil {
		return AssetSocialResult{}, err
	}

	openProposals, err := h.openProposalsForSymbol(ctx, proposals)
	if err != nil {
		return AssetSocialResult{}, err
	}
	result.OpenProposals = openProposals

	fills, err := h.store.ListFillsForSymbol(ctx, joinedGroupIDs, symbol, assetSocialActivityLimit)
	if err != nil {
		return AssetSocialResult{}, err
	}
	result.Activity = assetActivityFeed(proposals, fills, assetSocialActivityLimit)

	return result, nil
}

type assetHoldingBuild struct {
	row         AssetSocialHolding
	valueMicros int64
}

func assetHoldingRow(groupName, groupID string, marked marks.MarkedHolding) (assetHoldingBuild, error) {
	units, err := tokenAtomicsToDecimalUnits(marked.Units)
	if err != nil {
		return assetHoldingBuild{}, err
	}
	valueMicros, err := domain.MulDivFloor(marked.Units, marked.MarkUsdc, b20.TokenAtomicScale)
	if err != nil {
		return assetHoldingBuild{}, fmt.Errorf("value %s holding: %w", marked.Symbol, err)
	}

	row := AssetSocialHolding{
		GroupID:      groupID,
		Name:         groupName,
		Units:        string(units),
		TokenAmount:  strconv.FormatInt(marked.Units, 10),
		MarkUsd:      formatMicrosAsUsdDecimal(marked.MarkUsdc),
		ValueUsd:     formatMicrosAsUsdDecimal(valueMicros),
		CostBasisUsd: formatMicrosAsUsdDecimal(marked.CostBasis),
		DollarPnL:    formatSignedDollarPnL(valueMicros - marked.CostBasis),
		AfterHours:   marked.AfterHours,
	}
	if marked.CostBasis > 0 {
		ratio := float64(valueMicros-marked.CostBasis) / float64(marked.CostBasis)
		row.PercentReturn = formatPercentReturnDecimal(ratio)
	}
	return assetHoldingBuild{row: row, valueMicros: valueMicros}, nil
}

// openProposalsForSymbol keeps the open votes and attaches who voted, in one extra
// query for the whole set rather than one per proposal.
func (h *HomeService) openProposalsForSymbol(ctx context.Context, rows []postgres.SymbolProposalRow) ([]AssetSocialProposal, error) {
	open := make([]postgres.SymbolProposalRow, 0, len(rows))
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.Status != domain.ProposalOpen {
			continue
		}
		open = append(open, row)
		ids = append(ids, row.ID)
	}
	if len(open) == 0 {
		return []AssetSocialProposal{}, nil
	}

	votes, err := h.store.ListVotesForProposals(ctx, ids)
	if err != nil {
		return nil, err
	}
	byProposal := make(map[string][]AssetSocialVoter, len(open))
	for _, vote := range votes {
		byProposal[vote.ProposalID] = append(byProposal[vote.ProposalID], AssetSocialVoter{
			UserID:          vote.VoterID,
			DisplayName:     vote.DisplayName,
			ProfilePhotoURL: vote.ProfilePhotoURL,
			Choice:          string(vote.Choice),
		})
	}

	memberCounts := make(map[string]int, len(open))
	for _, row := range open {
		if _, seen := memberCounts[row.GroupID]; seen {
			continue
		}
		memberIDs, err := h.store.ListGroupMemberIDs(ctx, row.GroupID)
		if err != nil {
			return nil, err
		}
		memberCounts[row.GroupID] = len(memberIDs)
	}

	out := make([]AssetSocialProposal, 0, len(open))
	for _, row := range open {
		out = append(out, AssetSocialProposal{
			ID:          row.ID,
			GroupID:     row.GroupID,
			GroupName:   row.GroupName,
			Kind:        string(row.Kind),
			Status:      string(row.Status),
			UsdcMicros:  row.UsdcMicros,
			TokenAmount: row.TokenAmount,
			Thesis:      row.Thesis,
			Yes:         row.Yes,
			No:          row.No,
			MemberCount: memberCounts[row.GroupID],
			MyVote:      row.ViewerChoice,
			Voters:      byProposal[row.ID],
			ExpiresAt:   row.ExpiresAt.UTC(),
			CreatedAt:   row.CreatedAt.UTC(),
		})
	}
	return out, nil
}

// assetActivityFeed merges what the cabals decided with what actually filled, newest
// first. It is a pure function of its two inputs so the ordering and the wording can
// be tested without a database.
func assetActivityFeed(proposals []postgres.SymbolProposalRow, fills []postgres.SymbolFillRow, limit int) []AssetSocialActivity {
	items := make([]AssetSocialActivity, 0, len(proposals)+len(fills))
	for _, proposal := range proposals {
		items = append(items, AssetSocialActivity{
			ID:          proposal.ID,
			GroupID:     proposal.GroupID,
			GroupName:   proposal.GroupName,
			Kind:        activityKindForStatus(proposal.Status),
			Action:      string(proposal.Kind),
			Status:      string(proposal.Status),
			UsdcMicros:  proposal.UsdcMicros,
			TokenAmount: proposal.TokenAmount,
			CreatedAt:   proposal.CreatedAt.UTC(),
		})
	}
	for _, fill := range fills {
		// Only a settled swap is a fill. One still in flight is the proposal's
		// "passed" line, which is already in the list.
		if !strings.EqualFold(fill.Status, postgres.TransactionStatusConfirmed) {
			continue
		}
		// A sell's amount column is token atomics, not micros: only the recorded
		// proceeds are dollars. Unknown leaves UsdcMicros at zero and the row
		// falls back to its share count.
		usdcMicros, _ := postgres.SwapUsdcMicros(fill.Action, fill.Status, fill.Amount, fill.CostBasisAmount)
		items = append(items, AssetSocialActivity{
			ID:          fill.ID,
			GroupID:     fill.GroupID,
			GroupName:   fill.GroupName,
			Kind:        AssetActivityFilled,
			Action:      fill.Action,
			Status:      fill.Status,
			UsdcMicros:  usdcMicros,
			TokenAmount: fill.TokenAmount,
			ActorName:   fill.ActorName,
			TxHash:      fill.TxHash,
			CreatedAt:   fill.CreatedAt.UTC(),
		})
	}

	sort.SliceStable(items, func(a, b int) bool {
		if items[a].CreatedAt.Equal(items[b].CreatedAt) {
			// A fill and the proposal it came from can share a timestamp to the
			// second. The fill is the later fact, so it sorts above.
			return items[a].Kind == AssetActivityFilled && items[b].Kind != AssetActivityFilled
		}
		return items[a].CreatedAt.After(items[b].CreatedAt)
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}

func activityKindForStatus(status domain.ProposalStatus) string {
	switch status {
	case domain.ProposalPassed:
		return AssetActivityPassed
	case domain.ProposalFailed:
		return AssetActivityFailed
	case domain.ProposalExpired:
		return AssetActivityExpired
	default:
		return AssetActivityProposed
	}
}

// usdDecimalLess compares two "1234.56" strings without going through float64,
// which cannot hold a large pot's cents exactly.
func usdDecimalLess(a, b string) bool {
	aWhole, aFrac := splitUSDDecimal(a)
	bWhole, bFrac := splitUSDDecimal(b)
	if aWhole != bWhole {
		return aWhole < bWhole
	}
	return aFrac < bFrac
}

func splitUSDDecimal(raw string) (int64, int64) {
	raw = strings.TrimSpace(raw)
	negative := strings.HasPrefix(raw, "-")
	raw = strings.TrimPrefix(raw, "-")
	whole, frac, _ := strings.Cut(raw, ".")
	wholeValue, _ := strconv.ParseInt(whole, 10, 64)
	frac = (frac + "000000")[:6]
	fracValue, _ := strconv.ParseInt(frac, 10, 64)
	if negative {
		return -wholeValue, -fracValue
	}
	return wholeValue, fracValue
}

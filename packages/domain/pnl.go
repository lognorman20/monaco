package domain

import (
	"fmt"
	"math/big"
	"sort"
)

// MemberPosition is one member's position columns in a group.
type MemberPosition struct {
	UserID          string
	ShareUnits      ShareUnits
	AmountDeposited USDCMicros
	AmountWithdrawn USDCMicros
}

// MemberPnL is computed return data for one member in one group.
type MemberPnL struct {
	UserID        string
	Equity        USDCMicros
	NetUsdcIn     USDCMicros
	PercentReturn *float64
}

// GroupBoardInput is one group row for the app-wide group board.
type GroupBoardInput struct {
	GroupID   string
	GroupName string
	PotNav    USDCMicros
	NetUsdcIn USDCMicros
}

// GroupBoardRow is a ranked group on the app-wide board.
type GroupBoardRow struct {
	GroupID       string
	GroupName     string
	PotNav        USDCMicros
	NetUsdcIn     USDCMicros
	PercentReturn float64
	DollarPnL     USDCMicros
}

// PersonBoardInput aggregates per-user cross-group totals.
type PersonBoardInput struct {
	UserID         string
	TotalEquity    USDCMicros
	TotalNetUsdcIn USDCMicros
}

// PersonBoardRow is a ranked person on the app-wide people board.
type PersonBoardRow struct {
	UserID         string
	TotalEquity    USDCMicros
	TotalNetUsdcIn USDCMicros
	PercentReturn  *float64
	DollarPnL      USDCMicros
}

// NetUsdcIn returns deposits credited minus withdrawals paid for a position.
func NetUsdcIn(deposited, withdrawn USDCMicros) USDCMicros {
	return deposited - withdrawn
}

// MemberEquity returns the member's slice of pot NAV from share fraction.
func MemberEquity(memberShares, totalShares ShareUnits, potNav USDCMicros) (USDCMicros, error) {
	if potNav < 0 {
		return 0, fmt.Errorf("pot nav must be non-negative")
	}
	if memberShares.IsZero() {
		return 0, nil
	}
	if totalShares.IsZero() {
		return 0, fmt.Errorf("total shares must be positive when member has shares")
	}

	member, ok := new(big.Rat).SetString(string(memberShares))
	if !ok || member.Sign() < 0 {
		return 0, fmt.Errorf("invalid member shares: %q", memberShares)
	}
	total, ok := new(big.Rat).SetString(string(totalShares))
	if !ok || total.Sign() <= 0 {
		return 0, fmt.Errorf("invalid total shares: %q", totalShares)
	}

	fraction := new(big.Rat).Quo(member, total)
	product := new(big.Rat).Mul(fraction, big.NewRat(int64(potNav), 1))
	return USDCMicros(ratRoundToInt64(product)), nil
}

// PercentReturn computes equity/netIn - 1. Returns nil when netIn is zero.
func PercentReturn(equity, netIn USDCMicros) *float64 {
	if netIn == 0 {
		return nil
	}
	ratio := new(big.Rat).Quo(
		big.NewRat(int64(equity), 1),
		big.NewRat(int64(netIn), 1),
	)
	ratio.Sub(ratio, big.NewRat(1, 1))
	value, _ := ratio.Float64()
	return &value
}

// IncludeInGroupBoard reports whether a member should appear on the in-group board.
func IncludeInGroupBoard(shareUnits ShareUnits, netIn USDCMicros) bool {
	if shareUnits.IsZero() {
		return false
	}
	return netIn != 0
}

// IncludeOnBoard reports whether a row should appear on group or people boards.
func IncludeOnBoard(netIn USDCMicros) bool {
	return netIn != 0
}

// ComputeMemberPnL derives equity and percent return for one member.
func ComputeMemberPnL(pos MemberPosition, totalShares ShareUnits, potNav USDCMicros) (MemberPnL, error) {
	netIn := NetUsdcIn(pos.AmountDeposited, pos.AmountWithdrawn)
	equity, err := MemberEquity(pos.ShareUnits, totalShares, potNav)
	if err != nil {
		return MemberPnL{}, err
	}
	return MemberPnL{
		UserID:        pos.UserID,
		Equity:        equity,
		NetUsdcIn:     netIn,
		PercentReturn: PercentReturn(equity, netIn),
	}, nil
}

// BuildInGroupViewBoard assembles ranked in-group board rows for the group view.
// Every member is included; members without a computable percent return sort last.
func BuildInGroupViewBoard(members []MemberPosition, totalShares ShareUnits, potNav USDCMicros) ([]MemberPnL, error) {
	rows := make([]MemberPnL, 0, len(members))
	for _, member := range members {
		pnl, err := ComputeMemberPnL(member, totalShares, potNav)
		if err != nil {
			return nil, err
		}
		rows = append(rows, pnl)
	}
	RankMemberPnLForGroupView(rows)
	return rows, nil
}

// RankMemberPnLForGroupView sorts by percent return descending, then user id.
// Members without a percent return rank after those with one.
func RankMemberPnLForGroupView(rows []MemberPnL) {
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
}

// BuildInGroupBoard assembles ranked in-group board rows, skipping ineligible members.
func BuildInGroupBoard(members []MemberPosition, totalShares ShareUnits, potNav USDCMicros) ([]MemberPnL, error) {
	rows := make([]MemberPnL, 0, len(members))
	for _, member := range members {
		netIn := NetUsdcIn(member.AmountDeposited, member.AmountWithdrawn)
		if !IncludeInGroupBoard(member.ShareUnits, netIn) {
			continue
		}
		pnl, err := ComputeMemberPnL(member, totalShares, potNav)
		if err != nil {
			return nil, err
		}
		if pnl.PercentReturn == nil {
			continue
		}
		rows = append(rows, pnl)
	}
	RankMemberPnLByPercentReturn(rows)
	return rows, nil
}

// RankMemberPnLByPercentReturn sorts descending by percent return; ties by user id.
func RankMemberPnLByPercentReturn(rows []MemberPnL) {
	sort.SliceStable(rows, func(i, j int) bool {
		pi := *rows[i].PercentReturn
		pj := *rows[j].PercentReturn
		if pi != pj {
			return pi > pj
		}
		return rows[i].UserID < rows[j].UserID
	})
}

// BuildGroupBoard assembles the app-wide group board ranked by pot percent return.
func BuildGroupBoard(groups []GroupBoardInput) []GroupBoardRow {
	rows := make([]GroupBoardRow, 0, len(groups))
	for _, group := range groups {
		if !IncludeOnBoard(group.NetUsdcIn) {
			continue
		}
		pct := PercentReturn(group.PotNav, group.NetUsdcIn)
		if pct == nil {
			continue
		}
		rows = append(rows, GroupBoardRow{
			GroupID:       group.GroupID,
			GroupName:     group.GroupName,
			PotNav:        group.PotNav,
			NetUsdcIn:     group.NetUsdcIn,
			PercentReturn: *pct,
			DollarPnL:     group.PotNav - group.NetUsdcIn,
		})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].PercentReturn != rows[j].PercentReturn {
			return rows[i].PercentReturn > rows[j].PercentReturn
		}
		return rows[i].GroupID < rows[j].GroupID
	})
	return rows
}

// AggregatePersonPnL sums equity and net USDC in across groups for one user.
func AggregatePersonPnL(entries []MemberPnL) PersonBoardInput {
	var out PersonBoardInput
	if len(entries) == 0 {
		return out
	}
	out.UserID = entries[0].UserID
	for _, entry := range entries {
		out.TotalEquity += entry.Equity
		out.TotalNetUsdcIn += entry.NetUsdcIn
	}
	return out
}

// BuildPeopleBoard assembles the app-wide people board from cross-group aggregates.
// Every person with group membership is included; funded members rank first by percent
// return, unfunded members appear at the bottom with nil percent return.
func BuildPeopleBoard(people []PersonBoardInput) []PersonBoardRow {
	rows := make([]PersonBoardRow, 0, len(people))
	for _, person := range people {
		if person.UserID == "" {
			continue
		}
		rows = append(rows, PersonBoardRow{
			UserID:         person.UserID,
			TotalEquity:    person.TotalEquity,
			TotalNetUsdcIn: person.TotalNetUsdcIn,
			PercentReturn:  PercentReturn(person.TotalEquity, person.TotalNetUsdcIn),
			DollarPnL:      person.TotalEquity - person.TotalNetUsdcIn,
		})
	}
	RankPeopleBoard(rows)
	return rows
}

// RankPeopleBoard sorts funded members by percent return descending, then unfunded by user id.
func RankPeopleBoard(rows []PersonBoardRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		iFunded := rows[i].PercentReturn != nil
		jFunded := rows[j].PercentReturn != nil
		if iFunded != jFunded {
			return iFunded
		}
		if iFunded {
			pi := *rows[i].PercentReturn
			pj := *rows[j].PercentReturn
			if pi != pj {
				return pi > pj
			}
		}
		return rows[i].UserID < rows[j].UserID
	})
}

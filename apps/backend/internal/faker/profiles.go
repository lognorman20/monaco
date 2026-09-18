// Package faker seeds demo/test data (#153) into a local database.
//
// Two profiles exist:
//   - mixed: ghost members (Maya Chen, Jordan Hale, Priya Shah) on the operator's REAL club.
//     Display-only positions, deposits, proposals, and votes. No member_wallets, no transactions.
//   - scale: three wholly fake clubs (groups.is_faker) with a dummy treasury, fake deposits,
//     a confirmed buy (real xStock mint + cost basis), an optional sell, failed/open/passed
//     proposals, votes, and a week of NAV snapshots.
//
// Every seeded row is flagged (users.is_faker / groups.is_faker) so product code keeps it away
// from Privy, Solana RPC, and Jupiter. Seeding is idempotent: users are keyed by privy_user_id,
// scale clubs by groups.faker_key, and each run replaces the faker-owned child rows.
// All timestamps are UTC and spread over the last week relative to the seed time.
package faker

import "github.com/monaco/monaco/apps/backend/internal/jupiter"

const usdc = int64(1_000_000)

// person is a seeded faker user.
type person struct {
	Slug string
	Name string
}

// deposit is one confirmed (or pending/failed) deposit into a club.
type depositSpec struct {
	Who      string  // person slug
	USDC     int64   // whole USDC
	HoursAgo float64 // relative to seed time
	SharePx  float64 // NAV per share at deposit (share_units = amount / SharePx)
	Status   string  // "" = confirmed
}

type voteSpec struct {
	Who    string
	Choice string // yes | no
}

type proposalSpec struct {
	Key          string
	Proposer     string
	Symbol       string
	USDC         int64
	Status       string // open | passed | failed | expired
	HoursAgo     float64
	ExpiresHours float64 // relative to created_at
	Votes        []voteSpec
	// Buy marks the passed proposal that carries the confirmed fake swap (scale clubs only).
	Buy bool
}

type clubSpec struct {
	Key        string
	Name       string
	Creator    person
	Members    []person
	Deposits   []depositSpec
	Proposals  []proposalSpec
	BuySymbol  string
	BuyMint    string
	BuyCostPx  float64 // cost basis as a multiple of the reference mark
	SellFrac   float64 // fraction of tokens sold later (0 = no sell)
	SellPx     float64 // sell price as a multiple of cost basis
	SellHours  float64
	ChartDrift float64 // end-of-week NAV drift used for snapshot charts
}

var mixedPeople = []person{
	{Slug: "maya", Name: "Maya Chen"},
	{Slug: "jordan", Name: "Jordan Hale"},
	{Slug: "priya", Name: "Priya Shah"},
}

// mixedDeposits are ghost deposits on the operator's club. SharePx < 1 means the ghost's
// seeded position is up; it is displayed at the real club NAV per share.
var mixedDeposits = []depositSpec{
	{Who: "maya", USDC: 1500, HoursAgo: 6 * 24, SharePx: 0.89},
	{Who: "jordan", USDC: 800, HoursAgo: 5 * 24, SharePx: 1.09},
	{Who: "priya", USDC: 1200, HoursAgo: 3 * 24, SharePx: 0.94},
	{Who: "maya", USDC: 500, HoursAgo: 2 * 24, SharePx: 0.96},
	{Who: "jordan", USDC: 300, HoursAgo: 2, Status: "pending"},
	{Who: "priya", USDC: 250, HoursAgo: 30, Status: "failed: submit_sweep"},
}

var mixedProposals = []proposalSpec{
	{Key: "open", Proposer: "jordan", Symbol: "TSLAx", USDC: 400, Status: "open", HoursAgo: 5, ExpiresHours: 24,
		Votes: []voteSpec{{"jordan", "yes"}, {"maya", "yes"}}},
	{Key: "passed", Proposer: "maya", Symbol: "AAPLx", USDC: 500, Status: "passed", HoursAgo: 48, ExpiresHours: 24,
		Votes: []voteSpec{{"maya", "yes"}, {"jordan", "yes"}, {"priya", "yes"}}},
	{Key: "failed", Proposer: "priya", Symbol: "AAPLx", USDC: 600, Status: "failed", HoursAgo: 96, ExpiresHours: 24,
		Votes: []voteSpec{{"priya", "yes"}, {"maya", "no"}, {"jordan", "no"}}},
	{Key: "expired", Proposer: "jordan", Symbol: "AAPLx", USDC: 300, Status: "expired", HoursAgo: 120, ExpiresHours: 24,
		Votes: []voteSpec{{"priya", "yes"}}},
}

var scaleClubs = []clubSpec{
	{
		Key: "ridgewood", Name: "Ridgewood Value Club",
		Creator: person{"rowan", "Rowan Ellis"},
		Members: []person{{"tess", "Tess Morgan"}, {"diego", "Diego Alvarez"}, {"hana", "Hana Kim"}, {"marcus", "Marcus Webb"}, {"lena", "Lena Fischer"}},
		Deposits: []depositSpec{
			{Who: "rowan", USDC: 4000, HoursAgo: 7*24 + 2, SharePx: 1},
			{Who: "tess", USDC: 2500, HoursAgo: 6*24 + 20, SharePx: 1},
			{Who: "diego", USDC: 1800, HoursAgo: 6 * 24, SharePx: 1},
			{Who: "hana", USDC: 3000, HoursAgo: 5*24 + 6, SharePx: 1},
			{Who: "marcus", USDC: 1200, HoursAgo: 3 * 24, SharePx: 1.06},
			{Who: "lena", USDC: 900, HoursAgo: 30, SharePx: 1.09},
		},
		Proposals: []proposalSpec{
			{Key: "buy", Proposer: "rowan", Symbol: "AAPLx", USDC: 6000, Status: "passed", HoursAgo: 4*24 + 3, ExpiresHours: 24, Buy: true,
				Votes: []voteSpec{{"rowan", "yes"}, {"tess", "yes"}, {"hana", "yes"}, {"diego", "no"}}},
			{Key: "failed", Proposer: "diego", Symbol: "TSLAx", USDC: 2000, Status: "failed", HoursAgo: 3*24 + 5, ExpiresHours: 24,
				Votes: []voteSpec{{"diego", "yes"}, {"rowan", "no"}, {"tess", "no"}, {"hana", "no"}}},
			{Key: "open", Proposer: "tess", Symbol: "TSLAx", USDC: 1500, Status: "open", HoursAgo: 6, ExpiresHours: 24,
				Votes: []voteSpec{{"tess", "yes"}, {"marcus", "yes"}}},
		},
		BuySymbol: "AAPLx", BuyMint: jupiter.AAPLxMint, BuyCostPx: 0.9,
		SellFrac: 0.25, SellPx: 1.14, SellHours: 26, ChartDrift: 0.09,
	},
	{
		Key: "night-shift", Name: "Night Shift Traders",
		Creator: person{"kai", "Kai Brooks"},
		Members: []person{{"sofia", "Sofia Reyes"}, {"omar", "Omar Haddad"}, {"jules", "Jules Martin"}, {"nina", "Nina Patel"}, {"theo", "Theo Grant"}},
		Deposits: []depositSpec{
			{Who: "kai", USDC: 2000, HoursAgo: 7*24 + 1, SharePx: 1},
			{Who: "sofia", USDC: 1500, HoursAgo: 6*24 + 12, SharePx: 1},
			{Who: "omar", USDC: 2500, HoursAgo: 6 * 24, SharePx: 1},
			{Who: "jules", USDC: 800, HoursAgo: 5 * 24, SharePx: 1},
			{Who: "nina", USDC: 1100, HoursAgo: 2*24 + 4, SharePx: 0.95},
			{Who: "theo", USDC: 600, HoursAgo: 20, SharePx: 0.93},
		},
		Proposals: []proposalSpec{
			{Key: "buy", Proposer: "kai", Symbol: "TSLAx", USDC: 5000, Status: "passed", HoursAgo: 4 * 24, ExpiresHours: 12, Buy: true,
				Votes: []voteSpec{{"kai", "yes"}, {"omar", "yes"}, {"sofia", "yes"}}},
			{Key: "failed", Proposer: "jules", Symbol: "AAPLx", USDC: 1000, Status: "failed", HoursAgo: 2*24 + 10, ExpiresHours: 12,
				Votes: []voteSpec{{"jules", "yes"}, {"kai", "no"}, {"omar", "no"}, {"sofia", "no"}}},
			{Key: "open", Proposer: "omar", Symbol: "AAPLx", USDC: 800, Status: "open", HoursAgo: 3, ExpiresHours: 20,
				Votes: []voteSpec{{"omar", "yes"}}},
		},
		BuySymbol: "TSLAx", BuyMint: jupiter.TSLAxMint, BuyCostPx: 1.07,
		SellFrac: 0.3, SellPx: 0.9, SellHours: 30, ChartDrift: -0.06,
	},
	{
		Key: "harbor", Name: "Harbor Street Fund",
		Creator: person{"ava", "Ava Lindqvist"},
		Members: []person{{"ben", "Ben Carter"}, {"ivy", "Ivy Nakamura"}, {"leo", "Leo Santos"}, {"zoe", "Zoe Adler"}, {"sam", "Sam Okafor"}},
		Deposits: []depositSpec{
			{Who: "ava", USDC: 5000, HoursAgo: 7*24 + 3, SharePx: 1},
			{Who: "ben", USDC: 1000, HoursAgo: 6*24 + 2, SharePx: 1},
			{Who: "ivy", USDC: 2200, HoursAgo: 5*24 + 12, SharePx: 1},
			{Who: "leo", USDC: 1300, HoursAgo: 5 * 24, SharePx: 1},
			{Who: "zoe", USDC: 700, HoursAgo: 2 * 24, SharePx: 1.02},
			{Who: "sam", USDC: 1600, HoursAgo: 12, SharePx: 1.03},
		},
		Proposals: []proposalSpec{
			{Key: "buy", Proposer: "ava", Symbol: "AAPLx", USDC: 4500, Status: "passed", HoursAgo: 4*24 + 8, ExpiresHours: 24, Buy: true,
				Votes: []voteSpec{{"ava", "yes"}, {"ivy", "yes"}, {"leo", "yes"}, {"ben", "yes"}}},
			{Key: "failed", Proposer: "ben", Symbol: "TSLAx", USDC: 1200, Status: "failed", HoursAgo: 3 * 24, ExpiresHours: 24,
				Votes: []voteSpec{{"ben", "yes"}, {"ava", "no"}, {"ivy", "no"}, {"leo", "no"}}},
			{Key: "expired", Proposer: "leo", Symbol: "TSLAx", USDC: 500, Status: "expired", HoursAgo: 2*24 + 6, ExpiresHours: 24,
				Votes: []voteSpec{{"leo", "yes"}}},
			{Key: "open", Proposer: "ivy", Symbol: "TSLAx", USDC: 1000, Status: "open", HoursAgo: 8, ExpiresHours: 24,
				Votes: []voteSpec{{"ivy", "yes"}, {"zoe", "no"}}},
		},
		BuySymbol: "AAPLx", BuyMint: jupiter.AAPLxMint, BuyCostPx: 1.03, ChartDrift: 0.02,
	},
}

// fallbackMarkMicros are reference whole-share prices used when no live Pyth mark is available.
var fallbackMarkMicros = map[string]int64{
	"AAPLx": 230 * usdc,
	"TSLAx": 330 * usdc,
}

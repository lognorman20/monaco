// Package faker seeds demo/test data (#153) into a local database.
//
// Two profiles exist:
//   - mixed: ghost members (Maya Chen, Jordan Hale, Priya Shah) on the operator's REAL club.
//     Display-only positions, deposits, proposals, and votes. No member_wallets, no transactions.
//   - scale: six wholly fake clubs (groups.is_faker) with a dummy treasury, fake deposits,
//     a confirmed buy (real xStock mint + cost basis), an optional sell, failed/open/passed
//     proposals, votes, and a week of NAV snapshots.
//
// Every seeded row is flagged (users.is_faker / groups.is_faker) so product code keeps it away
// from Privy, Solana RPC, and Jupiter. Seeding is idempotent: users are keyed by privy_user_id,
// scale clubs by groups.faker_key, and each run replaces the faker-owned child rows.
// All timestamps are UTC and spread over the last week relative to the seed time.
package faker

import "github.com/monaco/monaco/apps/backend/internal/dex"

const usdc = int64(1_000_000)

// person is a seeded faker user. Photo marks the ghosts whose portraits the operator uploads
// under FAKER_PHOTO_BASE_URL (see docs/ops-profile-photos.md); everyone else gets initials.
type person struct {
	Slug  string
	Name  string
	Photo bool
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
	// Thesis is the proposer's reason, shown as "Why buy" on the proposal detail.
	Thesis string
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
	{Slug: "maya", Name: "Maya Chen", Photo: true},
	{Slug: "jordan", Name: "Jordan Hale", Photo: true},
	{Slug: "priya", Name: "Priya Shah", Photo: true},
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
		Votes:  []voteSpec{{"jordan", "yes"}, {"maya", "yes"}},
		Thesis: "Deliveries beat last quarter and the chart is basing. Small position before the call."},
	{Key: "passed", Proposer: "maya", Symbol: "AAPLx", USDC: 500, Status: "passed", HoursAgo: 48, ExpiresHours: 24,
		Votes:  []voteSpec{{"maya", "yes"}, {"jordan", "yes"}, {"priya", "yes"}},
		Thesis: "Buybacks keep shrinking the share count. A boring first holding for the pot."},
	{Key: "failed", Proposer: "priya", Symbol: "AAPLx", USDC: 600, Status: "failed", HoursAgo: 96, ExpiresHours: 24,
		Votes: []voteSpec{{"priya", "yes"}, {"maya", "no"}, {"jordan", "no"}}},
	{Key: "expired", Proposer: "jordan", Symbol: "AAPLx", USDC: 300, Status: "expired", HoursAgo: 120, ExpiresHours: 24,
		Votes: []voteSpec{{"priya", "yes"}}},
}

var scaleClubs = []clubSpec{
	{
		Key: "ridgewood", Name: "Ridgewood Value Club",
		Creator: person{Slug: "rowan", Name: "Rowan Ellis"},
		Members: []person{{Slug: "tess", Name: "Tess Morgan"}, {Slug: "diego", Name: "Diego Alvarez"}, {Slug: "hana", Name: "Hana Kim"}, {Slug: "marcus", Name: "Marcus Webb"}, {Slug: "lena", Name: "Lena Fischer"}},
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
				Votes:  []voteSpec{{"rowan", "yes"}, {"tess", "yes"}, {"hana", "yes"}, {"diego", "no"}},
				Thesis: "Cash pile, steady services growth, cheap versus its own history. Our core holding."},
			{Key: "failed", Proposer: "diego", Symbol: "TSLAx", USDC: 2000, Status: "failed", HoursAgo: 3*24 + 5, ExpiresHours: 24,
				Votes: []voteSpec{{"diego", "yes"}, {"rowan", "no"}, {"tess", "no"}, {"hana", "no"}}},
			{Key: "open", Proposer: "tess", Symbol: "TSLAx", USDC: 1500, Status: "open", HoursAgo: 6, ExpiresHours: 24,
				Votes:  []voteSpec{{"tess", "yes"}, {"marcus", "yes"}},
				Thesis: "Energy storage is the part nobody prices in. Small bet, we can add later."},
		},
		BuySymbol: "AAPLx", BuyMint: "0xb200000000000000000000c2e324d24d7eecd1fb", BuyCostPx: 0.9,
		SellFrac: 0.25, SellPx: 1.14, SellHours: 26, ChartDrift: 0.09,
	},
	{
		Key: "night-shift", Name: "Night Shift Traders",
		Creator: person{Slug: "kai", Name: "Kai Brooks"},
		Members: []person{{Slug: "sofia", Name: "Sofia Reyes"}, {Slug: "omar", Name: "Omar Haddad"}, {Slug: "jules", Name: "Jules Martin"}, {Slug: "nina", Name: "Nina Patel"}, {Slug: "theo", Name: "Theo Grant"}},
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
				Votes:  []voteSpec{{"kai", "yes"}, {"omar", "yes"}, {"sofia", "yes"}},
				Thesis: "Volatile, which is the point for this club. Sized so a bad week doesn't hurt."},
			{Key: "failed", Proposer: "jules", Symbol: "AAPLx", USDC: 1000, Status: "failed", HoursAgo: 2*24 + 10, ExpiresHours: 12,
				Votes: []voteSpec{{"jules", "yes"}, {"kai", "no"}, {"omar", "no"}, {"sofia", "no"}}},
			{Key: "open", Proposer: "omar", Symbol: "AAPLx", USDC: 800, Status: "open", HoursAgo: 3, ExpiresHours: 20,
				Votes:  []voteSpec{{"omar", "yes"}},
				Thesis: "Something calmer next to the Tesla position. Earnings are next week."},
		},
		BuySymbol: "TSLAx", BuyMint: "0xb2000000000000000000000000000000000004", BuyCostPx: 1.07,
		SellFrac: 0.3, SellPx: 0.9, SellHours: 30, ChartDrift: -0.06,
	},
	{
		Key: "harbor", Name: "Harbor Street Fund",
		Creator: person{Slug: "ava", Name: "Ava Lindqvist"},
		Members: []person{{Slug: "ben", Name: "Ben Carter"}, {Slug: "ivy", Name: "Ivy Nakamura"}, {Slug: "leo", Name: "Leo Santos"}, {Slug: "zoe", Name: "Zoe Adler"}, {Slug: "sam", Name: "Sam Okafor"}},
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
				Votes:  []voteSpec{{"ava", "yes"}, {"ivy", "yes"}, {"leo", "yes"}, {"ben", "yes"}},
				Thesis: "Everyone here already uses the products. Hold it and stop checking the price."},
			{Key: "failed", Proposer: "ben", Symbol: "TSLAx", USDC: 1200, Status: "failed", HoursAgo: 3 * 24, ExpiresHours: 24,
				Votes: []voteSpec{{"ben", "yes"}, {"ava", "no"}, {"ivy", "no"}, {"leo", "no"}}},
			{Key: "expired", Proposer: "leo", Symbol: "TSLAx", USDC: 500, Status: "expired", HoursAgo: 2*24 + 6, ExpiresHours: 24,
				Votes: []voteSpec{{"leo", "yes"}}},
			{Key: "open", Proposer: "ivy", Symbol: "TSLAx", USDC: 1000, Status: "open", HoursAgo: 8, ExpiresHours: 24,
				Votes:  []voteSpec{{"ivy", "yes"}, {"zoe", "no"}},
				Thesis: "Down a lot from the high. If the robotaxi news lands, we want to own some."},
		},
		BuySymbol: "AAPLx", BuyMint: "0xb200000000000000000000c2e324d24d7eecd1fb", BuyCostPx: 1.03, ChartDrift: 0.02,
	},
	{
		// Small, loud winners: most of the pot went into one buy well below today's mark.
		Key: "dorm-4b", Name: "Dorm 4B fund",
		Creator: person{Slug: "ellie", Name: "Ellie Novak"},
		Members: []person{{Slug: "raj", Name: "Raj Mehta"}, {Slug: "chloe", Name: "Chloe Dubois"}, {Slug: "noah", Name: "Noah Kim"}},
		Deposits: []depositSpec{
			{Who: "ellie", USDC: 400, HoursAgo: 7*24 + 4, SharePx: 1},
			{Who: "raj", USDC: 300, HoursAgo: 6 * 24, SharePx: 1},
			{Who: "chloe", USDC: 250, HoursAgo: 5*24 + 10, SharePx: 1},
			{Who: "noah", USDC: 200, HoursAgo: 4 * 24, SharePx: 1},
		},
		Proposals: []proposalSpec{
			{Key: "buy", Proposer: "ellie", Symbol: "TSLAx", USDC: 1100, Status: "passed", HoursAgo: 3*24 + 20, ExpiresHours: 24, Buy: true,
				Votes:  []voteSpec{{"ellie", "yes"}, {"raj", "yes"}, {"noah", "yes"}},
				Thesis: "All in on one name. We're students, we can afford to be wrong."},
			{Key: "open", Proposer: "chloe", Symbol: "AAPLx", USDC: 40, Status: "open", HoursAgo: 4, ExpiresHours: 24,
				Votes:  []voteSpec{{"chloe", "yes"}},
				Thesis: "Take a little off the table into something boring."},
		},
		BuySymbol: "TSLAx", BuyMint: "0xb2000000000000000000000000000000000004", BuyCostPx: 0.76, ChartDrift: 0.32,
	},
	{
		// Slightly underwater: bought near a local top.
		Key: "rent-money", Name: "Rent money",
		Creator: person{Slug: "mateo", Name: "Mateo Rossi"},
		Members: []person{{Slug: "grace", Name: "Grace Liu"}, {Slug: "felix", Name: "Felix Wagner"}, {Slug: "amara", Name: "Amara Obi"}},
		Deposits: []depositSpec{
			{Who: "mateo", USDC: 900, HoursAgo: 7*24 + 1, SharePx: 1},
			{Who: "grace", USDC: 700, HoursAgo: 6*24 + 8, SharePx: 1},
			{Who: "felix", USDC: 500, HoursAgo: 5 * 24, SharePx: 1},
			{Who: "amara", USDC: 600, HoursAgo: 3 * 24, SharePx: 1},
		},
		Proposals: []proposalSpec{
			{Key: "buy", Proposer: "grace", Symbol: "AAPLx", USDC: 2400, Status: "passed", HoursAgo: 2*24 + 12, ExpiresHours: 24, Buy: true,
				Votes:  []voteSpec{{"grace", "yes"}, {"mateo", "yes"}, {"amara", "yes"}, {"felix", "no"}},
				Thesis: "Safe enough to park rent money for a month. Famous last words."},
		},
		BuySymbol: "AAPLx", BuyMint: "0xb200000000000000000000c2e324d24d7eecd1fb", BuyCostPx: 1.045, ChartDrift: -0.04,
	},
	{
		// Flat and patient.
		Key: "index-huggers", Name: "Index huggers",
		Creator: person{Slug: "iris", Name: "Iris Holm"},
		Members: []person{{Slug: "owen", Name: "Owen Price"}, {Slug: "lucia", Name: "Lucia Moreno"}, {Slug: "dev", Name: "Dev Anand"}, {Slug: "maria", Name: "Maria Costa"}},
		Deposits: []depositSpec{
			{Who: "iris", USDC: 1500, HoursAgo: 7*24 + 6, SharePx: 1},
			{Who: "owen", USDC: 1000, HoursAgo: 6*24 + 2, SharePx: 1},
			{Who: "lucia", USDC: 800, HoursAgo: 5*24 + 3, SharePx: 1},
			{Who: "dev", USDC: 1200, HoursAgo: 4 * 24, SharePx: 1},
			{Who: "maria", USDC: 500, HoursAgo: 2 * 24, SharePx: 1},
		},
		Proposals: []proposalSpec{
			{Key: "buy", Proposer: "iris", Symbol: "AAPLx", USDC: 3000, Status: "passed", HoursAgo: 5 * 24, ExpiresHours: 24, Buy: true,
				Votes:  []voteSpec{{"iris", "yes"}, {"owen", "yes"}, {"dev", "yes"}},
				Thesis: "Closest thing to the index we can buy here. Then we leave it alone."},
		},
		BuySymbol: "AAPLx", BuyMint: "0xb200000000000000000000c2e324d24d7eecd1fb", BuyCostPx: 0.97, ChartDrift: 0.02,
	},
}

// messageSpec is one ghost chat message in the demo cabal.
type messageSpec struct {
	Who        string
	MinutesAgo float64
	Body       string
}

// demoMessages read as a friend group talking about the next buy. Spread over the last 90 minutes.
var demoMessages = []messageSpec{
	{Who: "maya", MinutesAgo: 88, Body: "Apple reports Thursday. Anyone want in before?"},
	{Who: "jordan", MinutesAgo: 74, Body: "Thinking $50 from the pot."},
	{Who: "priya", MinutesAgo: 61, Body: "Tesla instead? Or split it."},
	{Who: "maya", MinutesAgo: 43, Body: "Services revenue keeps compounding. I'm a yes."},
	{Who: "priya", MinutesAgo: 30, Body: "Rent is due Friday so I'm sitting this one out."},
	{Who: "jordan", MinutesAgo: 12, Body: "If it passes we're 40% cash. Fine by me."},
}

// commentSpec is one ghost comment. Parent names another comment's Key in the same thread.
type commentSpec struct {
	Key        string
	Who        string
	MinutesAgo float64
	Body       string
	Parent     string
}

// realProposalComments go on the operator's real (votable) proposal when -proposal-id is set.
var realProposalComments = []commentSpec{
	{Key: "why-not-split", Who: "maya", MinutesAgo: 25, Body: "Why not split with NVDA?"},
	{Key: "smaller-drawdown", Who: "jordan", MinutesAgo: 18, Parent: "why-not-split",
		Body: "Smaller drawdown for our first buy. Nvidia can be next."},
}

// ghostOpenProposalComments go on the ghost open proposal in the demo profile.
var ghostOpenProposalComments = []commentSpec{
	{Key: "under-400", Who: "priya", MinutesAgo: 140, Body: "I'm in if we keep it at $400 and not a dollar more."},
}

// fallbackMarkMicros are reference whole-share prices used when no live Pyth mark is available.
var fallbackMarkMicros = map[string]int64{
	"AAPLx": 230 * usdc,
	"TSLAx": 330 * usdc,
}

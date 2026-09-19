package faker

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
)

// ErrGroupNotFound means the mixed-profile target group does not exist.
var ErrGroupNotFound = errors.New("faker: group not found")

// ErrGroupIsFaker means the mixed profile was pointed at a faker scale club.
var ErrGroupIsFaker = errors.New("faker: mixed profile requires a real group")

// MarkSource returns a live whole-share mark in USDC micros for symbol, or ok=false.
// The server wires Pyth Hermes; nil or failures fall back to fixed reference marks.
type MarkSource func(ctx context.Context, symbol, mint string) (int64, bool)

// Seeder writes faker profiles. It only touches the database: no Privy, RPC, or Jupiter.
type Seeder struct {
	store *postgres.Store
	marks MarkSource
	now   func() time.Time
	// prefix namespaces faker keys and privy ids (tests use a per-lane prefix; production uses "").
	prefix string
}

// NewSeeder builds a seeder. marks may be nil.
func NewSeeder(store *postgres.Store, marks MarkSource) *Seeder {
	return &Seeder{store: store, marks: marks, now: time.Now}
}

// WithPrefix returns a copy whose faker keys and privy ids are namespaced (tests only).
func (s *Seeder) WithPrefix(prefix string) *Seeder {
	c := *s
	c.prefix = prefix
	return &c
}

// WithClock overrides the seed time (tests only).
func (s *Seeder) WithClock(now func() time.Time) *Seeder {
	c := *s
	c.now = now
	return &c
}

// MixedResult summarizes a mixed-profile run.
type MixedResult struct {
	GroupID     string   `json:"groupId"`
	UserIDs     []string `json:"fakerUserIds"`
	ProposalIDs []string `json:"proposalIds"`
}

// ScaleClub is one seeded scale club.
type ScaleClub struct {
	GroupID string   `json:"groupId"`
	Name    string   `json:"name"`
	UserIDs []string `json:"fakerUserIds"`
}

// ScaleResult summarizes a scale-profile run.
type ScaleResult struct {
	Clubs []ScaleClub `json:"clubs"`
}

func (s *Seeder) privyID(slug string) string { return "faker:user:" + s.prefix + slug }

func (s *Seeder) inTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.store.BeginTx(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("faker commit: %w", err)
	}
	return nil
}

// SeedMixed adds ghost members to a real group. Caller authorization (creator check) is the
// endpoint's job; the seeder refuses faker groups and missing groups.
func (s *Seeder) SeedMixed(ctx context.Context, groupID string) (MixedResult, error) {
	now := s.now().UTC()
	result := MixedResult{GroupID: groupID}
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var isFaker bool
		err := tx.QueryRowContext(ctx, `SELECT is_faker FROM groups WHERE id = $1 FOR UPDATE`, groupID).Scan(&isFaker)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrGroupNotFound
		}
		if err != nil {
			return fmt.Errorf("lock group: %w", err)
		}
		if isFaker {
			return ErrGroupIsFaker
		}

		ids, err := s.upsertPeople(ctx, tx, mixedPeople, now.Add(-7*24*time.Hour))
		if err != nil {
			return err
		}
		for _, p := range mixedPeople {
			result.UserIDs = append(result.UserIDs, ids[p.Slug])
		}
		if err := deleteFakerRowsInRealGroup(ctx, tx, groupID); err != nil {
			return err
		}

		joined := map[string]time.Time{}
		for _, d := range mixedDeposits {
			at := hoursAgo(now, d.HoursAgo)
			if t, ok := joined[d.Who]; !ok || at.Before(t) {
				joined[d.Who] = at.Add(-time.Hour)
			}
		}
		for _, p := range mixedPeople {
			if _, err := tx.ExecContext(ctx, `INSERT INTO group_members (group_id, user_id, joined_at) VALUES ($1, $2, $3)`, groupID, ids[p.Slug], joined[p.Slug]); err != nil {
				return fmt.Errorf("insert ghost member: %w", err)
			}
		}
		if err := insertDepositsAndPositions(ctx, tx, groupID, "mixed-"+groupID, ids, mixedDeposits, now); err != nil {
			return err
		}
		pids, err := insertProposals(ctx, tx, groupID, ids, mixedProposals, now)
		if err != nil {
			return err
		}
		for _, p := range mixedProposals {
			result.ProposalIDs = append(result.ProposalIDs, pids[p.Key])
		}
		return nil
	})
	return result, err
}

// SeedScale upserts the three wholly fake scale clubs.
func (s *Seeder) SeedScale(ctx context.Context) (ScaleResult, error) {
	now := s.now().UTC()
	var result ScaleResult
	for _, club := range scaleClubs {
		mark := s.referenceMark(ctx, club.BuySymbol, club.BuyMint)
		var seeded ScaleClub
		err := s.inTx(ctx, func(tx *sql.Tx) error {
			var err error
			seeded, err = s.seedClub(ctx, tx, club, mark, now)
			return err
		})
		if err != nil {
			return ScaleResult{}, fmt.Errorf("seed %s: %w", club.Key, err)
		}
		result.Clubs = append(result.Clubs, seeded)
	}
	return result, nil
}

func (s *Seeder) referenceMark(ctx context.Context, symbol, mint string) int64 {
	if s.marks != nil {
		if m, ok := s.marks(ctx, symbol, mint); ok && m > 0 {
			return m
		}
	}
	return fallbackMarkMicros[symbol]
}

func (s *Seeder) seedClub(ctx context.Context, tx *sql.Tx, club clubSpec, mark int64, now time.Time) (ScaleClub, error) {
	people := append([]person{club.Creator}, club.Members...)
	ids, err := s.upsertPeople(ctx, tx, people, hoursAgo(now, 8*24))
	if err != nil {
		return ScaleClub{}, err
	}

	fakerKey := "scale:" + s.prefix + club.Key
	createdAt := hoursAgo(now, club.Deposits[0].HoursAgo+1)
	var groupID string
	err = tx.QueryRowContext(ctx, `
INSERT INTO groups (name, creator_user_id, is_faker, faker_key, created_at)
VALUES ($1, $2, true, $3, $4)
ON CONFLICT (faker_key) WHERE faker_key IS NOT NULL DO UPDATE
  SET name = EXCLUDED.name, creator_user_id = EXCLUDED.creator_user_id, created_at = EXCLUDED.created_at
  WHERE groups.is_faker
RETURNING id`, club.Name, ids[club.Creator.Slug], fakerKey, createdAt).Scan(&groupID)
	if err != nil {
		return ScaleClub{}, fmt.Errorf("upsert faker group: %w", err)
	}
	if err := deleteFakerGroupChildren(ctx, tx, groupID); err != nil {
		return ScaleClub{}, err
	}

	// Dummy treasury: schema needs one, but it is never a Privy wallet and never a FAKE* address.
	if _, err := tx.ExecContext(ctx, `INSERT INTO treasuries (group_id, privy_wallet_id, solana_address, created_at) VALUES ($1, $2, $3, $4)`,
		groupID, "faker:treasury:"+s.prefix+club.Key, "faker-treasury-"+s.prefix+club.Key, createdAt); err != nil {
		return ScaleClub{}, fmt.Errorf("insert dummy treasury: %w", err)
	}

	firstDeposit := map[string]float64{}
	for _, d := range club.Deposits {
		if h, ok := firstDeposit[d.Who]; !ok || d.HoursAgo > h {
			firstDeposit[d.Who] = d.HoursAgo
		}
	}
	out := ScaleClub{GroupID: groupID, Name: club.Name}
	for _, p := range people {
		out.UserIDs = append(out.UserIDs, ids[p.Slug])
		if _, err := tx.ExecContext(ctx, `INSERT INTO group_members (group_id, user_id, joined_at) VALUES ($1, $2, $3)`,
			groupID, ids[p.Slug], hoursAgo(now, firstDeposit[p.Slug]+0.5)); err != nil {
			return ScaleClub{}, fmt.Errorf("insert faker member: %w", err)
		}
	}
	if err := insertDepositsAndPositions(ctx, tx, groupID, s.prefix+club.Key, ids, club.Deposits, now); err != nil {
		return ScaleClub{}, err
	}
	pids, err := insertProposals(ctx, tx, groupID, ids, club.Proposals, now)
	if err != nil {
		return ScaleClub{}, err
	}
	if err := insertSwaps(ctx, tx, groupID, s.prefix+club.Key, club, ids, pids, mark, now); err != nil {
		return ScaleClub{}, err
	}
	if err := insertNavSnapshots(ctx, tx, groupID, club, now); err != nil {
		return ScaleClub{}, err
	}
	return out, nil
}

// upsertPeople inserts or refreshes faker users. It never converts a real user: a privy id
// collision with a non-faker row fails the run.
func (s *Seeder) upsertPeople(ctx context.Context, tx *sql.Tx, people []person, createdAt time.Time) (map[string]string, error) {
	ids := make(map[string]string, len(people))
	for _, p := range people {
		var id string
		err := tx.QueryRowContext(ctx, `
INSERT INTO users (privy_user_id, display_name, is_faker, created_at)
VALUES ($1, $2, true, $3)
ON CONFLICT (privy_user_id) DO UPDATE SET display_name = EXCLUDED.display_name
  WHERE users.is_faker
RETURNING id`, s.privyID(p.Slug), p.Name, createdAt).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("faker: privy id %s belongs to a real user", s.privyID(p.Slug))
		}
		if err != nil {
			return nil, fmt.Errorf("upsert faker user %s: %w", p.Slug, err)
		}
		ids[p.Slug] = id
	}
	return ids, nil
}

// deleteFakerRowsInRealGroup removes only faker-owned rows from a real group (mixed re-run).
func deleteFakerRowsInRealGroup(ctx context.Context, tx *sql.Tx, groupID string) error {
	steps := []string{
		`DELETE FROM votes v USING proposals p, users u
		   WHERE v.proposal_id = p.id AND p.group_id = $1 AND u.id = v.voter_id AND u.is_faker`,
		`DELETE FROM votes v USING proposals p, users u
		   WHERE v.proposal_id = p.id AND p.group_id = $1 AND u.id = p.proposer_id AND u.is_faker`,
		`DELETE FROM proposals p USING users u WHERE p.group_id = $1 AND u.id = p.proposer_id AND u.is_faker
		   AND NOT EXISTS (SELECT 1 FROM transactions t WHERE t.proposal_id = p.id)`,
		`DELETE FROM deposits d USING users u WHERE d.group_id = $1 AND u.id = d.user_id AND u.is_faker`,
		`DELETE FROM positions p USING users u WHERE p.group_id = $1 AND u.id = p.user_id AND u.is_faker`,
		`DELETE FROM group_voters gv USING users u WHERE gv.group_id = $1 AND u.id = gv.user_id AND u.is_faker`,
		`DELETE FROM group_members gm USING users u WHERE gm.group_id = $1 AND u.id = gm.user_id AND u.is_faker`,
	}
	for _, stmt := range steps {
		if _, err := tx.ExecContext(ctx, stmt, groupID); err != nil {
			return fmt.Errorf("reset ghost rows: %w", err)
		}
	}
	return nil
}

// deleteFakerGroupChildren clears a faker club before re-seeding. Guarded on is_faker.
func deleteFakerGroupChildren(ctx context.Context, tx *sql.Tx, groupID string) error {
	var isFaker bool
	if err := tx.QueryRowContext(ctx, `SELECT is_faker FROM groups WHERE id = $1`, groupID).Scan(&isFaker); err != nil {
		return fmt.Errorf("check faker group: %w", err)
	}
	if !isFaker {
		return fmt.Errorf("faker: refusing to reset real group %s", groupID)
	}
	steps := []string{
		`DELETE FROM agent_intents WHERE group_id = $1`,
		`DELETE FROM votes WHERE proposal_id IN (SELECT id FROM proposals WHERE group_id = $1)`,
		`DELETE FROM transactions WHERE group_id = $1`,
		`DELETE FROM group_agents WHERE group_id = $1`,
		`DELETE FROM proposals WHERE group_id = $1`,
		`DELETE FROM redeem_jobs WHERE group_id = $1`,
		`DELETE FROM payout_proofs WHERE group_id = $1`,
		`DELETE FROM nav_snapshots WHERE group_id = $1`,
		`DELETE FROM group_join_requests WHERE group_id = $1`,
		`DELETE FROM group_voters WHERE group_id = $1`,
		`DELETE FROM group_members WHERE group_id = $1`,
		`DELETE FROM positions WHERE group_id = $1`,
		`DELETE FROM deposits WHERE group_id = $1`,
		`DELETE FROM withdrawals WHERE group_id = $1`,
		`DELETE FROM treasuries WHERE group_id = $1`,
	}
	for _, stmt := range steps {
		if _, err := tx.ExecContext(ctx, stmt, groupID); err != nil {
			return fmt.Errorf("reset faker group: %w", err)
		}
	}
	return nil
}

func insertDepositsAndPositions(ctx context.Context, tx *sql.Tx, groupID, sigKey string, ids map[string]string, deposits []depositSpec, now time.Time) error {
	type pos struct{ shares, deposited int64 }
	positions := map[string]*pos{}
	var order []string
	for i, d := range deposits {
		status := d.Status
		if status == "" {
			status = "confirmed"
		}
		var sig any
		if status == "confirmed" {
			sig = fmt.Sprintf("faker-%s-dep-%d", sigKey, i+1)
		}
		amount := d.USDC * usdc
		if _, err := tx.ExecContext(ctx, `
INSERT INTO deposits (user_id, group_id, amount, from_address, status, tx_signature, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)`, ids[d.Who], groupID, amount, "faker-wallet-"+d.Who, status, sig, hoursAgo(now, d.HoursAgo)); err != nil {
			return fmt.Errorf("insert faker deposit: %w", err)
		}
		if status != "confirmed" {
			continue
		}
		p, ok := positions[d.Who]
		if !ok {
			p = &pos{}
			positions[d.Who] = p
			order = append(order, d.Who)
		}
		px := d.SharePx
		if px <= 0 {
			px = 1
		}
		p.shares += int64(math.Round(float64(amount) / px))
		p.deposited += amount
	}
	// Exact values (not additive), so re-runs never compound.
	for _, who := range order {
		p := positions[who]
		if _, err := tx.ExecContext(ctx, `
INSERT INTO positions (user_id, group_id, share_units, amount_deposited, amount_withdrawn)
VALUES ($1, $2, $3, $4, 0)
ON CONFLICT (user_id, group_id) DO UPDATE
  SET share_units = EXCLUDED.share_units, amount_deposited = EXCLUDED.amount_deposited, amount_withdrawn = 0`,
			ids[who], groupID, p.shares, p.deposited); err != nil {
			return fmt.Errorf("upsert faker position: %w", err)
		}
	}
	return nil
}

func insertProposals(ctx context.Context, tx *sql.Tx, groupID string, ids map[string]string, proposals []proposalSpec, now time.Time) (map[string]string, error) {
	out := make(map[string]string, len(proposals))
	for _, p := range proposals {
		created := hoursAgo(now, p.HoursAgo)
		expires := created.Add(time.Duration(p.ExpiresHours * float64(time.Hour)))
		var id string
		if err := tx.QueryRowContext(ctx, `
INSERT INTO proposals (group_id, proposer_id, symbol, kind, usdc_micros, status, expires_at, created_at)
VALUES ($1, $2, $3, 'buy', $4, $5, $6, $7) RETURNING id`,
			groupID, ids[p.Proposer], p.Symbol, p.USDC*usdc, p.Status, expires, created).Scan(&id); err != nil {
			return nil, fmt.Errorf("insert faker proposal: %w", err)
		}
		out[p.Key] = id
		if err := insertVotes(ctx, tx, id, ids, p.Votes, created); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func insertVotes(ctx context.Context, tx *sql.Tx, proposalID string, ids map[string]string, votes []voteSpec, created time.Time) error {
	for i, v := range votes {
		castAt := created.Add(time.Duration(i+1) * 37 * time.Minute)
		if _, err := tx.ExecContext(ctx, `INSERT INTO votes (proposal_id, voter_id, choice, cast_at) VALUES ($1, $2, $3, $4)`,
			proposalID, ids[v.Who], v.Choice, castAt); err != nil {
			return fmt.Errorf("insert faker vote: %w", err)
		}
	}
	return nil
}

// tokenAtomics converts USDC spent at a whole-share price into xStock SPL atomics
// (8 decimals, jupiter.XStockAtomicScale per whole share), matching real Jupiter fills.
func tokenAtomics(usdcMicros, pxMicros int64) int64 {
	if pxMicros <= 0 {
		return 0
	}
	return usdcMicros * jupiter.XStockAtomicScale / pxMicros
}

func insertSwaps(ctx context.Context, tx *sql.Tx, groupID, sigKey string, club clubSpec, ids map[string]string, pids map[string]string, mark int64, now time.Time) error {
	var buy *proposalSpec
	for i := range club.Proposals {
		if club.Proposals[i].Buy {
			buy = &club.Proposals[i]
		}
	}
	if buy == nil {
		return nil
	}
	costPx := int64(math.Round(float64(mark) * club.BuyCostPx))
	spent := buy.USDC * usdc
	tokens := tokenAtomics(spent, costPx)
	confirmed := hoursAgo(now, buy.HoursAgo-2)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO transactions (group_id, proposal_id, amount, action, input_mint, output_mint, status,
                          tx_signature, execute_request_id, cost_basis_price, cost_basis_amount, created_at, confirmed_at)
VALUES ($1, $2, $3, 'buy', $4, $5, 'confirmed', $6, $7, $3, $8, $9, $9)`,
		groupID, pids[buy.Key], spent, jupiter.USDCMint, club.BuyMint,
		"faker-"+sigKey+"-buy", "faker-"+sigKey+"-buy-req", tokens, confirmed); err != nil {
		return fmt.Errorf("insert faker buy: %w", err)
	}
	if club.SellFrac <= 0 {
		return nil
	}
	sold := int64(float64(tokens) * club.SellFrac)
	proceeds := int64(math.Round(float64(sold) * float64(costPx) * club.SellPx / float64(jupiter.XStockAtomicScale)))
	soldAt := hoursAgo(now, club.SellHours)

	// Governed sell (main's proposal kind model): passed sell proposal carrying token_amount,
	// linked to the confirmed sell transaction.
	sellCreated := hoursAgo(now, club.SellHours+3)
	var sellProposalID string
	if err := tx.QueryRowContext(ctx, `
INSERT INTO proposals (group_id, proposer_id, symbol, kind, token_amount, status, expires_at, created_at)
VALUES ($1, $2, $3, 'sell', $4, 'passed', $5, $6) RETURNING id`,
		groupID, ids[club.Creator.Slug], club.BuySymbol, sold, sellCreated.Add(12*time.Hour), sellCreated).Scan(&sellProposalID); err != nil {
		return fmt.Errorf("insert faker sell proposal: %w", err)
	}
	var sellVotes []voteSpec
	for _, v := range buy.Votes {
		sellVotes = append(sellVotes, voteSpec{Who: v.Who, Choice: "yes"})
	}
	if err := insertVotes(ctx, tx, sellProposalID, ids, sellVotes, sellCreated); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO transactions (group_id, proposal_id, amount, action, input_mint, output_mint, status,
                          tx_signature, execute_request_id, cost_basis_amount, created_at, confirmed_at)
VALUES ($1, $2, $3, 'sell', $4, $5, 'confirmed', $6, $7, $8, $9, $9)`,
		groupID, sellProposalID, sold, club.BuyMint, jupiter.USDCMint,
		"faker-"+sigKey+"-sell", "faker-"+sigKey+"-sell-req", proceeds, soldAt); err != nil {
		return fmt.Errorf("insert faker sell: %w", err)
	}
	return nil
}

// insertNavSnapshots writes a week of synthetic NAV points (every 12h plus event points) so
// P&L charts and ranged leaderboards have history. Each point records the net USDC in at
// that instant, as the app's own snapshots do, so the Groups tab P&L series is exact.
func insertNavSnapshots(ctx context.Context, tx *sql.Tx, groupID string, club clubSpec, now time.Time) error {
	type ev struct {
		at             time.Time
		amount, shares int64
	}
	var events []ev
	for _, d := range club.Deposits {
		if d.Status != "" {
			continue
		}
		px := d.SharePx
		if px <= 0 {
			px = 1
		}
		amount := d.USDC * usdc
		events = append(events, ev{hoursAgo(now, d.HoursAgo), amount, int64(math.Round(float64(amount) / px))})
	}
	sort.Slice(events, func(i, j int) bool { return events[i].at.Before(events[j].at) })
	if len(events) == 0 {
		return nil
	}
	start := events[0].at
	span := now.Sub(start)
	var points []time.Time
	for t := start.Add(time.Minute); t.Before(now.Add(-20 * time.Minute)); t = t.Add(12 * time.Hour) {
		points = append(points, t)
	}
	for _, e := range events[1:] {
		points = append(points, e.at.Add(time.Minute))
	}
	points = append(points, now.Add(-45*time.Minute), now.Add(-10*time.Minute))
	sort.Slice(points, func(i, j int) bool { return points[i].Before(points[j]) })

	for i, at := range points {
		var netIn, shares int64
		for _, e := range events {
			if !e.at.After(at) {
				netIn += e.amount
				shares += e.shares
			}
		}
		if shares == 0 {
			continue
		}
		progress := float64(at.Sub(start)) / float64(span)
		wiggle := 0.012 * math.Sin(float64(i)*1.7)
		pot := int64(float64(netIn) * (1 + club.ChartDrift*progress + wiggle*progress))
		navPerShare := pot * 1_000_000 / shares
		reason := "deposit"
		if i%3 == 2 {
			reason = "transaction_confirm"
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO nav_snapshots (group_id, pot_nav_micros, nav_per_share_micros, total_shares, reason, net_contributed_micros, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)`, groupID, pot, navPerShare, shares, reason, netIn, at); err != nil {
			return fmt.Errorf("insert faker nav snapshot: %w", err)
		}
	}
	return nil
}

func hoursAgo(now time.Time, h float64) time.Time {
	return now.Add(-time.Duration(h * float64(time.Hour))).UTC()
}

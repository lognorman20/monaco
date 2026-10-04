package trading_test

import (
	"bytes"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/monaco/monaco/apps/backend/internal/modules/trading/domain"
	"github.com/monaco/monaco/apps/backend/internal/modules/trading/sqlc"
	"github.com/monaco/monaco/apps/backend/internal/testkit"
)

const (
	usdcMint  = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	aaplxMint = "XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp"
	tslaxMint = "XsDoVfqeBukxuZHWhdvWHBhgEHjGNst4MLodqsJHzoB"
	uniqueErr = "23505"
	checkErr  = "23514"
)

type swapDB struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
	ids  *testkit.IDs
	now  time.Time
}

func newSwapDB(t *testing.T) swapDB {
	t.Helper()
	pool := testkit.DB(t)
	return swapDB{
		pool: pool,
		q:    sqlc.New(pool),
		ids:  testkit.NewIDs(1),
		now:  time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
	}
}

func (d swapDB) created(source uuid.UUID, inMint string) sqlc.InsertCreatedParams {
	return sqlc.InsertCreatedParams{
		ID: d.ids.NewV7(), SourceKind: "proposal", SourceID: source, CabalID: d.ids.NewV7(),
		TreasuryAddress: "9xQeWvG816bUx9EPjHmaT23yvVM2ZWbrrpZb9PusVFin", Action: "buy", Symbol: "AAPLx",
		InMint: inMint, OutMint: aaplxMint, OutDecimals: 8, InAmount: 25_000_000,
		QuoteOutAmount: pgtype.Int8{Int64: 105_000_000, Valid: true}, SlippageBps: 100, SourceBatchSize: 1,
		CreatedAt: d.now,
	}
}

func (d swapDB) insert(t *testing.T, p sqlc.InsertCreatedParams) {
	t.Helper()
	if err := d.q.InsertCreated(t.Context(), p); err != nil {
		t.Fatal(err)
	}
}

func (d swapDB) submit(t *testing.T, id uuid.UUID, requestID, sig string) int64 {
	t.Helper()
	n, err := d.q.MarkSubmitted(t.Context(), sqlc.MarkSubmittedParams{
		ID: id, ExecuteRequestID: requestID, SignedTx: []byte{1, 2, 3}, TxSignature: sig, SubmittedAt: d.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (d swapDB) confirm(t *testing.T, id uuid.UUID) int64 {
	t.Helper()
	n, err := d.q.FinishConfirmed(t.Context(), sqlc.FinishConfirmedParams{
		ID: id, OutAmount: 104_900_000, FeeMicros: 5_000, ConfirmedAt: d.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (d swapDB) fail(t *testing.T, id uuid.UUID, code string) int64 {
	t.Helper()
	n, err := d.q.FinishFailed(t.Context(), sqlc.FinishFailedParams{ID: id, FailureCode: code, FailedAt: d.now})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (d swapDB) status(t *testing.T, id uuid.UUID) string {
	t.Helper()
	var s string
	if err := d.pool.QueryRow(t.Context(), `SELECT status FROM swaps WHERE id = $1`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func wantUniqueViolation(t *testing.T, err error, index string) {
	t.Helper()
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != uniqueErr || pg.ConstraintName != index {
		t.Fatalf("err = %v, want a unique violation on %s", err, index)
	}
}

func TestSwaps_oneLiveRowPerSourceAndMintUntilItFails(t *testing.T) {
	t.Parallel()
	d := newSwapDB(t)
	source := d.ids.NewV7()
	first := d.created(source, usdcMint)
	d.insert(t, first)
	wantUniqueViolation(t, d.q.InsertCreated(t.Context(), d.created(source, usdcMint)), "swaps_live_source_idx")
	d.insert(t, d.created(source, tslaxMint))
	if n := d.fail(t, first.ID, "never_submitted"); n != 1 {
		t.Fatalf("FinishFailed on the created row changed %d rows, want 1", n)
	}
	retry := d.created(source, usdcMint)
	d.insert(t, retry)
	if got := d.status(t, retry.ID); got != "created" {
		t.Fatalf("retry status = %s, want created", got)
	}
}

func TestSwaps_liveIndexCoversSubmittedAndConfirmedRows(t *testing.T) {
	t.Parallel()
	d := newSwapDB(t)
	source := d.ids.NewV7()
	row := d.created(source, usdcMint)
	d.insert(t, row)
	d.submit(t, row.ID, "req-1", "sig-1")
	wantUniqueViolation(t, d.q.InsertCreated(t.Context(), d.created(source, usdcMint)), "swaps_live_source_idx")
	d.confirm(t, row.ID)
	wantUniqueViolation(t, d.q.InsertCreated(t.Context(), d.created(source, usdcMint)), "swaps_live_source_idx")
}

func TestSwaps_requestIDAndSignatureAreUnique(t *testing.T) {
	t.Parallel()
	d := newSwapDB(t)
	a, b, c := d.created(
		d.ids.NewV7(),
		usdcMint,
	), d.created(
		d.ids.NewV7(),
		usdcMint,
	), d.created(
		d.ids.NewV7(),
		usdcMint,
	)
	for _, p := range []sqlc.InsertCreatedParams{a, b, c} {
		d.insert(t, p)
	}
	d.submit(t, a.ID, "req-1", "sig-1")
	_, err := d.q.MarkSubmitted(t.Context(), sqlc.MarkSubmittedParams{
		ID: b.ID, ExecuteRequestID: "req-1", SignedTx: []byte{1}, TxSignature: "sig-2", SubmittedAt: d.now,
	})
	wantUniqueViolation(t, err, "swaps_execute_request_id_key")
	_, err = d.q.MarkSubmitted(t.Context(), sqlc.MarkSubmittedParams{
		ID: c.ID, ExecuteRequestID: "req-3", SignedTx: []byte{1}, TxSignature: "sig-1", SubmittedAt: d.now,
	})
	wantUniqueViolation(t, err, "swaps_tx_signature_key")
}

func TestSwaps_guardedUpdatesMoveOnlyFromTheExpectedStatus(t *testing.T) {
	t.Parallel()
	d := newSwapDB(t)
	row := d.created(d.ids.NewV7(), usdcMint)
	d.insert(t, row)
	if n := d.confirm(t, row.ID); n != 0 {
		t.Fatalf("FinishConfirmed on a created row changed %d rows, want 0", n)
	}
	if n := d.fail(t, row.ID, "jupiter_failed"); n != 0 {
		t.Fatalf("FinishFailed(jupiter_failed) on a created row changed %d rows, want 0", n)
	}
	if n := d.submit(t, row.ID, "req-1", "sig-1"); n != 1 {
		t.Fatalf("MarkSubmitted on a created row changed %d rows, want 1", n)
	}
	if n := d.submit(t, row.ID, "req-2", "sig-2"); n != 0 {
		t.Fatalf("MarkSubmitted on a submitted row changed %d rows, want 0", n)
	}
	if n := d.fail(t, row.ID, "never_submitted"); n != 0 {
		t.Fatalf("FinishFailed(never_submitted) on a submitted row changed %d rows, want 0", n)
	}
	if n := d.fail(t, row.ID, "blockhash_expired"); n != 1 {
		t.Fatalf("FinishFailed(blockhash_expired) on a submitted row changed %d rows, want 1", n)
	}
	for name, n := range map[string]int64{
		"MarkSubmitted":   d.submit(t, row.ID, "req-3", "sig-3"),
		"FinishConfirmed": d.confirm(t, row.ID),
		"FinishFailed":    d.fail(t, row.ID, "force_resolved"),
	} {
		if n != 0 {
			t.Errorf("%s on a failed row changed %d rows, want 0", name, n)
		}
	}
	if got := d.status(t, row.ID); got != "failed" {
		t.Fatalf("status = %s, want failed", got)
	}
}

func TestSwaps_confirmedAcceptsNothing(t *testing.T) {
	t.Parallel()
	d := newSwapDB(t)
	row := d.created(d.ids.NewV7(), usdcMint)
	d.insert(t, row)
	d.submit(t, row.ID, "req-1", "sig-1")
	if n := d.confirm(t, row.ID); n != 1 {
		t.Fatalf("FinishConfirmed on a submitted row changed %d rows, want 1", n)
	}
	for name, n := range map[string]int64{
		"MarkSubmitted":   d.submit(t, row.ID, "req-2", "sig-2"),
		"FinishConfirmed": d.confirm(t, row.ID),
		"FinishFailed":    d.fail(t, row.ID, "blockhash_expired"),
	} {
		if n != 0 {
			t.Errorf("%s on a confirmed row changed %d rows, want 0", name, n)
		}
	}
	var out, fee int64
	var confirmedAt time.Time
	if err := d.pool.QueryRow(t.Context(), `SELECT out_amount, fee_micros, confirmed_at FROM swaps WHERE id = $1`,
		row.ID).Scan(&out, &fee, &confirmedAt); err != nil {
		t.Fatal(err)
	}
	if out != 104_900_000 || fee != 5_000 || !confirmedAt.Equal(d.now) {
		t.Fatalf("fill = %d out, %d fee, confirmed %s", out, fee, confirmedAt)
	}
}

func TestSwaps_claimLiveFindsTheLiveRowOnly(t *testing.T) {
	t.Parallel()
	d := newSwapDB(t)
	source := d.ids.NewV7()
	row := d.created(source, usdcMint)
	d.insert(t, row)
	claim := sqlc.ClaimLiveParams{SourceKind: "proposal", SourceID: source, InMint: usdcMint}
	got, err := d.q.ClaimLive(t.Context(), claim)
	if err != nil || got != (sqlc.ClaimLiveRow{ID: row.ID, Status: "created"}) {
		t.Fatalf("ClaimLive = %+v, %v, want the created row", got, err)
	}
	other := claim
	other.SourceKind = "cashout"
	if _, err := d.q.ClaimLive(t.Context(), other); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("ClaimLive for another source kind err = %v, want no rows", err)
	}
	d.fail(t, row.ID, "never_submitted")
	if _, err := d.q.ClaimLive(t.Context(), claim); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("ClaimLive after the row failed err = %v, want no rows", err)
	}
}

func TestSwaps_markSubmittedStoresTheSignedBytesBeforeAnySend(t *testing.T) {
	t.Parallel()
	d := newSwapDB(t)
	row := d.created(d.ids.NewV7(), usdcMint)
	d.insert(t, row)
	if n := d.submit(t, row.ID, "req-1", "sig-1"); n != 1 {
		t.Fatalf("MarkSubmitted changed %d rows, want 1", n)
	}
	var signed []byte
	var requestID, sig string
	var submittedAt time.Time
	if err := d.pool.QueryRow(t.Context(),
		`SELECT signed_tx, execute_request_id, tx_signature, submitted_at FROM swaps WHERE id = $1`,
		row.ID).Scan(&signed, &requestID, &sig, &submittedAt); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(signed, []byte{1, 2, 3}) || requestID != "req-1" || sig != "sig-1" || !submittedAt.Equal(d.now) {
		t.Fatalf("stored %v, %q, %q, %s; want the submitted payload", signed, requestID, sig, submittedAt)
	}
}

func TestSwaps_markSubmittedRefusesAnEmptySignedTx(t *testing.T) {
	t.Parallel()
	d := newSwapDB(t)
	row := d.created(d.ids.NewV7(), usdcMint)
	d.insert(t, row)
	for name, signed := range map[string][]byte{"nil": nil, "empty": {}} {
		n, err := d.q.MarkSubmitted(t.Context(), sqlc.MarkSubmittedParams{
			ID: row.ID, ExecuteRequestID: "req-1", SignedTx: signed, TxSignature: "sig-1", SubmittedAt: d.now,
		})
		if err != nil || n != 0 {
			t.Errorf("MarkSubmitted with a %s signed_tx = %d rows, %v, want 0 rows", name, n, err)
		}
	}
	if got := d.status(t, row.ID); got != "created" {
		t.Fatalf("status = %s, want created", got)
	}
}

func TestSwaps_checkConstraintsRefuseValuesTheDomainDoesNotDefine(t *testing.T) {
	t.Parallel()
	d := newSwapDB(t)
	row := d.created(d.ids.NewV7(), usdcMint)
	d.insert(t, row)
	for constraint, set := range map[string]string{
		"swaps_status_check":       `status = 'lost'`,
		"swaps_source_kind_check":  `source_kind = 'unplanned_kind'`,
		"swaps_failure_code_check": `failure_code = 'gremlins'`,
	} {
		_, err := d.pool.Exec(t.Context(), `UPDATE swaps SET `+set+` WHERE id = $1`, row.ID)
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != checkErr || pg.ConstraintName != constraint {
			t.Errorf("UPDATE SET %s err = %v, want a check violation on %s", set, err, constraint)
		}
	}
	for _, code := range domain.FailureCodes() {
		_, err := d.pool.Exec(t.Context(), `UPDATE swaps SET failure_code = $2 WHERE id = $1`, row.ID, string(code))
		if err != nil {
			t.Errorf("UPDATE SET failure_code = %s err = %v, want it accepted", code, err)
		}
	}
}

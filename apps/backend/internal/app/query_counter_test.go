package app

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

// countingDB is a test database that counts every statement the store actually sends
// to Postgres.
//
// An N+1 is a claim about round trips, so the test that pins it has to count round
// trips rather than count calls to a hand-written fake that could drift from the SQL.
// The counter sits in a driver.Connector wrapper, below database/sql and above pgx, so
// nothing in the store or the service can get a statement past it.
type countingDB struct {
	*sql.DB
	statements *atomic.Int64
	log        *statementLog
}

// count returns statements sent so far.
func (c *countingDB) count() int64 { return c.statements.Load() }

// since returns statements sent since mark.
func (c *countingDB) since(mark int64) int64 { return c.statements.Load() - mark }

// statementLog records the SQL sent, so a test can say which read scales with the
// cabal count rather than only that the total moved.
type statementLog struct {
	mu   sync.Mutex
	sql  []string
	keep bool
}

func (l *statementLog) record(query string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	if l.keep {
		l.sql = append(l.sql, query)
	}
	l.mu.Unlock()
}

// start begins recording and returns a stop function yielding what was recorded.
func (l *statementLog) start() func() []string {
	l.mu.Lock()
	l.keep = true
	l.sql = nil
	l.mu.Unlock()
	return func() []string {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.keep = false
		out := make([]string, len(l.sql))
		copy(out, l.sql)
		return out
	}
}

// firstLineOf names a statement by its first meaningful line, for readable output.
func firstLineOf(query string) string {
	for _, line := range strings.Split(query, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return "(empty)"
}

// countStatements tallies recorded SQL by its opening line.
func countStatements(recorded []string) map[string]int {
	counts := make(map[string]int, len(recorded))
	for _, query := range recorded {
		counts[firstLineOf(query)]++
	}
	return counts
}

// formatStatementCounts renders a tally for a failure message, so a regression names
// the query that started repeating.
func formatStatementCounts(recorded []string) string {
	counts := countStatements(recorded)
	lines := make([]string, 0, len(counts))
	for statement, sent := range counts {
		lines = append(lines, fmt.Sprintf("  %3d  %s", sent, statement))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// openCountingTestDB opens a second connection to the same test database as
// postgres.OpenTestDB, through a counting connector. The caller must have opened the
// plain test DB first: that is what creates and migrates the database.
func openCountingTestDB(t *testing.T) *countingDB {
	t.Helper()

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Fatalf("DATABASE_URL is not set")
	}
	testURL, err := postgres.DeriveTestDatabaseURL(databaseURL)
	if err != nil {
		t.Fatalf("derive test database url: %v", err)
	}

	statements := &atomic.Int64{}
	log := &statementLog{}
	db := sql.OpenDB(&countingConnector{
		dsn:        testURL,
		base:       stdlib.GetDefaultDriver(),
		statements: statements,
		log:        log,
	})
	// One connection: a query count is only comparable across runs if the pool is not
	// also opening connections (and running pgx's own startup statements) under it.
	db.SetMaxOpenConns(1)
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		t.Fatalf("ping counting test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// The ping itself is not a statement the store sent.
	statements.Store(0)
	return &countingDB{DB: db, statements: statements, log: log}
}

type countingConnector struct {
	dsn        string
	base       driver.Driver
	statements *atomic.Int64
	log        *statementLog
}

func (c *countingConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := c.base.Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return &countingConn{base: conn, statements: c.statements, log: c.log}, nil
}

func (c *countingConnector) Driver() driver.Driver { return c.base }

type countingConn struct {
	base       driver.Conn
	statements *atomic.Int64
	log        *statementLog
}

var (
	_ driver.Conn               = (*countingConn)(nil)
	_ driver.ConnPrepareContext = (*countingConn)(nil)
	_ driver.ConnBeginTx        = (*countingConn)(nil)
	_ driver.ExecerContext      = (*countingConn)(nil)
	_ driver.QueryerContext     = (*countingConn)(nil)
	_ driver.Pinger             = (*countingConn)(nil)
	_ driver.NamedValueChecker  = (*countingConn)(nil)
	_ driver.SessionResetter    = (*countingConn)(nil)
)

func (c *countingConn) Prepare(query string) (driver.Stmt, error) {
	stmt, err := c.base.Prepare(query)
	if err != nil {
		return nil, err
	}
	return &countingStmt{base: stmt, query: query, statements: c.statements, log: c.log}, nil
}

func (c *countingConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	stmt, err := c.base.(driver.ConnPrepareContext).PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	return &countingStmt{base: stmt, query: query, statements: c.statements, log: c.log}, nil
}

func (c *countingConn) Close() error { return c.base.Close() }

func (c *countingConn) Begin() (driver.Tx, error) { return c.base.Begin() }

func (c *countingConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return c.base.(driver.ConnBeginTx).BeginTx(ctx, opts)
}

func (c *countingConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.statements.Add(1)
	c.log.record(query)
	return c.base.(driver.ExecerContext).ExecContext(ctx, query, args)
}

func (c *countingConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.statements.Add(1)
	c.log.record(query)
	return c.base.(driver.QueryerContext).QueryContext(ctx, query, args)
}

func (c *countingConn) Ping(ctx context.Context) error {
	return c.base.(driver.Pinger).Ping(ctx)
}

func (c *countingConn) CheckNamedValue(v *driver.NamedValue) error {
	return c.base.(driver.NamedValueChecker).CheckNamedValue(v)
}

func (c *countingConn) ResetSession(ctx context.Context) error {
	return c.base.(driver.SessionResetter).ResetSession(ctx)
}

type countingStmt struct {
	base       driver.Stmt
	query      string
	statements *atomic.Int64
	log        *statementLog
}

func (s *countingStmt) Close() error  { return s.base.Close() }
func (s *countingStmt) NumInput() int { return s.base.NumInput() }

func (s *countingStmt) Exec(args []driver.Value) (driver.Result, error) {
	s.statements.Add(1)
	s.log.record(s.query)
	return s.base.Exec(args) //nolint:staticcheck // driver fallback path
}

func (s *countingStmt) Query(args []driver.Value) (driver.Rows, error) {
	s.statements.Add(1)
	s.log.record(s.query)
	return s.base.Query(args) //nolint:staticcheck // driver fallback path
}

func (s *countingStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	s.statements.Add(1)
	s.log.record(s.query)
	return s.base.(driver.StmtExecContext).ExecContext(ctx, args)
}

func (s *countingStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	s.statements.Add(1)
	s.log.record(s.query)
	return s.base.(driver.StmtQueryContext).QueryContext(ctx, args)
}

// countingRPC wraps a privy client and counts the treasury balance reads, which are
// live Solana RPC calls in production.
type countingRPC struct {
	privy.Client
	mu    sync.Mutex
	calls int
}

func (c *countingRPC) TreasuryUSDCBalance(ctx context.Context, treasuryAddress string) (int64, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	return c.Client.TreasuryUSDCBalance(ctx, treasuryAddress)
}

func (c *countingRPC) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

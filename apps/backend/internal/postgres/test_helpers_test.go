package postgres

import (
	"database/sql"
	"testing"
)

func integrationDB(t *testing.T) *sql.DB {
	t.Helper()
	return OpenTestDB(t)
}

func prepareIsolation(t *testing.T, db *sql.DB) *TestIsolation {
	t.Helper()
	return PrepareTestDB(t, db)
}

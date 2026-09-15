package postgres

import (
	"context"
	"database/sql"
	"fmt"
)

// BeginTx starts a database transaction.
func (s *Store) BeginTx(ctx context.Context) (*sql.Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	return tx, nil
}

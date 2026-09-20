package postgres

import (
	"context"
	"testing"
)

func TestMigration000013_renamesAndDrops(t *testing.T) {
	ctx := context.Background()
	db := OpenTestDB(t)
	iso := PrepareTestDB(t, db)
	_ = iso

	var exists bool
	for _, q := range []struct {
		name  string
		query string
	}{
		{"users.dynamic_user_id", `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='users' AND column_name='dynamic_user_id')`},
		{"member_wallets.address", `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='member_wallets' AND column_name='address')`},
		{"transactions.tx_hash", `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='transactions' AND column_name='tx_hash')`},
		{"transactions.input_token", `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='transactions' AND column_name='input_token')`},
	} {
		if err := db.QueryRowContext(ctx, q.query).Scan(&exists); err != nil {
			t.Fatalf("%s: %v", q.name, err)
		}
		if !exists {
			t.Fatalf("expected column %s", q.name)
		}
	}

	if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name='payout_proofs')`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("payout_proofs should be dropped")
	}
}

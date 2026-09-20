package wallets

import (
	"context"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
)

type postgresWalletStore struct {
	store *postgres.Store
}

// AdaptPostgresStore exposes postgres.Store as WalletStore.
func AdaptPostgresStore(store *postgres.Store) WalletStore {
	if store == nil {
		return nil
	}
	return &postgresWalletStore{store: store}
}

func (p *postgresWalletStore) GetMemberWalletByUserID(ctx context.Context, userID string) (StoredWallet, bool, error) {
	m, ok, err := p.store.GetMemberWalletMaterial(ctx, userID)
	if err != nil || !ok {
		return StoredWallet{}, ok, err
	}
	return StoredWallet{UserID: m.UserID, WalletID: m.WalletID, Address: m.Address, Metadata: m.Metadata, KeySharesEnc: m.KeySharesEnc}, true, nil
}

func (p *postgresWalletStore) InsertMemberWalletFull(ctx context.Context, w StoredWallet) (StoredWallet, error) {
	m, err := p.store.InsertMemberWalletMaterial(ctx, postgres.WalletMaterial{
		UserID: w.UserID, WalletID: w.WalletID, Address: w.Address, Metadata: w.Metadata, KeySharesEnc: w.KeySharesEnc,
	})
	if err != nil {
		return StoredWallet{}, err
	}
	return StoredWallet{UserID: m.UserID, WalletID: m.WalletID, Address: m.Address, Metadata: m.Metadata, KeySharesEnc: m.KeySharesEnc}, nil
}

func (p *postgresWalletStore) GetTreasuryByGroupID(ctx context.Context, groupID string) (StoredTreasury, bool, error) {
	m, ok, err := p.store.GetTreasuryMaterial(ctx, groupID)
	if err != nil || !ok {
		return StoredTreasury{}, ok, err
	}
	return StoredTreasury{GroupID: m.GroupID, WalletID: m.WalletID, Address: m.Address, Metadata: m.Metadata, KeySharesEnc: m.KeySharesEnc}, true, nil
}

func (p *postgresWalletStore) InsertTreasuryFull(ctx context.Context, t StoredTreasury) (StoredTreasury, error) {
	m, err := p.store.InsertTreasuryMaterial(ctx, postgres.WalletMaterial{
		GroupID: t.GroupID, WalletID: t.WalletID, Address: t.Address, Metadata: t.Metadata, KeySharesEnc: t.KeySharesEnc,
	})
	if err != nil {
		return StoredTreasury{}, err
	}
	return StoredTreasury{GroupID: m.GroupID, WalletID: m.WalletID, Address: m.Address, Metadata: m.Metadata, KeySharesEnc: m.KeySharesEnc}, nil
}

func (p *postgresWalletStore) SetTreasuryGasToppedUpAt(ctx context.Context, groupID string, at time.Time) error {
	return p.store.SetTreasuryGasToppedUpAt(ctx, groupID, at)
}

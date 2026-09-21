package wallets

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// StoredWallet is a persisted member wallet including MPC material.
type StoredWallet struct {
	UserID       string
	WalletID     string
	Address      string
	Metadata     json.RawMessage
	KeySharesEnc string
}

// StoredTreasury is a persisted treasury wallet including MPC material.
type StoredTreasury struct {
	GroupID      string
	WalletID     string
	Address      string
	Metadata     json.RawMessage
	KeySharesEnc string
}

// WalletStore persists member and treasury wallets.
type WalletStore interface {
	GetMemberWalletByUserID(ctx context.Context, userID string) (StoredWallet, bool, error)
	GetMemberWalletByAddress(ctx context.Context, address string) (StoredWallet, bool, error)
	InsertMemberWalletFull(ctx context.Context, w StoredWallet) (StoredWallet, error)
	GetTreasuryByGroupID(ctx context.Context, groupID string) (StoredTreasury, bool, error)
	GetTreasuryByAddress(ctx context.Context, address string) (StoredTreasury, bool, error)
	InsertTreasuryFull(ctx context.Context, t StoredTreasury) (StoredTreasury, error)
	SetTreasuryGasToppedUpAt(ctx context.Context, groupID string, at time.Time) error
}

type memoryStore struct {
	mu         sync.Mutex
	members    map[string]StoredWallet
	treasuries map[string]StoredTreasury
}

// NewMemoryStore is an in-memory WalletStore for tests.
func NewMemoryStore() WalletStore {
	return &memoryStore{
		members:    make(map[string]StoredWallet),
		treasuries: make(map[string]StoredTreasury),
	}
}

func (m *memoryStore) GetMemberWalletByUserID(ctx context.Context, userID string) (StoredWallet, bool, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.members[userID]
	return w, ok, nil
}

func (m *memoryStore) GetMemberWalletByAddress(ctx context.Context, address string) (StoredWallet, bool, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, w := range m.members {
		if strings.EqualFold(w.Address, address) {
			return w, true, nil
		}
	}
	return StoredWallet{}, false, nil
}

func (m *memoryStore) InsertMemberWalletFull(ctx context.Context, w StoredWallet) (StoredWallet, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	m.members[w.UserID] = w
	return w, nil
}

func (m *memoryStore) GetTreasuryByGroupID(ctx context.Context, groupID string) (StoredTreasury, bool, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.treasuries[groupID]
	return t, ok, nil
}

func (m *memoryStore) GetTreasuryByAddress(ctx context.Context, address string) (StoredTreasury, bool, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.treasuries {
		if strings.EqualFold(t.Address, address) {
			return t, true, nil
		}
	}
	return StoredTreasury{}, false, nil
}

func (m *memoryStore) InsertTreasuryFull(ctx context.Context, t StoredTreasury) (StoredTreasury, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	m.treasuries[t.GroupID] = t
	return t, nil
}

func (m *memoryStore) SetTreasuryGasToppedUpAt(ctx context.Context, groupID string, at time.Time) error {
	_ = ctx
	_ = groupID
	_ = at
	return nil
}

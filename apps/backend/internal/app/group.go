package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

// GroupService orchestrates group create flows.
type GroupService struct {
	store *postgres.Store
	privy privy.Client
}

// NewGroupService wires group dependencies.
func NewGroupService(store *postgres.Store, privyClient privy.Client) *GroupService {
	return &GroupService{
		store: store,
		privy: privyClient,
	}
}

// CreateGroupResult is the created group and treasury for POST /v1/groups.
type CreateGroupResult struct {
	GroupID         string
	Name            string
	TreasuryAddress string
}

// CreateGroup inserts a group row and provisions its treasury wallet.
func (g *GroupService) CreateGroup(ctx context.Context, accessToken string, name string) (CreateGroupResult, error) {
	if name == "" {
		return CreateGroupResult{}, fmt.Errorf("name is required")
	}

	identity, err := g.privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			return CreateGroupResult{}, privy.ErrInvalidToken
		}
		return CreateGroupResult{}, fmt.Errorf("verify session: %w", err)
	}

	user, found, err := g.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		return CreateGroupResult{}, err
	}
	if !found {
		return CreateGroupResult{}, ErrUserNotFound
	}

	tx, err := g.store.BeginTx(ctx)
	if err != nil {
		return CreateGroupResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	group, err := g.store.InsertGroupTx(ctx, tx, name, user.ID)
	if err != nil {
		return CreateGroupResult{}, err
	}

	treasuryRef, err := g.privy.EnsureTreasury(ctx, privy.GroupID(group.ID))
	if err != nil {
		return CreateGroupResult{}, fmt.Errorf("privy ensure treasury: %w", err)
	}

	_, err = g.store.InsertTreasuryTx(ctx, tx, group.ID, treasuryRef.PrivyWalletID, treasuryRef.SolanaAddress)
	if err != nil {
		return CreateGroupResult{}, err
	}

	if err := tx.Commit(); err != nil {
		return CreateGroupResult{}, fmt.Errorf("commit create group: %w", err)
	}
	committed = true

	return CreateGroupResult{
		GroupID:         group.ID,
		Name:            group.Name,
		TreasuryAddress: treasuryRef.SolanaAddress,
	}, nil
}

package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/monaco/monaco/apps/backend/internal/auth"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
)

// GroupService orchestrates group create flows.
type GroupService struct {
	store   *postgres.Store
	auth    auth.Verifier
	wallets wallets.Client
}

// NewGroupService wires group dependencies.
func NewGroupService(store *postgres.Store, verifier auth.Verifier, walletClient wallets.Client) *GroupService {
	return &GroupService{
		store:   store,
		auth:    verifier,
		wallets: walletClient,
	}
}

// ErrGroupNotFound means the group does not exist or the caller cannot access it.
var ErrGroupNotFound = errors.New("group not found")

// CreateGroupResult is the created group and treasury for POST /v1/groups.
type CreateGroupResult struct {
	GroupID         string
	Name            string
	TreasuryAddress string
}

// GetGroupResult is the group name and treasury for GET /v1/groups/{id}.
type GetGroupResult struct {
	Name            string
	TreasuryAddress string
}

// CreateGroup inserts a group row and provisions its treasury wallet.
func (g *GroupService) CreateGroup(ctx context.Context, accessToken string, name string) (CreateGroupResult, error) {
	if name == "" {
		logGroupBranchWarn("group create rejected", "name required")
		return CreateGroupResult{}, fmt.Errorf("name is required")
	}

	identity, err := g.auth.VerifySession(ctx, auth.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			logGroupBranchWarn("group create rejected", "invalid token", "name", name)
			return CreateGroupResult{}, auth.ErrUnauthorized
		}
		logGroupBranchError("group create verify session failed", err, "name", name)
		return CreateGroupResult{}, fmt.Errorf("verify session: %w", err)
	}

	user, found, err := g.store.GetUserByDynamicUserID(ctx, identity.DynamicUserID)
	if err != nil {
		logGroupBranchError("group create lookup user failed", err, "name", name)
		return CreateGroupResult{}, err
	}
	if !found {
		logGroupBranchWarn("group create rejected", "user not found", "name", name)
		return CreateGroupResult{}, ErrUserNotFound
	}
	logGroupCreateStart(user.ID, name)

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
	// The creator is always a member; every member-scoped route authorizes via group_members.
	if err := g.store.InsertGroupMemberTx(ctx, tx, group.ID, user.ID); err != nil {
		return CreateGroupResult{}, err
	}

	if err := tx.Commit(); err != nil {
		logGroupBranchError("group create commit failed", err, "user_id", user.ID, "name", name)
		return CreateGroupResult{}, fmt.Errorf("commit create group: %w", err)
	}
	committed = true

	treasuryRef, err := g.wallets.EnsureTreasury(ctx, wallets.GroupID(group.ID))
	if err != nil {
		_ = g.store.DeleteUnprovisionedGroup(ctx, group.ID)
		return CreateGroupResult{}, fmt.Errorf("ensure treasury: %w", err)
	}
	if err := persistTreasuryIfMissing(ctx, g.store, group.ID, treasuryRef.WalletID, treasuryRef.Address); err != nil {
		return CreateGroupResult{}, err
	}

	logGroupCreateSuccess(group.ID, user.ID, name)
	return CreateGroupResult{
		GroupID:         group.ID,
		Name:            group.Name,
		TreasuryAddress: treasuryRef.Address,
	}, nil
}

// GetGroup returns group name and treasury address for an authenticated group member.
func (g *GroupService) GetGroup(ctx context.Context, accessToken string, groupID string) (GetGroupResult, error) {
	if groupID == "" {
		logGroupBranchWarn("group get rejected", "group id required")
		return GetGroupResult{}, fmt.Errorf("group id is required")
	}

	identity, err := g.auth.VerifySession(ctx, auth.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			logGroupBranchWarn("group get rejected", "invalid token", "group_id", groupID)
			return GetGroupResult{}, auth.ErrUnauthorized
		}
		logGroupBranchError("group get verify session failed", err, "group_id", groupID)
		return GetGroupResult{}, fmt.Errorf("verify session: %w", err)
	}

	user, found, err := g.store.GetUserByDynamicUserID(ctx, identity.DynamicUserID)
	if err != nil {
		logGroupBranchError("group get lookup user failed", err, "group_id", groupID)
		return GetGroupResult{}, err
	}
	if !found {
		logGroupBranchWarn("group get rejected", "user not found", "group_id", groupID)
		return GetGroupResult{}, ErrUserNotFound
	}
	logGroupGetStart(user.ID, groupID)

	group, found, err := g.store.GetGroupByID(ctx, groupID)
	if err != nil {
		logGroupBranchError("group get lookup group failed", err, "group_id", groupID, "user_id", user.ID)
		return GetGroupResult{}, err
	}
	if !found {
		logGroupBranchWarn("group get rejected", "group not found", "group_id", groupID, "user_id", user.ID)
		return GetGroupResult{}, ErrGroupNotFound
	}
	member, err := g.store.IsGroupMember(ctx, group.ID, user.ID)
	if err != nil {
		logGroupBranchError("group get membership check failed", err, "group_id", groupID, "user_id", user.ID)
		return GetGroupResult{}, err
	}
	if !member {
		logGroupBranchWarn("group get rejected", "not group member", "group_id", groupID, "user_id", user.ID)
		return GetGroupResult{}, ErrGroupNotFound
	}

	treasury, found, err := g.store.GetTreasuryByGroupID(ctx, groupID)
	if err != nil {
		return GetGroupResult{}, err
	}
	if !found {
		return GetGroupResult{}, ErrGroupNotFound
	}

	logGroupGetSuccess(groupID)
	return GetGroupResult{
		Name:            group.Name,
		TreasuryAddress: treasury.Address,
	}, nil
}

func persistTreasuryIfMissing(ctx context.Context, store *postgres.Store, groupID, walletID, address string) error {
	_, found, err := store.GetTreasuryByGroupID(ctx, groupID)
	if err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = store.InsertTreasury(ctx, groupID, walletID, address)
	if err != nil && (postgres.IsUniqueViolation(err) || errors.Is(err, sql.ErrNoRows)) {
		return nil
	}
	return err
}

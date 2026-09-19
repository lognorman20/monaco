package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/packages/domain"
)

// resolveAgentForGroup authenticates X-Monaco-Agent-Key for a group-scoped route.
func resolveAgentForGroup(ctx context.Context, store *postgres.Store, groupID, agentKey string) (postgres.GroupAgentRow, error) {
	if agentKey == "" {
		return postgres.GroupAgentRow{}, app.ErrInvalidAgentAPIKey
	}
	keyHash := app.HashAgentAPIKey(agentKey)
	agent, found, err := store.GetGroupAgentByAPIKeyHash(ctx, keyHash)
	if err != nil {
		return postgres.GroupAgentRow{}, err
	}
	if !found || !agent.APIKeyHash.Valid {
		return postgres.GroupAgentRow{}, app.ErrInvalidAgentAPIKey
	}
	if agent.GroupID != groupID {
		return postgres.GroupAgentRow{}, app.ErrAgentGroupMismatch
	}
	if agent.Status == domain.AgentStatusRevoked {
		return postgres.GroupAgentRow{}, app.ErrInvalidAgentAPIKey
	}
	return agent, nil
}

func writeAgentAuthError(ctx context.Context, log *requestLog, w http.ResponseWriter, err error, groupID string) {
	switch {
	case errors.Is(err, app.ErrInvalidAgentAPIKey):
		logJSONError(ctx, log, "invalid_agent_key", w, http.StatusUnauthorized, "invalid agent api key", "group_id", groupID)
	case errors.Is(err, app.ErrAgentGroupMismatch):
		logJSONError(ctx, log, "agent_group_mismatch", w, http.StatusForbidden, "agent key does not match group", "group_id", groupID)
	default:
		logJSONError(ctx, log, "agent_auth_failed", w, http.StatusInternalServerError, "internal server error", "group_id", groupID, "err", err.Error())
	}
}

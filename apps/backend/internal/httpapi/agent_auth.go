package httpapi

import (
	"context"
	"errors"
	"math"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/ratelimit"
	"github.com/monaco/monaco/packages/domain"
)

const (
	agentKeyFailureBurst    = 10
	agentKeyFailureInterval = time.Minute
)

// AgentKeyGuard throttles wrong X-Monaco-Agent-Key guesses. Agent keys are short so a
// cabal can read one off a phone; the guard is what makes guessing one impractical.
// Failures count against both the target group and the caller's address, so neither
// spreading guesses over groups nor over addresses gets an unthrottled path. Valid
// calls never spend from the limit.
type AgentKeyGuard struct {
	failures *ratelimit.Limiter
}

// NewAgentKeyGuard allows agentKeyFailureBurst wrong keys per group and per address,
// then one more per agentKeyFailureInterval.
func NewAgentKeyGuard() *AgentKeyGuard {
	return &AgentKeyGuard{failures: ratelimit.New(agentKeyFailureBurst, agentKeyFailureInterval)}
}

func agentKeyGuardKeys(r *http.Request, groupID string) []string {
	// RemoteAddr only: forwarding headers are caller-controlled.
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return []string{"group:" + groupID, "addr:" + host}
}

// blocked reports whether this caller or group has used up its wrong-key allowance.
// A nil guard never blocks.
func (g *AgentKeyGuard) blocked(r *http.Request, groupID string) (bool, time.Duration) {
	if g == nil {
		return false, 0
	}
	var longest time.Duration
	isBlocked := false
	for _, key := range agentKeyGuardKeys(r, groupID) {
		if over, wait := g.failures.Blocked(key); over {
			isBlocked = true
			longest = max(longest, wait)
		}
	}
	return isBlocked, longest
}

// recordFailure spends one wrong-key attempt when err is an authentication failure.
func (g *AgentKeyGuard) recordFailure(r *http.Request, groupID string, err error) {
	if g == nil || !isAgentAuthFailure(err) {
		return
	}
	for _, key := range agentKeyGuardKeys(r, groupID) {
		g.failures.Allow(key)
	}
}

func isAgentAuthFailure(err error) bool {
	return errors.Is(err, app.ErrInvalidAgentAPIKey) || errors.Is(err, app.ErrAgentGroupMismatch)
}

func writeAgentKeyThrottled(ctx context.Context, log *requestLog, w http.ResponseWriter, wait time.Duration, groupID string) {
	seconds := max(1, int(math.Ceil(wait.Seconds())))
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	logJSONError(ctx, log, "agent_key_throttled", w, http.StatusTooManyRequests, "too many invalid agent api keys, try again shortly", "group_id", groupID, "retry_after_s", seconds)
}

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
		// Same response as an unknown key: a distinct one would confirm the key is live elsewhere.
		logJSONError(ctx, log, "agent_group_mismatch", w, http.StatusUnauthorized, "invalid agent api key", "group_id", groupID)
	default:
		logJSONError(ctx, log, "agent_auth_failed", w, http.StatusInternalServerError, "internal server error", "group_id", groupID, "err", err.Error())
	}
}

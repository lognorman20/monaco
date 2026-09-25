package httpapi

import (
	"context"
	"errors"
	"math"
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

// AgentKeyGuard throttles wrong X-Monaco-Agent-Key guesses. Only failed attempts spend from
// it, and they count against both the target group and the caller's address.
//
// An address that has spent its allowance is refused before its key is looked at: answering
// its right guess with a 200 would turn the throttle into an oracle. The group allowance is
// different. Anyone can spend it by aiming ten bad keys at a group, so refusing on it would
// let a stranger lock a cabal's real bot out. Current keys carry ~158 bits and cannot be
// guessed, so a presented key of that shape is never refused on the group allowance. Keys
// minted before that format are five characters; for those the group allowance is what keeps
// guessing spread over many addresses impractical, so it still applies to them. A cabal on an
// old key gets out of that trade-off by voting the bot out and back in.
type AgentKeyGuard struct {
	failures *ratelimit.Limiter
	// trustProxyHeaders matches RateLimiter: key on X-Forwarded-For only behind a proxy that
	// overwrites it. Behind such a proxy RemoteAddr is the proxy, shared by every caller.
	trustProxyHeaders bool
}

// NewAgentKeyGuard allows agentKeyFailureBurst wrong keys per group and per address,
// then one more per agentKeyFailureInterval.
func NewAgentKeyGuard(trustProxyHeaders bool) *AgentKeyGuard {
	return &AgentKeyGuard{
		failures:          ratelimit.New(agentKeyFailureBurst, agentKeyFailureInterval),
		trustProxyHeaders: trustProxyHeaders,
	}
}

func (g *AgentKeyGuard) groupKey(groupID string) string {
	return "group:" + groupID
}

func (g *AgentKeyGuard) addressKey(r *http.Request) string {
	return "addr:" + clientIP(r, g.trustProxyHeaders)
}

// blocked reports whether a call presenting agentKey must be refused before the key is
// checked. A nil guard never blocks.
func (g *AgentKeyGuard) blocked(r *http.Request, groupID, agentKey string) (bool, time.Duration) {
	if g == nil {
		return false, 0
	}
	keys := []string{g.addressKey(r)}
	if !app.IsCurrentAgentKeyFormat(agentKey) {
		keys = append(keys, g.groupKey(groupID))
	}
	var longest time.Duration
	isBlocked := false
	for _, key := range keys {
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
	g.failures.Allow(g.groupKey(groupID))
	g.failures.Allow(g.addressKey(r))
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

// keyOnlyGuardScope stands in for the group on /v1/agent routes, where the key is the only
// credential. A short legacy key there is a guess against every cabal's agent at once, so all
// key-only failures share one group allowance: a flood of bad keys can make legacy-key agents
// wait on these routes (the group routes still work for them), but cannot spread guesses over
// many addresses. Current-format keys never check it.
const keyOnlyGuardScope = "key-only"

// resolveAgentByKey authenticates X-Monaco-Agent-Key on a route that takes no group id: the
// key alone names the agent and its cabal. A revoked agent's key no longer authenticates; a
// paused agent's still does, so it can read while it waits.
func resolveAgentByKey(ctx context.Context, store *postgres.Store, agentKey string) (postgres.GroupAgentRow, error) {
	if agentKey == "" {
		return postgres.GroupAgentRow{}, app.ErrInvalidAgentAPIKey
	}
	agent, found, err := store.GetGroupAgentByAPIKeyHash(ctx, app.HashAgentAPIKey(agentKey))
	if err != nil {
		return postgres.GroupAgentRow{}, err
	}
	if !found || !agent.APIKeyHash.Valid || agent.Status == domain.AgentStatusRevoked {
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

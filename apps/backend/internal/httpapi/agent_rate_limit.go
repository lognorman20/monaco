package httpapi

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
)

// Per-agent limits. Intents allow a burst of an hour's worth and refill evenly, so
// app.AgentIntentsPerHour sustained. Reads are looser: an agent polls its account and prices
// before deciding.
const (
	agentIntentBurst  = app.AgentIntentsPerHour
	agentIntentRefill = time.Hour / app.AgentIntentsPerHour
	agentReadBurst    = 120
	agentReadRefill   = 30 * time.Second
)

// AgentRateLimits caps each agent's intents and reads, keyed by agent id after its key
// authenticated, so the caps follow the agent rather than the address it calls from.
type AgentRateLimits struct {
	Intents *app.KeyedRateLimiter
	Reads   *app.KeyedRateLimiter
}

// NewAgentRateLimits returns the production per-agent limits.
func NewAgentRateLimits(now func() time.Time) *AgentRateLimits {
	return &AgentRateLimits{
		Intents: app.NewKeyedRateLimiter(agentIntentBurst, agentIntentRefill, now),
		Reads:   app.NewKeyedRateLimiter(agentReadBurst, agentReadRefill, now),
	}
}

func (l *AgentRateLimits) allowIntent(ctx context.Context, log *requestLog, w http.ResponseWriter, agentID string) bool {
	if l == nil {
		return true
	}
	return allowAgent(ctx, log, w, l.Intents, agentID, "too many intents, try again later")
}

func (l *AgentRateLimits) allowRead(ctx context.Context, log *requestLog, w http.ResponseWriter, agentID string) bool {
	if l == nil {
		return true
	}
	return allowAgent(ctx, log, w, l.Reads, agentID, "too many requests, try again later")
}

func allowAgent(ctx context.Context, log *requestLog, w http.ResponseWriter, limiter *app.KeyedRateLimiter, agentID, message string) bool {
	ok, wait := limiter.Allow(agentID)
	if ok {
		return true
	}
	seconds := max(1, int(math.Ceil(wait.Seconds())))
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	logJSONError(ctx, log, "agent_rate_limited", w, http.StatusTooManyRequests, message, "agent_id", agentID, "retry_after_s", seconds)
	return false
}

package domain

import "fmt"

// AgentStatus is persisted on group_agents.status.
type AgentStatus string

const (
	AgentStatusPending AgentStatus = "pending"
	AgentStatusActive  AgentStatus = "active"
	AgentStatusPaused  AgentStatus = "paused"
	AgentStatusRevoked AgentStatus = "revoked"
)

// ParseAgentStatus parses a group_agents.status column value.
func ParseAgentStatus(raw string) (AgentStatus, error) {
	switch AgentStatus(raw) {
	case AgentStatusPending, AgentStatusActive, AgentStatusPaused, AgentStatusRevoked:
		return AgentStatus(raw), nil
	default:
		return "", fmt.Errorf("invalid agent status: %q", raw)
	}
}

// AgentIntentSide is buy or sell for agent trade intents.
type AgentIntentSide string

const (
	AgentIntentBuy  AgentIntentSide = "buy"
	AgentIntentSell AgentIntentSide = "sell"
)

// ParseAgentIntentSide parses an agent_intents.side column value.
func ParseAgentIntentSide(raw string) (AgentIntentSide, error) {
	switch AgentIntentSide(raw) {
	case AgentIntentBuy, AgentIntentSell:
		return AgentIntentSide(raw), nil
	default:
		return "", fmt.Errorf("invalid agent intent side: %q", raw)
	}
}

// GroupAgent is the approved in-cabal trading agent record.
type GroupAgent struct {
	ID                   string
	GroupID              string
	Status               AgentStatus
	AgentDisplayName     string
	AllocationUsdcMicros int64
}

// AgentIntentRequest is the normalized intent an agent submits.
type AgentIntentRequest struct {
	Side        AgentIntentSide
	Symbol      string
	UsdcMicros  int64
	TokenAmount int64
}

// AgentTreasurySnapshot is treasury state for intent validation.
type AgentTreasurySnapshot struct {
	TreasuryUsdcMicros     int64
	AgentSpentUsdcMicros   int64
	PendingAgentUsdcMicros int64
	TokenHoldingsBySymbol  map[string]int64
}

// ValidateIntent rejects intents that breach agent status or allocation.
func ValidateIntent(agent GroupAgent, intent AgentIntentRequest, snap AgentTreasurySnapshot) error {
	if agent.Status != AgentStatusActive {
		if agent.Status == AgentStatusPaused {
			return fmt.Errorf("agent is paused")
		}
		return fmt.Errorf("agent is not active")
	}
	if intent.Symbol == "" {
		return fmt.Errorf("symbol is required")
	}

	switch intent.Side {
	case AgentIntentBuy:
		if intent.UsdcMicros <= 0 {
			return fmt.Errorf("usdc amount must be positive")
		}
		available, err := agentAvailableUsdcMicros(agent, snap)
		if err != nil {
			return err
		}
		if intent.UsdcMicros > available {
			return fmt.Errorf("trade exceeds agent allocation")
		}
		if intent.UsdcMicros > snap.TreasuryUsdcMicros {
			return fmt.Errorf("insufficient treasury usdc")
		}
	case AgentIntentSell:
		if intent.TokenAmount <= 0 {
			return fmt.Errorf("token amount must be positive")
		}
		held := snap.TokenHoldingsBySymbol[intent.Symbol]
		if intent.TokenAmount > held {
			return fmt.Errorf("insufficient treasury holding")
		}
	default:
		return fmt.Errorf("invalid intent side")
	}
	return nil
}

// agentAvailableUsdcMicros returns allocation − executed − pending, floored at zero.
// Operands must be non-negative so each subtraction is ordered and cannot wrap:
// unchecked, 0 − MaxInt64 − MaxInt64 wraps to +2 and would approve a buy.
func agentAvailableUsdcMicros(agent GroupAgent, snap AgentTreasurySnapshot) (int64, error) {
	if agent.AllocationUsdcMicros < 0 {
		return 0, fmt.Errorf("agent allocation must be non-negative")
	}
	if snap.AgentSpentUsdcMicros < 0 || snap.PendingAgentUsdcMicros < 0 {
		return 0, fmt.Errorf("agent spent and pending usdc must be non-negative")
	}
	if snap.AgentSpentUsdcMicros >= agent.AllocationUsdcMicros {
		return 0, nil
	}
	remaining := agent.AllocationUsdcMicros - snap.AgentSpentUsdcMicros
	if snap.PendingAgentUsdcMicros >= remaining {
		return 0, nil
	}
	return remaining - snap.PendingAgentUsdcMicros, nil
}

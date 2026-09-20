package domain

import "testing"

func TestValidateIntent_rejectsPausedAgent(t *testing.T) {
	agent := GroupAgent{Status: AgentStatusPaused, AllocationUsdcMicros: 1_000_000}
	err := ValidateIntent(agent, AgentIntentRequest{Side: AgentIntentBuy, Symbol: "AAPLx", UsdcMicros: 100_000}, AgentTreasurySnapshot{
		TreasuryUsdcMicros: 1_000_000,
	})
	if err == nil {
		t.Fatal("expected paused agent rejection")
	}
}

func TestValidateIntent_rejectsOverAllocation(t *testing.T) {
	agent := GroupAgent{Status: AgentStatusActive, AllocationUsdcMicros: 500_000}
	err := ValidateIntent(agent, AgentIntentRequest{Side: AgentIntentBuy, Symbol: "AAPLx", UsdcMicros: 600_000}, AgentTreasurySnapshot{
		TreasuryUsdcMicros: 1_000_000,
	})
	if err == nil {
		t.Fatal("expected allocation rejection")
	}
}

func TestValidateIntent_allowsBuyWithinAllocation(t *testing.T) {
	agent := GroupAgent{Status: AgentStatusActive, AllocationUsdcMicros: 500_000}
	err := ValidateIntent(agent, AgentIntentRequest{Side: AgentIntentBuy, Symbol: "AAPLx", UsdcMicros: 400_000}, AgentTreasurySnapshot{
		TreasuryUsdcMicros: 1_000_000,
	})
	if err != nil {
		t.Fatalf("ValidateIntent: %v", err)
	}
}

func TestValidateIntent_countsCommittedBuysAgainstAllocation(t *testing.T) {
	agent := GroupAgent{Status: AgentStatusActive, AllocationUsdcMicros: 500_000}
	err := ValidateIntent(agent, AgentIntentRequest{Side: AgentIntentBuy, Symbol: "AAPLx", UsdcMicros: 200_000}, AgentTreasurySnapshot{
		TreasuryUsdcMicros:     1_000_000,
		AgentSpentUsdcMicros:   300_000,
		PendingAgentUsdcMicros: 100_000,
	})
	if err == nil {
		t.Fatal("expected allocation rejection once committed buys leave too little room")
	}
}

func TestValidateIntent_rejectsSellOfMemberBoughtPosition(t *testing.T) {
	agent := GroupAgent{Status: AgentStatusActive, AllocationUsdcMicros: 500_000}
	err := ValidateIntent(agent, AgentIntentRequest{Side: AgentIntentSell, Symbol: "AAPLx", TokenAmount: 1}, AgentTreasurySnapshot{
		TokenHoldingsBySymbol: map[string]int64{"AAPLx": 1_000_000},
	})
	if err == nil {
		t.Fatal("expected rejection: the treasury holds AAPLx but the agent bought none of it")
	}
}

func TestValidateIntent_allowsSellUpToAgentPosition(t *testing.T) {
	agent := GroupAgent{Status: AgentStatusActive, AllocationUsdcMicros: 500_000}
	snap := AgentTreasurySnapshot{
		TokenHoldingsBySymbol:      map[string]int64{"AAPLx": 1_000_000},
		AgentTokenHoldingsBySymbol: map[string]int64{"AAPLx": 300_000},
	}
	if err := ValidateIntent(agent, AgentIntentRequest{Side: AgentIntentSell, Symbol: "AAPLx", TokenAmount: 300_000}, snap); err != nil {
		t.Fatalf("sell of the agent's whole position: %v", err)
	}
	if err := ValidateIntent(agent, AgentIntentRequest{Side: AgentIntentSell, Symbol: "AAPLx", TokenAmount: 300_001}, snap); err == nil {
		t.Fatal("expected rejection one atomic past the agent's position")
	}
}

func TestValidateIntent_rejectsSellBeyondTreasuryHolding(t *testing.T) {
	agent := GroupAgent{Status: AgentStatusActive, AllocationUsdcMicros: 500_000}
	err := ValidateIntent(agent, AgentIntentRequest{Side: AgentIntentSell, Symbol: "AAPLx", TokenAmount: 300_000}, AgentTreasurySnapshot{
		TokenHoldingsBySymbol:      map[string]int64{"AAPLx": 100_000},
		AgentTokenHoldingsBySymbol: map[string]int64{"AAPLx": 300_000},
	})
	if err == nil {
		t.Fatal("expected rejection: a member-voted sell already took part of what the agent bought")
	}
}

func TestValidateIntent_rejectsNegativeAgentPosition(t *testing.T) {
	agent := GroupAgent{Status: AgentStatusActive, AllocationUsdcMicros: 500_000}
	err := ValidateIntent(agent, AgentIntentRequest{Side: AgentIntentSell, Symbol: "AAPLx", TokenAmount: 1}, AgentTreasurySnapshot{
		TokenHoldingsBySymbol:      map[string]int64{"AAPLx": 1_000_000},
		AgentTokenHoldingsBySymbol: map[string]int64{"AAPLx": -1},
	})
	if err == nil {
		t.Fatal("expected rejection of a negative agent position")
	}
}

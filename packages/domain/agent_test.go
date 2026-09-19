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

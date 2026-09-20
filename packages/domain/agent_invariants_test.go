package domain

import (
	"math"
	"math/big"
	"math/rand"
	"testing"
)

// assertIntentAcceptanceIsSafe re-derives, in arbitrary precision, every limit an
// accepted intent must respect. It is shared by the property test and the fuzzer.
func assertIntentAcceptanceIsSafe(t *testing.T, agent GroupAgent, intent AgentIntentRequest, snap AgentTreasurySnapshot) {
	t.Helper()
	if agent.Status != AgentStatusActive {
		t.Fatalf("accepted intent for %q agent", agent.Status)
	}
	if intent.Symbol == "" {
		t.Fatal("accepted intent without a symbol")
	}
	switch intent.Side {
	case AgentIntentBuy:
		if intent.UsdcMicros <= 0 {
			t.Fatalf("accepted buy of %d micros", intent.UsdcMicros)
		}
		available := big.NewInt(agent.AllocationUsdcMicros)
		available.Sub(available, big.NewInt(snap.AgentSpentUsdcMicros))
		available.Sub(available, big.NewInt(snap.PendingAgentUsdcMicros))
		if big.NewInt(intent.UsdcMicros).Cmp(available) > 0 {
			t.Fatalf("accepted buy of %d with allocation %d − spent %d − pending %d = %s",
				intent.UsdcMicros, agent.AllocationUsdcMicros, snap.AgentSpentUsdcMicros, snap.PendingAgentUsdcMicros, available)
		}
		if intent.UsdcMicros > agent.AllocationUsdcMicros {
			t.Fatalf("accepted buy of %d above total allocation %d", intent.UsdcMicros, agent.AllocationUsdcMicros)
		}
		if intent.UsdcMicros > snap.TreasuryUsdcMicros {
			t.Fatalf("accepted buy of %d with treasury %d", intent.UsdcMicros, snap.TreasuryUsdcMicros)
		}
	case AgentIntentSell:
		if intent.TokenAmount <= 0 {
			t.Fatalf("accepted sell of %d tokens", intent.TokenAmount)
		}
		if held := snap.TokenHoldingsBySymbol[intent.Symbol]; intent.TokenAmount > held {
			t.Fatalf("accepted sell of %d %s with %d held", intent.TokenAmount, intent.Symbol, held)
		}
		if own := snap.AgentTokenHoldingsBySymbol[intent.Symbol]; intent.TokenAmount > own {
			t.Fatalf("accepted sell of %d %s with only %d bought by the agent", intent.TokenAmount, intent.Symbol, own)
		}
	default:
		t.Fatalf("accepted intent with side %q", intent.Side)
	}
}

// randomAmount mixes ordinary values with int64 edges so wraps get exercised.
func randomAmount(rng *rand.Rand) int64 {
	switch rng.Intn(8) {
	case 0:
		return 0
	case 1:
		return math.MaxInt64 - rng.Int63n(3)
	case 2:
		return math.MinInt64 + rng.Int63n(3)
	case 3:
		return -rng.Int63n(1_000_000)
	default:
		return rng.Int63n(2_000_000_000)
	}
}

func TestValidateIntent_randomInputs_acceptedIntentsRespectEveryLimit(t *testing.T) {
	rng := newSeededRand(t, 47)
	statuses := []AgentStatus{AgentStatusActive, AgentStatusActive, AgentStatusActive, AgentStatusPending, AgentStatusPaused, AgentStatusRevoked, ""}
	sides := []AgentIntentSide{AgentIntentBuy, AgentIntentSell, "hold", ""}
	symbols := []string{"AAPLx", "TSLAx", ""}
	accepted := 0
	for i := 0; i < 50_000; i++ {
		// Arrange
		agent := buildActiveAgent(func(a *GroupAgent) {
			a.Status = statuses[rng.Intn(len(statuses))]
			a.AllocationUsdcMicros = randomAmount(rng)
		})
		snap := buildTreasurySnapshot(func(s *AgentTreasurySnapshot) {
			s.TreasuryUsdcMicros = randomAmount(rng)
			s.AgentSpentUsdcMicros = randomAmount(rng)
			s.PendingAgentUsdcMicros = randomAmount(rng)
			s.TokenHoldingsBySymbol = map[string]int64{"AAPLx": randomAmount(rng)}
			s.AgentTokenHoldingsBySymbol = map[string]int64{"AAPLx": randomAmount(rng)}
		})
		intent := AgentIntentRequest{
			Side:        sides[rng.Intn(len(sides))],
			Symbol:      symbols[rng.Intn(len(symbols))],
			UsdcMicros:  randomAmount(rng),
			TokenAmount: randomAmount(rng),
		}

		// Act
		err := ValidateIntent(agent, intent, snap)

		// Assert
		if err == nil {
			accepted++
			assertIntentAcceptanceIsSafe(t, agent, intent, snap)
		}
	}
	if accepted == 0 {
		t.Fatal("generator never produced an accepted intent")
	}
}

func TestValidateIntent_nonActiveStatus_neverAccepted(t *testing.T) {
	for _, status := range []AgentStatus{AgentStatusPending, AgentStatusPaused, AgentStatusRevoked, "", "ACTIVE"} {
		for _, intent := range []AgentIntentRequest{
			{Side: AgentIntentBuy, Symbol: "AAPLx", UsdcMicros: 1},
			{Side: AgentIntentSell, Symbol: "AAPLx", TokenAmount: 1},
		} {
			// Arrange
			agent := buildActiveAgent(func(a *GroupAgent) { a.Status = status })

			// Act
			err := ValidateIntent(agent, intent, buildTreasurySnapshot(nil))

			// Assert
			if err == nil {
				t.Fatalf("status %q accepted a %s intent", status, intent.Side)
			}
		}
	}
}

func TestValidateIntent_buyAtExactRemainingAllocation_boundary(t *testing.T) {
	// Arrange — $500 allocation, $200 executed, $100 pending leaves exactly $200.
	agent := buildActiveAgent(nil)
	snap := buildTreasurySnapshot(func(s *AgentTreasurySnapshot) {
		s.AgentSpentUsdcMicros = 200_000_000
		s.PendingAgentUsdcMicros = 100_000_000
	})

	// Act
	errAt := ValidateIntent(agent, AgentIntentRequest{Side: AgentIntentBuy, Symbol: "AAPLx", UsdcMicros: 200_000_000}, snap)
	errOver := ValidateIntent(agent, AgentIntentRequest{Side: AgentIntentBuy, Symbol: "AAPLx", UsdcMicros: 200_000_001}, snap)

	// Assert
	if errAt != nil {
		t.Fatalf("buy at exact remaining allocation rejected: %v", errAt)
	}
	if errOver == nil {
		t.Fatal("buy one micro over remaining allocation accepted")
	}
}

func TestValidateIntent_buyAboveTreasury_rejectedEvenWithinAllocation(t *testing.T) {
	// Arrange
	agent := buildActiveAgent(nil)
	snap := buildTreasurySnapshot(func(s *AgentTreasurySnapshot) { s.TreasuryUsdcMicros = 99_999_999 })

	// Act
	err := ValidateIntent(agent, AgentIntentRequest{Side: AgentIntentBuy, Symbol: "AAPLx", UsdcMicros: 100_000_000}, snap)

	// Assert
	if err == nil {
		t.Fatal("expected insufficient treasury rejection")
	}
}

func TestValidateIntent_sellBoundaries(t *testing.T) {
	cases := []struct {
		name   string
		symbol string
		amount int64
		// agentOwn overrides how much of the holding the agent bought; zero keeps the factory's all of it.
		agentOwn int64
		wantErr  bool
	}{
		{name: "exactly held", symbol: "AAPLx", amount: 1_000_000, wantErr: false},
		{name: "one above held", symbol: "AAPLx", amount: 1_000_001, wantErr: true},
		{name: "one above the agent's own position", symbol: "AAPLx", amount: 600_001, agentOwn: 600_000, wantErr: true},
		{name: "exactly the agent's own position", symbol: "AAPLx", amount: 600_000, agentOwn: 600_000, wantErr: false},
		{name: "symbol not held", symbol: "TSLAx", amount: 1, wantErr: true},
		{name: "zero amount", symbol: "AAPLx", amount: 0, wantErr: true},
		{name: "negative amount", symbol: "AAPLx", amount: -1, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			intent := AgentIntentRequest{Side: AgentIntentSell, Symbol: tc.symbol, TokenAmount: tc.amount}

			// Act
			err := ValidateIntent(buildActiveAgent(nil), intent, buildTreasurySnapshot(func(s *AgentTreasurySnapshot) {
				if tc.agentOwn != 0 {
					s.AgentTokenHoldingsBySymbol = map[string]int64{"AAPLx": tc.agentOwn}
				}
			}))

			// Assert
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

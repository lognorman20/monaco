package pyth

import (
	"context"
	"testing"
)

func TestMarkedPot_afterHoursFlag_surfacesOnGroupView(t *testing.T) {
	// Arrange — T5 GroupView reads NavInput from MarkedPot; test the client contract here.
	pythClient := NewFakeClient()
	treasury := TreasuryRef{GroupID: "group-1", Address: "FAKEtreasury"}
	RegisterMarkedPot(pythClient, treasury, NavInput{
		TreasuryUsdc: 0,
		Holdings: []MarkedHolding{{
			Symbol:     "AAPLx",
			Mint:       "XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp",
			Units:      100,
			MarkUsdc:   185_000_000,
			CostBasis:  100_000_000,
			AfterHours: true,
		}},
		AfterHours: true,
	})

	// Act
	nav, err := pythClient.MarkedPot(context.Background(), treasury, []CostBasis{{
		Symbol: "AAPLx",
		Mint:   "XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp",
		Units:  100,
		Price:  100_000_000,
		Amount: 100,
	}})

	// Assert
	if err != nil {
		t.Fatalf("MarkedPot: %v", err)
	}
	if !nav.AfterHours {
		t.Fatal("expected pot after-hours flag on NavInput for GroupView")
	}
	if len(nav.Holdings) != 1 {
		t.Fatalf("expected one holding, got %d", len(nav.Holdings))
	}
	if !nav.Holdings[0].AfterHours {
		t.Fatal("expected holding after-hours flag on NavInput for GroupView")
	}
}

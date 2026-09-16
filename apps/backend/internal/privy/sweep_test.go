package privy

import (
	"context"
	"testing"
)

func TestSubmitSweep_callsPrivyClientWithMemberAndTreasuryAddresses(t *testing.T) {
	// Arrange
	client := NewFakeClient()
	req := SweepRequest{
		MemberAddress:   "FAKEmember123",
		TreasuryAddress: "FAKEtreasury456",
		Amount:          1_000_000,
		RelayerKey:      "relayer-key",
	}

	// Act
	result, err := client.SubmitSweep(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("SubmitSweep: %v", err)
	}
	if result.TxSignature == "" {
		t.Fatal("expected tx signature")
	}
	last, ok := LastSweepRequest(client)
	if !ok {
		t.Fatal("expected last sweep request")
	}
	if last.MemberAddress != req.MemberAddress {
		t.Fatalf("member = %q, want %q", last.MemberAddress, req.MemberAddress)
	}
	if last.TreasuryAddress != req.TreasuryAddress {
		t.Fatalf("treasury = %q, want %q", last.TreasuryAddress, req.TreasuryAddress)
	}
}

func TestSubmitSweep_includesRelayerAsFeePayer(t *testing.T) {
	// Arrange
	client := NewFakeClient()
	req, err := BuildSweepRequest("FAKEmember", "FAKEtreasury", 500_000, "relayer-fee-payer-key")
	if err != nil {
		t.Fatalf("BuildSweepRequest: %v", err)
	}

	// Act
	_, err = client.SubmitSweep(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("SubmitSweep: %v", err)
	}
	last, ok := LastSweepRequest(client)
	if !ok {
		t.Fatal("expected last sweep request")
	}
	if last.RelayerKey != "relayer-fee-payer-key" {
		t.Fatalf("relayer key = %q, want relayer-fee-payer-key", last.RelayerKey)
	}
}

package postgres

import (
	"context"
	"fmt"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
)

func TestListNetTokenHoldingsByGroup_returnsDecimalsPerMint(t *testing.T) {
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	_, groupID := seedProposalGroup(t, store, iso)

	const tesseraMint = "TSPXcLV76s6V2zDiZQ18kBfcbnjaE2ZzNT3ga2Pd99v"
	const buyTokens int64 = 1_000_000_000
	const buyUSDC int64 = 10_000_000

	_, _, err := store.ConfirmBuyTransaction(ctx, ConfirmBuyTransactionParams{
		GroupID:          groupID,
		Amount:           buyUSDC,
		InputMint:        jupiter.USDCMint,
		OutputMint:       tesseraMint,
		TxSignature:      fmt.Sprintf("sig-%s-tessera-decimals", iso.Suffix()),
		ExecuteRequestID: fmt.Sprintf("req-%s-tessera-decimals", iso.Suffix()),
		CostBasisPrice:   buyUSDC,
		CostBasisAmount:  buyTokens,
		TokenDecimals:    9,
	})
	if err != nil {
		t.Fatalf("ConfirmBuyTransaction: %v", err)
	}

	holdings, err := store.ListNetTokenHoldingsByGroup(ctx, groupID)
	if err != nil {
		t.Fatalf("ListNetTokenHoldingsByGroup: %v", err)
	}

	var found *TokenHoldingRow
	for i := range holdings {
		if holdings[i].Mint == tesseraMint {
			found = &holdings[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected holding for %s, got %+v", tesseraMint, holdings)
	}
	if found.TokenDecimals != 9 {
		t.Fatalf("TokenDecimals = %d, want 9", found.TokenDecimals)
	}
}

package app

import "github.com/monaco/monaco/apps/backend/internal/postgres"

func buildObservedSweep(overrides map[string]any) ObservedSweep {
	sweep := ObservedSweep{
		TxSignature: "SWEEP-test-signature",
		FromAddress: "FAKEmember",
		ToAddress:   "FAKEtreasury",
		Amount:      1_000_000,
		DepositID:   "00000000-0000-0000-0000-000000000001",
		UserID:      "00000000-0000-0000-0000-000000000002",
		GroupID:     "00000000-0000-0000-0000-000000000003",
	}
	if v, ok := overrides["TxSignature"].(string); ok {
		sweep.TxSignature = v
	}
	if v, ok := overrides["FromAddress"].(string); ok {
		sweep.FromAddress = v
	}
	if v, ok := overrides["ToAddress"].(string); ok {
		sweep.ToAddress = v
	}
	if v, ok := overrides["Amount"].(int64); ok {
		sweep.Amount = v
	}
	if v, ok := overrides["DepositID"].(string); ok {
		sweep.DepositID = v
	}
	if v, ok := overrides["UserID"].(string); ok {
		sweep.UserID = v
	}
	if v, ok := overrides["GroupID"].(string); ok {
		sweep.GroupID = v
	}
	return sweep
}

func depositRowToDeposit(row postgres.DepositRow) Deposit {
	return depositFromRow(row)
}

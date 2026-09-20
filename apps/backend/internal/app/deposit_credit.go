package app

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/monaco/monaco/packages/domain"
)

func (d *DepositService) shareCreditForSweep(ctx context.Context, tx *sql.Tx, groupID, treasuryAddress string, swept int64) (int64, error) {
	treasuryUsdc, err := d.wallets.TreasuryUSDCBalance(ctx, treasuryAddress)
	if err != nil {
		return 0, fmt.Errorf("treasury usdc balance: %w", err)
	}

	totalSharesMicro, err := d.store.SumShareUnitsByGroupTx(ctx, tx, groupID)
	if err != nil {
		return 0, err
	}
	totalShares, err := domain.ShareUnitsMicrosToDomain(totalSharesMicro)
	if err != nil {
		return 0, err
	}

	holdings, err := d.store.ListNetTokenHoldingsByGroupTx(ctx, tx, groupID)
	if err != nil {
		return 0, err
	}

	if len(holdings) == 0 {
		// M2 path: USDC-only pot credits share_units 1:1 with swept USDC.
		return swept, nil
	}

	// Post-sweep Privy balance includes inbound USDC; NAV for minting must use pre-credit treasury.
	treasuryUsdcPreCredit := treasuryUsdc - swept
	if treasuryUsdcPreCredit < 0 {
		treasuryUsdcPreCredit = 0
	}

	if d.pyth == nil {
		return 0, fmt.Errorf("pyth client is required for marked pot deposit credit")
	}
	pythInput, err := fetchMarkedPotInput(ctx, d.store, d.pyth, d.symbols, tx, groupID, treasuryAddress, treasuryUsdcPreCredit, holdings)
	if err != nil {
		return 0, err
	}
	navInput, err := domainNavInputFromPyth(pythInput, totalShares)
	if err != nil {
		return 0, err
	}

	nav, err := ComputePotNAV(navInput)
	if err != nil {
		return 0, fmt.Errorf("compute pot nav: %w", err)
	}
	return domain.ShareUnitsMicrosForDeposit(domain.USDCMicros(swept), nav)
}

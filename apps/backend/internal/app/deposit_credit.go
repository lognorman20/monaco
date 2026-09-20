package app

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/packages/domain"
)

// shareCreditForSweep prices a confirmed sweep against the pot as it stood before the USDC
// arrived and returns the share units to mint plus the NAV snapshot for the pot after the
// credit. It returns ErrPotMarkUnavailable when a holding has no live price: the deposit
// then stays pending and is retried, never minted at cost basis.
func (d *DepositService) shareCreditForSweep(ctx context.Context, tx *sql.Tx, groupID, treasuryAddress string, swept int64) (int64, postgres.NavSnapshotValues, error) {
	treasuryUsdc, err := d.privy.TreasuryUSDCBalance(ctx, treasuryAddress)
	if err != nil {
		return 0, postgres.NavSnapshotValues{}, fmt.Errorf("treasury usdc balance: %w", err)
	}

	// Post-sweep Privy balance includes inbound USDC; NAV for minting must use pre-credit treasury.
	treasuryUsdcPreCredit := treasuryUsdc - swept
	if treasuryUsdcPreCredit < 0 {
		treasuryUsdcPreCredit = 0
	}

	valuation, err := valuePot(ctx, d.store, d.pyth, d.symbols, tx, groupID, treasuryAddress, treasuryUsdcPreCredit, potMarksLiveOnly)
	if err != nil {
		return 0, postgres.NavSnapshotValues{}, err
	}

	shareUnits, err := domain.ShareUnitsMicrosForDeposit(domain.USDCMicros(swept), valuation.ShareBaseMicros, domain.USDCMicros(valuation.PotNavMicros))
	if err != nil {
		return 0, postgres.NavSnapshotValues{}, err
	}

	snapshot, err := navSnapshotValuesFor(valuation.PotNavMicros+swept, valuation.ShareBaseMicros+shareUnits)
	if err != nil {
		return 0, postgres.NavSnapshotValues{}, err
	}
	return shareUnits, snapshot, nil
}

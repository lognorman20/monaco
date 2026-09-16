package app

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/packages/domain"
)

const tokenAtomicScale int64 = 1_000_000

func (d *DepositService) shareCreditForSweep(ctx context.Context, groupID, treasuryAddress string, swept int64) (int64, error) {
	treasuryUsdc, err := d.privy.TreasuryUSDCBalance(ctx, treasuryAddress)
	if err != nil {
		return 0, fmt.Errorf("treasury usdc balance: %w", err)
	}

	totalSharesMicro, err := d.store.SumShareUnitsByGroup(ctx, groupID)
	if err != nil {
		return 0, err
	}
	totalShares, err := domain.ShareUnitsMicrosToDomain(totalSharesMicro)
	if err != nil {
		return 0, err
	}

	holdings, err := d.store.ListNetTokenHoldingsByGroup(ctx, groupID)
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
	costBasis, err := d.costBasisForGroup(ctx, groupID, holdings)
	if err != nil {
		return 0, err
	}
	pythInput, err := d.pyth.MarkedPot(ctx, pyth.TreasuryRef{
		GroupID:      groupID,
		Address:      treasuryAddress,
		TreasuryUsdc: treasuryUsdcPreCredit,
	}, costBasis)
	if err != nil {
		return 0, fmt.Errorf("marked pot: %w", err)
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

func (d *DepositService) costBasisForGroup(ctx context.Context, groupID string, holdings []postgres.TokenHoldingRow) ([]pyth.CostBasis, error) {
	out := make([]pyth.CostBasis, 0, len(holdings))
	for _, holding := range holdings {
		price, _, found, err := d.store.GetFillDerivedCostBasisByOutputMint(ctx, groupID, holding.Mint)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("cost basis not found for mint %s", holding.Mint)
		}
		out = append(out, pyth.CostBasis{
			Symbol: symbolForOutputMint(holding.Mint),
			Mint:   holding.Mint,
			Units:  holding.Amount,
			Price:  price,
			Amount: holding.Amount,
		})
	}
	return out, nil
}

func domainNavInputFromPyth(input pyth.NavInput, totalShares domain.ShareUnits) (domain.NavInput, error) {
	holdings := make([]domain.MarkedHolding, 0, len(input.Holdings))
	for _, holding := range input.Holdings {
		units, err := tokenAtomicsToDecimalUnits(holding.Units)
		if err != nil {
			return domain.NavInput{}, err
		}
		holdings = append(holdings, domain.MarkedHolding{
			Symbol:     holding.Symbol,
			Units:      string(units),
			MarkUsdc:   domain.USDCMicros(holding.MarkUsdc),
			CostBasis:  domain.USDCMicros(holding.CostBasis),
			AfterHours: holding.AfterHours,
		})
	}
	return domain.NavInput{
		Mode:         domain.NavMarked,
		TreasuryUsdc: domain.USDCMicros(input.TreasuryUsdc),
		TotalShares:  totalShares,
		Holdings:     holdings,
	}, nil
}

func tokenAtomicsToDecimalUnits(atomics int64) (domain.ShareUnits, error) {
	if atomics < 0 {
		return "", fmt.Errorf("token atomics must be non-negative")
	}
	if atomics == 0 {
		return domain.ShareUnits("0"), nil
	}
	r := new(big.Rat).SetFrac(big.NewInt(atomics), big.NewInt(tokenAtomicScale))
	s := strings.TrimRight(r.FloatString(6), "0")
	s = strings.TrimRight(s, ".")
	return domain.ShareUnits(s), nil
}

func symbolForOutputMint(mint string) string {
	switch mint {
	case jupiter.AAPLxMint:
		return "AAPLx"
	default:
		return mint
	}
}

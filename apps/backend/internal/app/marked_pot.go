package app

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math/big"
	"strconv"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/packages/domain"
)

func tokenAtomicsToDecimalUnits(atomics int64) (domain.ShareUnits, error) {
	if atomics < 0 {
		return "", fmt.Errorf("token atomics must be non-negative")
	}
	if atomics == 0 {
		return domain.ShareUnits("0"), nil
	}
	r := new(big.Rat).SetFrac(big.NewInt(atomics), big.NewInt(jupiter.XStockAtomicScale))
	s := strings.TrimRight(r.FloatString(8), "0")
	s = strings.TrimRight(s, ".")
	return domain.ShareUnits(s), nil
}

func costBasisMarkPerUnitMicros(totalUSDCMicros, tokenAtomics int64) (int64, error) {
	if totalUSDCMicros < 0 {
		return 0, fmt.Errorf("cost basis usdc must be non-negative")
	}
	if tokenAtomics <= 0 {
		return 0, fmt.Errorf("cost basis token amount must be positive")
	}
	mark, err := domain.MulDivFloor(totalUSDCMicros, jupiter.XStockAtomicScale, tokenAtomics)
	if err != nil {
		return 0, fmt.Errorf("derive mark per unit: %w", err)
	}
	if mark <= 0 {
		return 0, fmt.Errorf("derived mark per unit must be positive")
	}
	return mark, nil
}

// groupPotView is marked treasury NAV plus per-asset pot rows for group screens.
type groupPotView struct {
	PotNavMicros int64
	Rows         []GroupViewPotRow
	AfterHours   bool
}

func computeGroupPotView(
	ctx context.Context,
	store *postgres.Store,
	pythClient pyth.Client,
	symbols *SymbolResolver,
	groupID, treasuryAddress string,
	treasuryUSDC int64,
) (groupPotView, error) {
	if groupID == "" {
		return groupPotView{}, fmt.Errorf("group_id is required")
	}
	if treasuryUSDC < 0 {
		return groupPotView{}, fmt.Errorf("treasury usdc must be non-negative")
	}

	holdings, err := store.ListNetTokenHoldingsByGroup(ctx, groupID)
	if err != nil {
		return groupPotView{}, err
	}

	totalSharesMicro, err := store.SumShareUnitsByGroup(ctx, groupID)
	if err != nil {
		return groupPotView{}, err
	}
	totalShares, err := domain.ShareUnitsMicrosToDomain(totalSharesMicro)
	if err != nil {
		return groupPotView{}, err
	}

	if len(holdings) == 0 {
		potNav := treasuryUSDC
		if totalSharesMicro > 0 && potNav > totalSharesMicro {
			potNav = totalSharesMicro
		}
		if potNav == 0 && totalSharesMicro > 0 {
			potNav = totalSharesMicro
		}
		return groupPotView{
			PotNavMicros: potNav,
			Rows: []GroupViewPotRow{{
				Symbol:    "USDC",
				Units:     formatMicrosAsUsdDecimal(treasuryUSDC),
				MarkUsd:   "1.00",
				ValueUsd:  formatMicrosAsUsdDecimal(treasuryUSDC),
				DollarPnL: formatSignedDollarPnL(0),
			}},
		}, nil
	}

	pythInput, err := fetchMarkedPotInput(ctx, store, pythClient, symbols, nil, groupID, treasuryAddress, treasuryUSDC, holdings)
	if err != nil {
		return groupPotView{}, err
	}

	navInput, err := domainNavInputFromPyth(pythInput, totalShares)
	if err != nil {
		return groupPotView{}, err
	}
	nav, err := ComputePotNAV(navInput)
	if err != nil {
		return groupPotView{}, fmt.Errorf("compute pot nav: %w", err)
	}

	rows, err := potRowsFromPythInput(pythInput)
	if err != nil {
		return groupPotView{}, err
	}

	return groupPotView{
		PotNavMicros: int64(nav.TotalUsdc),
		Rows:         rows,
		AfterHours:   pythInput.AfterHours,
	}, nil
}

func fetchMarkedPotInput(
	ctx context.Context,
	store *postgres.Store,
	pythClient pyth.Client,
	symbols *SymbolResolver,
	tx *sql.Tx,
	groupID, treasuryAddress string,
	treasuryUSDC int64,
	holdings []postgres.TokenHoldingRow,
) (pyth.NavInput, error) {
	costBasis, err := costBasisForHoldings(ctx, store, symbols, tx, groupID, holdings)
	if err != nil {
		return pyth.NavInput{}, err
	}

	treasuryRef := pyth.TreasuryRef{
		GroupID:      groupID,
		Address:      treasuryAddress,
		TreasuryUsdc: treasuryUSDC,
	}

	if pythClient != nil {
		input, err := pythClient.MarkedPot(ctx, treasuryRef, costBasis)
		if err != nil {
			slog.Warn("marked pot pricing failed; using cost basis fallback",
				"group_id", groupID,
				"treasury_address", treasuryAddress,
				"err", err,
			)
			return costBasisMarkedPotInput(treasuryUSDC, costBasis)
		}
		if input.TreasuryUsdc == 0 {
			input.TreasuryUsdc = treasuryUSDC
		}
		logMarkedPotSources(groupID, tx != nil, input)
		return input, nil
	}

	return costBasisMarkedPotInput(treasuryUSDC, costBasis)
}

// logMarkedPotSources records which price source valued each holding, so a NAV (and any
// shares minted against it) can be traced back to the prices behind it.
func logMarkedPotSources(groupID string, mintsShares bool, input pyth.NavInput) {
	sources := make([]string, 0, len(input.Holdings))
	for _, holding := range input.Holdings {
		source := holding.Source
		if source == "" {
			source = pyth.MarkSourcePyth
		}
		sources = append(sources, fmt.Sprintf("%s=%s@%d", holding.Symbol, source, holding.MarkUsdc))
	}
	slog.Info("marked pot valued",
		"group_id", groupID,
		"mints_shares", mintsShares,
		"marks", strings.Join(sources, ","),
	)
}

func costBasisForHoldings(
	ctx context.Context,
	store *postgres.Store,
	symbols *SymbolResolver,
	tx *sql.Tx,
	groupID string,
	holdings []postgres.TokenHoldingRow,
) ([]pyth.CostBasis, error) {
	out := make([]pyth.CostBasis, 0, len(holdings))
	for _, holding := range holdings {
		if holding.Amount <= 0 {
			continue
		}
		price, amount, found, err := fillDerivedCostBasis(ctx, store, tx, groupID, holding.Mint)
		if err != nil {
			return nil, err
		}
		if !found || amount <= 0 {
			return nil, fmt.Errorf("cost basis not found for mint %s", holding.Mint)
		}
		out = append(out, pyth.CostBasis{
			Symbol: symbolForOutputMint(ctx, symbols, holding.Mint),
			Mint:   holding.Mint,
			Units:  holding.Amount,
			Price:  price,
			Amount: amount,
		})
	}
	return out, nil
}

func fillDerivedCostBasis(
	ctx context.Context,
	store *postgres.Store,
	tx *sql.Tx,
	groupID, mint string,
) (price, amount int64, found bool, err error) {
	if tx != nil {
		return store.GetFillDerivedCostBasisByOutputMintTx(ctx, tx, groupID, mint)
	}
	return store.GetFillDerivedCostBasisByOutputMint(ctx, groupID, mint)
}

func costBasisMarkedPotInput(treasuryUSDC int64, costBasis []pyth.CostBasis) (pyth.NavInput, error) {
	marked := make([]pyth.MarkedHolding, 0, len(costBasis))
	for _, holding := range costBasis {
		markPerUnit, err := costBasisMarkPerUnitMicros(holding.Price, holding.Amount)
		if err != nil {
			return pyth.NavInput{}, err
		}
		marked = append(marked, pyth.MarkedHolding{
			Symbol:    holding.Symbol,
			Mint:      holding.Mint,
			Units:     holding.Units,
			MarkUsdc:  markPerUnit,
			CostBasis: holding.Price,
			Source:    pyth.MarkSourceCostBasis,
		})
	}
	return pyth.NavInput{
		TreasuryUsdc: treasuryUSDC,
		Holdings:     marked,
		AfterHours:   pyth.PotAfterHours(marked),
	}, nil
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
	mode := domain.NavUSDCOnly
	if len(holdings) > 0 {
		mode = domain.NavMarked
	}
	return domain.NavInput{
		Mode:         mode,
		TreasuryUsdc: domain.USDCMicros(input.TreasuryUsdc),
		TotalShares:  totalShares,
		Holdings:     holdings,
	}, nil
}

func potRowsFromPythInput(input pyth.NavInput) ([]GroupViewPotRow, error) {
	rows := []GroupViewPotRow{{
		Symbol:    "USDC",
		Units:     formatMicrosAsUsdDecimal(input.TreasuryUsdc),
		MarkUsd:   "1.00",
		ValueUsd:  formatMicrosAsUsdDecimal(input.TreasuryUsdc),
		DollarPnL: formatSignedDollarPnL(0),
	}}

	for _, holding := range input.Holdings {
		if holding.Units <= 0 {
			continue
		}
		units, err := tokenAtomicsToDecimalUnits(holding.Units)
		if err != nil {
			return nil, err
		}
		valueMicros, err := domain.MulDivFloor(holding.Units, holding.MarkUsdc, jupiter.XStockAtomicScale)
		if err != nil {
			return nil, fmt.Errorf("value %s holding: %w", holding.Symbol, err)
		}
		var afterHours *bool
		if holding.AfterHours {
			afterHours = boolPtr(true)
		}
		rows = append(rows, GroupViewPotRow{
			Symbol:      holding.Symbol,
			Units:       string(units),
			MarkUsd:     formatMicrosAsUsdDecimal(holding.MarkUsdc),
			ValueUsd:    formatMicrosAsUsdDecimal(valueMicros),
			DollarPnL:   formatSignedDollarPnL(valueMicros - holding.CostBasis),
			AfterHours:  afterHours,
			TokenAmount: strconv.FormatInt(holding.Units, 10),
		})
	}
	return rows, nil
}

func boolPtr(v bool) *bool {
	return &v
}

func symbolForOutputMint(ctx context.Context, symbols *SymbolResolver, mint string) string {
	if symbols != nil {
		return symbols.SymbolForMint(ctx, mint)
	}
	if symbol, ok := knownMintSymbol(mint); ok {
		return symbol
	}
	if looksLikeSolanaMint(mint) {
		return unknownStockSymbol
	}
	return mint
}

package app

import (
	"testing"
	"time"
)

func TestFoldHeldRows_oneSymbolAcrossTwoCabals(t *testing.T) {
	t.Parallel()
	got := foldHeldRows([]heldCabalRow{
		{symbol: "AAPLx", groupID: "g1", groupName: "Weekend investors", units: "4.2", valueMicros: 974_610_000, pnlMicros: 112_400_000, sliceMicros: 243_650_000},
		{symbol: "AAPLx", groupID: "g2", groupName: "Semis or bust", units: "1.1", valueMicros: 255_260_000, pnlMicros: -18_900_000, sliceMicros: 51_050_000},
	})
	if len(got) != 1 {
		t.Fatalf("held = %d rows, want 1", len(got))
	}
	if len(got[0].Cabals) != 2 {
		t.Fatalf("cabals = %d, want 2", len(got[0].Cabals))
	}
	if got[0].TotalValueUsd != "1229.87" {
		t.Fatalf("totalValueUsd = %q, want 1229.87", got[0].TotalValueUsd)
	}
	if got[0].TotalDollarPnL != "+93.50" {
		t.Fatalf("totalDollarPnl = %q, want +93.50", got[0].TotalDollarPnL)
	}
	if got[0].MySliceUsd != "294.70" {
		t.Fatalf("mySliceUsd = %q, want 294.70", got[0].MySliceUsd)
	}
}

func TestFoldHeldRows_biggestPositionFirst(t *testing.T) {
	t.Parallel()
	got := foldHeldRows([]heldCabalRow{
		{symbol: "SMALLx", groupID: "g1", valueMicros: 1_000_000},
		{symbol: "BIGx", groupID: "g1", valueMicros: 9_000_000},
		{symbol: "MIDx", groupID: "g1", valueMicros: 4_000_000},
	})
	want := []string{"BIGx", "MIDx", "SMALLx"}
	for i, symbol := range want {
		if got[i].Symbol != symbol {
			t.Fatalf("held[%d] = %q, want %q", i, got[i].Symbol, symbol)
		}
	}
}

func TestFoldHeldRows_sameSymbolDifferentCaseIsOneRow(t *testing.T) {
	t.Parallel()
	got := foldHeldRows([]heldCabalRow{
		{symbol: "AAPLx", groupID: "g1", valueMicros: 1_000_000},
		{symbol: "aaplx", groupID: "g2", valueMicros: 2_000_000},
	})
	if len(got) != 1 {
		t.Fatalf("held = %d rows, want 1", len(got))
	}
	// The spelling the first cabal reported is the one that ships; the app looks
	// the symbol up in the catalogue either way.
	if got[0].Symbol != "AAPLx" {
		t.Fatalf("symbol = %q, want AAPLx", got[0].Symbol)
	}
}

func TestFoldHeldRows_nothingHeldIsAnEmptyList(t *testing.T) {
	t.Parallel()
	if got := foldHeldRows(nil); len(got) != 0 {
		t.Fatalf("held = %v, want empty", got)
	}
}

func TestFoldOpenProposals_countsVotesAndNamesEachCabalOnce(t *testing.T) {
	t.Parallel()
	soon := time.Date(2026, time.September, 22, 18, 0, 0, 0, time.UTC)
	later := soon.Add(2 * time.Hour)
	got := foldOpenProposals([]openProposalRow{
		{symbol: "NVDAx", groupName: "Semis or bust", expiresAt: later},
		{symbol: "NVDAx", groupName: "Semis or bust", expiresAt: soon},
	})
	if len(got) != 1 {
		t.Fatalf("upForVote = %d rows, want 1", len(got))
	}
	if got[0].OpenProposals != 2 {
		t.Fatalf("openProposals = %d, want 2", got[0].OpenProposals)
	}
	if len(got[0].CabalNames) != 1 {
		t.Fatalf("cabalNames = %v, want one entry", got[0].CabalNames)
	}
	if got[0].SoonestExpiresAt == nil || !got[0].SoonestExpiresAt.Equal(soon) {
		t.Fatalf("soonestExpiresAt = %v, want %v", got[0].SoonestExpiresAt, soon)
	}
}

func TestFoldOpenProposals_closingSoonestLeads(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, time.September, 22, 18, 0, 0, 0, time.UTC)
	got := foldOpenProposals([]openProposalRow{
		{symbol: "LATEx", expiresAt: base.Add(8 * time.Hour)},
		{symbol: "SOONx", expiresAt: base.Add(30 * time.Minute)},
		{symbol: "MIDx", expiresAt: base.Add(3 * time.Hour)},
	})
	want := []string{"SOONx", "MIDx", "LATEx"}
	for i, symbol := range want {
		if got[i].Symbol != symbol {
			t.Fatalf("upForVote[%d] = %q, want %q", i, got[i].Symbol, symbol)
		}
	}
}

func TestFoldOpenProposals_isUTC(t *testing.T) {
	t.Parallel()
	zone := time.FixedZone("ET", -4*3600)
	local := time.Date(2026, time.September, 22, 14, 0, 0, 0, zone)
	got := foldOpenProposals([]openProposalRow{{symbol: "AAPLx", expiresAt: local.UTC()}})
	if got[0].SoonestExpiresAt.Location() != time.UTC {
		t.Fatalf("location = %v, want UTC", got[0].SoonestExpiresAt.Location())
	}
}

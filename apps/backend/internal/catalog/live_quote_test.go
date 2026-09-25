package catalog

import (
	"math/big"
	"testing"
)

func TestReferenceExposure_spacexFixture_tesseraWinsWithCorrectMultiplier(t *testing.T) {
	spacexExp, ok := ReferenceExposureUsdcMicros(16688071, 9, big.NewRat(5, 1), 149.32, 100)
	if !ok {
		t.Fatal("spacex exposure")
	}
	tesseraExp, ok := ReferenceExposureUsdcMicros(17711094, 9, big.NewRat(1, 1), 746.61, 20)
	if !ok {
		t.Fatal("tessera exposure")
	}
	if tesseraExp <= spacexExp {
		t.Fatalf("tessera %d must exceed spacex %d", tesseraExp, spacexExp)
	}
	inverted, ok := ReferenceExposureUsdcMicros(16688071, 9, big.NewRat(1, 5), 149.32, 100)
	if !ok || inverted >= spacexExp {
		t.Fatal("inverting multiplier must shrink SPACEX exposure")
	}
}

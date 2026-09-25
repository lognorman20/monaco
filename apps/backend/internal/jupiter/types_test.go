package jupiter

import "testing"

func TestAtomicScale_panicsOutOfRange(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for decimals 13")
		}
	}()
	AtomicScale(13)
}

func TestAtomicScale_nineDecimals(t *testing.T) {
	if got := AtomicScale(9); got != 1_000_000_000 {
		t.Fatalf("AtomicScale(9) = %d, want 1e9", got)
	}
}

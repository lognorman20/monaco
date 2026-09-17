package balance

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeBalanceReader struct {
	balance uint64
	err     error
}

func (f *fakeBalanceReader) GetBalance(ctx context.Context, address string) (uint64, error) {
	_ = ctx
	_ = address
	return f.balance, f.err
}

func TestMustHaveSOL_acceptsBalanceAboveThreshold(t *testing.T) {
	// Arrange
	reader := &fakeBalanceReader{balance: FeePayerMinLamports + 1}
	ctx := context.Background()

	// Act
	err := MustHaveSOL(ctx, reader, "Relayer11111111111111111111111111111111111", FeePayerMinLamports)

	// Assert
	if err != nil {
		t.Fatalf("MustHaveSOL: %v", err)
	}
}

func TestMustHaveSOL_rejectsBalanceAtThreshold(t *testing.T) {
	// Arrange
	reader := &fakeBalanceReader{balance: FeePayerMinLamports}
	ctx := context.Background()

	// Act
	err := MustHaveSOL(ctx, reader, "Relayer11111111111111111111111111111111111", FeePayerMinLamports)

	// Assert
	if err == nil {
		t.Fatal("expected error at threshold balance")
	}
	if !strings.Contains(err.Error(), "balance too low") {
		t.Fatalf("error = %q, want balance too low", err.Error())
	}
	if !strings.Contains(err.Error(), "Relayer11111111111111111111111111111111111") {
		t.Fatalf("error = %q, want pubkey logged", err.Error())
	}
}

func TestMustHaveSOL_rejectsBalanceBelowThreshold(t *testing.T) {
	// Arrange
	reader := &fakeBalanceReader{balance: FeePayerMinLamports - 1}
	ctx := context.Background()

	// Act
	err := MustHaveSOL(ctx, reader, "Relayer11111111111111111111111111111111111", FeePayerMinLamports)

	// Assert
	if err == nil {
		t.Fatal("expected error below threshold balance")
	}
}

func TestMustHaveSOL_propagatesReaderError(t *testing.T) {
	// Arrange
	reader := &fakeBalanceReader{err: errors.New("rpc down")}
	ctx := context.Background()

	// Act
	err := MustHaveSOL(ctx, reader, "Relayer11111111111111111111111111111111111", FeePayerMinLamports)

	// Assert
	if err == nil {
		t.Fatal("expected rpc error")
	}
	if !strings.Contains(err.Error(), "rpc down") {
		t.Fatalf("error = %q, want rpc down", err.Error())
	}
}

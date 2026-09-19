package app

import (
	"context"
	"errors"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/privy"
)

type testSolanaConfirmer struct {
	confirmed map[string]bool
}

func newTestSolanaConfirmer() *testSolanaConfirmer {
	return &testSolanaConfirmer{confirmed: make(map[string]bool)}
}

func (f *testSolanaConfirmer) IsConfirmed(ctx context.Context, txSignature string) (bool, error) {
	_ = ctx
	return f.confirmed[txSignature], nil
}

func (f *testSolanaConfirmer) Confirm(txSignature string) {
	f.confirmed[txSignature] = true
}

func newPlatformWithdrawHarness(t *testing.T) (integrationHarness, *PlatformWithdrawService, *testSolanaConfirmer) {
	t.Helper()
	h := integrationApp(t)
	solanaRPC := newTestSolanaConfirmer()
	svc := NewPlatformWithdrawService(h.Store, h.Privy, h.Deposits, solanaRPC, "relayer-test-key")
	return h, svc, solanaRPC
}

func TestCreatePlatformWithdrawal_success(t *testing.T) {
	h, svc, solanaRPC := newPlatformWithdrawHarness(t)
	ctx := context.Background()
	sessions := NewSessionService(h.Store, h.Privy)
	token := privy.AccessToken(h.ISO.UniqueToken("pw-success"))
	session := openTestSession(t, h.ISO, sessions, h.Privy, "pw-success", "Withdrawer")

	wallet, found, err := h.Store.GetMemberWalletByUserID(ctx, session.UserID)
	if err != nil || !found {
		t.Fatalf("GetMemberWalletByUserID: found=%v err=%v", found, err)
	}
	privy.SetMemberUSDCBalance(h.Privy, wallet.SolanaAddress, 3_000_000)

	dest := "11111111111111111111111111111112"
	result, err := svc.CreatePlatformWithdrawal(ctx, string(token), 1_000_000, dest)
	if err != nil {
		t.Fatalf("CreatePlatformWithdrawal: %v", err)
	}
	if result.TxSignature == "" {
		t.Fatal("expected tx signature")
	}
	if result.Status != PlatformWithdrawalStatusPending {
		t.Fatalf("status = %q, want pending before confirmation", result.Status)
	}

	solanaRPC.Confirm(result.TxSignature)
	got, err := svc.GetPlatformWithdrawal(ctx, string(token), result.ID)
	if err != nil {
		t.Fatalf("GetPlatformWithdrawal: %v", err)
	}
	if got.Status != PlatformWithdrawalStatusConfirmed {
		t.Fatalf("status = %q, want confirmed", got.Status)
	}

	transfer, ok := privy.LastTransferRequest(h.Privy)
	if !ok {
		t.Fatal("expected transfer request")
	}
	if transfer.Amount != 1_000_000 || transfer.ToAddress != dest {
		t.Fatalf("transfer = %+v", transfer)
	}
}

func TestCreatePlatformWithdrawal_rejectsOverBalance(t *testing.T) {
	h, svc, _ := newPlatformWithdrawHarness(t)
	ctx := context.Background()
	sessions := NewSessionService(h.Store, h.Privy)
	token := privy.AccessToken(h.ISO.UniqueToken("pw-over"))
	session := openTestSession(t, h.ISO, sessions, h.Privy, "pw-over", "Over")

	wallet, found, err := h.Store.GetMemberWalletByUserID(ctx, session.UserID)
	if err != nil || !found {
		t.Fatalf("GetMemberWalletByUserID: found=%v err=%v", found, err)
	}
	privy.SetMemberUSDCBalance(h.Privy, wallet.SolanaAddress, 500_000)

	_, err = svc.CreatePlatformWithdrawal(ctx, string(token), 750_000, "11111111111111111111111111111112")
	if !errors.Is(err, ErrInsufficientPlatformBalance) {
		t.Fatalf("err = %v, want ErrInsufficientPlatformBalance", err)
	}
}

func TestCreatePlatformWithdrawal_rejectsInvalidAddress(t *testing.T) {
	h, svc, _ := newPlatformWithdrawHarness(t)
	ctx := context.Background()
	sessions := NewSessionService(h.Store, h.Privy)
	token := privy.AccessToken(h.ISO.UniqueToken("pw-invalid"))
	openTestSession(t, h.ISO, sessions, h.Privy, "pw-invalid", "Invalid")

	_, err := svc.CreatePlatformWithdrawal(ctx, string(token), 100_000, "not-a-valid-address!!!")
	if !errors.Is(err, ErrInvalidWithdrawAddress) {
		t.Fatalf("err = %v, want ErrInvalidWithdrawAddress", err)
	}
}

func TestCreatePlatformWithdrawal_idempotentOnTxSignature(t *testing.T) {
	h, svc, solanaRPC := newPlatformWithdrawHarness(t)
	ctx := context.Background()
	sessions := NewSessionService(h.Store, h.Privy)
	token := privy.AccessToken(h.ISO.UniqueToken("pw-idem"))
	session := openTestSession(t, h.ISO, sessions, h.Privy, "pw-idem", "Idem")

	wallet, found, err := h.Store.GetMemberWalletByUserID(ctx, session.UserID)
	if err != nil || !found {
		t.Fatalf("wallet lookup failed")
	}
	privy.SetMemberUSDCBalance(h.Privy, wallet.SolanaAddress, 2_000_000)

	dest := "11111111111111111111111111111112"
	first, err := svc.CreatePlatformWithdrawal(ctx, string(token), 500_000, dest)
	if err != nil {
		t.Fatalf("first CreatePlatformWithdrawal: %v", err)
	}

	row, found, err := h.Store.GetPlatformWithdrawalByTxSignature(ctx, first.TxSignature)
	if err != nil || !found {
		t.Fatalf("GetPlatformWithdrawalByTxSignature: found=%v err=%v", found, err)
	}
	if row.ID != first.ID {
		t.Fatalf("signature row id = %q, want %q", row.ID, first.ID)
	}

	solanaRPC.Confirm(first.TxSignature)
	second, err := svc.GetPlatformWithdrawal(ctx, string(token), first.ID)
	if err != nil {
		t.Fatalf("GetPlatformWithdrawal: %v", err)
	}
	if second.Status != PlatformWithdrawalStatusConfirmed {
		t.Fatalf("status = %q, want confirmed", second.Status)
	}
}

func TestCreatePlatformWithdrawal_failsPendingRowWhenPersistSignatureFails(t *testing.T) {
	h, svc, _ := newPlatformWithdrawHarness(t)
	ctx := context.Background()
	sessions := NewSessionService(h.Store, h.Privy)
	token := privy.AccessToken(h.ISO.UniqueToken("pw-persist-fail"))
	session := openTestSession(t, h.ISO, sessions, h.Privy, "pw-persist-fail", "Persist Fail")

	wallet, found, err := h.Store.GetMemberWalletByUserID(ctx, session.UserID)
	if err != nil || !found {
		t.Fatalf("GetMemberWalletByUserID: found=%v err=%v", found, err)
	}
	privy.SetMemberUSDCBalance(h.Privy, wallet.SolanaAddress, 2_000_000)

	injectedErr := errors.New("injected persist signature failure")
	h.Store.SetFailPlatformWithdrawalBroadcastSignatureForTests(true, injectedErr)
	t.Cleanup(func() {
		h.Store.SetFailPlatformWithdrawalBroadcastSignatureForTests(false, nil)
	})

	dest := "11111111111111111111111111111112"
	_, err = svc.CreatePlatformWithdrawal(ctx, string(token), 500_000, dest)
	if err == nil {
		t.Fatal("expected persist signature error")
	}
	if !errors.Is(err, injectedErr) {
		t.Fatalf("err = %v, want injected persist signature failure", err)
	}

	hasPending, err := h.Store.HasPendingPlatformWithdrawalForUser(ctx, session.UserID)
	if err != nil {
		t.Fatalf("HasPendingPlatformWithdrawalForUser: %v", err)
	}
	if hasPending {
		t.Fatal("expected orphan pending row to be failed")
	}

	h.Store.SetFailPlatformWithdrawalBroadcastSignatureForTests(false, nil)
	_, err = svc.CreatePlatformWithdrawal(ctx, string(token), 500_000, dest)
	if err != nil {
		t.Fatalf("retry after failed persist should succeed: %v", err)
	}
}

func TestCreatePlatformWithdrawal_failsDuplicatePendingRowOnExistingSignature(t *testing.T) {
	h, svc, solanaRPC := newPlatformWithdrawHarness(t)
	ctx := context.Background()
	sessions := NewSessionService(h.Store, h.Privy)
	token := privy.AccessToken(h.ISO.UniqueToken("pw-dup-sig"))
	session := openTestSession(t, h.ISO, sessions, h.Privy, "pw-dup-sig", "Dup Sig")

	wallet, found, err := h.Store.GetMemberWalletByUserID(ctx, session.UserID)
	if err != nil || !found {
		t.Fatalf("GetMemberWalletByUserID: found=%v err=%v", found, err)
	}
	privy.SetMemberUSDCBalance(h.Privy, wallet.SolanaAddress, 3_000_000)

	dest := "11111111111111111111111111111112"
	first, err := svc.CreatePlatformWithdrawal(ctx, string(token), 500_000, dest)
	if err != nil {
		t.Fatalf("first CreatePlatformWithdrawal: %v", err)
	}

	solanaRPC.Confirm(first.TxSignature)
	confirmed, err := svc.GetPlatformWithdrawal(ctx, string(token), first.ID)
	if err != nil {
		t.Fatalf("GetPlatformWithdrawal: %v", err)
	}
	if confirmed.Status != PlatformWithdrawalStatusConfirmed {
		t.Fatalf("status = %q, want confirmed", confirmed.Status)
	}

	privy.SetForcedTransferSignature(h.Privy, first.TxSignature)
	t.Cleanup(func() {
		privy.SetForcedTransferSignature(h.Privy, "")
	})

	second, err := svc.CreatePlatformWithdrawal(ctx, string(token), 700_000, dest)
	if err != nil {
		t.Fatalf("duplicate signature CreatePlatformWithdrawal: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("duplicate signature id = %q, want %q", second.ID, first.ID)
	}

	hasPending, err := h.Store.HasPendingPlatformWithdrawalForUser(ctx, session.UserID)
	if err != nil {
		t.Fatalf("HasPendingPlatformWithdrawalForUser: %v", err)
	}
	if hasPending {
		t.Fatal("expected duplicate pending row to be failed")
	}

	privy.SetForcedTransferSignature(h.Privy, "")
	third, err := svc.CreatePlatformWithdrawal(ctx, string(token), 400_000, dest)
	if err != nil {
		t.Fatalf("withdrawal after duplicate cleanup: %v", err)
	}
	if third.ID == first.ID {
		t.Fatalf("expected new withdrawal row, got reused id %q", third.ID)
	}
}

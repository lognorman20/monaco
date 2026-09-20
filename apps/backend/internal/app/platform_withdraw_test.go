package app

import (
	"context"
	"errors"
	"github.com/monaco/monaco/apps/backend/internal/auth"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/wallets"
)

type testSolanaConfirmer struct {
	confirmed map[string]bool
}

func newTestSolanaConfirmer() *testSolanaConfirmer {
	return &testSolanaConfirmer{confirmed: make(map[string]bool)}
}

func (f *testSolanaConfirmer) IsConfirmed(ctx context.Context, txHash string) (bool, error) {
	_ = ctx
	return f.confirmed[txHash], nil
}

func (f *testSolanaConfirmer) Confirm(txHash string) {
	f.confirmed[txHash] = true
}

func newPlatformWithdrawHarness(t *testing.T) (integrationHarness, *PlatformWithdrawService, *testSolanaConfirmer) {
	t.Helper()
	h := integrationApp(t)
	solanaRPC := newTestSolanaConfirmer()
	svc := NewPlatformWithdrawService(h.Store, h.Auth, h.Wallets, h.Deposits, solanaRPC)
	return h, svc, solanaRPC
}

func TestCreatePlatformWithdrawal_success(t *testing.T) {
	h, svc, solanaRPC := newPlatformWithdrawHarness(t)
	ctx := context.Background()
	sessions := NewSessionService(h.Store, h.Auth, h.Wallets)
	token := auth.AccessToken(h.ISO.UniqueToken("pw-success"))
	session := openTestSession(t, h.ISO, sessions, h.Auth, "pw-success", "Withdrawer")

	wallet, found, err := h.Store.GetMemberWalletByUserID(ctx, session.UserID)
	if err != nil || !found {
		t.Fatalf("GetMemberWalletByUserID: found=%v err=%v", found, err)
	}
	wallets.SetMemberUSDCBalance(h.Privy, wallet.Address, 3_000_000)

	dest := "0x000000000000000000000000000000000000dEaD"
	result, err := svc.CreatePlatformWithdrawal(ctx, string(token), 1_000_000, dest)
	if err != nil {
		t.Fatalf("CreatePlatformWithdrawal: %v", err)
	}
	if result.TxHash == "" {
		t.Fatal("expected tx signature")
	}
	if result.Status != PlatformWithdrawalStatusPending {
		t.Fatalf("status = %q, want pending before confirmation", result.Status)
	}

	solanaRPC.Confirm(result.TxHash)
	got, err := svc.GetPlatformWithdrawal(ctx, string(token), result.ID)
	if err != nil {
		t.Fatalf("GetPlatformWithdrawal: %v", err)
	}
	if got.Status != PlatformWithdrawalStatusConfirmed {
		t.Fatalf("status = %q, want confirmed", got.Status)
	}

	transfer, ok := wallets.LastTransferRequest(h.Wallets)
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
	sessions := NewSessionService(h.Store, h.Auth, h.Wallets)
	token := auth.AccessToken(h.ISO.UniqueToken("pw-over"))
	session := openTestSession(t, h.ISO, sessions, h.Auth, "pw-over", "Over")

	wallet, found, err := h.Store.GetMemberWalletByUserID(ctx, session.UserID)
	if err != nil || !found {
		t.Fatalf("GetMemberWalletByUserID: found=%v err=%v", found, err)
	}
	wallets.SetMemberUSDCBalance(h.Privy, wallet.Address, 500_000)

	_, err = svc.CreatePlatformWithdrawal(ctx, string(token), 750_000, "0x000000000000000000000000000000000000dEaD")
	if !errors.Is(err, ErrInsufficientPlatformBalance) {
		t.Fatalf("err = %v, want ErrInsufficientPlatformBalance", err)
	}
}

func TestCreatePlatformWithdrawal_rejectsInvalidAddress(t *testing.T) {
	h, svc, _ := newPlatformWithdrawHarness(t)
	ctx := context.Background()
	sessions := NewSessionService(h.Store, h.Auth, h.Wallets)
	token := auth.AccessToken(h.ISO.UniqueToken("pw-invalid"))
	openTestSession(t, h.ISO, sessions, h.Auth, "pw-invalid", "Invalid")

	_, err := svc.CreatePlatformWithdrawal(ctx, string(token), 100_000, "not-a-valid-address!!!")
	if !errors.Is(err, ErrInvalidWithdrawAddress) {
		t.Fatalf("err = %v, want ErrInvalidWithdrawAddress", err)
	}
}

func TestCreatePlatformWithdrawal_idempotentOnTxHash(t *testing.T) {
	h, svc, solanaRPC := newPlatformWithdrawHarness(t)
	ctx := context.Background()
	sessions := NewSessionService(h.Store, h.Auth, h.Wallets)
	token := auth.AccessToken(h.ISO.UniqueToken("pw-idem"))
	session := openTestSession(t, h.ISO, sessions, h.Auth, "pw-idem", "Idem")

	wallet, found, err := h.Store.GetMemberWalletByUserID(ctx, session.UserID)
	if err != nil || !found {
		t.Fatalf("wallet lookup failed")
	}
	wallets.SetMemberUSDCBalance(h.Privy, wallet.Address, 2_000_000)

	dest := "0x000000000000000000000000000000000000dEaD"
	first, err := svc.CreatePlatformWithdrawal(ctx, string(token), 500_000, dest)
	if err != nil {
		t.Fatalf("first CreatePlatformWithdrawal: %v", err)
	}

	row, found, err := h.Store.GetPlatformWithdrawalByTxHash(ctx, first.TxHash)
	if err != nil || !found {
		t.Fatalf("GetPlatformWithdrawalByTxHash: found=%v err=%v", found, err)
	}
	if row.ID != first.ID {
		t.Fatalf("signature row id = %q, want %q", row.ID, first.ID)
	}

	solanaRPC.Confirm(first.TxHash)
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
	sessions := NewSessionService(h.Store, h.Auth, h.Wallets)
	token := auth.AccessToken(h.ISO.UniqueToken("pw-persist-fail"))
	session := openTestSession(t, h.ISO, sessions, h.Auth, "pw-persist-fail", "Persist Fail")

	wallet, found, err := h.Store.GetMemberWalletByUserID(ctx, session.UserID)
	if err != nil || !found {
		t.Fatalf("GetMemberWalletByUserID: found=%v err=%v", found, err)
	}
	wallets.SetMemberUSDCBalance(h.Privy, wallet.Address, 2_000_000)

	injectedErr := errors.New("injected persist signature failure")
	h.Store.SetFailPlatformWithdrawalBroadcastSignatureForTests(true, injectedErr)
	t.Cleanup(func() {
		h.Store.SetFailPlatformWithdrawalBroadcastSignatureForTests(false, nil)
	})

	dest := "0x000000000000000000000000000000000000dEaD"
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
	sessions := NewSessionService(h.Store, h.Auth, h.Wallets)
	token := auth.AccessToken(h.ISO.UniqueToken("pw-dup-sig"))
	session := openTestSession(t, h.ISO, sessions, h.Auth, "pw-dup-sig", "Dup Sig")

	wallet, found, err := h.Store.GetMemberWalletByUserID(ctx, session.UserID)
	if err != nil || !found {
		t.Fatalf("GetMemberWalletByUserID: found=%v err=%v", found, err)
	}
	wallets.SetMemberUSDCBalance(h.Privy, wallet.Address, 3_000_000)

	dest := "0x000000000000000000000000000000000000dEaD"
	first, err := svc.CreatePlatformWithdrawal(ctx, string(token), 500_000, dest)
	if err != nil {
		t.Fatalf("first CreatePlatformWithdrawal: %v", err)
	}

	solanaRPC.Confirm(first.TxHash)
	confirmed, err := svc.GetPlatformWithdrawal(ctx, string(token), first.ID)
	if err != nil {
		t.Fatalf("GetPlatformWithdrawal: %v", err)
	}
	if confirmed.Status != PlatformWithdrawalStatusConfirmed {
		t.Fatalf("status = %q, want confirmed", confirmed.Status)
	}

	wallets.SetForcedTransferHash(h.Wallets, first.TxHash)
	t.Cleanup(func() {
		wallets.SetForcedTransferHash(h.Wallets, "")
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

	wallets.SetForcedTransferHash(h.Wallets, "")
	third, err := svc.CreatePlatformWithdrawal(ctx, string(token), 400_000, dest)
	if err != nil {
		t.Fatalf("withdrawal after duplicate cleanup: %v", err)
	}
	if third.ID == first.ID {
		t.Fatalf("expected new withdrawal row, got reused id %q", third.ID)
	}
}

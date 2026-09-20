package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/privy"
)

// gatedBalancePrivy releases every member balance read at once, so all requests reach the
// reservation check together instead of trickling through it one by one.
type gatedBalancePrivy struct {
	privy.Client
	arrived sync.WaitGroup
	release chan struct{}
}

func (g *gatedBalancePrivy) MemberUSDCBalance(ctx context.Context, address string) (int64, error) {
	g.arrived.Done()
	select {
	case <-g.release:
	case <-time.After(100 * time.Millisecond):
		// Requests that serialize before the balance read never all arrive; let them through.
	}
	return g.Client.MemberUSDCBalance(ctx, address)
}

func TestFundGroup_concurrentRequests_neverReserveMoreThanTheBalance(t *testing.T) {
	// Arrange: 1 USDC available, many simultaneous requests for 0.6 USDC each.
	h := integrationApp(t)
	ctx := context.Background()
	sessions := NewSessionService(h.Store, h.Privy)
	token := privy.AccessToken(h.ISO.UniqueToken("fund-race"))
	session := openTestSession(t, h.ISO, sessions, h.Privy, "fund-race", "Racer")
	group, err := h.Groups.CreateGroup(ctx, string(token), testGroupName(h.ISO, "fund-race"))
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)
	ensureGroupMember(t, h.Store, group.GroupID, session.UserID)

	wallet, found, err := h.Store.GetMemberWalletByUserID(ctx, session.UserID)
	if err != nil || !found {
		t.Fatalf("GetMemberWalletByUserID: found=%v err=%v", found, err)
	}
	privy.SetMemberUSDCBalance(h.Privy, wallet.SolanaAddress, 1_000_000)
	const requests = 8
	gated := &gatedBalancePrivy{Client: h.Privy, release: make(chan struct{})}
	gated.arrived.Add(requests)
	deposits := NewDepositService(h.Store, gated, h.Pyth, h.Symbols)
	go func() {
		gated.arrived.Wait()
		close(gated.release)
	}()

	// Act
	errs := make([]error, requests)
	var wg sync.WaitGroup
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = deposits.FundGroup(ctx, string(token), group.GroupID, 600_000)
		}(i)
	}
	wg.Wait()

	// Assert: exactly one request fits; the rest are told so instead of pending forever.
	funded := 0
	for _, err := range errs {
		switch {
		case err == nil:
			funded++
		case errors.Is(err, ErrInsufficientPlatformBalance):
		default:
			t.Fatalf("FundGroup: unexpected error %v", err)
		}
	}
	if funded != 1 {
		t.Fatalf("funded requests = %d, want exactly 1", funded)
	}
	reserved, err := h.Store.SumPendingDepositAmountByUserID(ctx, session.UserID)
	if err != nil {
		t.Fatalf("SumPendingDepositAmountByUserID: %v", err)
	}
	if reserved != 600_000 {
		t.Fatalf("reserved = %d, want 600000 (never above the 1000000 balance)", reserved)
	}
}

func TestFundGroup_balanceReadFails_reservesNothing(t *testing.T) {
	// Arrange
	h := integrationApp(t)
	ctx := context.Background()
	sessions := NewSessionService(h.Store, h.Privy)
	token := privy.AccessToken(h.ISO.UniqueToken("fund-rpc-down"))
	session := openTestSession(t, h.ISO, sessions, h.Privy, "fund-rpc-down", "Unlucky")
	group, err := h.Groups.CreateGroup(ctx, string(token), testGroupName(h.ISO, "fund-rpc-down"))
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)
	ensureGroupMember(t, h.Store, group.GroupID, session.UserID)
	balanceErr := errors.New("privy balance status 503")
	deposits := NewDepositService(h.Store, &failingBalancePrivy{Client: h.Privy, err: balanceErr}, h.Pyth, h.Symbols)

	// Act
	_, err = deposits.FundGroup(ctx, string(token), group.GroupID, 600_000)

	// Assert: the error surfaces, the lock transaction rolled back, and a retry can proceed.
	if !errors.Is(err, balanceErr) {
		t.Fatalf("FundGroup err = %v, want %v", err, balanceErr)
	}
	reserved, err := h.Store.SumPendingDepositAmountByUserID(ctx, session.UserID)
	if err != nil {
		t.Fatalf("SumPendingDepositAmountByUserID: %v", err)
	}
	if reserved != 0 {
		t.Fatalf("reserved = %d, want 0", reserved)
	}
	wallet, found, err := h.Store.GetMemberWalletByUserID(ctx, session.UserID)
	if err != nil || !found {
		t.Fatalf("GetMemberWalletByUserID: found=%v err=%v", found, err)
	}
	privy.SetMemberUSDCBalance(h.Privy, wallet.SolanaAddress, 1_000_000)
	if _, err := h.Deposits.FundGroup(ctx, string(token), group.GroupID, 600_000); err != nil {
		t.Fatalf("FundGroup retry: %v", err)
	}
}

type failingBalancePrivy struct {
	privy.Client
	err error
}

func (f *failingBalancePrivy) MemberUSDCBalance(context.Context, string) (int64, error) {
	return 0, f.err
}

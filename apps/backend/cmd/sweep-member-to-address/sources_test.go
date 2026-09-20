package main

import (
	"context"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
)

type fakeSweepStore struct {
	members   []postgres.MemberWallet
	treasuries []postgres.Treasury
}

func (f fakeSweepStore) ListMemberWallets(context.Context) ([]postgres.MemberWallet, error) {
	return f.members, nil
}

func (f fakeSweepStore) ListTreasuries(context.Context) ([]postgres.Treasury, error) {
	return f.treasuries, nil
}

type fakePrivyLister struct {
	wallets []wallets.WalletRef
	err     error
}

func (f fakePrivyLister) ListAppSolanaWallets(context.Context) ([]wallets.WalletRef, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.wallets, nil
}

func TestLoadSweepSources_explicitList(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := fakeSweepStore{
		members: []postgres.MemberWallet{{Address: "Mem111"}},
		treasuries: []postgres.Treasury{{Address: "Tre111"}},
	}
	flags := sweepFlags{
		destination: "Dest111",
		sources:     []string{"Mem111", "Tre111", "Other111", "Mem111"},
	}

	sources, note, err := loadSweepSources(ctx, flags, store, fakePrivyLister{})
	if err != nil {
		t.Fatalf("loadSweepSources: %v", err)
	}
	if !strings.Contains(note, "explicit wallets") {
		t.Fatalf("note = %q", note)
	}
	if len(sources) != 3 {
		t.Fatalf("sources = %#v", sources)
	}
	if sources[0].kind != "member" || sources[0].address != "Mem111" {
		t.Fatalf("first source = %#v", sources[0])
	}
	if sources[1].kind != "treasury" || sources[1].address != "Tre111" {
		t.Fatalf("second source = %#v", sources[1])
	}
	if sources[2].kind != "explicit" || sources[2].address != "Other111" {
		t.Fatalf("third source = %#v", sources[2])
	}
}

func TestLoadSweepSources_allPrivyWallets(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := fakeSweepStore{
		members: []postgres.MemberWallet{{Address: "Mem111"}},
	}
	flags := sweepFlags{destination: "Dest111", all: true}
	privyLister := fakePrivyLister{
		wallets: []wallets.WalletRef{
			{WalletID: "pw1", Address: "Mem111"},
			{WalletID: "pw2", Address: "PrivyOnly111"},
		},
	}

	sources, note, err := loadSweepSources(ctx, flags, store, privyLister)
	if err != nil {
		t.Fatalf("loadSweepSources: %v", err)
	}
	if note != "privy app wallets (--all)" {
		t.Fatalf("note = %q", note)
	}
	if len(sources) != 2 {
		t.Fatalf("sources = %#v", sources)
	}
	if sources[0].walletID != "pw1" || sources[0].kind != "member" {
		t.Fatalf("first source = %#v", sources[0])
	}
	if sources[1].walletID != "pw2" || sources[1].kind != "privy" {
		t.Fatalf("second source = %#v", sources[1])
	}
}

func TestLoadSweepSources_dbDefault(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := fakeSweepStore{
		members:    []postgres.MemberWallet{{Address: "Mem111"}},
		treasuries: []postgres.Treasury{{Address: "Tre111"}},
	}
	flags := sweepFlags{destination: "Dest111"}

	sources, note, err := loadSweepSources(ctx, flags, store, fakePrivyLister{})
	if err != nil {
		t.Fatalf("loadSweepSources: %v", err)
	}
	if note != "postgres member_wallets + treasuries" {
		t.Fatalf("note = %q", note)
	}
	if len(sources) != 2 {
		t.Fatalf("sources = %#v", sources)
	}
}

package main

import (
	"context"
	"fmt"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
)

type walletKindReader interface {
	ListMemberWallets(ctx context.Context) ([]postgres.MemberWallet, error)
	ListTreasuries(ctx context.Context) ([]postgres.Treasury, error)
}

func loadSweepSources(ctx context.Context, flags sweepFlags, store walletKindReader, _ wallets.Client) ([]sweepSource, string, error) {
	switch {
	case len(flags.sources) > 0:
		memberSet, treasurySet, err := walletKindSets(ctx, store)
		if err != nil {
			return nil, "", err
		}
		sources, err := listExplicitSweepSources(flags.sources, memberSet, treasurySet)
		if err != nil {
			return nil, "", err
		}
		return sources, fmt.Sprintf("explicit wallets (%d --source)", len(sources)), nil
	case flags.all:
		sources, err := listDBSweepSources(ctx, store)
		if err != nil {
			return nil, "", err
		}
		return sources, "postgres member_wallets + treasuries (--all)", nil
	default:
		sources, err := listDBSweepSources(ctx, store)
		if err != nil {
			return nil, "", err
		}
		return sources, "postgres member_wallets + treasuries", nil
	}
}

func listExplicitSweepSources(addresses []string, members, treasuries map[string]struct{}) ([]sweepSource, error) {
	seen := map[string]struct{}{}
	var sources []sweepSource
	for _, address := range addresses {
		if address == "" {
			continue
		}
		if _, ok := seen[address]; ok {
			continue
		}
		seen[address] = struct{}{}
		sources = append(sources, sweepSource{
			kind:    classifyWalletKind(address, members, treasuries, "explicit"),
			address: address,
		})
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("no valid --source addresses")
	}
	return sources, nil
}

// print-relayer-pubkey prints the fee payer address and optional Base ETH balance.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/monaco/monaco/apps/backend/internal/config"
	"github.com/monaco/monaco/apps/backend/internal/evm"
)

func main() {
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	relayer, err := config.LoadRelayer(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "relayer: %v\n", err)
		os.Exit(1)
	}

	addr := relayer.Address()
	fmt.Printf("address  %s\n", addr)

	if cfg.BaseRPCURL == "" {
		fmt.Fprintln(os.Stderr, "note: BASE_RPC_URL unset; skipping live balance")
		return
	}

	chain := evm.NewJSONRPCClient(cfg.BaseRPCURL)
	wei, err := chain.ETHBalance(ctx, addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "eth balance: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("eth_wei  %s\n", wei.String())
}

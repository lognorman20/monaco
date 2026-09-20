// print-relayer-pubkey prints the fee payer pubkey and mainnet SOL/USDC balances for RELAYER_PRIVATE_KEY.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/monaco/monaco/apps/backend/internal/config"
	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/solana/balance"
	"github.com/monaco/monaco/apps/backend/internal/worker"
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

	pubkey := relayer.PublicKey()
	solanaRPC := worker.NewHTTPSolanaRPC(cfg.SolanaRPCEndpoint())
	lamports, err := solanaRPC.GetBalance(ctx, pubkey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "balance: %v\n", err)
		os.Exit(1)
	}
	usdcMicros, err := solanaRPC.GetSPLTokenBalance(ctx, pubkey, jupiter.USDCMint)
	if err != nil {
		fmt.Fprintf(os.Stderr, "usdc balance: %v\n", err)
		os.Exit(1)
	}

	if lamports <= balance.FeePayerMinLamports {
		fmt.Fprintf(
			os.Stderr,
			"warning: SOL balance <= 0.001 (need > %d lamports for backend startup)\n",
			balance.FeePayerMinLamports,
		)
	}

	fmt.Printf("address  %s\n", pubkey)
	fmt.Printf("sol      %s\n", formatSOL(lamports))
	fmt.Printf("usdc     %s\n", formatUSDC(usdcMicros))
}

// One-off ops helper: sweep member wallet USDC to any destination with relayer fee payer.
// Usage: dotenvx run -f .env.local -- go run ./scripts/sweep-member-to-address.go <member> <dest> [amount_micro]
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/monaco/monaco/apps/backend/internal/config"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "usage: %s <member_address> <dest_address> [amount_micro_usdc]\n", os.Args[0])
		os.Exit(2)
	}

	member := os.Args[1]
	dest := os.Args[2]

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	client := privy.NewHTTPClient(cfg)
	ctx := context.Background()

	amount := int64(0)
	if len(os.Args) >= 4 {
		amount, err = strconv.ParseInt(os.Args[3], 10, 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "amount: %v\n", err)
			os.Exit(1)
		}
	} else {
		amount, err = client.MemberUSDCBalance(ctx, member)
		if err != nil {
			fmt.Fprintf(os.Stderr, "balance: %v\n", err)
			os.Exit(1)
		}
	}

	if amount <= 0 {
		fmt.Println("nothing to sweep")
		return
	}

	req, err := privy.BuildSweepRequest(member, dest, amount, cfg.RelayerPrivateKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build request: %v\n", err)
		os.Exit(1)
	}

	result, err := client.SubmitSweep(ctx, req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "submit sweep: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("member=%s dest=%s amount=%d tx=%s\n", member, dest, amount, result.TxSignature)
}

// print-relayer-pubkey prints the base58 Solana public key for RELAYER_PRIVATE_KEY.
package main

import (
	"fmt"
	"os"

	"github.com/monaco/monaco/apps/backend/internal/config"
)

func main() {
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
	fmt.Println(relayer.PublicKey())
}

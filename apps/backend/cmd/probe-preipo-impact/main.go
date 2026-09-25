// probe-preipo-impact prints Jupiter Swap v2 price impact for pre-IPO mints at $25 and $250.
// Dev-only: requires JUPITER_API_KEY (via config) and exits without network when unset.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/config"
	"github.com/monaco/monaco/apps/backend/internal/jupiter"
)

type probeMint struct {
	Symbol string
	Mint   string
}

var preIPOMints = []probeMint{
	{Symbol: "SPACEX", Mint: "PreANxuXjsy2pvisWWMNB6YaJNzr7681wJJr2rHsfTh"},
	{Symbol: "OPENAI", Mint: "PreweJYECqtQwBtpxHL171nL2K6umo692gTm7Q3rpgF"},
	{Symbol: "KALSHI", Mint: "PreLWGkkeqG1s4HEfFZSy9moCrJ7btsHuUtfcCeoRua"},
	{Symbol: "ANTHROPIC", Mint: "Pren1FvFX6J3E4kXhJuCiAD5aDmGEb7qJRncwA8Lkhw"},
	{Symbol: "ANDURIL", Mint: "PresTj4Yc2bAR197Er7wz4UUKSfqt6FryBEdAriBoQB"},
	{Symbol: "NEURALINK", Mint: "PrekqLJvJ3qVdXmBGDiexvwUTF4rLFDa6HWS4HJbw9S"},
	{Symbol: "FIGUREAI", Mint: "PreZad18qfPtbxNpMtMuAuX2zVpvkEU8DnJx56faCWd"},
	{Symbol: "POLYMARKET", Mint: "Pre8AREmFPtoJFT8mQSXQLh56cwJmM7CFDRuoGBZiUP"},
	{Symbol: "tSpaceX", Mint: "TSPXcLV76s6V2zDiZQ18kBfcbnjaE2ZzNT3ga2Pd99v"},
	{Symbol: "tOpenAI", Mint: "TKLSidmLVt3cqGaaodG8tyRzoANfQwoh67AccjmubeZ"},
	{Symbol: "tKalshi", Mint: "oPAiAikWTaFj9RYoRFD35ccfwhnMcB3ThgBZRHSkjTZ"},
}

func main() {
	if strings.TrimSpace(os.Getenv("JUPITER_API_KEY")) == "" {
		fmt.Println("skip: set JUPITER_API_KEY to probe live Jupiter impact")
		os.Exit(0)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	_ = cfg

	client := jupiter.NewHTTPClient()
	ctx := context.Background()

	fmt.Printf("slippage_bps=%d\n", jupiter.PreIPOSlippageBps)
	for _, m := range preIPOMints {
		for _, usdc := range []int64{25_000_000, 250_000_000} {
			body, err := client.ProbeBuyOrderJSON(ctx, m.Mint, usdc, jupiter.PreIPOSlippageBps)
			if err != nil {
				fmt.Printf("%s\t$%d\t err=%v\n", m.Symbol, usdc/1_000_000, err)
				continue
			}
			impact, ok := jupiter.PriceImpactPctFromOrderJSON(body)
			if !ok {
				fmt.Printf("%s\t$%d\t impact=unknown\n", m.Symbol, usdc/1_000_000)
				continue
			}
			fmt.Printf("%s\t$%d\t priceImpactPct=%.4f\n", m.Symbol, usdc/1_000_000, impact)
		}
	}
}

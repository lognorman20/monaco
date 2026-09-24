package mintinfo

import (
	"math/big"
	"time"
)

type staticEntry struct {
	decimals       int
	multiplier     string
	transferFeeBps int
}

var staticByMint = map[string]staticEntry{
	"PreANxuXjsy2pvisWWMNB6YaJNzr7681wJJr2rHsfTh":  {9, "5", 100},
	"PreweJYECqtQwBtpxHL171nL2K6umo692gTm7Q3rpgF":  {9, "1.4861347", 100},
	"PresTj4Yc2bAR197Er7wz4UUKSfqt6FryBEdAriBoQB":  {9, "1", 100},
	"Pren1FvFX6J3E4kXhJuCiAD5aDmGEb7qJRncwA8Lkhw":  {9, "1", 100},
	"PreZad18qfPtbxNpMtMuAuX2zVpvkEU8DnJx56faCWd":  {9, "1", 100},
	"PreLWGkkeqG1s4HEfFZSy9moCrJ7btsHuUtfcCeoRua":  {9, "1", 100},
	"PrekqLJvJ3qVdXmBGDiexvwUTF4rLFDa6HWS4HJbw9S":  {9, "1", 100},
	"Pre8AREmFPtoJFT8mQSXQLh56cwJmM7CFDRuoGBZiUP": {9, "1", 100},
	"TSPXcLV76s6V2zDiZQ18kBfcbnjaE2ZzNT3ga2Pd99v":  {9, "1", 20},
	"TKLSidmLVt3cqGaaodG8tyRzoANfQwoh67AccjmubeZ":  {9, "1", 20},
	"oPAiAikWTaFj9RYoRFD35ccfwhnMcB3ThgBZRHSkjTZ":  {9, "1", 20},
}

// StaticFallback returns hard-coded mint info for known pre-IPO mints.
func StaticFallback(mint string, at time.Time) (Info, bool) {
	entry, ok := staticByMint[mint]
	if !ok {
		return Info{}, false
	}
	mult := parseMultiplierString(entry.multiplier)
	if mult == nil {
		mult = ratOne()
	}
	return Info{
		Mint:           mint,
		Decimals:       entry.decimals,
		TransferFeeBps: entry.transferFeeBps,
		UiMultiplier:   mult,
		Paused:         false,
		FetchedAt:      at,
	}, true
}

func parseMultiplierString(s string) *big.Rat {
	s = trimDecimal(s)
	if s == "" {
		return nil
	}
	r := new(big.Rat)
	if _, ok := r.SetString(s); !ok {
		return nil
	}
	if r.Sign() <= 0 {
		return nil
	}
	return r
}

func trimDecimal(s string) string {
	i := 0
	j := len(s)
	for i < j && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t') {
		j--
	}
	return s[i:j]
}
